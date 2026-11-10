package httpapi

import (
	"testing"
)

// TestSecurityHeaders proves every class of management API response —
// success, auth failure, origin rejection, and not-found — is stamped
// non-cacheable with MIME sniffing disabled.
func TestSecurityHeaders(t *testing.T) {
	env := setup(t)

	// Success (anonymous session probe is a 200).
	status, hdr, _ := env.newClient().do("GET", "/api/v1/session", nil, nil)
	if status != 200 {
		t.Fatalf("session probe: %d", status)
	}
	assertNoStore(t, hdr)

	// Authenticated success.
	_, token, csrf := env.createUserSession("hdr-owner")
	status, hdr, _ = env.clientFor(token, csrf).do("GET", "/api/v1/sites", nil, nil)
	if status != 200 {
		t.Fatalf("sites: %d", status)
	}
	assertNoStore(t, hdr)

	// Unauthenticated rejection (401 problem).
	status, hdr, _ = env.newClient().do("GET", "/api/v1/sites", nil, nil)
	if status != 401 {
		t.Fatalf("anonymous sites: %d", status)
	}
	assertNoStore(t, hdr)

	// Origin rejection on an unsafe method (403 problem).
	c := env.newClient()
	status, hdr, _ = c.do("POST", "/api/v1/auth/login/options", map[string]string{}, map[string]string{"Origin": "https://evil.example"})
	if status != 403 {
		t.Fatalf("evil origin: %d", status)
	}
	assertNoStore(t, hdr)

	// Not-found for a member-scoped resource (404 problem).
	status, hdr, _ = env.clientFor(token, csrf).do("GET", "/api/v1/sites/00000000-0000-0000-0000-000000000000", nil, nil)
	if status != 404 {
		t.Fatalf("missing site: %d", status)
	}
	assertNoStore(t, hdr)
}

func assertNoStore(t *testing.T, hdr map[string][]string) {
	t.Helper()
	if got := headerValue(hdr, "Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := headerValue(hdr, "X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

// headerValue reads from the canonicalized http.Header map shape returned by
// the test client.
func headerValue(hdr map[string][]string, name string) string {
	for key, values := range hdr {
		if key == name && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
