package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Audit metadata is deliberately an allowlist. Bodies, query strings,
// cookies, codes, credential material and headers never enter this record.
// ASVS 16.2.1/16.2.5: correlate events without retaining authentication secrets.
type AuditEvent struct {
	ID         uuid.UUID    `json:"id"`
	CreatedAt  time.Time    `json:"createdAt"`
	ActorID    *uuid.UUID   `json:"actorId"`
	ActorName  string       `json:"actorName"`
	Operation  string       `json:"operation"`
	ResourceID string       `json:"resourceId"`
	SiteID     *uuid.UUID   `json:"siteId"`
	Method     string       `json:"method"`
	Route      string       `json:"route"`
	PeerIP     string       `json:"peerIp"`
	Status     int          `json:"status"`
	Outcome    string       `json:"outcome"`
	Details    AuditDetails `json:"details"`
}

// Only canonical, non-secret values can enter audit details. This type must
// never be replaced by a request-body map.
type AuditDetails struct {
	TargetUsername string     `json:"targetUsername,omitempty"`
	Field          string     `json:"field,omitempty"`
	Before         *string    `json:"before,omitempty"`
	After          *string    `json:"after,omitempty"`
	SessionID      *uuid.UUID `json:"sessionId,omitempty"`
	PasskeyID      *uuid.UUID `json:"passkeyId,omitempty"`
}

type AuditRequest struct {
	Event     AuditEvent
	Committed bool
}
type auditContextKey struct{}

func WithAuditRequest(ctx context.Context, request *AuditRequest) context.Context {
	return context.WithValue(ctx, auditContextKey{}, request)
}
func AuditRequestFrom(ctx context.Context) *AuditRequest {
	request, _ := ctx.Value(auditContextKey{}).(*AuditRequest)
	return request
}

type auditExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func appendAudit(ctx context.Context, executor auditExecutor, event AuditEvent) error {
	details, err := json.Marshal(event.Details)
	if err != nil {
		return err
	}
	if event.Status == 0 {
		event.Status = 200
	}
	outcome := "success"
	if event.Status >= 400 {
		outcome = "failure"
	}
	_, err = executor.Exec(ctx, `INSERT INTO audit_events
	 (id,actor_id,actor_name,operation,resource_id,site_id,method,route,peer_ip,status,outcome,details)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`, newUUID(), event.ActorID, event.ActorName,
		event.Operation, event.ResourceID, event.SiteID, event.Method, event.Route, event.PeerIP, event.Status, outcome, details)
	return err
}
func (s *Store) AppendAudit(ctx context.Context, event AuditEvent) error {
	return appendAudit(ctx, s.Pool, event)
}

// Administrative changes and their success event commit together. Failures
// leave neither a partial change nor a misleading successful audit event.
func commitAudited(ctx context.Context, tx pgx.Tx, actorID, targetID uuid.UUID, operation string, details ...AuditDetails) error {
	event := AuditEvent{ActorID: &actorID, Operation: operation, ResourceID: targetID.String(), Status: 200}
	request := AuditRequestFrom(ctx)
	if request != nil {
		event = request.Event
		event.ActorID = &actorID
		event.Operation = operation
		event.ResourceID = targetID.String()
	}
	if event.ActorName == "" {
		if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, actorID).Scan(&event.ActorName); err != nil {
			return err
		}
	}
	if len(details) > 0 {
		event.Details = details[0]
	}
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id=$1`, targetID).Scan(&event.Details.TargetUsername); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, event); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if request != nil {
		request.Committed = true
	}
	return nil
}

type AuditQuery struct {
	Page, PageSize                    int
	Operation, Outcome, Actor, Search string
	Sort, Order                       string
	From, Until                       *time.Time
}
type AuditPage struct {
	Entries    []AuditEvent `json:"entries"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	PageSize   int          `json:"pageSize"`
	Operations []string     `json:"operations"`
}

func literalPattern(value string) any {
	if value == "" {
		return nil
	}
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value) + "%"
}

const auditFilter = ` WHERE ($1::text IS NULL OR operation=$1)
 AND ($2::text IS NULL OR outcome=$2)
 AND ($3::text IS NULL OR actor_name ILIKE $3)
 AND ($4::text IS NULL OR actor_name ILIKE $4 OR operation ILIKE $4 OR resource_id ILIKE $4 OR details->>'targetUsername' ILIKE $4)
 AND ($5::timestamptz IS NULL OR created_at >= $5)
 AND ($6::timestamptz IS NULL OR created_at <= $6)`

func (s *Store) ListAudit(ctx context.Context, query AuditQuery) (AuditPage, error) {
	out := AuditPage{Entries: []AuditEvent{}, Operations: []string{}, Page: query.Page, PageSize: query.PageSize}
	args := []any{nilIfEmpty(query.Operation), nilIfEmpty(query.Outcome), literalPattern(query.Actor), literalPattern(query.Search), query.From, query.Until}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`+auditFilter, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id,created_at,actor_id,actor_name,operation,resource_id,site_id,method,route,peer_ip,status,outcome,details FROM audit_events`+auditFilter+` ORDER BY
 CASE WHEN $9::text = 'createdAt' AND $10::text = 'asc' THEN created_at END ASC,
 CASE WHEN ($9::text = 'createdAt' AND $10::text = 'desc') OR $9::text = '' THEN created_at END DESC,
 CASE WHEN $9::text = 'actorName' AND $10::text = 'asc' THEN actor_name END ASC,
 CASE WHEN $9::text = 'actorName' AND $10::text = 'desc' THEN actor_name END DESC,
 CASE WHEN $9::text = 'operation' AND $10::text = 'asc' THEN operation END ASC,
 CASE WHEN $9::text = 'operation' AND $10::text = 'desc' THEN operation END DESC,
 CASE WHEN $9::text = 'resourceId' AND $10::text = 'asc' THEN resource_id END ASC,
 CASE WHEN $9::text = 'resourceId' AND $10::text = 'desc' THEN resource_id END DESC,
 CASE WHEN $9::text = 'outcome' AND $10::text = 'asc' THEN outcome END ASC,
 CASE WHEN $9::text = 'outcome' AND $10::text = 'desc' THEN outcome END DESC,
 CASE WHEN $9::text = 'peerIp' AND $10::text = 'asc' THEN peer_ip END ASC,
 CASE WHEN $9::text = 'peerIp' AND $10::text = 'desc' THEN peer_ip END DESC,
 id DESC LIMIT $7 OFFSET $8`, append(args, query.PageSize, (query.Page-1)*query.PageSize, query.Sort, query.Order)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.CreatedAt, &event.ActorID, &event.ActorName, &event.Operation, &event.ResourceID, &event.SiteID, &event.Method, &event.Route, &event.PeerIP, &event.Status, &event.Outcome, &event.Details); err != nil {
			rows.Close()
			return out, err
		}
		out.Entries = append(out.Entries, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT DISTINCT operation FROM audit_events ORDER BY operation`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var operation string
		if err := rows.Scan(&operation); err != nil {
			return out, err
		}
		out.Operations = append(out.Operations, operation)
	}
	return out, rows.Err()
}
