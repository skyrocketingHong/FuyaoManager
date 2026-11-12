// Package siteprovision creates and maintains the per-site on-disk tree that
// the management platform owns: the content root (posts, data, albums) and
// the config root (site.yaml, content.yaml, favicon) that later feed the
// frozen-snapshot theme builds. All paths are derived from a single
// operator-configured sites root; nothing here trusts browser-supplied paths.
package siteprovision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// Provisioner owns the sites root directory.
type Provisioner struct {
	// Root is the base directory holding one subtree per site id.
	Root string
	// SeedFaviconDir optionally points at an operator-configured default
	// favicon set; an empty site favicon directory is seeded from it at
	// provisioning time. Production builds additionally seed an empty
	// favicon directory from the selected theme's own static/favicon (see
	// buildrunner), so this field is a convenience default, not a
	// requirement.
	SeedFaviconDir string
}

// New returns a Provisioner rooted at root. The root itself is created
// eagerly so a misconfigured path fails at service startup, not at first use.
func New(root string) (*Provisioner, error) {
	if root == "" {
		return nil, fmt.Errorf("sites root must not be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, fmt.Errorf("create sites root %s: %w", abs, err)
	}
	return &Provisioner{Root: abs}, nil
}

// ContentRoot is the site's content directory consumed by the build inputs.
func (p *Provisioner) ContentRoot(siteID uuid.UUID) string {
	return filepath.Join(p.Root, siteID.String(), "content")
}

// ConfigRoot is the site's config directory (site.yaml, content.yaml).
func (p *Provisioner) ConfigRoot(siteID uuid.UUID) string {
	return filepath.Join(p.Root, siteID.String(), "config")
}

// FaviconRoot is the site's favicon directory consumed by the build inputs.
func (p *Provisioner) FaviconRoot(siteID uuid.UUID) string {
	return filepath.Join(p.Root, siteID.String(), "config", "favicon")
}

// Provision idempotently creates the full directory tree and scaffold files
// for the site. baseURL seeds site.yaml's site.url with the site's real
// entry (http://<host>:<port>); existing files are never overwritten, so
// retrying a failed provisioning or re-running on an existing tree is safe
// and never clobbers a user-edited configuration. Only the trees this call
// writes into — the content/config trees and their ancestors — are checked
// for dangerous symlinks; other links in the site directory (e.g. a
// published-site `current` release pointer) stay untouched.
func (p *Provisioner) Provision(site model.Site, settings model.SiteSettings, baseURL string) error {
	id := site.ID
	if err := p.rejectEscapingSymlinks(id); err != nil {
		return err
	}
	dirs := []string{
		filepath.Join(p.ContentRoot(id), "posts"),
		filepath.Join(p.ContentRoot(id), "data"),
		filepath.Join(p.ContentRoot(id), "albums", "photos"),
		filepath.Join(p.ContentRoot(id), "albums", "thumbnails"),
		filepath.Join(p.ContentRoot(id), "albums", "metadata"),
		p.FaviconRoot(id),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := p.seedFavicon(p.FaviconRoot(id)); err != nil {
		return err
	}

	files := map[string][]byte{
		// Content data scaffolds mirror the schema the theme consumes
		// (site/src loadYaml consumers); empty collections are valid input.
		filepath.Join(p.ContentRoot(id), "data", "friends.yaml"):             []byte("online: []\noffline: []\n"),
		filepath.Join(p.ContentRoot(id), "data", "footprints.yaml"):          []byte("cities: []\n"),
		filepath.Join(p.ContentRoot(id), "data", "social-links.yaml"):        []byte("[]\n"),
		filepath.Join(p.ContentRoot(id), "data", "payments.yaml"):            []byte("[]\n"),
		filepath.Join(p.ContentRoot(id), "albums", "metadata", "index.json"): emptyAlbumIndex(),
		filepath.Join(p.ConfigRoot(id), "content.yaml"):                      []byte(contentYAML),
	}
	siteYAML, err := renderSiteYAML(settings, site.Slug, baseURL)
	if err != nil {
		return err
	}
	files[filepath.Join(p.ConfigRoot(id), "site.yaml")] = siteYAML

	for path, content := range files {
		if err := writeIfAbsent(path, content, 0o640); err != nil {
			return err
		}
	}
	return nil
}

// seedFavicon copies the default favicon set into an empty favicon
// directory; an already-populated directory is never touched.
func (p *Provisioner) seedFavicon(target string) error {
	if p.SeedFaviconDir == "" {
		return nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	seed, err := os.ReadDir(p.SeedFaviconDir)
	if err != nil {
		// A configured but absent seed is a provisioning error, not a skip.
		return fmt.Errorf("read favicon seed %s: %w", p.SeedFaviconDir, err)
	}
	for _, entry := range seed {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.SeedFaviconDir, entry.Name()))
		if err != nil {
			return err
		}
		// No-replace placement: an entry that appears between the emptiness
		// check and the write (a racing provisioner, a user edit) wins.
		if err := WriteFileNoReplace(filepath.Join(target, entry.Name()), data, 0o640); err != nil {
			return err
		}
	}
	return nil
}

// rejectEscapingSymlinks inspects exactly what this call may write into:
// the content and config trees are checked recursively, while their
// ancestor (the site directory) is checked at the directory level only —
// links beside these trees, such as the published-site `current` release
// pointer, stay untouched. A symlink in the inspected set whose target
// escapes the configured sites root or lands on ANOTHER site's tree (an
// in-sitesRoot link to a sibling site is just as dangerous as an
// out-of-root one) is rejected.
func (p *Provisioner) rejectEscapingSymlinks(siteID uuid.UUID) error {
	resolvedRoot, err := filepath.EvalSymlinks(p.Root)
	if err != nil {
		return err
	}
	siteDir := filepath.Join(p.Root, siteID.String())
	// Ancestor: directory-level check only, no traversal — links next to
	// the write targets (the current release pointer) are not provisioning
	// write paths and stay untouched.
	if info, err := os.Lstat(siteDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := p.checkLinkTarget(resolvedRoot, siteID, siteDir); err != nil {
			return err
		}
	}
	for _, tree := range []string{p.ContentRoot(siteID), p.ConfigRoot(siteID)} {
		if err := p.checkTreeSymlinks(resolvedRoot, siteID, tree); err != nil {
			return err
		}
	}
	return nil
}

// checkTreeSymlinks checks one write target and, when it is an existing
// directory, every symlink inside it. A symlink target must resolve inside
// the configured root AND under this site's own subtree.
func (p *Provisioner) checkTreeSymlinks(resolvedRoot string, siteID uuid.UUID, tree string) error {
	info, err := os.Lstat(tree)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := p.checkLinkTarget(resolvedRoot, siteID, tree); err != nil {
			return err
		}
	} else if !info.IsDir() {
		return nil // a regular file blocks creation; nothing to traverse
	}
	var walkErr error
	_ = filepath.WalkDir(tree, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries surface through the writes below
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		if err := p.checkLinkTarget(resolvedRoot, siteID, path); err != nil {
			walkErr = err
			return filepath.SkipAll
		}
		return nil
	})
	return walkErr
}

// checkLinkTarget resolves one symlink (dangling links resolve through
// their deepest existing ancestor) and validates the target: inside the
// configured root and under this site's own subtree — never another site's.
func (p *Provisioner) checkLinkTarget(resolvedRoot string, siteID uuid.UUID, linkPath string) error {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return nil
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		probe := target
		rel := ""
		resolved = ""
		for {
			parent := filepath.Dir(probe)
			if parent == probe {
				break
			}
			rel = filepath.Join(filepath.Base(probe), rel)
			if found, ferr := filepath.EvalSymlinks(parent); ferr == nil {
				resolved = filepath.Join(found, rel)
				break
			}
			probe = parent
		}
		if resolved == "" {
			resolved = filepath.Clean(target)
		}
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to provision: symlink %s escapes the configured sites root", linkPath)
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	// An upward rel is exactly ".." or starts with "../" — a literal sibling
	// directory named "..foo" inside the root is not an escape.
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to provision: symlink %s points at the sites root itself", linkPath)
	}
	first := rel
	if idx := strings.IndexByte(rel, filepath.Separator); idx >= 0 {
		first = rel[:idx]
	}
	if first != siteID.String() {
		return fmt.Errorf("refusing to provision: symlink %s points at another site's tree (%s)", linkPath, first)
	}
	return nil
}

// WriteFileNoReplace atomically creates path with content when it does not
// exist yet; an existing file — including a concurrently created one — is
// never overwritten, so user edits survive re-provisioning and racing
// provisioners. The final name must not be a symlink.
func WriteFileNoReplace(path string, content []byte, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write %s: it is a symlink", path)
		}
		return nil // existing content always wins
	} else if !os.IsNotExist(err) {
		return err
	}
	// A unique temporary file removes the fixed-.tmp collision between
	// racing writers; os.Link fails with EEXIST when another writer won,
	// which is the atomic no-replace placement.
	tmp := path + ".tmp-" + uuid.NewString()
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Link(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if os.IsExist(err) {
			return nil // the existing file won the race and stays untouched
		}
		return fmt.Errorf("place %s: %w", path, err)
	}
	_ = os.Remove(tmp)
	return nil
}

// writeIfAbsent writes content to path only when the file does not exist yet.
func writeIfAbsent(path string, content []byte, mode os.FileMode) error {
	return WriteFileNoReplace(path, content, mode)
}

func emptyAlbumIndex() []byte {
	doc := map[string]any{
		"schemaVersion": 1,
		"generatedAt":   time.Now().UTC().Format(time.RFC3339),
		"totalPhotos":   0,
		"years":         []int{},
		"devices":       []any{},
	}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return append(data, '\n')
}

// renderSiteYAML writes a minimal production-valid site.yaml seeded from the
// site settings. The theme's config schema rejects empty required strings in
// every mode and example/placeholder values in production mode
// (site/src/lib/config/schema.ts), so every default here is non-empty and
// placeholder-free; all values stay editable through the site config surface.
func renderSiteYAML(settings model.SiteSettings, slug, baseURL string) ([]byte, error) {
	name := settings.Name
	if name == "" {
		name = slug
	}
	description := settings.Description
	if description == "" {
		description = name
	}
	var b strings.Builder
	fmt.Fprintf(&b, "schemaVersion: 1\n")
	fmt.Fprintf(&b, "site:\n")
	fmt.Fprintf(&b, "  name: %s\n", yamlString(name))
	url := baseURL
	if url == "" {
		// No managed entry is known (CLI-created site before the platform
		// allocates one): seed with the loopback placeholder-free form; the
		// platform rewrites it with the real entry on first provisioning.
		url = "http://127.0.0.1"
	}
	fmt.Fprintf(&b, "  url: %s\n", yamlString(url))
	fmt.Fprintf(&b, "  startDate: '0001-01-01'\n")
	fmt.Fprintf(&b, "  startYear: %d\n", time.Now().Year())
	fmt.Fprintf(&b, "  defaultLocale: %s\n", settings.DefaultLocale)
	fmt.Fprintf(&b, "profile:\n")
	fmt.Fprintf(&b, "  name: %s\n", yamlString(name))
	fmt.Fprintf(&b, "  birthDate: '0001-01-01'\n")
	fmt.Fprintf(&b, "  avatarUrl: https://github.com/github.png\n")
	fmt.Fprintf(&b, "  roles:\n")
	fmt.Fprintf(&b, "    zh-CN: %s\n", yamlString(name))
	fmt.Fprintf(&b, "    en-US: %s\n", yamlString(name))
	fmt.Fprintf(&b, "  quotes:\n")
	fmt.Fprintf(&b, "    zh-CN: %s\n", yamlString(description))
	fmt.Fprintf(&b, "    en-US: %s\n", yamlString(description))
	fmt.Fprintf(&b, "repository:\n")
	fmt.Fprintf(&b, "  name: FuyaoHomepage\n")
	fmt.Fprintf(&b, "  url: https://github.com/skyrocketingHong/FuyaoHomepage\n")
	fmt.Fprintf(&b, "  owner: skyrocketingHong\n")
	fmt.Fprintf(&b, "seo:\n")
	fmt.Fprintf(&b, "  author: %s\n", yamlString(name))
	fmt.Fprintf(&b, "  description: %s\n", yamlString(description))
	fmt.Fprintf(&b, "  keywords: []\n")
	fmt.Fprintf(&b, "services:\n")
	fmt.Fprintf(&b, "  wallpaper:\n")
	fmt.Fprintf(&b, "    defaultUrl: https://picsum.photos/seed/%s/1920/1080\n", settings.SiteID.String())
	fmt.Fprintf(&b, "  amap: {}\n")
	return []byte(b.String()), nil
}

// yamlString quotes a scalar for YAML output. Empty and special-leading
// values are quoted; plain safe values pass through.
func yamlString(v string) string {
	if v == "" {
		return "''"
	}
	if strings.ContainsAny(v, ":#{}[]&*!|>'\"%@`\n") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		// Escape backslashes and double quotes, then double-quote the scalar.
		escaped := strings.ReplaceAll(v, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return v
}

const contentYAML = `schemaVersion: 1
paths:
  posts: posts
  data: data
  albumPhotos: albums/photos
  albumThumbnails: albums/thumbnails
  albumMetadata: albums/metadata
media:
  albumPublicBase: /media/albums
  mode: external
`
