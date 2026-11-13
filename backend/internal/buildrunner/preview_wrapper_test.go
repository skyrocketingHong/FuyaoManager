package buildrunner

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func wrappedPreview(t *testing.T) (*PreviewServer, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	p, _ := documentServer(t, map[string]string{"index.html": "<html><body>root</body></html>", "blog.html": "<html><body>blog</body></html>"})
	p.cfg.ManagerOrigin = "https://manager.example.test"
	p.cfg.ExternalBase = "https://preview.manager.example.test"
	site, build := uuid.New(), uuid.New()
	link, _, err := p.IssueWrapper(site, build, "formal.example.test")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(link)
	if parsed.Host != "preview.manager.example.test" || parsed.Path != "/formal.example.test/" {
		t.Fatal(link)
	}
	return p, site, build, parsed.Query().Get("lease")
}

func TestPreviewWrapperReloadMintsFreshEntryWithoutReopening(t *testing.T) {
	p, site, build, lease := wrappedPreview(t)
	first, err := p.FrameWrapper(lease, "formal.example.test", "/blog.html?view=1#entry")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.FrameWrapper(lease, "formal.example.test", "/blog.html?view=2")
	if err != nil {
		t.Fatal(err)
	}
	if first.InstanceID != second.InstanceID || first.EntryURL == second.EntryURL {
		t.Fatal("reload reused a token or replaced its live origin")
	}
	entry, _ := url.Parse(first.EntryURL)
	if entry.Query().Get("next") != "/blog.html?view=1#entry" {
		t.Fatal(entry)
	}
	local := "http://127.0.0.1:" + entry.Port() + entry.RequestURI()
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(local)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 303 || response.Header.Get("Location") != "/blog.html?view=1#entry" {
		t.Fatalf("entry = %d %s", response.StatusCode, response.Header.Get("Location"))
	}
	if !response.Cookies()[0].Secure {
		t.Fatal("TLS preview cookie is not secure")
	}
	replayed, err := client.Get(local)
	if err != nil {
		t.Fatal(err)
	}
	replayed.Body.Close()
	if replayed.StatusCode != 404 {
		t.Fatal("entry token was reused")
	}
	if err := p.CloseOrigin(site, build); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FrameWrapper(lease, "formal.example.test", "/"); !errors.Is(err, ErrPreviewClosed) {
		t.Fatalf("closed lease revived: %v", err)
	}
	if _, _, err := p.IssueWrapper(site, build, "formal.example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.LookupWrapper(lease, "formal.example.test"); !errors.Is(err, ErrPreviewClosed) {
		t.Fatal("old lease rode a new origin instance")
	}
}

func TestPreviewWrapperRejectsUnsafeOriginsAndPaths(t *testing.T) {
	p, site, build, lease := wrappedPreview(t)
	for _, route := range []string{"https://manager.example.test/api", "//manager.example.test/api", "/../api", "/%2e%2e/api", `/\manager.example.test`, "/bad\nheader"} {
		if _, err := p.FrameWrapper(lease, "formal.example.test", route); err == nil {
			t.Fatalf("unsafe path accepted: %q", route)
		}
	}
	if _, err := p.LookupWrapper(lease, "different.example.test"); err == nil {
		t.Fatal("lease crossed formal hosts")
	}
	for _, origin := range []string{"https://manager.example.test", "http://preview.manager.example.test", "https://preview.manager.example.test:20000", "https://preview.manager.example.test/path"} {
		p.cfg.ExternalBase = origin
		if _, _, err := p.IssueWrapper(site, build, "formal.example.test"); !errors.Is(err, ErrPreviewUnsafeOrigin) {
			t.Fatalf("unsafe origin %s: %v", origin, err)
		}
	}
}

func TestPreviewWrapperLifecyclePublishesOnlyToFixedParent(t *testing.T) {
	script := previewLifecycleScript("instance-1", "https://preview.manager.example.test")
	if !strings.Contains(script, `const parentOrigin="https://preview.manager.example.test"`) || !strings.Contains(script, "parent.postMessage") || !strings.Contains(script, "Reflect.apply(original,this,args)") {
		t.Fatal("route synchronization is missing or not pinned")
	}
}

func TestPreviewExternalPortOffsetKeepsBackendAllocation(t *testing.T) {
	ports := &fakePortSource{used: map[int]bool{}}
	p, _ := lifecycleServer(t, time.Hour, 1, ports, 29000, 29000)
	p.cfg.ExternalBase = "https://preview.manager.example.test"
	p.cfg.ManagerOrigin = "https://manager.example.test"
	p.cfg.ExternalPortOffset = 10000
	site, build := uuid.New(), uuid.New()
	link, _, err := p.IssueWrapper(site, build, "formal.example.test")
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(link)
	if parsed.Port() != "" {
		t.Fatal("wrapper moved away from HTTPS 443")
	}
	frame, err := p.FrameWrapper(parsed.Query().Get("lease"), "formal.example.test", "/blog/")
	if err != nil {
		t.Fatal(err)
	}
	if frame.Origin != "https://preview.manager.example.test:39000" {
		t.Fatal(frame.Origin)
	}
	entry, _ := url.Parse(frame.EntryURL)
	if entry.Port() != "39000" {
		t.Fatal(frame.EntryURL)
	}
	if p.origins[build].port != 29000 || p.origins[build].ln.Addr().String() != "127.0.0.1:29000" {
		t.Fatal("edge offset changed the backend listener")
	}
	raw, _, err := p.Issue(site, build)
	if err != nil {
		t.Fatal(err)
	}
	rawURL, _ := url.Parse(raw)
	if rawURL.Port() != "39000" {
		t.Fatal("raw preview link omitted the edge offset")
	}
	for _, offset := range []int{-1, 65535} {
		p.cfg.ExternalPortOffset = offset
		if _, err := p.externalOrigin(29000); !errors.Is(err, ErrPreviewUnsafeOrigin) {
			t.Fatalf("invalid offset %d: %v", offset, err)
		}
	}
}
