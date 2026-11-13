package caddymanage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFallbackRegistrationIsSimpleOnlyAndPreservesUnpublishedBytes(t *testing.T) {
	f, _ := setupPublication(t)
	pub := *f.reg.Publication
	pub.Paths = nil
	pub.ExistingRouting = false
	for _, source := range []string{
		"ordinary.example {\nroot /safe\nfile_server\n}\n",
		"ordinary.example {__FUYAO_SECURITY_HEADERS__\nroot * /safe\nencode zstd gzip\nheader {\nX-Frame-Options SAMEORIGIN\n}\nfile_server {\nprecompressed br gzip\nhide .git\n}\n}\n",
	} {
		base, registered, err := AttachPublication([]byte(source), "/safe", pub, nil)
		if err != nil {
			t.Fatal("simple source rejected", err)
		}
		if registered.FallbackMarker != PublicationFallbackMarker || registered.ExistingRouting {
			t.Fatal("simple source has no finite fallback")
		}
		reg := Registration{Publication: registered}
		before := strings.ReplaceAll(source, "__FUYAO_SECURITY_HEADERS__", "")
		actual, err := Render(reg, Settings{}, base)
		if err != nil || string(actual) != before {
			t.Fatal("unpublished source changed", err)
		}
	}
	for _, directive := range []string{
		"try_files {path} /200.html\nfile_server",
		"rewrite * /other\nfile_server",
		"handle /pages/* {\nfile_server\n}",
		"handle_path /pages/* {\nfile_server\n}",
		"route {\nfile_server\n}",
		"import app_rules\nfile_server",
		"@assets path /assets/*\nfile_server @assets",
		"file_server {\nfs custom\n}",
	} {
		source := []byte("ordinary.example {\nroot /safe\n" + directive + "\n}\n")
		if _, _, err := AttachPublication(source, "/safe", pub, nil); !errors.Is(err, ErrUnsupported) {
			t.Fatal("complex routes were guessed", directive, err)
		}
		approved := pub
		approved.ExistingRouting = true
		base, registered, err := AttachPublication(source, "/safe", approved, nil)
		if err != nil || registered.FallbackMarker != "" || !registered.ExistingRouting {
			t.Fatal("operator-reviewed existing routing not preserved", err)
		}
		actual, err := Render(Registration{Publication: registered}, Settings{}, base)
		if err != nil || !bytes.Equal(source, actual) {
			t.Fatal("existing route bytes changed", err)
		}
	}
}

func TestPublicationFallbackRequiresSealed200AndRestoresToEmpty(t *testing.T) {
	for _, with200 := range []bool{false, true} {
		t.Run(map[bool]string{false: "without-200", true: "with-200"}[with200], func(t *testing.T) {
			f, request := setupPublication(t)
			pub := *f.reg.Publication
			pub.Paths = nil
			pub.ExistingRouting = false
			original := []byte("ordinary.example {\nroot " + f.reg.StaticRoot + "\nfile_server\n}\n")
			base, registered, err := AttachPublication(original, f.reg.StaticRoot, pub, nil)
			if err != nil {
				t.Fatal(err)
			}
			f.reg.Publication = registered
			f.reg.Capabilities = Capabilities{}
			f.reg.Preserve = Preserve{}
			f.reg.InitialDigest = Digest(original)
			f.source = original
			encoded, _ := json.Marshal(f.reg)
			for path, data := range map[string][]byte{f.reg.BaseTemplate: base, f.reg.SourceFile: original, filepath.Join(f.engine.registryRoot, f.reg.SiteKey+".json"): encoded} {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			f.runner.active, _ = f.runner.Adapt(t.Context(), f.reg.MasterFile)
			request.ExpectedDigest = Digest(original)
			if with200 {
				if err := os.WriteFile(filepath.Join(pub.ReleasesRoot, request.SiteID, request.ReleaseID, "200.html"), []byte("<html>SPA</html>"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			baseline := adopt(t, f)
			result, err := f.engine.Run(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			deployed, _ := os.ReadFile(f.reg.SourceFile)
			if strings.Contains(string(deployed), "route @fuyao_publication_page") != with200 || bytes.Contains(deployed, []byte("__FUYAO_")) {
				t.Fatal("fallback rendered without a sealed 200.html or was lost")
			}
			if _, err := f.engine.Run(t.Context(), Request{Operation: "restore", SiteKey: f.reg.SiteKey, ExpectedDigest: result.SourceDigest, RevisionID: baseline.RootRevisionID}); err != nil {
				t.Fatal(err)
			}
			actual, _ := os.ReadFile(f.reg.SourceFile)
			if !bytes.Equal(actual, original) {
				t.Fatal("restore did not remove publication-only fallback")
			}
		})
	}
}
