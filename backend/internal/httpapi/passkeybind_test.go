package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/testauth"
)

// bindOptions drives POST /api/v1/auth/passkey/register/options.
func bindOptions(t *testing.T, env *testEnv, c *tclient) (int, creationOptionsResp, problemResp) {
	t.Helper()
	status, _, raw := c.do("POST", "/api/v1/auth/passkey/register/options", map[string]string{}, nil)
	var options creationOptionsResp
	var problem problemResp
	if status == 200 {
		decodeJSONBody(t, raw, &options)
	} else {
		decodeJSONBody(t, raw, &problem)
	}
	return status, options, problem
}

// bindVerify drives POST /api/v1/auth/passkey/register/verify.
func bindVerify(t *testing.T, c *tclient, response any) (int, ceremonyDoneResp, problemResp) {
	t.Helper()
	status, _, raw := c.do("POST", "/api/v1/auth/passkey/register/verify", response, nil)
	var done ceremonyDoneResp
	var problem problemResp
	if status == 200 {
		decodeJSONBody(t, raw, &done)
	} else {
		decodeJSONBody(t, raw, &problem)
	}
	return status, done, problem
}

// bindPasskeyViaAPI completes a binding ceremony for the client's current
// session and adopts the rotated CSRF token.
func bindPasskeyViaAPI(t *testing.T, env *testEnv, c *tclient, auth *testauth.Authenticator) {
	t.Helper()
	status, options, problem := bindOptions(t, env, c)
	if status != 200 {
		t.Fatalf("bind options status = %d problem = %+v", status, problem)
	}
	response := auth.CreateCredential("localhost", env.base, options.PublicKey.Challenge)
	status, done, problem := bindVerify(t, c, response)
	if status != 200 {
		t.Fatalf("bind verify status = %d problem = %+v", status, problem)
	}
	if done.SessionScope != "full" || !done.PasskeyBound || done.CSRFToken == "" {
		t.Fatalf("bind response must report the rotated full session: %+v", done)
	}
	c.csrf = done.CSRFToken
}

// createZeroKeyUser constructs an active administrator without any
// credential — the recovery scenario for accounts activated before passkey
// binding was required.
func (e *testEnv) createZeroKeyUser(username string) model.User {
	e.t.Helper()
	ctx := context.Background()
	user, err := e.store.Users.Create(ctx, username, username)
	if err != nil {
		e.t.Fatalf("create user %s: %v", username, err)
	}
	if err := e.store.Users.Activate(ctx, user.ID); err != nil {
		e.t.Fatalf("activate user %s: %v", username, err)
	}
	return user
}

// exchangeAs issues a login code for the username and redeems it on a fresh
// client, returning the client and the exchange outcome.
func exchangeAs(t *testing.T, env *testEnv, username string) (*tclient, ceremonyDoneResp) {
	t.Helper()
	code, _, err := env.store.IssueLoginCode(context.Background(), username, 10*time.Minute)
	if err != nil {
		t.Fatalf("login-code for %s: %v", username, err)
	}
	c := env.newClient()
	status, done, problem := exchangeCode(c, code)
	if status != 200 {
		t.Fatalf("exchange for %s status = %d problem = %+v", username, status, problem)
	}
	c.csrf = done.CSRFToken
	return c, done
}

// TestPasskeyBindZeroKeyAccountRecovery proves an account that was activated
// without ever owning a credential can still recover: the login code yields
// a restricted session, business APIs stay closed, the binding ceremony
// rotates into a full session, and other restricted sessions of the same
// account are NOT upgraded by that one completion.
func TestPasskeyBindZeroKeyAccountRecovery(t *testing.T) {
	env := setup(t)
	env.createZeroKeyUser("zeroadmin")

	c1, done1 := exchangeAs(t, env, "zeroadmin")
	if done1.SessionScope != "restricted" || done1.PasskeyBound {
		t.Fatalf("zero-key exchange must be restricted: %+v", done1)
	}
	c2, done2 := exchangeAs(t, env, "zeroadmin")
	if done2.SessionScope != "restricted" {
		t.Fatalf("second zero-key exchange must be restricted: %+v", done2)
	}

	// Both restricted sessions are locked out of business APIs.
	for i, c := range []*tclient{c1, c2} {
		status, _, raw := c.do("GET", "/api/v1/themes", nil, nil)
		var problem problemResp
		decodeJSONBody(t, raw, &problem)
		if status != 403 || problem.Code != "passkey_binding_required" {
			t.Fatalf("restricted client %d themes status = %d problem = %+v", i, status, problem)
		}
	}

	// c1 completes the binding.
	bindPasskeyViaAPI(t, env, c1, testauth.New())
	if status, _, _ := c1.do("GET", "/api/v1/sites", nil, nil); status != 200 {
		t.Fatalf("bound client sites status = %d", status)
	}

	// c2's restricted session is untouched: still restricted, still refused.
	status, _, raw := c2.do("GET", "/api/v1/session", nil, nil)
	var probe sessionResp
	decodeJSONBody(t, raw, &probe)
	if status != 200 || probe.SessionScope != "restricted" {
		t.Fatalf("other restricted session must stay restricted: %+v status = %d", probe, status)
	}
	if status, _, _ := c2.do("GET", "/api/v1/sites", nil, nil); status != 403 {
		t.Fatalf("other restricted session sites status = %d, want 403", status)
	}

	// Once bound, a fresh login code yields a full session immediately.
	c3, done3 := exchangeAs(t, env, "zeroadmin")
	if done3.SessionScope != "full" || !done3.PasskeyBound {
		t.Fatalf("bound account login code must yield a full session: %+v", done3)
	}
	if status, _, _ := c3.do("GET", "/api/v1/sites", nil, nil); status != 200 {
		t.Fatalf("login-code full session sites status = %d", status)
	}
}

// stealAuthTx copies the ceremony cookie of one client into another's jar,
// standing in for a challenge presented under the wrong session.
func stealAuthTx(t *testing.T, env *testEnv, from, to *tclient) {
	t.Helper()
	u, err := url.Parse(env.base + "/api/v1/auth/passkey/register/verify")
	if err != nil {
		t.Fatalf("parse verify URL: %v", err)
	}
	var stolen *http.Cookie
	for _, ck := range from.hc.Jar.Cookies(u) {
		if ck.Name == authTxCookieName {
			stolen = ck
		}
	}
	if stolen == nil {
		t.Fatal("no ceremony cookie to steal")
	}
	to.hc.Jar.SetCookies(u, []*http.Cookie{stolen})
}

// TestPasskeyBindChallengeGuards covers replay and cross-session challenge
// use: a consumed challenge is dead, and a challenge minted for one session
// cannot complete under another session of the same account.
func TestPasskeyBindChallengeGuards(t *testing.T) {
	env := setup(t)
	env.createZeroKeyUser("guarduser")

	c1, _ := exchangeAs(t, env, "guarduser")
	c2, _ := exchangeAs(t, env, "guarduser")

	// Cross-session: c1's challenge presented under c2's session must fail.
	status, options, _ := bindOptions(t, env, c1)
	if status != 200 {
		t.Fatalf("options for c1: %d", status)
	}
	authA := testauth.New()
	responseA := authA.CreateCredential("localhost", env.base, options.PublicKey.Challenge)
	stealAuthTx(t, env, c1, c2)
	status, _, problem := bindVerify(t, c2, responseA)
	if status != 401 || problem.Code != "challenge_invalid" {
		t.Fatalf("cross-session verify status = %d problem = %+v", status, problem)
	}
	// The wrong-session attempt consumed the challenge; the owning session
	// cannot replay it either.
	status, _, problem = bindVerify(t, c1, responseA)
	if status != 401 || problem.Code != "challenge_invalid" {
		t.Fatalf("replayed verify status = %d problem = %+v", status, problem)
	}

	// Retry after a consumed/aborted challenge works while the restricted
	// session is alive — no fresh code redemption is required.
	bindPasskeyViaAPI(t, env, c1, authA)

	// The rotated session's own challenge cannot be replayed either.
	status, options2, _ := bindOptions(t, env, c1)
	if status != 200 {
		t.Fatalf("second options: %d", status)
	}
	response2 := testauth.New().CreateCredential("localhost", env.base, options2.PublicKey.Challenge)
	status, done2, problem := bindVerify(t, c1, response2)
	if status != 200 {
		t.Fatalf("second bind should succeed on the full session: %d %+v", status, problem)
	}
	c1.csrf = done2.CSRFToken
	status, _, problem = bindVerify(t, c1, response2)
	if status != 401 || problem.Code != "challenge_invalid" {
		t.Fatalf("verify replay status = %d problem = %+v", status, problem)
	}
}

// TestPasskeyBindDuplicateCredential proves re-registering an authenticator
// credential that is already stored is rejected as a conflict, never
// silently re-bound.
func TestPasskeyBindDuplicateCredential(t *testing.T) {
	env := setup(t)
	env.createZeroKeyUser("dupuser")
	c, _ := exchangeAs(t, env, "dupuser")

	auth := testauth.New()
	bindPasskeyViaAPI(t, env, c, auth)

	// The account is bound now, so a fresh login code yields a full session;
	// binding the SAME credential again must hit the duplicate guard.
	c2, done2 := exchangeAs(t, env, "dupuser")
	if done2.SessionScope != "full" {
		t.Fatalf("bound account exchange must be full: %+v", done2)
	}
	status, options, _ := bindOptions(t, env, c2)
	if status != 200 {
		t.Fatalf("options: %d", status)
	}
	response := auth.CreateCredential("localhost", env.base, options.PublicKey.Challenge)
	status, _, problem := bindVerify(t, c2, response)
	if status != 409 || problem.Code != "credential_duplicate" {
		t.Fatalf("duplicate credential status = %d problem = %+v", status, problem)
	}
}

// TestPasskeyBindConcurrency proves a double-submitted completion (the same
// ceremony response racing itself, as a retried double click would) rotates
// the session exactly once: one request wins, the loser is rejected with the
// precise challenge failure instead of silently upgrading twice.
func TestPasskeyBindConcurrency(t *testing.T) {
	env := setup(t)
	env.createZeroKeyUser("raceuser")
	c, _ := exchangeAs(t, env, "raceuser")

	status, options, _ := bindOptions(t, env, c)
	if status != 200 {
		t.Fatalf("options: %d", status)
	}
	response := testauth.New().CreateCredential("localhost", env.base, options.PublicKey.Challenge)

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	problems := make([]problemResp, 2)
	dones := make([]ceremonyDoneResp, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], dones[i], problems[i] = bindVerify(t, c, response)
		}(i)
	}
	wg.Wait()

	successes := 0
	for i, s := range statuses {
		switch {
		case s == 200:
			successes++
			c.csrf = dones[i].CSRFToken
		// The loser either finds the challenge already consumed, or arrives
		// after the winner revoked the carrying session — both are the
		// precise rejection, never a second silent upgrade.
		case s == 401 && (problems[i].Code == "challenge_invalid" || problems[i].Code == "unauthenticated"):
		default:
			t.Fatalf("concurrent verify %d: unexpected status %d problem %+v", i, s, problems[i])
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent bind successes = %d, want exactly 1 (statuses %v)", successes, statuses)
	}

	// The account ends up bound with exactly one credential and a working
	// full session.
	var probe sessionResp
	status, _, raw := c.do("GET", "/api/v1/session", nil, nil)
	decodeJSONBody(t, raw, &probe)
	if status != 200 || probe.SessionScope != "full" || !probe.PasskeyBound {
		t.Fatalf("post-race probe = %+v status = %d", probe, status)
	}
	var credentials int
	if err := env.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM passkeys WHERE user_id = (SELECT id FROM users WHERE username = 'raceuser')`).Scan(&credentials); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if credentials != 1 {
		t.Fatalf("credentials after race = %d, want exactly 1", credentials)
	}
}

// TestExchangeWithForeignRPCredential proves a credential registered under a
// different relying party never counts as bound for the current RP: the
// account keeps its historical rows but still goes through binding.
func TestExchangeWithForeignRPCredential(t *testing.T) {
	env := setup(t)
	user := env.createZeroKeyUser("legacyrp")

	// A credential registered under some earlier relying party ID.
	foreign := testauth.New()
	credID, publicKey := foreign.ExportCredential()
	if err := env.store.Passkeys.Create(context.Background(), model.Passkey{
		UserID:       user.ID,
		RPID:         "manager.old-example.invalid",
		CredentialID: credID,
		PublicKey:    publicKey,
		Transports:   []string{},
		UserVerified: true,
	}); err != nil {
		t.Fatalf("seed foreign-rp passkey: %v", err)
	}

	_, done := exchangeAs(t, env, "legacyrp")
	if done.SessionScope != "restricted" || done.PasskeyBound {
		t.Fatalf("foreign-RP credential must not count as bound: %+v", done)
	}

	// The historical row is preserved untouched.
	var rpID string
	if err := env.pool.QueryRow(context.Background(),
		`SELECT rp_id FROM passkeys WHERE user_id = $1`, user.ID).Scan(&rpID); err != nil {
		t.Fatalf("read foreign rp_id: %v", err)
	}
	if rpID != "manager.old-example.invalid" {
		t.Fatalf("historical credential rp_id changed: %q", rpID)
	}
}
