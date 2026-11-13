package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

type UserSiteAccess struct {
	SiteID uuid.UUID `json:"siteId"`
	Name   string    `json:"name"`
	Slug   string    `json:"slug"`
	Role   string    `json:"role"`
}
type AdminPasskey struct {
	ID             uuid.UUID  `json:"id"`
	RPID           string     `json:"rpId"`
	CreatedAt      time.Time  `json:"createdAt"`
	LastUsedAt     *time.Time `json:"lastUsedAt"`
	BackupEligible bool       `json:"backupEligible"`
	BackupState    bool       `json:"backupState"`
	CurrentRP      bool       `json:"currentRp"`
}
type AdminSession struct {
	ID         uuid.UUID `json:"id"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Current    bool      `json:"current"`
}
type AccountAccess struct {
	Sites    []UserSiteAccess `json:"sites"`
	Passkeys []AdminPasskey   `json:"passkeys"`
	Sessions []AdminSession   `json:"sessions"`
}

func (s *Store) AccountAccess(ctx context.Context, userID uuid.UUID, rpID string, sessionID uuid.UUID) (AccountAccess, error) {
	out := AccountAccess{Sites: []UserSiteAccess{}, Passkeys: []AdminPasskey{}, Sessions: []AdminSession{}}
	rows, err := s.Pool.Query(ctx, `SELECT s.id,settings.name,s.slug,COALESCE(m.role,'')
	 FROM sites s JOIN site_settings settings ON settings.site_id=s.id
	 LEFT JOIN site_memberships m ON m.site_id=s.id AND m.user_id=$1 ORDER BY settings.name,s.id`, userID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var site UserSiteAccess
		if err := rows.Scan(&site.SiteID, &site.Name, &site.Slug, &site.Role); err != nil {
			rows.Close()
			return out, err
		}
		out.Sites = append(out.Sites, site)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT id,rp_id,created_at,last_used_at,backup_eligible,backup_state FROM passkeys WHERE user_id=$1 ORDER BY created_at,id`, userID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var key AdminPasskey
		if err := rows.Scan(&key.ID, &key.RPID, &key.CreatedAt, &key.LastUsedAt, &key.BackupEligible, &key.BackupState); err != nil {
			rows.Close()
			return out, err
		}
		key.CurrentRP = key.RPID == rpID
		out.Passkeys = append(out.Passkeys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = s.Pool.Query(ctx, `SELECT s.id,s.kind,s.created_at,s.last_seen_at,s.expires_at FROM sessions s
	 JOIN users u ON u.id=s.user_id WHERE s.user_id=$1 AND u.status='active' AND s.revoked_at IS NULL
	 AND s.expires_at>clock_timestamp() ORDER BY s.last_seen_at DESC,s.id`, userID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var session AdminSession
		if err := rows.Scan(&session.ID, &session.Kind, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt); err != nil {
			return out, err
		}
		session.Current = session.ID == sessionID
		out.Sessions = append(out.Sessions, session)
	}
	return out, rows.Err()
}

func (s *Store) SetAccountSiteAccess(ctx context.Context, operatorID, userID, siteID uuid.UUID, role string) error {
	if role != "" && role != model.RoleOwner && role != model.RoleEditor && role != model.RoleViewer {
		return fmt.Errorf("invalid site role")
	}
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1`, userID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != model.UserStatusActive && role != "" {
		return ErrConflict
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id=$1)`, siteID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	var oldRole string
	err = tx.QueryRow(ctx, `SELECT role FROM site_memberships WHERE user_id=$1 AND site_id=$2`, userID, siteID).Scan(&oldRole)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	// The same lock as lifecycle writes protects the last effective owner.
	if oldRole == model.RoleOwner && role != model.RoleOwner && status == model.UserStatusActive {
		var other bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM site_memberships m JOIN users u ON u.id=m.user_id
		 WHERE m.site_id=$1 AND m.role='owner' AND m.user_id<>$2 AND u.status='active')`, siteID, userID).Scan(&other)
		if err != nil {
			return err
		}
		if !other {
			return ErrLastOwner
		}
	}
	if role == "" {
		_, err = tx.Exec(ctx, `DELETE FROM site_memberships WHERE site_id=$1 AND user_id=$2`, siteID, userID)
	} else {
		_, err = tx.Exec(ctx, `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,$3)
		 ON CONFLICT(site_id,user_id) DO UPDATE SET role=EXCLUDED.role`, siteID, userID, role)
	}
	if err != nil {
		return err
	}
	return commitAudited(ctx, tx, operatorID, userID, "setUserSiteAccess", AuditDetails{Field: "siteRole", Before: &oldRole, After: &role})
}

func (s *Store) AdminRevokeSessions(ctx context.Context, operatorID, userID uuid.UUID, sessionID *uuid.UUID) error {
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	// Bind completion locks the old session before minting its replacement.
	// Wait for that transaction, then sweep with a fresh statement snapshot.
	if _, err := tx.Exec(ctx, `SELECT id FROM sessions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL
	 AND ($2::uuid IS NULL OR id=$2)`, userID, sessionID)
	if err != nil {
		return err
	}
	if sessionID != nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	operation := "revokeUserSessions"
	if sessionID != nil {
		operation = "revokeUserSession"
	}
	return commitAudited(ctx, tx, operatorID, userID, operation, AuditDetails{SessionID: sessionID})
}

var ErrLastPasskey = errors.New("cannot remove your last passkey for the current relying party")

func (s *Store) AdminRevokePasskey(ctx context.Context, operatorID, userID, keyID uuid.UUID, rpID string) error {
	tx, err := s.beginAccountLifecycle(ctx, operatorID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM sessions WHERE user_id=$1 ORDER BY id FOR UPDATE`, userID); err != nil {
		return err
	}
	var keyRP string
	if err := tx.QueryRow(ctx, `SELECT rp_id FROM passkeys WHERE id=$1 AND user_id=$2 FOR UPDATE`, keyID, userID).Scan(&keyRP); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if operatorID == userID && keyRP == rpID {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM passkeys WHERE user_id=$1 AND rp_id=$2`, userID, rpID).Scan(&count); err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastPasskey
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM passkeys WHERE id=$1 AND user_id=$2`, keyID, userID); err != nil {
		return err
	}
	if err := revokeAccountArtifacts(ctx, tx, userID); err != nil {
		return err
	}
	return commitAudited(ctx, tx, operatorID, userID, "revokeUserPasskey", AuditDetails{PasskeyID: &keyID})
}
