package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

func TestPreviewFrozenMediaOnlyAppliesToProduction(t *testing.T) {
	root := t.TempDir()
	site, build := uuid.New(), uuid.New()
	if previewFrozenMediaRoot("ci", root) != nil {
		t.Fatal("CI artifact assets were replaced with a nonexistent production media product")
	}
	resolve := previewFrozenMediaRoot("production", root)
	if resolve == nil {
		t.Fatal("production preview did not require frozen media")
	}
	if _, err := resolve(site, build); err == nil {
		t.Fatal("production media resolved missing snapshot")
	}
	frozen := filepath.Join(root, build.String(), "publication-media")
	if err := os.MkdirAll(frozen, 0750); err != nil {
		t.Fatal(err)
	}
	write := func(s, b uuid.UUID) {
		t.Helper()
		data, err := json.Marshal(publicationmedia.Manifest{Version: 1, SiteID: s.String(), BuildID: b.String(), Files: []publicationmedia.File{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(frozen, publicationmedia.ManifestName), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(uuid.New(), build)
	if _, err := resolve(site, build); err == nil {
		t.Fatal("preview used another site's frozen media")
	}
	write(site, uuid.New())
	if _, err := resolve(site, build); err == nil {
		t.Fatal("preview used another build's frozen media")
	}
	write(site, build)
	resolved, err := resolve(site, build)
	if err != nil || resolved != frozen {
		t.Fatalf("valid production media failed: %s %v", resolved, err)
	}
	if err := os.Remove(filepath.Join(frozen, publicationmedia.ManifestName)); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(site, build); err == nil {
		t.Fatal("missing production manifest fell back to mutable content")
	}
}
