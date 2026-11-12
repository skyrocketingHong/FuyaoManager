package httpapi

import (
	"errors"
	"net/http"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// accountProfileUpdate is the whole request body of PATCH /account/profile:
// the username, nothing else. The target is always the caller's own account —
// the endpoint never accepts a user id, so self-service renaming cannot touch
// another account.
type accountProfileUpdate struct {
	Username string `json:"username"`
}

// handleUpdateAccountProfile renames the current user. The rename is one
// atomic UPDATE: the account's UUID, WebAuthn user handle, every passkey
// credential, and all role memberships are untouched, so existing
// credentials keep working across the rename. A username collision with
// another account answers 409 username_taken; an invalid format answers 422
// validation_error (the database CHECK backs the same rule).
func (a *API) handleUpdateAccountProfile(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	var body accountProfileUpdate
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if !store.ValidUsername(body.Username) {
		writeProblem(w, http.StatusUnprocessableEntity, "validation_error",
			"The username must match ^[a-z0-9][a-z0-9._-]{2,63}$.")
		return
	}
	updated, err := a.store.Users.UpdateUsername(r.Context(), auth.User.ID, body.Username)
	switch {
	case errors.Is(err, store.ErrUsernameTaken):
		writeProblem(w, http.StatusConflict, "username_taken",
			"Another account already uses this username.")
		return
	case errors.Is(err, store.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "not_found", "The account does not exist.")
		return
	case err != nil:
		a.logger.Error("update account profile", "userId", auth.User.ID, "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to update the account.")
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(updated))
}
