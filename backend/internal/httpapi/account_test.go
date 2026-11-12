package httpapi

// Self-service account rename: PATCH /api/v1/account/profile targets always
// the caller's own account, never accepts a user id, and keeps every
// credential and identifier immutable — the tests prove the real login-code
// path keeps working across the rename.

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func renameBody(username string) map[string]string {
	return map[string]string{"username": username}
}

func accountPath() string { return "/api/v1/account/profile" }

func TestAccountRenameHappyPath(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	owner, token, csrf := env.createUserSession("rename-me")
	client := env.clientFor(token, csrf)

	status, _, raw := client.do("PATCH", accountPath(), renameBody("renamed.owner"), nil)
	if status != http.StatusOK {
		t.Fatalf("rename: status %d body %s, want 200", status, raw)
	}
	var updated userDTO
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	if updated.Username != "renamed.owner" {
		t.Fatalf("renamed username = %q", updated.Username)
	}
	if updated.ID != owner.ID.String() {
		t.Fatalf("account id changed during rename: %s", updated.ID)
	}

	// The database row confirms the immutable handle column.
	fresh, err := env.store.Users.GetByID(ctx, owner.ID)
	if err != nil || fresh.Username != "renamed.owner" {
		t.Fatalf("store row after rename: err=%v username=%q", err, fresh.Username)
	}
}

func TestAccountRenameRejectsInvalidFormat(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("formatowner")
	client := env.clientFor(token, csrf)

	for _, username := range []string{"UPPER", "a", "-lead", "with space", "", "café"} {
		status, _, raw := client.do("PATCH", accountPath(), renameBody(username), nil)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid username %q: status %d body %s, want 422", username, status, raw)
		}
	}
	// The account keeps its original username.
	fresh, err := env.store.Users.GetByID(ctx, owner.ID)
	if err != nil || fresh.Username != "formatowner" {
		t.Fatalf("invalid input changed the username: err=%v username=%q", err, fresh.Username)
	}
}

func TestAccountRenameRejectsTakenUsername(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("firstuser")
	if _, err := env.store.Users.Create(ctx, "seconduser", "Second"); err != nil {
		t.Fatal(err)
	}
	client := env.clientFor(token, csrf)

	status, _, raw := client.do("PATCH", accountPath(), renameBody("seconduser"), nil)
	if status != http.StatusConflict {
		t.Fatalf("taken username: status %d body %s, want 409", status, raw)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "username_taken" {
		t.Fatalf("taken problem code = %q err=%v, want username_taken", problem.Code, err)
	}
	// Both accounts kept their own usernames.
	if u, err := env.store.Users.GetByUsername(ctx, "seconduser"); err != nil || u.Username != "seconduser" {
		t.Fatalf("other account was disturbed: err=%v", err)
	}
	if u, err := env.store.Users.GetByID(ctx, owner.ID); err != nil || u.Username != "firstuser" {
		t.Fatalf("caller's username changed on a rejected rename: err=%v", err)
	}
}

func TestAccountRenameRequiresFullSessionAndCSRF(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	owner, token, _ := env.createUserSession("guarded")
	pendingUser, err := env.store.Users.Create(ctx, "pendinguser", "Pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.Users.Activate(ctx, pendingUser.ID); err != nil {
		t.Fatal(err)
	}

	// Anonymous: a client with valid origin headers but no session cookie.
	anonymous := env.clientFor("", "")
	if status, _, _ := anonymous.do("PATCH", accountPath(), renameBody("renamed.owner"), nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous rename: status %d, want 401", status)
	}

	// A restricted registration session may not reach business APIs.
	tok, csrfTok, _, err := env.store.Sessions.CreateWithKind(ctx, pendingUser.ID, "restricted", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	restrictedClient := env.clientFor(tok, csrfTok)
	if status, _, raw := restrictedClient.do("PATCH", accountPath(), renameBody("pendinguser"), nil); status != http.StatusForbidden {
		t.Fatalf("restricted rename: status %d body %s, want 403", status, raw)
	}

	// A full session without the CSRF header is rejected.
	noCSRF := env.clientFor(token, "")
	if status, _, raw := noCSRF.do("PATCH", accountPath(), renameBody("renamed.owner"), nil); status != http.StatusForbidden {
		t.Fatalf("missing CSRF: status %d body %s, want 403", status, raw)
	}
	if u, err := env.store.Users.GetByID(ctx, owner.ID); err != nil || u.Username != "guarded" {
		t.Fatalf("username changed despite rejections: err=%v username=%q", err, u.Username)
	}
}

func TestAccountRenameKeepsCredentialsWorking(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	owner, token, csrf := env.createUserSession("credowner")
	client := env.clientFor(token, csrf)

	if status, _, _ := client.do("PATCH", accountPath(), renameBody("credrenamed"), nil); status != http.StatusOK {
		t.Fatalf("rename: status %d, want 200", status)
	}

	// The live session keeps working and reports the new username.
	status, _, raw := client.do("GET", "/api/v1/session", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("session after rename: status %d", status)
	}
	var info struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if info.User.Username != "credrenamed" || info.User.ID != owner.ID.String() {
		t.Fatalf("session identity after rename: %+v", info.User)
	}

	// The real login-code path works under the new username and issues a
	// session bound to the same immutable account id.
	code, expiresAt, err := env.store.IssueLoginCode(ctx, "credrenamed", time.Minute)
	if err != nil {
		t.Fatalf("issue login code under the renamed username: %v", err)
	}
	_ = expiresAt
	exchangeClient := env.clientFor("", "")
	exStatus, _, exRaw := exchangeClient.do("POST", "/api/v1/auth/exchange", map[string]string{"code": code}, nil)
	if exStatus != http.StatusOK {
		t.Fatalf("exchange under renamed username: status %d body %s", exStatus, exRaw)
	}
	var done struct {
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(exRaw, &done); err != nil {
		t.Fatal(err)
	}
	if done.User.ID != owner.ID.String() || done.User.Username != "credrenamed" {
		t.Fatalf("exchanged identity: %+v, want id %s username credrenamed", done.User, owner.ID)
	}

	// The old username no longer resolves for login-code issuance.
	if _, _, err := env.store.IssueLoginCode(ctx, "credowner", time.Minute); err == nil {
		t.Fatal("the old username must no longer resolve for login codes")
	}

	// A body carrying a userId field is rejected outright: the target is
	// always the caller's own account, never a client-chosen id.
	status, _, raw = client.do("PATCH", accountPath(),
		map[string]any{"username": "renamed.owner", "userId": "99999999-9999-9999-9999-999999999999"}, nil)
	if status == http.StatusOK {
		t.Fatalf("a body with userId must be rejected: status %d body %s", status, raw)
	}
}
