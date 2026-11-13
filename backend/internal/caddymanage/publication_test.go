package caddymanage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

func setupPublication(t *testing.T) (fixture, Request) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("publication inputs must belong to a non-root application UID")
	}
	f := setupEngine(t)
	base, err := os.ReadFile(f.reg.BaseTemplate)
	if err != nil {
		t.Fatal(err)
	}
	base = bytes.Replace(base, []byte("root /unchanged"), []byte("root "+f.reg.StaticRoot), 1)
	base = bytes.Replace(base, []byte("__FUYAO_SECURITY_HEADERS__root"), []byte("__FUYAO_SECURITY_HEADERS__\nroot"), 1)
	parent := filepath.Dir(f.reg.StaticRoot)
	pub := Publication{SiteID: uuid.NewString(), ReleasesRoot: filepath.Join(parent, "inputs-releases"), BuildsRoot: filepath.Join(parent, "inputs-builds"), SourceUID: uint32(os.Geteuid()), AllowedThemes: []string{"manager-test-theme"}, ExistingRouting: true}
	base, f.reg.Publication, err = AttachPublication(base, f.reg.StaticRoot, pub, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.source, err = Render(f.reg, Settings{}, base)
	if err != nil {
		t.Fatal(err)
	}
	f.reg.InitialDigest = Digest(f.source)
	encoded, _ := json.Marshal(f.reg)
	for path, data := range map[string][]byte{f.reg.BaseTemplate: base, f.reg.SourceFile: f.source, filepath.Join(f.engine.registryRoot, f.reg.SiteKey+".json"): encoded} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.runner.active, _ = f.runner.Adapt(t.Context(), f.reg.MasterFile)
	releaseID := uuid.NewString()
	artifact := filepath.Join(pub.ReleasesRoot, pub.SiteID, releaseID)
	if err := os.MkdirAll(artifact, 0700); err != nil {
		t.Fatal(err)
	}
	releaseDoc := map[string]any{"contract": "fuyao.theme.release/v1", "schemaVersion": 1, "releaseId": releaseID, "theme": map[string]any{"id": "manager-test-theme", "version": "1.0.0"}}
	releaseData, _ := json.Marshal(releaseDoc)
	for name, data := range map[string][]byte{"release.json": releaseData, "index.html": []byte("<html>frozen build</html>")} {
		if err := os.WriteFile(filepath.Join(artifact, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	content := filepath.Join(parent, "editable-content")
	if err := os.MkdirAll(filepath.Join(content, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(content, "images", "photo.jpg"), []byte("at-build-time"), 0600); err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(pub.BuildsRoot, releaseID, "publication-media")
	if err := os.MkdirAll(filepath.Dir(media), 0700); err != nil {
		t.Fatal(err)
	}
	mediaDigest, err := publicationmedia.Freeze(content, media, uuid.MustParse(pub.SiteID), uuid.MustParse(releaseID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publicationmedia.Remove(pub.BuildsRoot); _ = publicationmedia.Remove(f.engine.stateRoot) })
	if err := os.WriteFile(filepath.Join(content, "images", "photo.jpg"), []byte("edited-after-preview"), 0600); err != nil {
		t.Fatal(err)
	}
	return f, Request{Operation: "release", SiteKey: f.reg.SiteKey, ExpectedDigest: Digest(f.source), SiteID: pub.SiteID, ReleaseID: releaseID, ManifestDigest: Digest(releaseData), MediaManifestDigest: mediaDigest}
}

func TestPublicationSealsFrozenBytesPreservesRulesAndRollsBack(t *testing.T) {
	f, request := setupPublication(t)
	baseline := adopt(t, f)
	result, err := f.engine.Run(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.PublicationEnabled || result.PublishedReleaseID != request.ReleaseID {
		t.Fatal("publication not reflected")
	}
	source, _ := os.ReadFile(f.reg.SourceFile)
	if !bytes.Contains(source, []byte(filepath.Join(f.engine.stateRoot, "releases", f.reg.SiteKey, request.ReleaseID, "site"))) || !bytes.Contains(source, []byte("import fuyao_amap_proxy")) || !bytes.Contains(source, []byte("SENSITIVE")) {
		t.Fatal("root changed without preserving complex source")
	}
	photo := filepath.Join(f.engine.publicationRoot(f.reg.SiteKey, request.ReleaseID), "media", "images", "photo.jpg")
	data, err := os.ReadFile(photo)
	if err != nil || string(data) != "at-build-time" {
		t.Fatal("publication read current editable media", err)
	}
	info, _ := os.Stat(photo)
	if info.Mode().Perm()&0222 != 0 {
		t.Fatal("sealed photo remains writable")
	}
	plan, err := f.engine.Run(t.Context(), Request{Operation: "validate", SiteKey: f.reg.SiteKey, ExpectedDigest: result.SourceDigest, Settings: Settings{Compression: "zstd"}})
	if err != nil {
		t.Fatal(err)
	}
	configured, err := f.engine.Run(t.Context(), Request{Operation: "deploy", SiteKey: f.reg.SiteKey, ExpectedDigest: result.SourceDigest, ValidationID: plan.ValidationID})
	if err != nil || configured.PublishedReleaseID != request.ReleaseID {
		t.Fatal("settings deploy lost published root", err)
	}
	rolledSettings, err := f.engine.Run(t.Context(), Request{Operation: "rollback", SiteKey: f.reg.SiteKey, ExpectedDigest: configured.SourceDigest, RevisionID: baseline.RootRevisionID})
	if err != nil || rolledSettings.PublishedReleaseID != request.ReleaseID {
		t.Fatal("configuration rollback changed the published release", err)
	}
	rolled, err := f.engine.Run(t.Context(), Request{Operation: "restore", SiteKey: f.reg.SiteKey, ExpectedDigest: rolledSettings.SourceDigest, RevisionID: baseline.RootRevisionID})
	if err != nil || rolled.PublishedReleaseID != "" {
		t.Fatal("rollback did not restore legacy source", err)
	}
	actual, _ := os.ReadFile(f.reg.SourceFile)
	if !bytes.Equal(actual, f.source) {
		t.Fatal("rollback changed legacy rules")
	}
}

func TestPublicationRejectsWrongTenantChangedMediaAndSymlinkInput(t *testing.T) {
	for _, mode := range []string{"wrong-tenant", "wrong-release", "changed-media", "symlink", "unapproved-theme"} {
		t.Run(mode, func(t *testing.T) {
			f, request := setupPublication(t)
			adopt(t, f)
			switch mode {
			case "wrong-tenant":
				request.SiteID = uuid.NewString()
			case "wrong-release":
				request.ReleaseID = uuid.NewString()
			case "changed-media":
				path := filepath.Join(f.reg.Publication.BuildsRoot, request.ReleaseID, "publication-media", "images", "photo.jpg")
				if err := os.Chmod(path, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("changed frozen media"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				path := filepath.Join(f.reg.Publication.ReleasesRoot, request.SiteID, request.ReleaseID, "leak")
				if err := os.Symlink("/etc/passwd", path); err != nil {
					t.Fatal(err)
				}
			case "unapproved-theme":
				path := filepath.Join(f.reg.Publication.ReleasesRoot, request.SiteID, request.ReleaseID, "release.json")
				data, _ := os.ReadFile(path)
				data = bytes.ReplaceAll(data, []byte("manager-test-theme"), []byte("unapproved-theme"))
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				request.ManifestDigest = Digest(data)
			}
			if _, err := f.engine.Run(t.Context(), request); err == nil {
				t.Fatal("unsafe publication accepted")
			}
			actual, _ := os.ReadFile(f.reg.SourceFile)
			if !bytes.Equal(actual, f.source) || f.runner.reloads != 0 {
				t.Fatal("rejected publication changed live source")
			}
		})
	}
}

func TestPublicationRequestAndRootSlotAllowlist(t *testing.T) {
	f, request := setupPublication(t)
	request.ReleaseID = "../another-site"
	if _, err := f.engine.Run(t.Context(), request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("path accepted as release ID", err)
	}
	data := `{"operation":"release","siteKey":"registered-site","root":"/etc"}`
	if _, err := DecodeRequest(strings.NewReader(data)); err == nil {
		t.Fatal("raw source path accepted")
	}
	pub := *f.reg.Publication
	pub.Paths = nil
	if _, _, err := AttachPublication([]byte("example.test {\nroot * /safe\nroot /legacy/images\nreverse_proxy /api/* 127.0.0.1:3191\n}\n"), "/safe", pub, map[string]string{"images": "/missing"}); !errors.Is(err, ErrUnsupported) {
		t.Fatal("unmatched media root accepted")
	}
}

func TestPlainFileServerPublicationUsesFrozenMediaAtPublicURLs(t *testing.T) {
	f, request := setupPublication(t)
	pub := f.reg.Publication
	if len(pub.Paths) != 1 || pub.Paths[0].MediaRelative != "" {
		t.Fatal("fixture must use only the ordinary artifact root")
	}
	base := []byte("ordinary.example {\nroot " + pub.Paths[0].Marker + "\nfile_server\n}\n")
	f.reg.Capabilities = Capabilities{}
	f.reg.Preserve = Preserve{}
	var err error
	f.source, err = Render(f.reg, Settings{}, base)
	if err != nil {
		t.Fatal(err)
	}
	f.reg.InitialDigest = Digest(f.source)
	registry, _ := json.Marshal(f.reg)
	for path, data := range map[string][]byte{f.reg.BaseTemplate: base, f.reg.SourceFile: f.source, filepath.Join(f.engine.registryRoot, f.reg.SiteKey+".json"): registry} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.runner.active, _ = f.runner.Adapt(t.Context(), f.reg.MasterFile)
	request.ExpectedDigest = Digest(f.source)
	content := filepath.Join(filepath.Dir(f.reg.StaticRoot), "editable-content")
	expected := map[string]string{"images/photo.jpg": "at-build-time", "wp-content/logo.svg": "frozen-wordpress", "albums/photos/full.jpg": "frozen-original", "albums/thumbnails/thumb.webp": "frozen-thumbnail"}
	for path, value := range expected {
		full := filepath.Join(content, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	media := filepath.Join(pub.BuildsRoot, request.ReleaseID, "publication-media")
	if err := publicationmedia.Remove(media); err != nil {
		t.Fatal(err)
	}
	request.MediaManifestDigest, err = publicationmedia.Freeze(content, media, uuid.MustParse(request.SiteID), uuid.MustParse(request.ReleaseID))
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(pub.ReleasesRoot, request.SiteID, request.ReleaseID)
	for relative := range expected {
		published := relative
		if strings.HasPrefix(relative, "albums/") {
			published = "media/" + relative
		}
		path := filepath.Join(artifact, filepath.FromSlash(published))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("artifact collision"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(path), "artifact-only.txt"), []byte("must not serve"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(content, filepath.FromSlash(relative)), []byte("edited live media"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(artifact, "media", "keep.txt"), []byte("unrelated artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	adopt(t, f)
	result, err := f.engine.Run(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	root := f.engine.publicationRoot(f.reg.SiteKey, request.ReleaseID)
	for relative, value := range expected {
		published := relative
		if strings.HasPrefix(relative, "albums/") {
			published = "media/" + relative
		}
		path := filepath.Join(root, "site", filepath.FromSlash(published))
		data, err := os.ReadFile(path)
		if err != nil || string(data) != value {
			t.Fatal("ordinary file_server cannot serve frozen media", relative, err)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 {
			t.Fatal("published media is not an immutable regular file")
		}
		independent, err := os.ReadFile(filepath.Join(root, "media", filepath.FromSlash(relative)))
		if err != nil || !bytes.Equal(data, independent) {
			t.Fatal("independent complex-site media seal changed", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "artifact-only.txt")); !os.IsNotExist(err) {
			t.Fatal("artifact-only file survived frozen-prefix replacement")
		}
	}
	keep, err := os.ReadFile(filepath.Join(root, "site", "media", "keep.txt"))
	if err != nil || string(keep) != "unrelated artifact" {
		t.Fatal("unrelated artifact path changed", err)
	}
	var seal publicationSeal
	if err := f.engine.readRecord(filepath.Join(root, "seal.json"), &seal); err != nil {
		t.Fatal(err)
	}
	seal.LayoutVersion = 0
	if err := f.engine.writeRecord(filepath.Join(root, "seal.json"), seal); err != nil {
		t.Fatal(err)
	}
	request.ExpectedDigest = result.SourceDigest
	if _, err := f.engine.Run(t.Context(), request); !errors.Is(err, ErrConflict) {
		t.Fatal("older media layout was silently reused", err)
	}
}
