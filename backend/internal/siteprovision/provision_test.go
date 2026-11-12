package siteprovision

// Provisioning safety: user edits survive re-provisioning and racing
// provisioners (atomic no-replace writes), and a pre-existing tree whose
// symlinks escape the configured root is rejected instead of redirected.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

func testSite(t *testing.T) (model.Site, model.SiteSettings) {
	t.Helper()
	id := uuid.New()
	return model.Site{ID: id, Slug: "provision-test"},
		model.SiteSettings{SiteID: id, Name: "Provision Test", Description: "desc", DefaultLocale: "zh-CN"}
}

func mustProvision(t *testing.T, p *Provisioner, site model.Site, settings model.SiteSettings, baseURL string) {
	t.Helper()
	if err := p.Provision(site, settings, baseURL); err != nil {
		t.Fatalf("provision: %v", err)
	}
}

func TestProvisionKeepsExistingEdits(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site, settings := testSite(t)
	mustProvision(t, p, site, settings, "http://127.0.0.1:20001")

	siteYAML := filepath.Join(p.ConfigRoot(site.ID), "site.yaml")
	edited := "name: user-edited\n"
	if err := os.WriteFile(siteYAML, []byte(edited), 0o640); err != nil {
		t.Fatal(err)
	}

	// Re-provisioning a fully provisioned tree never clobbers user edits.
	mustProvision(t, p, site, settings, "http://127.0.0.1:20001")
	data, err := os.ReadFile(siteYAML)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != edited {
		t.Fatalf("user edit was overwritten: %q", data)
	}
}

func TestProvisionConcurrentNoReplace(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site, settings := testSite(t)

	// The fixed-.tmp rename race is gone: racing provisioners all succeed
	// and the produced files are complete, never truncated by a rename.
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			errs[slot] = p.Provision(site, settings, fmt.Sprintf("http://127.0.0.1:200%02d", slot))
		}(i)
	}
	wg.Wait()
	for slot, err := range errs {
		if err != nil {
			t.Fatalf("provision #%d: %v", slot, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(p.ConfigRoot(site.ID), "site.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "url:") {
		t.Fatalf("site.yaml incomplete: %q", data)
	}
	// No temporary leftovers anywhere in the tree.
	_ = filepath.WalkDir(p.Root, func(path string, d os.DirEntry, err error) error {
		if err == nil && strings.Contains(d.Name(), ".tmp-") {
			t.Fatalf("temporary file leaked: %s", path)
		}
		return nil
	})
}

func TestProvisionRejectsSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	site, settings := testSite(t)

	// A planted symlink inside the site tree pointing outside the root.
	if err := os.MkdirAll(p.ContentRoot(site.ID), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(p.ContentRoot(site.ID), "escape")); err != nil {
		t.Fatal(err)
	}
	if err := p.Provision(site, settings, ""); err == nil {
		t.Fatal("a tree whose symlink escapes the root must be rejected")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("the escaping symlink target must stay untouched")
	}

	// Remove the escape so the remaining tree is clean; a symlink resolving
	// inside the root is not an escape and provisions.
	if err := os.Remove(filepath.Join(p.ContentRoot(site.ID), "escape")); err != nil {
		t.Fatal(err)
	}
	inRoot := filepath.Join(p.ContentRoot(site.ID), "in-root-link")
	if err := os.Symlink(p.FaviconRoot(site.ID), inRoot); err != nil {
		t.Fatal(err)
	}
	if err := p.Provision(site, settings, ""); err != nil {
		t.Fatalf("in-root symlink must be allowed: %v", err)
	}
}

func TestWriteFileNoReplaceRefusesSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(victim, []byte("original"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(victim, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileNoReplace(link, []byte("replaced"), 0o640); err == nil {
		t.Fatal("writing through a symlink must be refused")
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "original" {
		t.Fatalf("symlink target was modified: err=%v data=%q", err, data)
	}
}

// TestProvisionLeavesPublishedPointerAlone proves the scan only covers the
// trees provisioning writes into: a published-site style pointer next to
// them (siteDir/current -> a releases tree outside the sites root) stays
// untouched and does not block provisioning.
func TestProvisionLeavesPublishedPointerAlone(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	releasesRoot := t.TempDir()
	site, settings := testSite(t)

	siteDir := filepath.Join(p.Root, site.ID.String())
	if err := os.MkdirAll(siteDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(releasesRoot, filepath.Join(siteDir, "current")); err != nil {
		t.Fatal(err)
	}

	if err := p.Provision(site, settings, ""); err != nil {
		t.Fatalf("provisioning must ignore links outside the content/config trees: %v", err)
	}
	link, err := os.Readlink(filepath.Join(siteDir, "current"))
	if err != nil || link != releasesRoot {
		t.Fatalf("the published pointer was disturbed: err=%v link=%q", err, link)
	}
}

// TestProvisionRejectsCrossSiteLinks proves a symlink that stays inside the
// sites root but points at another site's tree is rejected — both inside
// the content tree and as the config tree itself.
func TestProvisionRejectsCrossSiteLinks(t *testing.T) {
	p, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	site, settings := testSite(t)
	other := uuid.New()
	if err := os.MkdirAll(filepath.Join(p.Root, other.String(), "content"), 0o750); err != nil {
		t.Fatal(err)
	}

	// A link inside content pointing at the sibling site.
	if err := os.MkdirAll(p.ContentRoot(site.ID), 0o750); err != nil {
		t.Fatal(err)
	}
	cross := filepath.Join(p.ContentRoot(site.ID), "sibling")
	if err := os.Symlink(filepath.Join(p.Root, other.String()), cross); err != nil {
		t.Fatal(err)
	}
	if err := p.Provision(site, settings, ""); err == nil {
		t.Fatal("an in-root link to another site must be rejected")
	}
	if err := os.Remove(cross); err != nil {
		t.Fatal(err)
	}

	// The config tree itself pointing at the sibling site.
	if err := os.Symlink(filepath.Join(p.Root, other.String()), p.ConfigRoot(site.ID)); err != nil {
		t.Fatal(err)
	}
	if err := p.Provision(site, settings, ""); err == nil {
		t.Fatal("a config tree symlinked to another site must be rejected")
	}
	// The sibling site's tree stayed untouched.
	if entries, _ := os.ReadDir(filepath.Join(p.Root, other.String(), "content")); len(entries) != 0 {
		t.Fatal("the other site's tree must stay untouched")
	}
}
