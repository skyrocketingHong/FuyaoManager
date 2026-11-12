package buildrunner

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// documentTarget is a PreviewTarget over one fixed artifact dir (the
// lifecycle test double without TTL knobs).
type documentTarget struct{ path string }

func (t documentTarget) GetPreviewTarget(ctx context.Context, id uuid.UUID) (string, string, error) {
	return t.path, "", nil
}

// writeArtifact creates the artifact dir with the given files under a fresh
// builds root and wires a preview server over it.
func documentServer(t *testing.T, files map[string]string) (*PreviewServer, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "artifact")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPreviewServer(PreviewConfig{
		Target: documentTarget{dir}, BuildsRoot: root,
		TTL: time.Hour, MaxOrigins: 4,
	})
	t.Cleanup(p.Close)
	return p, dir
}

func documentBody(t *testing.T, client *http.Client, url string) (string, *http.Response) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: err=%v status=%v", url, err, resp)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body), resp
}

// TestPreviewHTMLInjectsLifecycleScript proves served HTML documents carry
// the lifecycle script immediately before their closing </body> tag, bound
// to this origin's instance id, while the frozen artifact file stays
// byte-identical.
func TestPreviewHTMLInjectsLifecycleScript(t *testing.T) {
	page := "<!doctype html><html><head><title>doc</title></head><body><p>hello</p></body></html>"
	p, dir := documentServer(t, map[string]string{"index.html": page})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	instance := p.origins[build].instanceID

	body, _ := documentBody(t, client, base+"/")
	marker := strings.Index(body, `data-fuyao-preview-lifecycle`)
	if marker < 0 {
		t.Fatal("served HTML lacks the lifecycle script")
	}
	closeAt := strings.LastIndex(body, "</body>")
	if marker > closeAt {
		t.Fatal("lifecycle script must sit before </body>")
	}
	if !strings.Contains(body, previewStatusPath+"?'") || !strings.Contains(body, "{instance:inst}") {
		t.Fatal("lifecycle script must poll the status endpoint with the instance id")
	}
	if !strings.Contains(body, "inst='"+instance+"'") {
		t.Fatal("lifecycle script must carry this origin's instance id")
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil || string(frozen) != page {
		t.Fatalf("frozen artifact was modified: err=%v", err)
	}
}

// TestPreviewHTMLWithoutBodyStillStops proves documents without a closing
// </body> element get the stop script before </html> — every served HTML
// document has a working stop mechanism — while the frozen artifact file
// stays byte-identical.
func TestPreviewHTMLWithoutBodyStillStops(t *testing.T) {
	page := "<!doctype html><html><head></head><p>fragment without a body close</p></html>"
	p, dir := documentServer(t, map[string]string{"odd.html": page})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	instance := p.origins[build].instanceID

	body, _ := documentBody(t, client, base+"/odd.html")
	marker := strings.Index(body, `data-fuyao-preview-lifecycle`)
	if marker < 0 {
		t.Fatal("document without </body> must still carry the stop script")
	}
	htmlAt := strings.LastIndex(body, "</html>")
	if marker > htmlAt {
		t.Fatal("script must sit before </html>")
	}
	if !strings.Contains(body, "inst='"+instance+"'") {
		t.Fatal("script must carry the instance id")
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "odd.html"))
	if err != nil || string(frozen) != page {
		t.Fatalf("frozen artifact was modified: err=%v", err)
	}
}

// TestPreviewHTMLFragmentStillStops proves documents with neither </body>
// nor </html> get the stop script appended at the very end, where HTML
// error recovery still executes it.
func TestPreviewHTMLFragmentStillStops(t *testing.T) {
	page := "<p>loose fragment, no closing tags at all</p>"
	p, dir := documentServer(t, map[string]string{"frag.html": page})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	instance := p.origins[build].instanceID

	body, _ := documentBody(t, client, base+"/frag.html")
	if !strings.HasSuffix(body, "</script>") || !strings.Contains(body, "inst='"+instance+"'") {
		t.Fatal("fragment must end with the stop script carrying the instance id")
	}
	if !strings.HasPrefix(body, page) {
		t.Fatal("fragment content must precede the script unchanged")
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "frag.html"))
	if err != nil || string(frozen) != page {
		t.Fatalf("frozen artifact was modified: err=%v", err)
	}
}

// TestPreviewOversizedHTMLStillStops proves documents beyond the buffering
// cap are transformed as a stream: the stop script heads the response and
// the frozen bytes follow unchanged.
func TestPreviewOversizedHTMLStillStops(t *testing.T) {
	page := "<!doctype html><html><body>" + strings.Repeat("x", 9<<20) + "</body></html>"
	p, dir := documentServer(t, map[string]string{"big.html": page})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)
	instance := p.origins[build].instanceID

	body, _ := documentBody(t, client, base+"/big.html")
	if !strings.HasPrefix(body, "<script data-fuyao-preview-lifecycle>") {
		t.Fatal("oversized document must start with the stop script")
	}
	if !strings.Contains(body, "inst='"+instance+"'") {
		t.Fatal("streamed script must carry the instance id")
	}
	if !strings.Contains(body, "</body>") {
		t.Fatal("oversized document must keep its bytes after the script")
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "big.html"))
	if err != nil || string(frozen) != page {
		t.Fatalf("frozen artifact was modified: err=%v", err)
	}
}

// TestPreviewNonHTMLAssetsStayUntouched proves the transform applies to HTML
// documents only; other assets keep their bytes and content types.
func TestPreviewNonHTMLAssetsStayUntouched(t *testing.T) {
	css := "body{color:#123}"
	p, dir := documentServer(t, map[string]string{"index.html": "<html><body></body></html>", "style.css": css})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)

	body, _ := documentBody(t, client, base+"/style.css")
	if strings.Contains(body, `data-fuyao-preview-lifecycle`) || body != css {
		t.Fatal("non-HTML asset must keep its exact bytes")
	}
	frozen, err := os.ReadFile(filepath.Join(dir, "style.css"))
	if err != nil || string(frozen) != css {
		t.Fatalf("frozen asset was modified: err=%v", err)
	}
}

// TestPreviewServiceWorkerRegistrationBlocked proves the origin rejects the
// browser's service-worker registration fetch wherever it points — the
// registration is the only request carrying "Service-Worker: script", so
// rejection is path-independent, with no file-name guessing. Ordinary
// script resources at the very same paths keep serving, so the theme's
// normal assets stay untouched. The status probe stays the only liveness
// authority.
func TestPreviewServiceWorkerRegistrationBlocked(t *testing.T) {
	p, dir := documentServer(t, map[string]string{
		"index.html":        "<html><body></body></html>",
		"service-worker.js": "self.addEventListener('fetch', () => {});",
		"sw.js":             "self.addEventListener('fetch', () => {});",
	})
	site, build := uuid.New(), uuid.New()
	client, base := previewEntry(t, p, site, build)

	workerPaths := []string{"/service-worker.js", "/sw.js", "/nested/deep/worker.js"}
	for _, workerPath := range workerPaths {
		req, err := http.NewRequest(http.MethodGet, base+workerPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Service-Worker", "script")
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusNotFound {
			t.Fatalf("registration fetch %s must 404: err=%v status=%v", workerPath, err, resp)
		}
		resp.Body.Close()
	}

	for _, workerPath := range []string{"/service-worker.js", "/sw.js"} {
		resp, err := client.Get(base + workerPath)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("ordinary script %s must still serve: err=%v status=%v", workerPath, err, resp)
		}
		body := new(strings.Builder)
		if _, err := io.Copy(body, resp.Body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if body.String() != "self.addEventListener('fetch', () => {});" {
			t.Fatalf("ordinary script %s must keep its exact bytes", workerPath)
		}
	}

	frozen, err := os.ReadFile(filepath.Join(dir, "sw.js"))
	if err != nil || string(frozen) != "self.addEventListener('fetch', () => {});" {
		t.Fatalf("frozen asset was modified: err=%v", err)
	}
}

// TestPreviewLifecycleScriptResumesAfterBFCache proves the injected script
// restarts its polling on every pageshow: the pagehide pause is repeatable
// (no once:true listener), so a back/forward cache return resumes probing
// and a closed preview still ends the revived document. The instance id is
// embedded in the probe URL.
func TestPreviewLifecycleScriptResumesAfterBFCache(t *testing.T) {
	script := previewLifecycleScript("inst-check-1")
	if !strings.Contains(script, "addEventListener('pagehide',stop);") {
		t.Fatal("pagehide pause must be a repeatable listener, not once:true")
	}
	if !strings.Contains(script, "addEventListener('pageshow',()=>{if(!ended){start();check();}});") {
		t.Fatal("pageshow must restart the polling interval and probe immediately")
	}
	if !strings.Contains(script, "start();check();") {
		t.Fatal("polling must be running from the first load, not only after a hide/show cycle")
	}
	if !strings.Contains(script, "const inst='inst-check-1';") {
		t.Fatal("the script must embed its origin's instance id")
	}
	if strings.Contains(script, "{once:true}") {
		t.Fatal("lifecycle listeners must survive repeated bfcache round-trips")
	}
}
