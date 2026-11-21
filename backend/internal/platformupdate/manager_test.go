package platformupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "releases"), 0755); err != nil {
		t.Fatal(err)
	}
	return Config{Repository: "owner/repo", Root: root, Socket: filepath.Join(root, "control.sock"), Group: "service", HealthURL: "http://127.0.0.1:8787/api/v1/version", PrepareCommand: "/fixed/prepare"}
}

func fixture(t *testing.T, cfg Config, name, commit string) (string, Version) {
	t.Helper()
	dir := filepath.Join(cfg.Root, "releases", name)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Minimal ELF header, sufficient for format/architecture verification. The
	// command runner is injected; tests never execute these fixture programs.
	elf := make([]byte, 64)
	copy(elf, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(elf[16:], 2)
	binary.LittleEndian.PutUint16(elf[18:], 62)
	binary.LittleEndian.PutUint32(elf[20:], 1)
	binary.LittleEndian.PutUint16(elf[52:], 64)
	files := map[string][]byte{"fuyao-admin": elf, "fuyao-manager-updater": elf, "fuyao-caddy-control": elf, "fuyao-caddy-inventory": elf, "fuyao-caddy-register": elf, "frontend/dist/index.html": []byte("app"), "contracts/openapi/openapi.yaml": []byte("openapi"), "backend/go.mod": []byte("module test")}
	v := Version{Version: "0.7.0", Commit: commit, RunID: 1, Attempt: 1, Branch: "main"}
	m := Manifest{Version: v, Format: 1, Repository: cfg.Repository, Architecture: "linux/amd64", DatabaseSchema: 19, Files: map[string]string{}}
	for rel, raw := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, raw, 0644); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		m.Files[rel] = hex.EncodeToString(digest[:])
	}
	if err := atomicJSON(filepath.Join(dir, "release.json"), m); err != nil {
		t.Fatal(err)
	}
	return dir, v
}

func TestReleaseRejectsMutationAndProvenance(t *testing.T) {
	cfg := testConfig(t)
	dir, v := fixture(t, cfg, "one", strings.Repeat("a", 40))
	if _, err := verifyRelease(dir, cfg.Repository, v); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "fuyao-admin"))
	if info.Mode().Perm() != 0755 {
		t.Fatal("executable mode was not normalized")
	}
	wrong := v
	wrong.Commit = strings.Repeat("b", 40)
	if _, err := verifyRelease(dir, cfg.Repository, wrong); err == nil {
		t.Fatal("accepted a different commit")
	}
	if err := os.WriteFile(filepath.Join(dir, "frontend/dist/index.html"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRelease(dir, cfg.Repository, v); err == nil {
		t.Fatal("accepted mutated file")
	}
}

func TestArchiveRejectsTraversalLinksDuplicatesAndCorruption(t *testing.T) {
	for _, scenario := range []string{"traversal", "link", "duplicate", "corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			var buffer bytes.Buffer
			gz := gzip.NewWriter(&buffer)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: "file", Mode: 0644, Size: 4, Typeflag: tar.TypeReg}
			if scenario == "traversal" {
				h.Name = "../outside"
			}
			if scenario == "link" {
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "/etc/passwd"
				h.Size = 0
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Size > 0 {
				_, _ = tw.Write([]byte("data"))
			}
			if scenario == "duplicate" {
				_ = tw.WriteHeader(h)
				_, _ = tw.Write([]byte("data"))
			}
			_ = tw.Close()
			_ = gz.Close()
			raw := buffer.Bytes()
			if scenario == "corrupt" {
				raw[len(raw)-5] ^= 255
			}
			p := filepath.Join(t.TempDir(), "input.gz")
			_ = os.WriteFile(p, raw, 0600)
			if err := extractArchive(p, t.TempDir()); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}

func TestFailedActivationRestoresPreviousCommit(t *testing.T) {
	cfg := testConfig(t)
	previous, _ := fixture(t, cfg, "previous", strings.Repeat("a", 40))
	candidate, v := fixture(t, cfg, "candidate", strings.Repeat("b", 40))
	if err := os.Symlink(previous, filepath.Join(cfg.Root, "current")); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.state.Task = &Task{ID: "original-task", Status: "running", Phase: "accepted", Commit: v.Commit, Previous: previous}
	m.run = func(context.Context, string, ...string) error { return nil }
	checked := []string{}
	m.health = func(_ context.Context, commit string) error {
		checked = append(checked, commit)
		if commit == v.Commit {
			return errors.New("candidate unhealthy")
		}
		return nil
	}
	m.deploy(v, true, candidate)
	current, _ := m.currentPath()
	if current != previous {
		t.Fatalf("current=%s", current)
	}
	status := m.Info()
	if status.Task.Status != "rolled_back" || len(checked) != 2 || checked[1] != strings.Repeat("a", 40) {
		t.Fatalf("not verified rollback: %+v %v", status.Task, checked)
	}
	reloaded, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Info().Task.ID != "original-task" || reloaded.Info().Task.Status != "rolled_back" {
		t.Fatal("task receipt lost after restart")
	}
}

func TestRecoverInterruptedSwitchWithoutResubmitting(t *testing.T) {
	cfg := testConfig(t)
	previous, _ := fixture(t, cfg, "previous", strings.Repeat("a", 40))
	candidate, v := fixture(t, cfg, "candidate", strings.Repeat("b", 40))
	_ = os.Symlink(candidate, filepath.Join(cfg.Root, "current"))
	_ = os.Mkdir(filepath.Join(cfg.Root, "update"), 0700)
	m := &Manager{cfg: cfg, state: diskState{Channel: "main", Task: &Task{ID: "interrupted", Status: "running", Phase: "switching", Commit: v.Commit, Previous: previous, Candidate: candidate}}}
	commands := []string{}
	m.run = func(_ context.Context, name string, args ...string) error {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil
	}
	m.health = func(context.Context, string) error { return nil }
	if err := m.recover(); err != nil {
		t.Fatal(err)
	}
	current, _ := m.currentPath()
	if current != previous || m.state.Task.Status != "rolled_back" || len(commands) != 2 {
		t.Fatal("did not recover only the original switch", commands)
	}
}

func TestAdmissionPinsAndOperationExclusion(t *testing.T) {
	cfg := testConfig(t)
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if err = m.Action(t.Context(), Action{Action: "pin", On: &on}); err != nil {
		t.Fatal(err)
	}
	if err = m.Action(t.Context(), Action{Action: "update"}); err == nil {
		t.Fatal("pin bypass")
	}
	m.op.Lock()
	if err = m.Action(t.Context(), Action{Action: "channel", Channel: "dev"}); err == nil {
		t.Fatal("source changed during deployment")
	}
	m.op.Unlock()
	if err = m.Action(t.Context(), Action{Action: "token", Token: func() *string { s := "secret\nheader"; return &s }()}); err == nil {
		t.Fatal("accepted multiline token")
	}
	if m.Info().Channel != "main" {
		t.Fatal("channel changed")
	}
}

func TestPreviousReleaseWithDifferentSchemaIsNotOffered(t *testing.T) {
	cfg := testConfig(t)
	previous, _ := fixture(t, cfg, "previous", strings.Repeat("a", 40))
	current, _ := fixture(t, cfg, "current", strings.Repeat("b", 40))
	manifest, err := readManifest(current)
	if err != nil {
		t.Fatal(err)
	}
	manifest.DatabaseSchema++
	if err := atomicJSON(filepath.Join(current, "release.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(current, filepath.Join(cfg.Root, "current")); err != nil {
		t.Fatal(err)
	}
	m, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.state.Previous = previous
	if m.Info().Previous != nil {
		t.Fatal("incompatible previous version was offered")
	}
	if err := m.Action(t.Context(), Action{Action: "rollback"}); err == nil || !strings.Contains(err.Error(), "incompatible database schema") {
		t.Fatalf("rollback was not rejected: %v", err)
	}
}

func TestGitHubCatalogExcludesForkPRAndFailedRuns(t *testing.T) {
	runs := []workflowRun{}
	for _, event := range []string{"push", "pull_request", "workflow_dispatch"} {
		runs = append(runs, workflowRun{ID: 1, Attempt: 1, Commit: strings.Repeat("a", 40), Branch: "main", Event: event, Conclusion: "success", Path: ".github/workflows/ci.yml", Repository: struct {
			Name string `json:"full_name"`
		}{"owner/repo"}})
	}
	fork := runs[0]
	fork.Repository.Name = "attacker/repo"
	runs = append(runs, fork)
	failed := runs[0]
	failed.Conclusion = "failure"
	runs = append(runs, failed)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/releases" {
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"tag_name": "build-1-1", "name": "27.2.10 · main · aaaaaaaa", "assets": []map[string]string{{"name": archiveName}},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": runs})
	}))
	defer server.Close()
	g := newGitHub("owner/repo", "")
	g.base = server.URL
	versions, err := g.versions(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0].Version != "27.2.10" {
		t.Fatalf("accepted untrusted runs: %d", len(versions))
	}
}

func TestUpdateHandlerRejectsExtraJSONAndUnknownCommands(t *testing.T) {
	m, err := New(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"action":"pin","on":true} {}`, `{"action":"shell","command":"id"}`, `{"action":"shell"}`} {
		r := httptest.NewRequest("POST", "/action", strings.NewReader(body))
		w := httptest.NewRecorder()
		m.Handler().ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("accepted invalid command", body)
		}
	}
}

func TestProductVersionUpdateOrder(t *testing.T) {
	if !newerProductVersion("27.2.10", "27.2.9") ||
		newerProductVersion("27.2.10", "27.2.10") ||
		newerProductVersion("27.2.9", "27.2.10") ||
		newerProductVersion("", "27.2.10") ||
		newerProductVersion("27.2.10", "") {
		t.Fatal("automatic updates must compare known numeric application versions")
	}
}
