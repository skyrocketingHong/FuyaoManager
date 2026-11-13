package buildrunner

import (
	"errors"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// previewEntry returns a no-redirect client plus the origin base of an
// issued entry URL, redeeming the entry to store the authorization cookie.
func previewEntry(t *testing.T, p *PreviewServer, siteID, buildID uuid.UUID) (client *http.Client, base string) {
	t.Helper()
	url, _, err := p.Issue(siteID, buildID)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(url, "/__preview__/")
	if at < 0 {
		t.Fatalf("entry URL lacks the token path: %s", url)
	}
	base = url[:at]
	jar, _ := cookiejar.New(nil)
	client = &http.Client{
		Jar:     jar,
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("preview entry: err=%v status=%v", err, resp)
	}
	resp.Body.Close()
	return client, base
}

// TestPreviewIsActiveTracksLifecycle proves previewActive reflects live
// in-process state only: true for the issuing site while a token is valid,
// false for any other site or build, and false after an explicit close or
// token expiry — never a stale port assignment.
func TestPreviewIsActiveTracksLifecycle(t *testing.T) {
	p, _ := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	siteA, siteB, build := uuid.New(), uuid.New(), uuid.New()
	if _, _, err := p.Issue(siteA, build); err != nil {
		t.Fatal(err)
	}
	if !p.IsActive(siteA, build) {
		t.Fatal("live preview must report active for its own site")
	}
	if p.IsActive(siteB, build) {
		t.Fatal("another site must not see the preview as active")
	}
	if p.IsActive(siteA, uuid.New()) {
		t.Fatal("another build must not report active")
	}
	if err := p.CloseOrigin(siteA, build); err != nil {
		t.Fatal(err)
	}
	if p.IsActive(siteA, build) {
		t.Fatal("closed preview must not report active")
	}
}

// TestPreviewIsActiveAfterTokenExpiry proves an origin whose tokens have all
// expired stops reporting active even before the sweeper reclaims it.
func TestPreviewIsActiveAfterTokenExpiry(t *testing.T) {
	p, _ := lifecycleServer(t, 20*time.Millisecond, 4, nil, 0, 0)
	site, build := uuid.New(), uuid.New()
	if _, _, err := p.Issue(site, build); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if p.IsActive(site, build) {
		t.Fatal("expired preview must not report active")
	}
}

// TestPreviewStatusEndpointLifecycle proves the injected page script's probe
// behaves per its contract: 204 while the origin is live, authorized, and
// carrying the injecting instance id; 410 for missing or invalid
// credentials, a foreign instance id, a close, and non-GET methods. The
// probe — not any service worker — is the liveness authority.
func TestPreviewStatusEndpointLifecycle(t *testing.T) {
	p, _ := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	instance := p.origins[build].instanceID
	probe := base + previewStatusPath + "?" + previewInstanceParam + "=" + instance

	resp, err := client.Get(probe)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("live status: err=%v status=%v", err, resp)
	}
	resp.Body.Close()

	foreign, err := client.Get(base + previewStatusPath + "?" + previewInstanceParam + "=other-instance")
	if err != nil || foreign.StatusCode != http.StatusGone {
		t.Fatalf("foreign instance probe must be gone: err=%v status=%v", err, foreign)
	}
	foreign.Body.Close()

	bare := &http.Client{Timeout: 2 * time.Second}
	anon, err := bare.Get(probe)
	if err != nil || anon.StatusCode != http.StatusGone {
		t.Fatalf("anonymous status must be gone: err=%v status=%v", err, anon)
	}
	anon.Body.Close()

	post, err := client.Post(probe, "text/plain", strings.NewReader(""))
	if err != nil || post.StatusCode != http.StatusGone {
		t.Fatalf("non-GET status must be gone: err=%v status=%v", err, post)
	}
	post.Body.Close()

	if err := p.CloseOrigin(site, build); err != nil {
		t.Fatal(err)
	}
	// After the explicit close the listener itself is gone: the probe fails
	// at the connection level, exactly how a closed preview stops answering.
	dead, err := client.Get(probe)
	if err == nil {
		dead.Body.Close()
		t.Fatalf("closed preview listener must be gone, got status %d", dead.StatusCode)
	}
}

// TestPreviewProbeRequiresMatchingInstance proves the probe is bound to the
// origin instance that injected it: after the same build closes and reopens
// on the same platform port, a stale page cannot ride the fresh
// incarnation's cookie (same cookie name, overwritten by the new entry),
// while the fresh page keeps probing with its own id.
func TestPreviewProbeRequiresMatchingInstance(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "artifact")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	alloc := &fakePortSource{used: map[int]bool{}}
	p := NewPreviewServer(PreviewConfig{
		Target: lifecycleTarget{dir}, BuildsRoot: root,
		TTL: time.Hour, MaxOrigins: 4,
		Ports: alloc, PortMin: 29000, PortMax: 29010,
	})
	defer p.Close()

	site, build := uuid.New(), uuid.New()
	urlOld, _, err := p.Issue(site, build)
	if err != nil {
		t.Fatal(err)
	}
	at := strings.Index(urlOld, "/__preview__/")
	base := urlOld[:at]
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	entry, err := client.Get(urlOld)
	if err != nil || entry.StatusCode != http.StatusSeeOther {
		t.Fatalf("first entry: err=%v status=%v", err, entry)
	}
	entry.Body.Close()
	oldInstance := p.origins[build].instanceID

	if err := p.CloseOrigin(site, build); err != nil {
		t.Fatal(err)
	}
	urlNew, _, err := p.Issue(site, build)
	if err != nil {
		t.Fatal(err)
	}
	if urlNew[:strings.Index(urlNew, "/__preview__/")] != base {
		t.Fatalf("reopen must reuse the platform port: %s vs %s", urlNew, urlOld)
	}
	newInstance := p.origins[build].instanceID
	if newInstance == oldInstance {
		t.Fatal("reopen must mint a new instance id")
	}

	// The same browser redeems the fresh entry: the shared cookie name is
	// now overwritten with the new token — exactly what a stale page sends.
	again, err := client.Get(urlNew)
	if err != nil || again.StatusCode != http.StatusSeeOther {
		t.Fatalf("reopened entry: err=%v status=%v", err, again)
	}
	again.Body.Close()

	// The stale page's probe carries its old instance id: gone, even though
	// the request now carries the new cookie.
	stale, err := client.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + oldInstance)
	if err != nil || stale.StatusCode != http.StatusGone {
		t.Fatalf("stale probe with fresh cookie must be gone: err=%v status=%v", err, stale)
	}
	stale.Body.Close()

	// The fresh page's probe matches the current instance: alive.
	fresh, err := client.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + newInstance)
	if err != nil || fresh.StatusCode != http.StatusNoContent {
		t.Fatalf("fresh probe must be alive: err=%v status=%v", err, fresh)
	}
	fresh.Body.Close()

	// Cookie-less requests never pass, even with a matching instance id.
	bare := &http.Client{Timeout: 2 * time.Second}
	anon, err := bare.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + newInstance)
	if err != nil || anon.StatusCode != http.StatusGone {
		t.Fatalf("cookie-less probe must be gone: err=%v status=%v", err, anon)
	}
	anon.Body.Close()
}

// TestPreviewProbeDiesAcrossPortReuse proves a page from build A cannot
// survive build B reopening the same platform port: the probe's instance id
// belongs to A's origin, B's origin rejects it, and B's own page probes
// alive. A's cookie name differs from B's, so even a matching id fails
// without B's cookie.
func TestPreviewProbeDiesAcrossPortReuse(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "artifact")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	alloc := &fakePortSource{used: map[int]bool{}}
	p := NewPreviewServer(PreviewConfig{
		Target: lifecycleTarget{dir}, BuildsRoot: root,
		TTL: time.Hour, MaxOrigins: 4,
		Ports: alloc, PortMin: 29000, PortMax: 29010,
	})
	defer p.Close()

	siteA, siteB, buildA, buildB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	clientA, base := previewEntry(t, p, siteA, buildA)
	instanceA := p.origins[buildA].instanceID
	if err := p.CloseOrigin(siteA, buildA); err != nil {
		t.Fatal(err)
	}
	clientB, baseB := previewEntry(t, p, siteB, buildB)
	instanceB := p.origins[buildB].instanceID
	if baseB != base {
		t.Fatalf("build B must reuse the freed port: %s vs %s", baseB, base)
	}

	// A's stale page probing B's origin: gone for A's id and for B's id
	// (A's cookie name does not authorize on B's origin).
	staleA, err := clientA.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + instanceA)
	if err != nil || staleA.StatusCode != http.StatusGone {
		t.Fatalf("stale cross-build probe must be gone: err=%v status=%v", err, staleA)
	}
	staleA.Body.Close()
	mixed, err := clientA.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + instanceB)
	if err != nil || mixed.StatusCode != http.StatusGone {
		t.Fatalf("cross-build probe with foreign id must be gone: err=%v status=%v", err, mixed)
	}
	mixed.Body.Close()

	// B's own page probes alive on the reused port.
	live, err := clientB.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + instanceB)
	if err != nil || live.StatusCode != http.StatusNoContent {
		t.Fatalf("fresh cross-build probe must be alive: err=%v status=%v", err, live)
	}
	live.Body.Close()
}

// TestPreviewStatusAfterTokenExpiry proves a probe with expired credentials
// gets 410 while the origin still answers (before the sweeper reclaims it):
// the injected page script ends the document on this response.
func TestPreviewStatusAfterTokenExpiry(t *testing.T) {
	p, _ := lifecycleServer(t, 20*time.Millisecond, 4, nil, 0, 0)
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	// Tokens expire after 20ms; the sweeper fires at its 250ms floor, so a
	// probe now meets a still-listening origin with dead credentials.
	time.Sleep(80 * time.Millisecond)
	resp, err := client.Get(base + previewStatusPath)
	if err != nil || resp.StatusCode != http.StatusGone {
		t.Fatalf("expired-token status must be gone: err=%v status=%v", err, resp)
	}
	resp.Body.Close()
}

// TestPreviewCloseCrossSiteRejected proves a close attempt from another site
// is rejected without touching the origin: the preview stays active and
// reachable, and only the owning site can tear it down.
func TestPreviewCloseCrossSiteRejected(t *testing.T) {
	p, _ := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	siteA, siteB, build := uuid.New(), uuid.New(), uuid.New()
	client, base := previewEntry(t, p, siteA, build)
	probe := base + previewStatusPath + "?" + previewInstanceParam + "=" + p.origins[build].instanceID

	if err := p.CloseOrigin(siteB, build); err == nil {
		t.Fatal("cross-site close must be rejected")
	}
	if !p.IsActive(siteA, build) {
		t.Fatal("rejected cross-site close must leave the preview active")
	}
	resp, err := client.Get(probe)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preview must survive the rejected close: err=%v status=%v", err, resp)
	}
	resp.Body.Close()

	if err := p.CloseOrigin(siteA, build); err != nil {
		t.Fatalf("owning site close: %v", err)
	}
	if p.IsActive(siteA, build) {
		t.Fatal("owning site close must deactivate the preview")
	}
}

// TestPreviewConcurrentIssueCloseReopen is the serialization regression:
// concurrent issues, closes, and sweeper reclaims over one build must
// complete without deadlock or data race and always leave the build in a
// consistent state that can reopen with a fresh working listener.
func TestPreviewConcurrentIssueCloseReopen(t *testing.T) {
	p, _ := lifecycleServer(t, 40*time.Millisecond, 8, nil, 0, 0)
	site, build := uuid.New(), uuid.New()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(closer bool) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if _, _, err := p.Issue(site, build); err != nil && !errors.Is(err, ErrPreviewClosed) {
					t.Errorf("concurrent issue: %v", err)
					return
				}
				if closer {
					if err := p.CloseOrigin(site, build); err != nil {
						t.Errorf("concurrent close: %v", err)
						return
					}
				}
			}
		}(i%2 == 0)
	}
	wg.Wait()

	// Whatever interleaving happened, a final close is idempotent and the
	// build reopens cleanly with a fresh listener.
	if err := p.CloseOrigin(site, build); err != nil {
		t.Fatalf("final close: %v", err)
	}
	client, base := previewEntry(t, p, site, build)
	resp, err := client.Get(base + previewStatusPath + "?" + previewInstanceParam + "=" + p.origins[build].instanceID)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reopened preview listener: err=%v status=%v", err, resp)
	}
	resp.Body.Close()
}
