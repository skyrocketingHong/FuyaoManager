// Package edge manages the platform's published-site entries: the
// operator-approved IP:port pairs, the per-site `current` release pointer on
// disk, and the Caddy snippet the reverse proxy imports. It renders, never
// executes, Caddy configuration: the snippet contains only addresses, roots,
// and fixed directives generated from database state — no browser-supplied
// paths or commands.
package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// ErrEdgeDisabled marks calls on a deployment without snippet management.
var ErrEdgeDisabled = errors.New("edge management is not configured")

// ErrApplyFailed means the rendered snippet failed validation or reload; the
// previous snippet is restored and the running config keeps serving.
var ErrApplyFailed = errors.New("edge apply failed")

// Config assembles the edge manager.
type Config struct {
	Pool         *pgxpool.Pool
	Entries      *store.NetworkEntryStore
	SnippetDir   string   // empty disables edge management
	ApplyCmd     []string // validate+reload command with fixed arguments
	PrivateBind  string   // address private entries bind and are reachable on
	PublicBind   string   // address public entries bind; ":" (empty) = all interfaces
	PublicHost   string   // URL host rendered for public entries (bind may be ":")
	ReleasesRoot string   // <root>/<siteID>/<releaseDir> plus the current pointer
	SitesRoot    string   // per-site content root for /media/albums
	PortMin      int
	PortMax      int
	Logger       *slog.Logger
}

// entryRow is one rendered edge entry: the database view of a published site.
type entryRow struct {
	SiteID     uuid.UUID
	Slug       string
	Visibility string
	Port       int
	ReleaseDir string
}

// Manager owns the snippet lifecycle and the current pointers.
type Manager struct {
	cfg Config
}

// New assembles the manager.
func New(cfg Config) *Manager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{cfg: cfg}
}

// Enabled reports whether snippet management is active.
func (m *Manager) Enabled() bool { return m.cfg.SnippetDir != "" }

// EntryURL renders the externally reachable URL of a site entry.
func (m *Manager) EntryURL(visibility string, port int) string {
	host := m.cfg.PublicHost
	if host == "" {
		host = m.cfg.PublicBind
	}
	if visibility == model.SiteVisibilityPrivate {
		host = m.cfg.PrivateBind
	}
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return "http://" + host + ":" + fmt.Sprint(port)
}

// OnLive switches the site's current pointer to the release directory and
// applies the edge configuration. Called after the database already holds
// the live release (publish or rollback commit), so a failure here leaves a
// consistent, repairable state: the DB is live, the old edge config keeps
// serving, and Reconcile (or the next publish) repairs the pointer.
func (m *Manager) OnLive(ctx context.Context, siteID uuid.UUID, release model.Release) error {
	if err := m.SwitchCurrent(siteID, release.ReleaseDir); err != nil {
		return err
	}
	if !m.Enabled() {
		return nil
	}
	if _, _, err := m.cfg.Entries.EnsureSiteEntry(ctx, siteID, m.cfg.PortMin, m.cfg.PortMax); err != nil {
		return fmt.Errorf("allocate site entry: %w", err)
	}
	return m.ApplyAll(ctx)
}

// SwitchCurrent atomically points <ReleasesRoot>/<siteID>/current at the
// release directory: a temporary symlink is renamed over the old pointer, so
// readers see either the old or the new release, never a missing path.
func (m *Manager) SwitchCurrent(siteID uuid.UUID, releaseDir string) error {
	siteRoot := filepath.Join(m.cfg.ReleasesRoot, siteID.String())
	target := filepath.Join(siteRoot, releaseDir)
	if _, err := os.Stat(filepath.Join(target, "release.json")); err != nil {
		return fmt.Errorf("release directory missing: %w", err)
	}
	if err := os.MkdirAll(siteRoot, 0o755); err != nil {
		return err
	}
	current := filepath.Join(siteRoot, "current")
	tmp := current + ".tmp-" + uuid.NewString()[:8]
	if err := os.Symlink(releaseDir, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, current); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Reconcile repairs drift between database live releases, on-disk current
// pointers, and the rendered snippet. Safe to run at startup and on demand.
func (m *Manager) Reconcile(ctx context.Context) error {
	rows, err := m.liveRows(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := m.SwitchCurrent(row.SiteID, row.ReleaseDir); err != nil {
			m.cfg.Logger.Error("edge reconcile pointer", "siteId", row.SiteID, "error", err)
		}
	}
	if !m.Enabled() {
		return nil
	}
	changed := false
	for _, row := range rows {
		if _, created, err := m.cfg.Entries.EnsureSiteEntry(ctx, row.SiteID, m.cfg.PortMin, m.cfg.PortMax); err != nil {
			m.cfg.Logger.Error("edge reconcile entry", "siteId", row.SiteID, "error", err)
		} else if created {
			changed = true
		}
	}
	if changed {
		return m.ApplyAll(ctx)
	}
	// Idempotent re-apply keeps the snippet authoritative even after manual
	// edits; validation failure restores the previous file and is reported.
	return m.ApplyAll(ctx)
}

// ApplyAll re-renders and applies the managed snippet for every published
// site. The previous snippet is restored when validation or reload fails.
func (m *Manager) ApplyAll(ctx context.Context) error {
	if !m.Enabled() {
		return ErrEdgeDisabled
	}
	rows, err := m.liveRows(ctx)
	if err != nil {
		return err
	}
	rendered, err := m.render(rows)
	if err != nil {
		return err
	}
	snippetPath := filepath.Join(m.cfg.SnippetDir, "fuyao-admin.caddy")
	previous, readErr := os.ReadFile(snippetPath)
	tmp := snippetPath + ".tmp"
	if err := os.WriteFile(tmp, rendered, 0o640); err != nil {
		return fmt.Errorf("write snippet: %w", err)
	}
	if err := os.Rename(tmp, snippetPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("place snippet: %w", err)
	}
	if err := m.apply(ctx); err != nil {
		// Restore the last good snippet so the file on disk matches what the
		// running Caddy actually serves.
		if readErr == nil {
			_ = os.WriteFile(snippetPath, previous, 0o640)
		} else {
			_ = os.Remove(snippetPath)
		}
		return fmt.Errorf("%w: %v", ErrApplyFailed, err)
	}
	m.cfg.Logger.Info("edge applied", "sites", len(rows))
	return nil
}

// apply runs the operator-controlled validate+reload command.
func (m *Manager) apply(ctx context.Context) error {
	if len(m.cfg.ApplyCmd) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, m.cfg.ApplyCmd[0], m.cfg.ApplyCmd[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// liveRows reads every site with a live release plus its entry, visibility,
// and live release directory.
func (m *Manager) liveRows(ctx context.Context) ([]entryRow, error) {
	rows, err := m.cfg.Pool.Query(ctx, `
		SELECT si.id, si.slug, si.visibility, e.port, r.release_dir
		FROM sites si
		JOIN site_releases r ON r.id = si.current_release_id AND r.status = 'live'
		LEFT JOIN site_network_entries e ON e.site_id = si.id AND e.kind = 'site'
		ORDER BY si.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entryRow
	for rows.Next() {
		var row entryRow
		var port *int
		if err := rows.Scan(&row.SiteID, &row.Slug, &row.Visibility, &port, &row.ReleaseDir); err != nil {
			return nil, err
		}
		if port != nil {
			row.Port = *port
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// render produces the managed snippet: one site block per published site.
// Private entries bind exactly the private address; public entries bind all
// interfaces. Roots point at the frozen release trees through the stable
// `current` pointer and at the site's own album media for /media/albums.
func (m *Manager) render(rows []entryRow) ([]byte, error) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	var b bytes.Buffer
	b.WriteString("# Managed by fuyao-admin; changes are overwritten by the platform.\n")
	if len(rows) == 0 {
		b.WriteString("# No published sites yet.\n")
		return b.Bytes(), nil
	}
	for _, row := range rows {
		if row.Port == 0 {
			continue // no entry allocated yet; reconcile assigns one
		}
		address := fmt.Sprintf(":%d", row.Port)
		if row.Visibility == model.SiteVisibilityPrivate {
			address = urlHostPort(m.cfg.PrivateBind, row.Port)
		}
		releaseRoot := filepath.Join(m.cfg.ReleasesRoot, row.SiteID.String(), "current")
		photosRoot := filepath.Join(m.cfg.SitesRoot, row.SiteID.String(), "content", "albums", "photos")
		thumbsRoot := filepath.Join(m.cfg.SitesRoot, row.SiteID.String(), "content", "albums", "thumbnails")
		fmt.Fprintf(&b, "\nhttp://%s {\n", address)
		fmt.Fprintf(&b, "\troot * %s\n", quotePath(releaseRoot))
		// Static content (indexes, data YAML, fonts, images) is served as
		// files; only HTML routes fall back to the SPA shell — mirroring the
		// operator's production site matcher.
		fmt.Fprintf(&b, "\t@static {\n")
		fmt.Fprintf(&b, "\t\tpath *.js *.css *.png *.jpg *.jpeg *.gif *.svg *.ico *.woff *.woff2 *.webp *.json *.xml *.txt *.yaml *.webmanifest\n")
		fmt.Fprintf(&b, "\t\tpath /fonts/* /posts/* /data/*\n")
		fmt.Fprintf(&b, "\t}\n")
		fmt.Fprintf(&b, "\thandle @static {\n\t\tfile_server\n\t}\n")
		fmt.Fprintf(&b, "\ttry_files {path}/index.html {path}index.html {path}.html /200.html\n")
		fmt.Fprintf(&b, "\tencode gzip zstd\n")
		fmt.Fprintf(&b, "\t@mutable path /release.json /service-worker.js /posts/* /data/* /albums/* /favicon/* /images/* /blog/rss.xml /sitemap.xml\n")
		fmt.Fprintf(&b, "\theader @mutable Cache-Control \"no-store, no-transform, must-revalidate\"\n")
		fmt.Fprintf(&b, "\thandle_path /media/albums/photos/* {\n\t\troot * %s\n\t\tfile_server\n\t}\n", quotePath(photosRoot))
		fmt.Fprintf(&b, "\thandle_path /media/albums/thumbnails/* {\n\t\troot * %s\n\t\tfile_server\n\t}\n", quotePath(thumbsRoot))
		fmt.Fprintf(&b, "\tfile_server\n}\n")
	}
	return b.Bytes(), nil
}

func urlHostPort(host string, port int) string {
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	return (&url.URL{Host: fmt.Sprintf("%s:%d", host, port)}).Host
}

// quotePath escapes a filesystem path for the Caddyfile tokenizer.
func quotePath(path string) string {
	if strings.ContainsAny(path, " \t\"#{}") {
		return "\"" + strings.ReplaceAll(path, "\"", "\\\"") + "\""
	}
	return path
}
