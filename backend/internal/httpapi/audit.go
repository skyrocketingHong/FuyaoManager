package httpapi

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func setAuditActor(r *http.Request, user model.User) {
	if request := store.AuditRequestFrom(r.Context()); request != nil {
		id := user.ID
		request.Event.ActorID = &id
		request.Event.ActorName = user.Username
	}
}

// ASVS 16.3.1/16.3.2: record mutations, authentication outcomes and denied
// requests. Route templates and UUIDs replace raw paths and request bodies.
func (a *API) auditRoute(route routeSpec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		peer, _, _ := net.SplitHostPort(r.RemoteAddr)
		event := store.AuditEvent{Operation: route.OperationID, Method: r.Method, Route: route.Pattern, PeerIP: peer, Status: http.StatusOK}
		if r.Method == http.MethodDelete {
			event.Status = http.StatusNoContent
		}
		if route.OperationID == "createUser" {
			event.Status = http.StatusCreated
		}
		for _, parameter := range []string{"userId", "siteId", "buildId", "releaseId"} {
			if id, err := uuid.Parse(r.PathValue(parameter)); err == nil {
				if event.ResourceID == "" {
					event.ResourceID = id.String()
				}
				if parameter == "siteId" {
					event.SiteID = &id
				}
			}
		}
		request := &store.AuditRequest{Event: event}
		if id, err := uuid.Parse(r.PathValue("sessionId")); err == nil {
			request.Event.Details.SessionID = &id
		}
		if id, err := uuid.Parse(r.PathValue("passkeyId")); err == nil {
			request.Event.Details.PasskeyID = &id
		}
		r = r.WithContext(store.WithAuditRequest(r.Context(), request))
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		completed := false
		defer func() {
			// Filter changes cancel superseded reads; they are not failed
			// business operations and must not pollute the audit history.
			if r.Method == http.MethodGet && r.Context().Err() != nil {
				return
			}
			if !completed {
				recorder.status = http.StatusInternalServerError
			}
			if request.Committed && recorder.status < 400 {
				return
			}
			if r.Method == http.MethodGet && recorder.status < 400 {
				return
			}
			request.Event.Status = recorder.status
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
			defer cancel()
			if err := a.store.AppendAudit(ctx, request.Event); err != nil {
				a.logger.Error("audit event persistence failed", "operation", route.OperationID, "status", recorder.status, "error", err)
			}
		}()
		route.Handler(recorder, r)
		completed = true
	}
}

func pagination(w http.ResponseWriter, r *http.Request) (page, size int, ok bool) {
	page, size = 1, 50
	for _, field := range []struct {
		key    string
		target *int
		max    int
	}{{"page", &page, 100000}, {"pageSize", &size, 200}} {
		if value := r.URL.Query().Get(field.key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > field.max {
				writeProblem(w, http.StatusBadRequest, "validation_error", "Pagination is outside the supported range.")
				return 0, 0, false
			}
			*field.target = n
		}
	}
	return page, size, true
}

func validListSort(field, order string, allowed ...string) bool {
	if field == "" && order == "" {
		return true
	}
	if order != "asc" && order != "desc" {
		return false
	}
	for _, candidate := range allowed {
		if field == candidate {
			return true
		}
	}
	return false
}

func (a *API) handleListAudit(w http.ResponseWriter, r *http.Request, _ store.AuthSession) {
	page, size, ok := pagination(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := store.AuditQuery{Page: page, PageSize: size, Search: query.Get("q"), Actor: query.Get("actor"), Operation: query.Get("operation"), Outcome: query.Get("outcome"), Sort: query.Get("sort"), Order: query.Get("order")}
	if !validListSort(filter.Sort, filter.Order, "createdAt", "actorName", "operation", "resourceId", "outcome", "peerIp") {
		writeProblem(w, 400, "validation_error", "Unknown sort field or direction.")
		return
	}
	for _, value := range []string{filter.Search, filter.Actor, filter.Operation} {
		if utf8.RuneCountInString(value) > 128 {
			writeProblem(w, 400, "validation_error", "The search value is too long.")
			return
		}
	}
	if filter.Outcome != "" && filter.Outcome != "success" && filter.Outcome != "failure" {
		writeProblem(w, 400, "validation_error", "Unknown audit outcome.")
		return
	}
	for _, field := range []struct {
		key    string
		target **time.Time
	}{{"from", &filter.From}, {"until", &filter.Until}} {
		if raw := query.Get(field.key); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				writeProblem(w, 400, "validation_error", "Audit timestamps must use RFC3339.")
				return
			}
			*field.target = &value
		}
	}
	if filter.From != nil && filter.Until != nil && filter.From.After(*filter.Until) {
		writeProblem(w, 400, "validation_error", "The audit time range is reversed.")
		return
	}
	result, err := a.store.ListAudit(r.Context(), filter)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
