package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/testauth"
)

// exchangeCode drives POST /api/v1/auth/exchange with a code string body.
func exchangeCode(c *tclient, code string) (int, ceremonyDoneResp, problemResp) {
	c.env.t.Helper()
	status, _, raw := c.do("POST", "/api/v1/auth/exchange", map[string]string{"code": code}, nil)
	var done ceremonyDoneResp
	var problem problemResp
	if status == 200 {
		decodeJSONBody(c.env.t, raw, &done)
	} else {
		decodeJSONBody(c.env.t, raw, &problem)
	}
	return status, done, problem
}

// TestAuthCodeInitFlow covers the full first-run binding: CLI issuance,
// platform-uninitialized probe, single redemption that activates the pending
// administrator and yields a restricted registration session, business-API
// rejection while restricted, the passkey-binding ceremony rotating into a
// full session, code reuse rejection, and logout.
func TestAuthCodeInitFlow(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	anon := env.newClient()

	// Before initialization the session probe reports the platform as
	// uninitialized so the UI can show CLI guidance instead of a login form.
	status, _, raw := anon.do("GET", "/api/v1/session", nil, nil)
	if status != 200 {
		t.Fatalf("session probe status = %d", status)
	}
	var probe sessionResp
	decodeJSONBody(t, raw, &probe)
	if probe.Authenticated || probe.PlatformInitialized {
		t.Fatalf("empty platform should report anonymous+uninitialized, got %+v", probe)
	}

	code, _, err := env.store.InitAdmin(ctx, "firstadmin", "First Admin", 10*time.Minute)
	if err != nil {
		t.Fatalf("init-admin: %v", err)
	}
	status, done, problem := exchangeCode(anon, code)
	if status != 200 {
		t.Fatalf("exchange init code status = %d problem = %+v", status, problem)
	}
	if done.User.Username != "firstadmin" || done.CSRFToken == "" {
		t.Fatalf("exchange response incomplete: %+v", done)
	}
	if done.SessionScope != "restricted" || done.PasskeyBound {
		t.Fatalf("first-run exchange must yield a restricted unbound session: %+v", done)
	}
	anon.csrf = done.CSRFToken

	// The restricted session is authenticated but not admitted to business
	// APIs, and the probe says exactly that.
	status, _, raw = anon.do("GET", "/api/v1/session", nil, nil)
	decodeJSONBody(t, raw, &probe)
	if status != 200 || !probe.Authenticated || !probe.PlatformInitialized ||
		probe.SessionScope != "restricted" || probe.PasskeyBound || !probe.PasskeyAvailable {
		t.Fatalf("post-exchange probe = %+v status = %d", probe, status)
	}
	status, _, raw = anon.do("GET", "/api/v1/sites", nil, nil)
	var restrictedProblem problemResp
	decodeJSONBody(t, raw, &restrictedProblem)
	if status != 403 || restrictedProblem.Code != "passkey_binding_required" {
		t.Fatalf("restricted sites status = %d problem = %+v", status, restrictedProblem)
	}

	// Binding completes the first-run flow: credential persisted under the
	// current RP, the restricted session rotated into a full one.
	bindPasskeyViaAPI(t, env, anon, testauth.New())
	status, _, raw = anon.do("GET", "/api/v1/session", nil, nil)
	decodeJSONBody(t, raw, &probe)
	if status != 200 || probe.SessionScope != "full" || !probe.PasskeyBound {
		t.Fatalf("post-bind probe = %+v status = %d", probe, status)
	}
	if status, _, _ := anon.do("GET", "/api/v1/sites", nil, nil); status != 200 {
		t.Fatalf("post-bind sites status = %d", status)
	}

	// Replaying the consumed code fails with the exact problem code.
	status, _, problem = exchangeCode(anon, code)
	if status != 409 || problem.Code != "code_used" {
		t.Fatalf("replay status = %d problem = %+v", status, problem)
	}

	// Logout kills the session server-side.
	status, _, _ = anon.do("POST", "/api/v1/auth/logout", map[string]string{}, nil)
	if status != 204 {
		t.Fatalf("logout status = %d", status)
	}
	status, _, _ = anon.do("GET", "/api/v1/sites", nil, nil)
	if status != 401 {
		t.Fatalf("post-logout sites status = %d, want 401", status)
	}
}

// TestAuthCodeLoginFlowAndConcurrency covers per-login codes for an active
// administrator and proves that two concurrent redemptions of one code can
// never both succeed.
func TestAuthCodeLoginFlowAndConcurrency(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	env.createUserSession("alice")

	code, _, err := env.store.IssueLoginCode(ctx, "alice", 10*time.Minute)
	if err != nil {
		t.Fatalf("login-code: %v", err)
	}

	c1 := env.newClient()
	status, done, problem := exchangeCode(c1, code)
	if status != 200 || done.User.Username != "alice" {
		t.Fatalf("login exchange status = %d done = %+v problem = %+v", status, done, problem)
	}
	c1.csrf = done.CSRFToken
	if status, _, _ := c1.do("GET", "/api/v1/sites", nil, nil); status != 200 {
		t.Fatalf("post-login sites status = %d", status)
	}

	// A second login needs a fresh code; wrong codes are rejected.
	if _, _, problem := exchangeCode(env.newClient(), "XXXX-XXXX-XXXX-XXXX"); problem.Code != "invalid_code" {
		t.Fatalf("wrong code problem = %+v", problem)
	}
	code2, _, err := env.store.IssueLoginCode(ctx, "alice", 10*time.Minute)
	if err != nil {
		t.Fatalf("second login-code: %v", err)
	}

	// Concurrency: two clients redeem the same fresh code simultaneously;
	// exactly one wins.
	var wg sync.WaitGroup
	results := make([]int, 2)
	clients := make([]*tclient, 2)
	for i := range clients {
		clients[i] = env.newClient()
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, _, p := exchangeCode(clients[i], code2)
			results[i] = s
			_ = p
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, s := range results {
		if s == 200 {
			wins++
		} else if s != 409 {
			t.Fatalf("unexpected concurrent exchange status %d", s)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent redemption wins = %d, want exactly 1 (results %v)", wins, results)
	}
}

// TestAuthCodePurposeSeparation proves init and login codes cannot be
// repurposed: a login code bound to a still-pending user is rejected, and
// issuing login codes for pending users is refused at the store layer.
func TestAuthCodePurposeSeparation(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	if _, _, err := env.store.IssueLoginCode(ctx, "ghost", time.Minute); err == nil {
		t.Fatal("login code for unknown user should fail")
	}

	// The issuer rejects a login code for a pending account. A stored code
	// from an older issuer must still be rejected at exchange time.
	pending, err := env.store.Users.Create(ctx, "pendinguser", "Pending")
	if err != nil {
		t.Fatalf("create pending user: %v", err)
	}
	if _, _, _, err := env.store.AuthCodes.Create(ctx, pending.ID, store.AuthCodePurposeLogin, time.Minute); !errors.Is(err, store.ErrCodePurpose) {
		t.Fatalf("issue pending-user login code: got %v, want ErrCodePurpose", err)
	}
	loginCode, codeID, _, err := env.store.AuthCodes.Create(ctx, pending.ID, store.AuthCodePurposeInit, time.Minute)
	if err != nil {
		t.Fatalf("seed legacy code: %v", err)
	}
	if _, err := env.pool.Exec(ctx, `UPDATE auth_codes SET purpose = 'login' WHERE id = $1`, codeID); err != nil {
		t.Fatalf("arrange legacy purpose mismatch: %v", err)
	}
	status, _, problem := exchangeCode(env.newClient(), loginCode)
	if status != 409 || problem.Code != "code_purpose_mismatch" {
		t.Fatalf("pending-user login exchange status = %d problem = %+v", status, problem)
	}

	// Once an active administrator exists, init-admin is closed.
	admin, _, _ := env.createUserSession("rootadmin")
	if _, _, _, err := env.store.AuthCodes.Create(ctx, admin.ID, store.AuthCodePurposeInit, time.Minute); !errors.Is(err, store.ErrCodePurpose) {
		t.Fatalf("issue active-user initialization code: got %v, want ErrCodePurpose", err)
	}
	if _, _, err := env.store.InitAdmin(ctx, "secondadmin", "Second", time.Minute); err == nil {
		t.Fatal("init-admin after an active administrator should fail")
	}
}

// TestAuthCodeExpiryAndAttempts covers expired codes and the presentation
// attempt lock.
func TestAuthCodeExpiryAndAttempts(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	user, _, _ := env.createUserSession("bob")

	// Expired code (created with a TTL in the past via direct store insert
	// using a negative window is impossible; use a short TTL and wait).
	code, _, _, err := env.store.AuthCodes.Create(ctx, user.ID, store.AuthCodePurposeLogin, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("issue short code: %v", err)
	}
	time.Sleep(80 * time.Millisecond)
	status, _, problem := exchangeCode(env.newClient(), code)
	if status != 409 || problem.Code != "code_expired" {
		t.Fatalf("expired exchange status = %d problem = %+v", status, problem)
	}

	// Repeated presentations of an expired-but-unused code keep returning the
	// stable expired problem (attempts burn down; no flapping between codes).
	staleCode, _, _, err := env.store.AuthCodes.Create(ctx, user.ID, store.AuthCodePurposeLogin, 40*time.Millisecond)
	if err != nil {
		t.Fatalf("issue stale code: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	c := env.newClient()
	for i := 1; i <= store.MaxCodeAttempts; i++ {
		s, _, p := exchangeCode(c, staleCode)
		if s == 429 {
			t.Fatalf("unexpected rate limit at presentation %d", i)
		}
		if p.Code != "code_expired" {
			t.Fatalf("presentation %d: unexpected problem %+v", i, p)
		}
	}

	// A used (consumed) code replays as code_used.
	code2, _, err := env.store.IssueLoginCode(ctx, "bob", time.Hour)
	if err != nil {
		t.Fatalf("issue code2: %v", err)
	}
	if s, _, _ := exchangeCode(c, code2); s != 200 {
		t.Fatalf("code2 first presentation failed: %d", s)
	}
	if _, _, p := exchangeCode(c, code2); p.Code != "code_used" {
		t.Fatalf("code2 replay problem = %+v", p)
	}
}

// TestSiteCreateAndProvision covers the in-browser site creation loop:
// creation via HTTP, on-disk provisioning, duplicate slug rejection, slug
// validation, listing, and idempotent provision retry.
func TestSiteCreateAndProvision(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	_, token, csrf := env.createUserSession("owner")
	c := env.clientFor(token, csrf)

	body := map[string]any{"slug": "personal", "name": "Personal Site", "defaultLocale": "zh-CN"}
	status, hdr, raw := c.do("POST", "/api/v1/sites", body, nil)
	if status != 201 {
		t.Fatalf("create site status = %d body = %s", status, raw)
	}
	var created siteDTO
	decodeJSONBody(t, raw, &created)
	if created.Slug != "personal" || created.Visibility != "private" || created.ProvisionState != "ready" || created.Role != "owner" {
		t.Fatalf("created site dto = %+v", created)
	}
	_ = hdr

	// On-disk provisioning really happened.
	siteID := uuid.MustParse(created.SiteID)
	for _, rel := range []string{
		filepath.Join("content", "posts"),
		filepath.Join("content", "data", "friends.yaml"),
		filepath.Join("content", "albums", "metadata", "index.json"),
		filepath.Join("config", "site.yaml"),
		filepath.Join("config", "content.yaml"),
	} {
		if _, err := os.Stat(filepath.Join(env.provisionRoot, siteID.String(), rel)); err != nil {
			t.Fatalf("provisioned path missing: %v", err)
		}
	}

	// Duplicate slug and invalid slug.
	status, _, raw = c.do("POST", "/api/v1/sites", map[string]any{"slug": "personal", "name": "Dup"}, nil)
	if status != 409 {
		t.Fatalf("duplicate slug status = %d body = %s", status, raw)
	}
	status, _, raw = c.do("POST", "/api/v1/sites", map[string]any{"slug": "Bad Slug", "name": "X"}, nil)
	if status != 400 {
		t.Fatalf("invalid slug status = %d body = %s", status, raw)
	}

	// The list shows the created site.
	status, _, raw = c.do("GET", "/api/v1/sites", nil, nil)
	var list siteListResp
	decodeJSONBody(t, raw, &list)
	if len(list.Sites) != 1 || list.Sites[0].Slug != "personal" {
		t.Fatalf("site list = %+v", list)
	}

	// Provision retry is idempotent on a ready site.
	status, _, raw = c.do("POST", "/api/v1/sites/"+created.SiteID+"/provision", map[string]string{}, nil)
	if status != 200 {
		t.Fatalf("provision retry status = %d body = %s", status, raw)
	}
	decodeJSONBody(t, raw, &created)
	if created.ProvisionState != "ready" {
		t.Fatalf("provision retry state = %s", created.ProvisionState)
	}

	// A non-member cannot even see the site.
	_, otherToken, otherCSRF := env.createUserSession("stranger")
	oc := env.clientFor(otherToken, otherCSRF)
	status, _, _ = oc.do("GET", "/api/v1/sites/"+created.SiteID, nil, nil)
	if status != 404 {
		t.Fatalf("stranger get site status = %d, want 404", status)
	}
	status, _, _ = oc.do("POST", "/api/v1/sites/"+created.SiteID+"/provision", map[string]string{}, nil)
	if status != 404 {
		t.Fatalf("stranger provision status = %d, want 404", status)
	}
	_ = ctx
}
