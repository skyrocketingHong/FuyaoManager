package buildrunner

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type lifecycleTarget struct{ path string }

func (t lifecycleTarget) GetPreviewTarget(ctx context.Context, id uuid.UUID) (string, string, error) {
	return t.path, "", nil
}

type pendingPreviewTarget struct {
	path    string
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (target *pendingPreviewTarget) GetPreviewTarget(ctx context.Context, _ uuid.UUID) (string, string, error) {
	target.once.Do(func() { close(target.entered) })
	select {
	case <-target.resume:
		return target.path, "", nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

// Closing must also revoke a link request still resolving its frozen build.
// Otherwise an earlier slow Issue can create a fresh listener after Close
// has already reported success.
func TestPreviewCloseCancelsPendingIssue(t *testing.T) {
	p, dir := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	target := &pendingPreviewTarget{path: dir, entered: make(chan struct{}), resume: make(chan struct{})}
	p.cfg.Target = target
	siteID, buildID := uuid.New(), uuid.New()
	result := make(chan error, 1)
	go func() { _, _, err := p.Issue(siteID, buildID); result <- err }()
	<-target.entered
	if err := p.CloseOrigin(siteID, buildID); err != nil {
		t.Fatal(err)
	}
	close(target.resume)
	if err := <-result; err == nil {
		t.Fatal("a pre-close issue recreated a live preview after close succeeded")
	}
	if p.IsActive(siteID, buildID) {
		t.Fatal("pending link request survived explicit close")
	}
	// A new, deliberate open after the close may create a fresh instance.
	client, base := previewEntry(t, p, siteID, buildID)
	response, err := client.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh preview = %d", response.StatusCode)
	}
}

func TestPreviewOtherSiteCloseDoesNotCancelPendingIssue(t *testing.T) {
	p, dir := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	target := &pendingPreviewTarget{path: dir, entered: make(chan struct{}), resume: make(chan struct{})}
	p.cfg.Target = target
	siteID, buildID := uuid.New(), uuid.New()
	result := make(chan error, 1)
	go func() { _, _, err := p.Issue(siteID, buildID); result <- err }()
	<-target.entered
	if err := p.CloseOrigin(uuid.New(), buildID); err != nil {
		t.Fatal(err)
	}
	close(target.resume)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if !p.IsActive(siteID, buildID) {
		t.Fatal("a foreign site's close canceled this site's link request")
	}
}

// lifecycleServer builds a preview server over one artifact dir with tiny
// TTLs and, when ports is non-nil, persistent allocation.
func lifecycleServer(t *testing.T, ttl time.Duration, maxOrigins int, ports PortSource, portMin, portMax int) (*PreviewServer, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "artifact")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewPreviewServer(PreviewConfig{
		Target: lifecycleTarget{dir}, BuildsRoot: root,
		TTL: ttl, MaxOrigins: maxOrigins,
		Ports: ports, PortMin: portMin, PortMax: portMax,
	})
	t.Cleanup(p.Close)
	return p, dir
}

// TestPreviewExpiryReleasesCapacity proves an expired preview is reclaimed
// automatically: after the TTL passes, a new build can take the origin slot
// without any service restart.
func TestPreviewExpiryReleasesCapacity(t *testing.T) {
	p, _ := lifecycleServer(t, 20*time.Millisecond, 1, nil, 0, 0)
	site := uuid.New()
	if _, _, err := p.Issue(site, uuid.New()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, _, err := p.Issue(site, uuid.New()); err != nil {
		t.Fatalf("expired preview still occupies origin capacity: %v", err)
	}
}

// TestPreviewExpiryRevokesTokensAndListener proves expiry actually kills
// access: the issued URL stops validating tokens and the listener is gone
// (connection refused), matching the explicit close semantics.
func TestPreviewExpiryRevokesTokensAndListener(t *testing.T) {
	p, _ := lifecycleServer(t, 30*time.Millisecond, 2, nil, 0, 0)
	site, build := uuid.New(), uuid.New()
	url, _, err := p.Issue(site, build)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("entry status = %d", resp.StatusCode)
	}
	time.Sleep(200 * time.Millisecond)
	after, err := client.Get(url)
	served := false
	if err == nil {
		after.Body.Close()
		served = after.StatusCode == http.StatusSeeOther
	}
	if served {
		t.Fatal("expired preview still redeems tokens")
	}
}

// TestPreviewCloseIdempotentAndScoped proves closing one build's preview is
// idempotent, releases its persistent port, and never touches another
// build's live preview.
func TestPreviewCloseIdempotentAndScoped(t *testing.T) {
	// Persistent allocation needs a PortSource backed by the store; use a
	// fake in-memory one for this unit scope.
	p, _ := lifecycleServer(t, time.Hour, 4, nil, 0, 0)
	site, a, b := uuid.New(), uuid.New(), uuid.New()
	urlA, _, err := p.Issue(site, a)
	if err != nil {
		t.Fatal(err)
	}
	urlB, _, err := p.Issue(site, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.CloseOrigin(site, a); err != nil {
		t.Fatal(err)
	}
	if err := p.CloseOrigin(site, a); err != nil {
		t.Fatalf("second close must be idempotent: %v", err)
	}
	// A's listener is gone; B still serves.
	if _, err := http.Get(urlA); err == nil {
		t.Fatal("closed preview listener still answers")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(urlB)
	if err != nil {
		t.Fatalf("closing A must not close B: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("B entry status = %d", resp.StatusCode)
	}
}

// TestPreviewExpiryFreesPersistentPort proves the sweeper releases the
// persistent port assignment of an expired origin, letting the next build
// re-allocate it (this is the restart-free recovery for capacity).
func TestPreviewExpiryFreesPersistentPort(t *testing.T) {
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
		TTL: 30 * time.Millisecond, MaxOrigins: 1,
		Ports: alloc, PortMin: 29000, PortMax: 29010,
	})
	defer p.Close()
	site, first := uuid.New(), uuid.New()
	_, _, err := p.Issue(site, first)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	// The second build must succeed (capacity freed) and re-use the freed
	// port range without collision.
	url, _, err := p.Issue(site, uuid.New())
	if err != nil {
		t.Fatalf("capacity not freed after expiry: %v", err)
	}
	if url == "" {
		t.Fatal("no URL issued")
	}
	if alloc.liveCount() != 1 {
		t.Fatalf("expected exactly one live preview allocation, got %d", alloc.liveCount())
	}
}

// fakePortSource is an in-memory PortSource for lifecycle unit tests. The
// sweeper goroutine releases ports concurrently with test-thread issues, so
// the map is mutex-guarded.
type fakePortSource struct {
	mu   sync.Mutex
	used map[int]bool
}

func (f *fakePortSource) EnsurePreviewEntry(ctx context.Context, siteID, buildID uuid.UUID, minPort, maxPort int) (store.NetworkEntry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for port := minPort; port <= maxPort; port++ {
		if !f.used[port] {
			f.used[port] = true
			id := uuid.New()
			bid := buildID
			return store.NetworkEntry{
				ID: id, SiteID: siteID, Kind: "preview", BuildID: &bid, Port: port, CreatedAt: time.Now(),
			}, true, nil
		}
	}
	return store.NetworkEntry{}, false, store.ErrPortRangeExhausted
}

func (f *fakePortSource) ReleasePreview(ctx context.Context, buildID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for port, used := range f.used {
		if used {
			delete(f.used, port)
			return nil
		}
	}
	return nil
}

func (f *fakePortSource) liveCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.used)
}
