package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/wa"
)

// ceremonyDone is the response body of a completed registration or login: the
// signed-in user plus the one-time CSRF token the client must echo on
// mutating requests.
type ceremonyDone struct {
	User      userDTO `json:"user"`
	CSRFToken string  `json:"csrfToken"`
}

// sessionResponse is the GET /api/v1/session body. An authenticated caller
// receives the session's stable CSRF token, identical across reads and tabs.
// platformInitialized tells an anonymous browser whether the CLI first-run
// binding already happened, so the login page can show the exact
// initialization guidance instead of a dead login form.
type sessionResponse struct {
	Authenticated       bool     `json:"authenticated"`
	User                *userDTO `json:"user,omitempty"`
	CSRFToken           string   `json:"csrfToken,omitempty"`
	PlatformInitialized bool     `json:"platformInitialized"`
}

// consumeAuthState resolves and atomically consumes the ceremony cookie.
// Every failure path is an indistinguishable 401 so challenge lifetime and
// replay state cannot be probed.
func (a *API) consumeAuthState(w http.ResponseWriter, r *http.Request, kind string) (store.AuthState, bool) {
	invalid := func() {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
	}
	cookie, err := r.Cookie(authTxCookieName)
	if err != nil || cookie.Value == "" {
		invalid()
		return store.AuthState{}, false
	}
	id, err := uuid.Parse(cookie.Value)
	if err != nil {
		invalid()
		return store.AuthState{}, false
	}
	state, err := a.store.AuthStates.Consume(r.Context(), id)
	if err != nil || state.Kind != kind {
		invalid()
		return store.AuthState{}, false
	}
	return state, true
}

// handleRegisterOptions starts passkey enrollment for a CLI-issued permit.
func (a *API) handleRegisterOptions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Permit string `json:"permit"`
	}
	if !decodeJSON(w, r, &req, false) {
		return
	}
	hasActive, err := a.store.Users.HasActiveUser(r.Context())
	if err != nil {
		a.logger.Error("check active administrators", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	if hasActive {
		writeProblem(w, http.StatusConflict, "bootstrap_closed", "An administrator is already registered; enrollment is closed.")
		return
	}
	permitID, userID, err := a.store.Permits.BeginAttempt(r.Context(), req.Permit)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_permit", "The enrollment permit is unknown, expired, or exhausted.")
		return
	}
	user, err := a.store.Users.GetByID(r.Context(), userID)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_permit", "The enrollment permit is unknown, expired, or exhausted.")
		return
	}

	a.store.AuthStates.PurgeExpired(r.Context())
	creation, sessionData, err := a.wa.BeginRegistration(&wa.User{Profile: user})
	if err != nil {
		a.logger.Error("begin registration", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	data, err := json.Marshal(sessionData)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	stateID, err := a.store.AuthStates.Create(r.Context(), "register", &userID, &permitID, data, a.cfg.ChallengeTTL)
	if err != nil {
		a.logger.Error("store auth state", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start registration.")
		return
	}
	a.setAuthTxCookie(w, stateID)
	writeJSON(w, http.StatusOK, creation)
}

// handleRegisterVerify verifies the attestation and completes enrollment in
// one transaction: permit validity, first-administrator exclusivity,
// credential write, and user activation commit or roll back together, so two
// concurrent completions can never both succeed.
func (a *API) handleRegisterVerify(w http.ResponseWriter, r *http.Request) {
	if !readStrictBody(w, r) {
		return
	}
	state, ok := a.consumeAuthState(w, r, "register")
	if !ok {
		return
	}
	if state.UserID == nil || state.PermitID == nil {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}
	user, err := a.store.Users.GetByID(r.Context(), *state.UserID)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}

	var sessionData webauthnlib.SessionData
	if err := json.Unmarshal(state.Data, &sessionData); err != nil {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}
	credential, err := a.wa.FinishRegistration(&wa.User{Profile: user}, sessionData, r)
	if err != nil {
		a.logger.Warn("registration rejected", "error", err)
		writeProblem(w, http.StatusUnauthorized, "credential_rejected", "The passkey registration response failed verification.")
		return
	}

	err = a.store.CompleteEnrollment(r.Context(), *state.PermitID, user, wa.FromCredential(user.ID, credential))
	switch {
	case err == nil:
	case errors.Is(err, store.ErrPermitUsed):
		writeProblem(w, http.StatusConflict, "permit_used", "The enrollment permit has already been used.")
		return
	case errors.Is(err, store.ErrPermitExpired):
		writeProblem(w, http.StatusConflict, "permit_expired", "The enrollment permit expired during enrollment.")
		return
	case errors.Is(err, store.ErrBootstrapClosed):
		writeProblem(w, http.StatusConflict, "bootstrap_closed", "An administrator is already registered; enrollment is closed.")
		return
	default:
		a.logger.Error("complete enrollment", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete registration.")
		return
	}
	a.finishLogin(w, r, user)
}

// handleLoginOptions starts a discoverable-credential (passkey) login.
func (a *API) handleLoginOptions(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !decodeJSON(w, r, &req, true) {
		return
	}
	a.store.AuthStates.PurgeExpired(r.Context())
	assertion, sessionData, err := a.wa.BeginDiscoverableLogin()
	if err != nil {
		a.logger.Error("begin discoverable login", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start login.")
		return
	}
	data, err := json.Marshal(sessionData)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start login.")
		return
	}
	stateID, err := a.store.AuthStates.Create(r.Context(), "login", nil, nil, data, a.cfg.ChallengeTTL)
	if err != nil {
		a.logger.Error("store auth state", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to start login.")
		return
	}
	a.setAuthTxCookie(w, stateID)
	writeJSON(w, http.StatusOK, assertion)
}

// handleLoginVerify verifies a discoverable assertion, updates the signature
// counter, and opens a session.
func (a *API) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	if !readStrictBody(w, r) {
		return
	}
	state, ok := a.consumeAuthState(w, r, "login")
	if !ok {
		return
	}
	var sessionData webauthnlib.SessionData
	if err := json.Unmarshal(state.Data, &sessionData); err != nil {
		writeProblem(w, http.StatusUnauthorized, "challenge_invalid", "The authentication challenge is unknown, expired, or already used.")
		return
	}

	lookupUser := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		user, err := a.store.Users.GetByHandle(r.Context(), userHandle)
		if err != nil {
			return nil, errors.New("unknown user handle")
		}
		if user.Status != "active" {
			return nil, errors.New("user is not active")
		}
		passkeys, err := a.store.Passkeys.ListByUser(r.Context(), user.ID)
		if err != nil {
			return nil, errors.New("credential lookup failed")
		}
		credentials := make([]webauthnlib.Credential, 0, len(passkeys))
		for _, pk := range passkeys {
			credentials = append(credentials, wa.ToCredential(pk))
		}
		return &wa.User{Profile: user, Credentials: credentials}, nil
	}

	user, credential, err := a.wa.FinishPasskeyLogin(lookupUser, sessionData, r)
	if err != nil {
		a.logger.Warn("login rejected", "error", err)
		writeProblem(w, http.StatusUnauthorized, "credential_rejected", "The passkey login response failed verification.")
		return
	}
	if err := a.store.Passkeys.UpdateAfterLogin(r.Context(), credential.ID,
		credential.Authenticator.SignCount, credential.Flags.BackupState, credential.Flags.UserVerified); err != nil {
		a.logger.Error("update passkey", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete login.")
		return
	}
	waUser, _ := user.(*wa.User)
	if waUser == nil {
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete login.")
		return
	}
	a.finishLogin(w, r, waUser.Profile)
}

// finishLogin issues the session cookie and returns the ceremony response.
func (a *API) finishLogin(w http.ResponseWriter, r *http.Request, user model.User) {
	token, csrf, _, err := a.store.Sessions.Create(r.Context(), user.ID, a.cfg.SessionTTL)
	if err != nil {
		a.logger.Error("create session", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to complete authentication.")
		return
	}
	a.setSessionCookie(w, token)
	a.clearAuthTxCookie(w)
	writeJSON(w, http.StatusOK, ceremonyDone{User: toUserDTO(user), CSRFToken: csrf})
}

// handleSession reports the current session. The CSRF token is stable for
// the lifetime of the session, so concurrent tabs all stay writable.
func (a *API) handleSession(w http.ResponseWriter, r *http.Request) {
	initialized, err := a.store.Users.HasActiveUser(r.Context())
	if err != nil {
		a.logger.Error("check platform initialization", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to read the session.")
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, PlatformInitialized: initialized})
		return
	}
	auth, err := a.store.Sessions.GetAndTouch(r.Context(), sha256Sum64(cookie.Value))
	if err != nil {
		a.clearSessionCookie(w)
		writeJSON(w, http.StatusOK, sessionResponse{Authenticated: false, PlatformInitialized: initialized})
		return
	}
	dto := toUserDTO(auth.User)
	writeJSON(w, http.StatusOK, sessionResponse{Authenticated: true, User: &dto, CSRFToken: auth.Session.CSRFToken, PlatformInitialized: initialized})
}

// handleLogout revokes the session server-side and clears the cookie.
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if err := a.store.Sessions.Revoke(r.Context(), auth.Session.ID); err != nil {
		a.logger.Error("revoke session", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to log out.")
		return
	}
	a.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
