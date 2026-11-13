package httpapi

import (
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

const accountCodeTTL = 10 * time.Minute

type adminUserDTO struct {
	userDTO
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func toAdminUser(user model.User) adminUserDTO {
	return adminUserDTO{userDTO: toUserDTO(user), Status: user.Status, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt}
}

// ASVS 8.2.1/8.3.1: all administrative reads and writes require a full
// administrator session. Lifecycle transactions recheck the operator too.
func (a *API) adminRoute(handler authedHandler) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		w.Header().Set("Cache-Control", "no-store")
		if auth.User.PlatformRole != model.PlatformRoleAdmin {
			writeProblem(w, http.StatusForbidden, "forbidden", "A platform administrator is required.")
			return
		}
		handler(w, r, auth)
	}
}

func (a *API) adminError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, detail := http.StatusInternalServerError, "internal_error", "The administration request failed."
	switch {
	case errors.Is(err, store.ErrOperatorForbidden):
		status, code, detail = 403, "forbidden", "A platform administrator is required."
	case errors.Is(err, store.ErrNotFound):
		status, code, detail = 404, "not_found", "The requested record does not exist."
	case errors.Is(err, store.ErrUsernameTaken):
		status, code, detail = 409, "username_taken", "Another account uses this username."
	case errors.Is(err, store.ErrSelfManagement):
		status, code, detail = 409, "self_management", "You cannot disable or delete your own account."
	case errors.Is(err, store.ErrLastAdmin):
		status, code, detail = 409, "last_admin", "At least one active administrator must remain."
	case errors.Is(err, store.ErrLastOwner):
		status, code, detail = 409, "last_owner", "Transfer site ownership to another active account first."
	case errors.Is(err, store.ErrLastPasskey):
		status, code, detail = 409, "last_passkey", "Keep at least one of your passkeys for this relying party."
	case errors.Is(err, store.ErrConflict):
		status, code, detail = 409, "account_conflict", "The account state no longer permits this action; refresh it."
	default:
		a.logger.Error("administration request failed", "error", err)
	}
	writeProblem(w, status, code, detail)
}

func adminID(w http.ResponseWriter, r *http.Request, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(key))
	if err != nil {
		writeProblem(w, 400, "validation_error", "The identifier must be a UUID.")
		return uuid.Nil, false
	}
	return id, true
}

func (a *API) handleListUsers(w http.ResponseWriter, r *http.Request, _ store.AuthSession) {
	page, size, ok := pagination(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	filter := store.UserAdminQuery{Page: page, PageSize: size, PlatformRole: query.Get("role"), Status: query.Get("status"), Search: query.Get("q")}
	if filter.PlatformRole != "" && filter.PlatformRole != model.PlatformRoleAdmin && filter.PlatformRole != model.PlatformRoleMember {
		writeProblem(w, 400, "validation_error", "Unknown platform role.")
		return
	}
	if filter.Status != "" && filter.Status != model.UserStatusActive && filter.Status != model.UserStatusDisabled && filter.Status != model.UserStatusPending && filter.Status != model.UserStatusDeleted {
		writeProblem(w, 400, "validation_error", "Unknown account status.")
		return
	}
	if utf8.RuneCountInString(filter.Search) > 128 {
		writeProblem(w, 400, "validation_error", "The search value is too long.")
		return
	}
	filter.IncludeDeleted = filter.Status == model.UserStatusDeleted
	result, err := a.store.Users.ListForAdmin(r.Context(), filter)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	users := make([]adminUserDTO, 0, len(result.Users))
	for _, user := range result.Users {
		users = append(users, toAdminUser(user))
	}
	writeJSON(w, 200, struct {
		Users    []adminUserDTO `json:"users"`
		Total    int            `json:"total"`
		Page     int            `json:"page"`
		PageSize int            `json:"pageSize"`
	}{users, result.Total, result.Page, result.PageSize})
}

type adminProfileInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

func readAdminProfile(w http.ResponseWriter, r *http.Request) (adminProfileInput, bool) {
	var input struct {
		Username    string  `json:"username"`
		DisplayName *string `json:"displayName"`
	}
	if !decodeJSON(w, r, &input, false) {
		return adminProfileInput{}, false
	}
	if input.DisplayName == nil {
		writeProblem(w, 422, "validation_error", "A displayName field is required.")
		return adminProfileInput{}, false
	}
	body := adminProfileInput{Username: input.Username, DisplayName: *input.DisplayName}
	if !store.ValidUsername(body.Username) || utf8.RuneCountInString(body.DisplayName) > 200 {
		writeProblem(w, 422, "validation_error", "Use a valid username and a display name of at most 200 characters.")
		return body, false
	}
	return body, true
}

type accountCodeDTO struct {
	User      adminUserDTO `json:"user"`
	Code      string       `json:"code"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

func (a *API) handleCreateUser(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	body, ok := readAdminProfile(w, r)
	if !ok {
		return
	}
	user, code, expiresAt, err := a.store.CreateAccountWithCode(r.Context(), auth.User.ID, body.Username, body.DisplayName, accountCodeTTL)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, accountCodeDTO{toAdminUser(user), code, expiresAt})
}
func (a *API) handleGetUser(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	user, err := a.store.Users.GetByID(r.Context(), id)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	access, err := a.store.AccountAccess(r.Context(), id, a.cfg.RPID, auth.Session.ID)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, struct {
		User   adminUserDTO        `json:"user"`
		Access store.AccountAccess `json:"access"`
	}{toAdminUser(user), access})
}
func (a *API) handleAdminUpdateProfile(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	body, ok := readAdminProfile(w, r)
	if !ok {
		return
	}
	user, err := a.store.AdminUpdateAccount(r.Context(), auth.User.ID, id, body.Username, body.DisplayName)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, toAdminUser(user))
}
func (a *API) handleSetUserRole(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	var body struct {
		Role string `json:"platformRole"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.Role != model.PlatformRoleAdmin && body.Role != model.PlatformRoleMember {
		writeProblem(w, 422, "validation_error", "Unknown platform role.")
		return
	}
	user, err := a.store.AdminSetPlatformRole(r.Context(), auth.User.ID, id, body.Role)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, toAdminUser(user))
}
func (a *API) handleSetUserStatus(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	var user model.User
	var err error
	switch body.Status {
	case model.UserStatusActive:
		user, err = a.store.EnableAccount(r.Context(), auth.User.ID, id)
	case model.UserStatusDisabled:
		user, err = a.store.DisableAccount(r.Context(), auth.User.ID, id)
	default:
		writeProblem(w, 422, "validation_error", "Only active or disabled is accepted.")
		return
	}
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, toAdminUser(user))
}
func (a *API) handleDeleteUser(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	if _, err := a.store.DeleteAccount(r.Context(), auth.User.ID, id); err != nil {
		a.adminError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) handleIssueUserCode(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	code, expiresAt, err := a.store.IssueAccountCode(r.Context(), auth.User.ID, id, accountCodeTTL)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	user, err := a.store.Users.GetByID(r.Context(), id)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, accountCodeDTO{toAdminUser(user), code, expiresAt})
}
func (a *API) handleSetUserSiteAccess(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	siteID, ok := adminID(w, r, "siteId")
	if !ok {
		return
	}
	var body struct {
		Role *string `json:"role"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if body.Role == nil || (*body.Role != "" && *body.Role != model.RoleOwner && *body.Role != model.RoleEditor && *body.Role != model.RoleViewer) {
		writeProblem(w, 422, "validation_error", "Unknown site role.")
		return
	}
	if err := a.store.SetAccountSiteAccess(r.Context(), auth.User.ID, id, siteID, *body.Role); err != nil {
		a.adminError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"siteId": siteID.String(), "role": *body.Role})
}
func (a *API) handleRevokeUserSessions(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	var sessionID *uuid.UUID
	if r.PathValue("sessionId") != "" {
		value, ok := adminID(w, r, "sessionId")
		if !ok {
			return
		}
		sessionID = &value
	}
	if err := a.store.AdminRevokeSessions(r.Context(), auth.User.ID, id, sessionID); err != nil {
		a.adminError(w, r, err)
		return
	}
	if id == auth.User.ID && (sessionID == nil || *sessionID == auth.Session.ID) {
		a.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *API) handleRevokeUserPasskey(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := adminID(w, r, "userId")
	if !ok {
		return
	}
	keyID, ok := adminID(w, r, "passkeyId")
	if !ok {
		return
	}
	if err := a.store.AdminRevokePasskey(r.Context(), auth.User.ID, id, keyID, a.cfg.RPID); err != nil {
		a.adminError(w, r, err)
		return
	}
	if id == auth.User.ID {
		a.clearSessionCookie(w)
	}
	w.WriteHeader(http.StatusNoContent)
}
