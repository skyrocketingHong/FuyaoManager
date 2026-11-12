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

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
)

// Provisioner owns the sites root directory.
type Provisioner struct {
	// Root is the base directory holding one subtree per site id.
	Root string
	// SeedFaviconDir optionally points at a default favicon set (the first
	// theme's static/favicon); an empty site favicon directory is seeded
	// from it so production builds pass the theme's favicon validation.
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
// and never clobbers a user-edited configuration.
func (p *Provisioner) Provision(site model.Site, settings model.SiteSettings, baseURL string) error {
	id := site.ID
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
		if err := os.WriteFile(filepath.Join(target, entry.Name()), data, 0o640); err != nil {
			return err
		}
	}
	return nil
}

// writeIfAbsent writes content to path only when the file does not exist yet.
func writeIfAbsent(path string, content []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("place %s: %w", path, err)
	}
	return nil
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
