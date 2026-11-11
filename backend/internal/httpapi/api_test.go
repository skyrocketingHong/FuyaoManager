package httpapi

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/testauth"
)

// bootstrapAndRegister runs the first-run flow: CLI bootstrap, permit, and a
// full passkey registration. It returns the user handle, authenticator, and
// the consumed permit code for later assertions.
func bootstrapAndRegister(e *testEnv, c *tclient, username string) ([]byte, *testauth.Authenticator, string) {
	e.t.Helper()
	ctx := context.Background()
	code, _, err := e.store.BootstrapAdmin(ctx, username, username, 10*time.Minute)
	if err != nil {
		e.t.Fatalf("bootstrap admin: %v", err)
	}

	status, _, body := c.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": code}, nil)
	if status != 200 {
		e.t.Fatalf("register options: status %d body %s", status, body)
	}
	var options creationOptionsResp
	decodeJSONBody(e.t, body, &options)
	if options.PublicKey.RP.ID != "localhost" || options.PublicKey.User.ID == "" {
		e.t.Fatalf("unexpected creation options: %s", body)
	}
	userHandle, err := base64.RawURLEncoding.DecodeString(options.PublicKey.User.ID)
	if err != nil {
		e.t.Fatalf("user handle decode: %v", err)
	}

	auth := testauth.New()
	status, _, body = c.do("POST", "/api/v1/auth/register/verify", auth.CreateCredential("localhost", e.base, options.PublicKey.Challenge), nil)
	if status != 200 {
		e.t.Fatalf("register verify: status %d body %s", status, body)
	}
	var done ceremonyDoneResp
	decodeJSONBody(e.t, body, &done)
	if done.User.Username != username || done.CSRFToken == "" {
		e.t.Fatalf("unexpected register verify response: %s", body)
	}
	c.csrf = done.CSRFToken
	return userHandle, auth, code
}

func loginWith(e *testEnv, c *tclient, auth *testauth.Authenticator, userHandle []byte) string {
	e.t.Helper()
	status, _, body := c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	if status != 200 {
		e.t.Fatalf("login options: status %d body %s", status, body)
	}
	var options requestOptionsResp
	decodeJSONBody(e.t, body, &options)
	if options.PublicKey.UserVerification != "required" {
		e.t.Fatalf("login options must require user verification: %s", body)
	}
	assertion := auth.Assert("localhost", e.base, options.PublicKey.Challenge, userHandle)
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", assertion, nil)
	if status != 200 {
		e.t.Fatalf("login verify: status %d body %s", status, body)
	}
	var done ceremonyDoneResp
	decodeJSONBody(e.t, body, &done)
	c.csrf = done.CSRFToken
	return done.CSRFToken
}

func TestAnonymousAccessDenied(t *testing.T) {
	env := setup(t)
	c := env.newClient()

	for _, target := range []struct {
		method, path string
	}{
		{"GET", "/api/v1/sites"},
		{"GET", "/api/v1/sites/00000000-0000-0000-0000-000000000000"},
		{"GET", "/api/v1/sites/00000000-0000-0000-0000-000000000000/settings"},
	} {
		status, _, body := c.do(target.method, target.path, nil, nil)
		if status != 401 {
			t.Fatalf("%s %s anonymous: got %d body %s, want 401", target.method, target.path, status, body)
		}
		var p problemResp
		decodeJSONBody(t, body, &p)
		if p.Code != "unauthenticated" {
			t.Fatalf("%s %s: problem code %q, want unauthenticated", target.method, target.path, p.Code)
		}
	}

	status, _, body := c.do("PATCH", "/api/v1/sites/00000000-0000-0000-0000-000000000000/settings", map[string]string{"name": "x"}, map[string]string{"If-Match": `"`})
	if status != 401 {
		t.Fatalf("PATCH anonymous: got %d body %s, want 401", status, body)
	}
}

func TestBootstrapRegisterLoginAndSettingsLifecycle(t *testing.T) {
	env := setup(t)
	c := env.newClient()
	ctx := context.Background()

	userHandle, auth, _ := bootstrapAndRegister(env, c, "alice")

	// Session query reports the signed-in administrator and rotates a CSRF
	// token that keeps working for mutations.
	status, _, body := c.do("GET", "/api/v1/session", nil, nil)
	if status != 200 {
		t.Fatalf("session: %d %s", status, body)
	}
	var session sessionResp
	decodeJSONBody(t, body, &session)
	if !session.Authenticated || session.CSRFToken == "" {
		t.Fatalf("session after registration: %s", body)
	}
	c.csrf = session.CSRFToken

	// Logout, then a fresh discoverable login.
	status, _, body = c.do("POST", "/api/v1/auth/logout", map[string]string{}, nil)
	if status != 204 {
		t.Fatalf("logout: %d %s", status, body)
	}
	status, _, body = c.do("GET", "/api/v1/sites", nil, nil)
	if status != 401 {
		t.Fatalf("sites after logout: %d %s", status, body)
	}
	loginWith(env, c, auth, userHandle)

	// Create a site as the active owner and read it back end to end.
	alice, err := env.store.Users.GetByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("load alice: %v", err)
	}
	rec := createSite(ctx, t, env, "blog", "Blog", "Initial", "zh-CN", alice.ID)
	sitePath := "/api/v1/sites/" + rec.Site.ID.String()
	settingsPath := sitePath + "/settings"

	status, _, body = c.do("GET", "/api/v1/sites", nil, nil)
	if status != 200 {
		t.Fatalf("list sites: %d %s", status, body)
	}
	var list siteListResp
	decodeJSONBody(t, body, &list)
	if len(list.Sites) != 1 || list.Sites[0].Slug != "blog" || list.Sites[0].Role != "owner" {
		t.Fatalf("list sites content: %s", body)
	}

	status, _, body = c.do("GET", sitePath, nil, nil)
	if status != 200 {
		t.Fatalf("site detail: %d %s", status, body)
	}

	status, hdr, body := c.do("GET", settingsPath, nil, nil)
	if status != 200 {
		t.Fatalf("get settings: %d %s", status, body)
	}
	etag := hdr.Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) {
		t.Fatalf("settings GET must return a strong ETag, got %q", etag)
	}
	var before settingsDTO
	decodeJSONBody(t, body, &before)
	if before.Name != "Blog" || before.Revision != 1 {
		t.Fatalf("initial settings: %s", body)
	}

	// Successful save, then read back.
	status, hdr, body = c.do("PATCH", settingsPath, map[string]any{"name": "New Name", "description": "Updated", "defaultLocale": "en-US"}, map[string]string{"If-Match": etag})
	if status != 200 {
		t.Fatalf("patch settings: %d %s", status, body)
	}
	var after settingsDTO
	decodeJSONBody(t, body, &after)
	if after.Name != "New Name" || after.DefaultLocale != "en-US" || after.Revision != before.Revision+1 {
		t.Fatalf("patched settings: %s", body)
	}
	if hdr.Get("ETag") == etag {
		t.Fatalf("ETag must change after update")
	}

	status, _, body = c.do("GET", settingsPath, nil, nil)
	if status != 200 {
		t.Fatalf("re-read settings: %d %s", status, body)
	}
	var reread settingsDTO
	decodeJSONBody(t, body, &reread)
	if reread.Name != "New Name" || reread.DefaultLocale != "en-US" {
		t.Fatalf("persisted settings: %s", body)
	}
}

func TestIfMatchPreconditions(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	user, token, csrf := env.createUserSession("owner1")
	owner := env.clientFor(token, csrf)
	rec := createSite(ctx, t, env, "site1", "Site One", "", "zh-CN", user.ID)
	settingsPath := "/api/v1/sites/" + rec.Site.ID.String() + "/settings"

	// Missing If-Match is rejected with 428.
	status, _, body := owner.do("PATCH", settingsPath, map[string]string{"name": "X"}, nil)
	if status != 428 {
		t.Fatalf("patch without If-Match: %d %s, want 428", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "precondition_required" {
		t.Fatalf("problem code %q, want precondition_required", p.Code)
	}

	_, hdr, _ := owner.do("GET", settingsPath, nil, nil)
	// rev1 is current now; the first write below succeeds and moves it on,
	// making this ETag stale for the second write.
	stale := "\"" + rec.Site.ID.String() + "-r1\""
	status, _, body = owner.do("PATCH", settingsPath, map[string]string{"name": "Second"}, map[string]string{"If-Match": stale})
	if status != 200 {
		t.Fatalf("first patch: %d %s", status, body)
	}
	status, hdr, body = owner.do("PATCH", settingsPath, map[string]string{"name": "Third"}, map[string]string{"If-Match": stale})
	if status != 412 {
		t.Fatalf("stale If-Match: %d %s, want 412", status, body)
	}
	decodeJSONBody(t, body, &p)
	if p.Code != "precondition_failed" {
		t.Fatalf("problem code %q, want precondition_failed", p.Code)
	}
	if hdr.Get("ETag") == "" {
		t.Fatalf("412 must expose the current ETag")
	}

	// If-Match: * must not bypass optimistic locking in this MVP.
	status, _, body = owner.do("PATCH", settingsPath, map[string]string{"name": "Star"}, map[string]string{"If-Match": "*"})
	if status != 400 {
		t.Fatalf("patch with If-Match *: %d %s, want 400", status, body)
	}
	var star problemResp
	decodeJSONBody(t, body, &star)
	if star.Code != "invalid_if_match" {
		t.Fatalf("If-Match * code %q, want invalid_if_match", star.Code)
	}
}

func TestCSRFAndOriginEnforcement(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	user, token, csrf := env.createUserSession("owner2")
	owner := env.clientFor(token, csrf)
	rec := createSite(ctx, t, env, "site2", "Site Two", "", "zh-CN", user.ID)
	settingsPath := "/api/v1/sites/" + rec.Site.ID.String() + "/settings"
	_, hdr, _ := owner.do("GET", settingsPath, nil, nil)
	etag := hdr.Get("ETag")
	body := map[string]string{"name": "CSRF"}

	cases := []struct {
		name string
		hdr  map[string]string
		want int
	}{
		{"missing csrf header", map[string]string{"If-Match": etag, "X-CSRF-Token": ""}, 403},
		{"wrong csrf token", map[string]string{"If-Match": etag, "X-CSRF-Token": "bogus-token"}, 403},
		{"foreign origin", map[string]string{"If-Match": etag, "Origin": "https://evil.example"}, 403},
		{"no origin and no fetch metadata", map[string]string{"If-Match": etag, "Origin": "none"}, 403},
	}
	for _, tc := range cases {
		status, _, raw := owner.do("PATCH", settingsPath, body, tc.hdr)
		if status != tc.want {
			t.Fatalf("%s: got %d body %s, want %d", tc.name, status, raw, tc.want)
		}
		var p problemResp
		decodeJSONBody(t, raw, &p)
		if p.Code != "csrf_token_invalid" && p.Code != "origin_rejected" {
			t.Fatalf("%s: problem code %q", tc.name, p.Code)
		}
	}

	// Same-origin marker without an Origin header is the accepted
	// non-cross-site path.
	status, _, raw := owner.do("PATCH", settingsPath, body, map[string]string{"If-Match": etag, "Origin": "none", "Sec-Fetch-Site": "same-origin"})
	if status != 200 {
		t.Fatalf("same-origin marker: got %d body %s, want 200", status, raw)
	}

	// Logout is equally CSRF-protected.
	status, _, raw = owner.do("POST", "/api/v1/auth/logout", map[string]string{}, map[string]string{"X-CSRF-Token": "bogus"})
	if status != 403 {
		t.Fatalf("logout without csrf: got %d %s, want 403", status, raw)
	}
}

func TestMembershipIsolationAndRoles(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	alice, aliceToken, aliceCSRF := env.createUserSession("alice")
	bob, bobToken, bobCSRF := env.createUserSession("bob")
	carol, carolToken, carolCSRF := env.createUserSession("carol")

	siteA := createSite(ctx, t, env, "alpha", "Alpha", "", "zh-CN", alice.ID)
	siteB := createSite(ctx, t, env, "beta", "Beta", "", "zh-CN", bob.ID)
	// bob views alpha; carol edits alpha.
	if _, err := env.pool.Exec(ctx, `INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'viewer')`, siteA.Site.ID, bob.ID); err != nil {
		t.Fatalf("add bob viewer: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `INSERT INTO site_memberships (site_id, user_id, role) VALUES ($1, $2, 'editor')`, siteA.Site.ID, carol.ID); err != nil {
		t.Fatalf("add carol editor: %v", err)
	}

	aliceC := env.clientFor(aliceToken, aliceCSRF)
	bobC := env.clientFor(bobToken, bobCSRF)
	carolC := env.clientFor(carolToken, carolCSRF)

	// Listings are scoped to membership.
	status, _, body := aliceC.do("GET", "/api/v1/sites", nil, nil)
	if status != 200 {
		t.Fatalf("alice list: %d %s", status, body)
	}
	var list siteListResp
	decodeJSONBody(t, body, &list)
	if len(list.Sites) != 1 || list.Sites[0].Slug != "alpha" {
		t.Fatalf("alice must only see alpha: %s", body)
	}
	status, _, body = bobC.do("GET", "/api/v1/sites", nil, nil)
	if status != 200 {
		t.Fatalf("bob list: %d %s", status, body)
	}
	decodeJSONBody(t, body, &list)
	if len(list.Sites) != 2 {
		t.Fatalf("bob must see alpha and beta: %s", body)
	}

	// Cross-site reads are 404 so site existence is not leaked.
	betaPath := "/api/v1/sites/" + siteB.Site.ID.String()
	status, _, body = aliceC.do("GET", betaPath, nil, nil)
	if status != 404 {
		t.Fatalf("alice reading beta: %d %s, want 404", status, body)
	}
	status, _, body = aliceC.do("GET", betaPath+"/settings", nil, nil)
	if status != 404 {
		t.Fatalf("alice reading beta settings: %d %s, want 404", status, body)
	}

	// Viewers cannot write; editors and owners can.
	alphaSettings := "/api/v1/sites/" + siteA.Site.ID.String() + "/settings"
	_, hdr, _ := bobC.do("GET", alphaSettings, nil, nil)
	etag := hdr.Get("ETag")
	status, _, body = bobC.do("PATCH", alphaSettings, map[string]string{"name": "Hijack"}, map[string]string{"If-Match": etag})
	if status != 403 {
		t.Fatalf("viewer patch: %d %s, want 403", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "forbidden" {
		t.Fatalf("viewer patch code %q, want forbidden", p.Code)
	}
	status, _, body = carolC.do("PATCH", alphaSettings, map[string]string{"name": "Editor Name"}, map[string]string{"If-Match": etag})
	if status != 200 {
		t.Fatalf("editor patch: %d %s, want 200", status, body)
	}
	_, hdr, _ = aliceC.do("GET", alphaSettings, nil, nil)
	status, _, body = aliceC.do("PATCH", alphaSettings, map[string]string{"name": "Owner Name"}, map[string]string{"If-Match": hdr.Get("ETag")})
	if status != 200 {
		t.Fatalf("owner patch: %d %s, want 200", status, body)
	}
}

func TestChallengeReplayAndPermitReuse(t *testing.T) {
	env := setup(t)
	c := env.newClient()
	userHandle, auth, permit := bootstrapAndRegister(env, c, "first")

	// A second bootstrap is refused once an administrator is active.
	if _, _, err := env.store.BootstrapAdmin(context.Background(), "second", "second", time.Minute); err == nil {
		t.Fatal("bootstrap must be refused after the first administrator registered")
	}

	// The consumed permit cannot start another ceremony: enrollment is
	// closed once an active administrator exists.
	status, _, body := c.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": permit}, nil)
	if status != 409 {
		t.Fatalf("permit reuse options: %d %s, want 409", status, body)
	}
	var permitProblem problemResp
	decodeJSONBody(t, body, &permitProblem)
	if permitProblem.Code != "bootstrap_closed" {
		t.Fatalf("permit reuse code %q, want bootstrap_closed", permitProblem.Code)
	}

	// Replaying the consumed registration state fails.
	options := requestOptionsResp{}
	_, _, body = c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	decodeJSONBody(t, body, &options)
	assertion := auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", assertion, nil)
	if status != 200 {
		t.Fatalf("first login: %d %s", status, body)
	}

	// Replaying the same assertion (same tx cookie) is rejected.
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", assertion, nil)
	if status != 401 {
		t.Fatalf("login replay: %d %s, want 401", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "challenge_invalid" {
		t.Fatalf("login replay code %q, want challenge_invalid", p.Code)
	}

	// A tampered signature fails verification and burns the challenge.
	_, _, body = c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	decodeJSONBody(t, body, &options)
	bad := auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)
	bad.Response.Signature = base64.RawURLEncoding.EncodeToString([]byte("garbage"))
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", bad, nil)
	if status != 401 {
		t.Fatalf("tampered signature: %d %s, want 401", status, body)
	}
	decodeJSONBody(t, body, &p)
	if p.Code != "credential_rejected" {
		t.Fatalf("tampered signature code %q, want credential_rejected", p.Code)
	}
	good := auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", good, nil)
	if status != 401 {
		t.Fatalf("challenge after failed attempt must be consumed: %d %s, want 401", status, body)
	}
}

func TestChallengeExpiry(t *testing.T) {
	env := setup(t)
	c := env.newClient()
	userHandle, auth, _ := bootstrapAndRegister(env, c, "expiry")

	// Start a login and force its ceremony state into the past.
	status, _, body := c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	if status != 200 {
		t.Fatalf("login options: %d %s", status, body)
	}
	ctx := context.Background()
	cookie := readCookie(c, authTxCookieName, "/api/v1/auth/login/verify")
	if _, err := env.pool.Exec(ctx,
		`UPDATE auth_states SET expires_at = now() - interval '1 second' WHERE id = $1`, cookie); err != nil {
		t.Fatalf("expire auth state: %v", err)
	}
	var options requestOptionsResp
	decodeJSONBody(t, body, &options)
	assertion := auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)
	status, _, body = c.do("POST", "/api/v1/auth/login/verify", assertion, nil)
	if status != 401 {
		t.Fatalf("expired challenge: %d %s, want 401", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "challenge_invalid" {
		t.Fatalf("expired challenge code %q, want challenge_invalid", p.Code)
	}
}

func TestPermitExpiry(t *testing.T) {
	env := setup(t)
	c := env.newClient()
	ctx := context.Background()

	user, err := env.store.Users.Create(ctx, "pending1", "pending1")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	code, _, _, err := env.store.Permits.Create(ctx, user.ID, -time.Minute)
	if err != nil {
		t.Fatalf("create expired permit: %v", err)
	}
	status, _, body := c.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": code}, nil)
	if status != 400 {
		t.Fatalf("expired permit: %d %s, want 400", status, body)
	}
}

func TestSettingsValidation(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	user, token, csrf := env.createUserSession("valid")
	owner := env.clientFor(token, csrf)
	rec := createSite(ctx, t, env, "site3", "Site Three", "", "zh-CN", user.ID)
	settingsPath := "/api/v1/sites/" + rec.Site.ID.String() + "/settings"
	_, hdr, _ := owner.do("GET", settingsPath, nil, nil)
	etag := hdr.Get("ETag")

	cases := []struct {
		name string
		body any
		want int
	}{
		{"unknown field", map[string]any{"name": "x", "unexpected": 1}, 400},
		{"bad locale", map[string]any{"defaultLocale": "fr-FR"}, 400},
		{"name too long", map[string]any{"name": strings.Repeat("n", 201)}, 400},
		{"empty name", map[string]any{"name": "   "}, 400},
		{"empty patch", map[string]any{}, 400},
	}
	for _, tc := range cases {
		status, _, body := owner.do("PATCH", settingsPath, tc.body, map[string]string{"If-Match": etag})
		if status != tc.want {
			t.Fatalf("%s: got %d body %s, want %d", tc.name, status, body, tc.want)
		}
		var p problemResp
		decodeJSONBody(t, body, &p)
		if p.Code != "validation_error" {
			t.Fatalf("%s: code %q, want validation_error", tc.name, p.Code)
		}
	}

	// Malformed siteId is a 400, not a 500.
	status, _, body := owner.do("GET", "/api/v1/sites/not-a-uuid/settings", nil, nil)
	if status != 400 {
		t.Fatalf("malformed siteId: %d %s, want 400", status, body)
	}
}
