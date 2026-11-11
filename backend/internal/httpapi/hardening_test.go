package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/testauth"
)

// TestConcurrentFirstEnrollment proves the first-administrator window closes
// atomically: two users holding two valid permits complete their ceremonies
// concurrently, exactly one succeeds, and the loser leaves no credential or
// activation behind.
func TestConcurrentFirstEnrollment(t *testing.T) {
	env := setup(t)
	ctx := context.Background()

	type contender struct {
		client  *tclient
		options creationOptionsResp
		auth    *testauth.Authenticator
	}
	ready := make([]contender, 2)
	for i := 0; i < 2; i++ {
		username := fmt.Sprintf("racer%d", i)
		code, _, err := env.store.BootstrapAdmin(ctx, username, username, 10*time.Minute)
		if err != nil {
			t.Fatalf("bootstrap %s: %v", username, err)
		}
		client := env.newClient()
		status, _, body := client.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": code}, nil)
		if status != 200 {
			t.Fatalf("racer %d options: %d %s", i, status, body)
		}
		var options creationOptionsResp
		decodeJSONBody(t, body, &options)
		ready[i] = contender{client: client, options: options, auth: testauth.New()}
	}

	results := make([]int, 2)
	codes := make([]string, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := ready[i]
			<-start
			resp := c.auth.CreateCredential("localhost", env.base, c.options.PublicKey.Challenge)
			status, _, body := c.client.do("POST", "/api/v1/auth/register/verify", resp, nil)
			results[i] = status
			var p problemResp
			decodeJSONBody(t, body, &p)
			codes[i] = p.Code
		}(i)
	}
	close(start)
	wg.Wait()

	if results[0] == 200 && results[1] == 200 {
		t.Fatalf("both concurrent enrollments succeeded: %v", results)
	}
	if results[0] != 200 && results[1] != 200 {
		t.Fatalf("neither concurrent enrollment succeeded: %d/%s %d/%s", results[0], codes[0], results[1], codes[1])
	}
	loser := 0
	if results[0] == 200 {
		loser = 1
	}
	if results[loser] != 409 || codes[loser] != "bootstrap_closed" {
		t.Fatalf("loser: status %d code %q, want 409 bootstrap_closed", results[loser], codes[loser])
	}

	// The loser keeps no credential and stays pending.
	var passkeyCount int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM passkeys`).Scan(&passkeyCount); err != nil {
		t.Fatalf("count passkeys: %v", err)
	}
	if passkeyCount != 1 {
		t.Fatalf("expected exactly one credential, got %d", passkeyCount)
	}
	var pendingCount int
	if err := env.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE status = 'pending'`).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected exactly one pending user, got %d", pendingCount)
	}
}

// TestPermitExpiredDuringCeremony proves a permit that expires after its
// challenge was issued cannot complete enrollment: the challenge must not
// extend the enrollment authorization.
func TestPermitExpiredDuringCeremony(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	c := env.newClient()

	code, _, err := env.store.BootstrapAdmin(ctx, "late", "late", 10*time.Minute)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	status, _, body := c.do("POST", "/api/v1/auth/register/options", map[string]string{"permit": code}, nil)
	if status != 200 {
		t.Fatalf("options: %d %s", status, body)
	}
	var options creationOptionsResp
	decodeJSONBody(t, body, &options)

	if _, err := env.pool.Exec(ctx,
		`UPDATE enrollment_permits SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatalf("expire permits: %v", err)
	}

	auth := testauth.New()
	resp := auth.CreateCredential("localhost", env.base, options.PublicKey.Challenge)
	status, _, body = c.do("POST", "/api/v1/auth/register/verify", resp, nil)
	if status != 409 {
		t.Fatalf("verify with expired permit: %d %s, want 409", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "permit_expired" {
		t.Fatalf("code %q, want permit_expired", p.Code)
	}

	// Nothing was written: the user stays pending without a credential.
	var passkeys, pending int
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM passkeys`).Scan(&passkeys)
	_ = env.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE status='active'`).Scan(&pending)
	if passkeys != 0 || pending != 0 {
		t.Fatalf("expired permit must not write state: passkeys=%d active=%d", passkeys, pending)
	}
}

// TestFullETagBinding proves If-Match must name the exact target resource:
// another site's ETag at the same revision and well-formed foreign tags are
// rejected, and only the site's own current ETag is accepted.
func TestFullETagBinding(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	user, token, csrf := env.createUserSession("owner-etag")
	owner := env.clientFor(token, csrf)

	siteA := createSite(ctx, t, env, "etag-a", "A", "", "zh-CN", user.ID)
	siteB := createSite(ctx, t, env, "etag-b", "B", "", "zh-CN", user.ID)
	pathA := "/api/v1/sites/" + siteA.Site.ID.String() + "/settings"
	pathB := "/api/v1/sites/" + siteB.Site.ID.String() + "/settings"

	// Move both sites to revision 2 so bare revision numbers would collide.
	_, hdrA, _ := owner.do("GET", pathA, nil, nil)
	if status, _, body := owner.do("PATCH", pathA, map[string]string{"name": "A2"}, map[string]string{"If-Match": hdrA.Get("ETag")}); status != 200 {
		t.Fatalf("bump A: %d %s", status, body)
	}
	_, hdrB, _ := owner.do("GET", pathB, nil, nil)
	if status, _, body := owner.do("PATCH", pathB, map[string]string{"name": "B2"}, map[string]string{"If-Match": hdrB.Get("ETag")}); status != 200 {
		t.Fatalf("bump B: %d %s", status, body)
	}
	_, hdrB, _ = owner.do("GET", pathB, nil, nil)
	foreignETag := hdrB.Get("ETag")

	cases := []struct {
		name    string
		ifMatch string
		want    int
		code    string
	}{
		{"other site same revision", foreignETag, 412, "precondition_failed"},
		{"foreign well-formed uuid", `"00000000-0000-0000-0000-000000000000-r2"`, 412, "precondition_failed"},
		{"forged suffix", `"` + siteA.Site.ID.String() + `-r2x"`, 400, "invalid_if_match"},
		{"wildcard", "*", 400, "invalid_if_match"},
	}
	for _, tc := range cases {
		status, _, body := owner.do("PATCH", pathA, map[string]string{"name": "Hijack"}, map[string]string{"If-Match": tc.ifMatch})
		if status != tc.want {
			t.Fatalf("%s: got %d body %s, want %d", tc.name, status, body, tc.want)
		}
		var p problemResp
		decodeJSONBody(t, body, &p)
		if p.Code != tc.code {
			t.Fatalf("%s: code %q, want %q", tc.name, p.Code, tc.code)
		}
	}

	// The site's own current ETag still succeeds.
	_, hdrA, _ = owner.do("GET", pathA, nil, nil)
	status, _, body := owner.do("PATCH", pathA, map[string]string{"name": "A3"}, map[string]string{"If-Match": hdrA.Get("ETag")})
	if status != 200 {
		t.Fatalf("own etag: %d %s", status, body)
	}
}

// TestStableCSRFAcrossClients proves the CSRF token is stable per session:
// repeated reads from two clients sharing one session return the same token
// and both can still write and log out.
func TestStableCSRFAcrossClients(t *testing.T) {
	env := setup(t)
	ctx := context.Background()
	user, token, csrf := env.createUserSession("tabs")
	tabA := env.clientFor(token, csrf)
	tabB := env.clientFor(token, csrf)

	rec := createSite(ctx, t, env, "tabs", "Tabs", "", "zh-CN", user.ID)
	settingsPath := "/api/v1/sites/" + rec.Site.ID.String() + "/settings"

	for i := 0; i < 2; i++ {
		for _, tab := range []*tclient{tabA, tabB} {
			status, _, body := tab.do("GET", "/api/v1/session", nil, nil)
			if status != 200 {
				t.Fatalf("session read: %d %s", status, body)
			}
			var session sessionResp
			decodeJSONBody(t, body, &session)
			if !session.Authenticated || session.CSRFToken != csrf {
				t.Fatalf("csrf token changed between reads: got %q, want %q", session.CSRFToken, csrf)
			}
		}
	}

	_, hdr, _ := tabA.do("GET", settingsPath, nil, nil)
	if status, _, body := tabA.do("PATCH", settingsPath, map[string]string{"name": "FromA"}, map[string]string{"If-Match": hdr.Get("ETag")}); status != 200 {
		t.Fatalf("tab A write: %d %s", status, body)
	}
	_, hdr, _ = tabB.do("GET", settingsPath, nil, nil)
	status, _, body := tabB.do("PATCH", settingsPath, map[string]string{"name": "FromB"}, map[string]string{"If-Match": hdr.Get("ETag")})
	if status != 200 {
		t.Fatalf("tab B write with same csrf: %d %s", status, body)
	}
	if status, _, body := tabB.do("POST", "/api/v1/auth/logout", map[string]string{}, nil); status != 204 {
		t.Fatalf("logout: %d %s", status, body)
	}
}

// TestVerifyBodyLimits proves the WebAuthn verify paths enforce the request
// size limit and reject trailing content before anything is consumed.
func TestVerifyBodyLimits(t *testing.T) {
	env := setup(t)
	c := env.newClient()
	userHandle, _, _ := bootstrapAndRegister(env, c, "limits")

	status, _, body := c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	if status != 200 {
		t.Fatalf("login options: %d %s", status, body)
	}
	var options requestOptionsResp
	decodeJSONBody(t, body, &options)

	auth := testauth.New()
	assertion := auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)

	// The same assertion with trailing garbage is rejected.
	raw, err := json.Marshal(assertion)
	if err != nil {
		t.Fatalf("marshal assertion: %v", err)
	}
	raw = append(raw, []byte(" {}")...)
	status, _, body = c.doRaw("POST", "/api/v1/auth/login/verify", string(raw), nil)
	if status != 400 {
		t.Fatalf("trailing garbage: %d %s, want 400", status, body)
	}
	var p problemResp
	decodeJSONBody(t, body, &p)
	if p.Code != "invalid_json" {
		t.Fatalf("trailing garbage code %q, want invalid_json", p.Code)
	}

	// An oversized body is rejected with 413 before the challenge is touched.
	status, _, body = c.do("POST", "/api/v1/auth/login/options", map[string]string{}, nil)
	if status != 200 {
		t.Fatalf("login options again: %d %s", status, body)
	}
	decodeJSONBody(t, body, &options)
	assertion = auth.Assert("localhost", env.base, options.PublicKey.Challenge, userHandle)
	big := `{"pad":"` + strings.Repeat("a", 2<<20) + `"}`
	status, _, body = c.doRaw("POST", "/api/v1/auth/login/verify", big, nil)
	if status != 413 {
		t.Fatalf("oversized body: %d %s, want 413", status, body)
	}
}
