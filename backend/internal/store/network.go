package store

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Network entry kinds: 'site' entries are the published-site edge (managed
// Caddy), 'preview' entries are private build previews served by the
// management process itself.
const (
	NetworkKindSite    = "site"
	NetworkKindPreview = "preview"
)

// NetworkEntry is one platform-assigned IP:port entry.
type NetworkEntry struct {
	ID        uuid.UUID
	SiteID    uuid.UUID
	Kind      string
	BuildID   *uuid.UUID
	Port      int
	CreatedAt time.Time
}

// NetworkEntryStore persists the platform's port assignments. Ports come
// from one operator-approved range; the UNIQUE(port) constraint plus a
// pre-insert trial bind keep two consumers off one port, and rows persist
// across restarts so an OS-recycled port never silently switches owners.
type NetworkEntryStore struct {
	pool *pgxpool.Pool
}

// ErrPortRangeExhausted means no port in the approved range is allocatable.
var ErrPortRangeExhausted = errors.New("network port range exhausted")

// EnsureSiteEntry returns the site's edge entry, allocating a port on first
// use. The second return is true when a new entry was created.
func (s *NetworkEntryStore) EnsureSiteEntry(ctx context.Context, siteID uuid.UUID, minPort, maxPort int) (NetworkEntry, bool, error) {
	return s.ensureEntry(ctx, siteID, nil, NetworkKindSite, minPort, maxPort)
}

// EnsurePreviewEntry returns the build's preview entry, allocating a port on
// first use.
func (s *NetworkEntryStore) EnsurePreviewEntry(ctx context.Context, siteID, buildID uuid.UUID, minPort, maxPort int) (NetworkEntry, bool, error) {
	return s.ensureEntry(ctx, siteID, &buildID, NetworkKindPreview, minPort, maxPort)
}

// ReleasePreview deletes the build's preview entry, freeing its port for
// reuse. Idempotent.
func (s *NetworkEntryStore) ReleasePreview(ctx context.Context, buildID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM site_network_entries WHERE kind = 'preview' AND build_id = $1`, buildID)
	return err
}

// List returns every entry, ordered by site then kind.
func (s *NetworkEntryStore) List(ctx context.Context) ([]NetworkEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, site_id, kind, build_id, port, created_at
		 FROM site_network_entries
		 ORDER BY site_id, kind, port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NetworkEntry
	for rows.Next() {
		var e NetworkEntry
		if err := rows.Scan(&e.ID, &e.SiteID, &e.Kind, &e.BuildID, &e.Port, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetSiteEntry returns the site's edge entry or ErrNotFound.
func (s *NetworkEntryStore) GetSiteEntry(ctx context.Context, siteID uuid.UUID) (NetworkEntry, error) {
	var e NetworkEntry
	err := s.pool.QueryRow(ctx,
		`SELECT id, site_id, kind, build_id, port, created_at
		 FROM site_network_entries WHERE kind = 'site' AND site_id = $1`, siteID,
	).Scan(&e.ID, &e.SiteID, &e.Kind, &e.BuildID, &e.Port, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NetworkEntry{}, ErrNotFound
	}
	if err != nil {
		return NetworkEntry{}, err
	}
	return e, nil
}

// GetPreviewEntry returns the build's preview entry or ErrNotFound.
func (s *NetworkEntryStore) GetPreviewEntry(ctx context.Context, buildID uuid.UUID) (NetworkEntry, error) {
	var e NetworkEntry
	err := s.pool.QueryRow(ctx,
		`SELECT id, site_id, kind, build_id, port, created_at
		 FROM site_network_entries WHERE kind = 'preview' AND build_id = $1`, buildID,
	).Scan(&e.ID, &e.SiteID, &e.Kind, &e.BuildID, &e.Port, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NetworkEntry{}, ErrNotFound
	}
	if err != nil {
		return NetworkEntry{}, err
	}
	return e, nil
}

// ensureEntry implements allocation: pick the lowest free port in the range
// (not taken in the table and free at the OS), insert, and retry on the rare
// race where another instance grabs the same port between check and insert.
func (s *NetworkEntryStore) ensureEntry(ctx context.Context, siteID uuid.UUID, buildID *uuid.UUID, kind string, minPort, maxPort int) (NetworkEntry, bool, error) {
	if existing, err := s.existing(ctx, siteID, buildID, kind); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return NetworkEntry{}, false, err
	}

	rows, err := s.pool.Query(ctx, `SELECT port FROM site_network_entries`)
	if err != nil {
		return NetworkEntry{}, false, err
	}
	taken := make(map[int]struct{})
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return NetworkEntry{}, false, err
		}
		taken[p] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return NetworkEntry{}, false, err
	}

	var candidates []int
	for p := minPort; p <= maxPort; p++ {
		if _, used := taken[p]; used {
			continue
		}
		ln, bindErr := net.Listen("tcp", joinHostPort("127.0.0.1", p))
		if bindErr != nil {
			continue // OS-busy (or outside the ephemeral guard): skip
		}
		_ = ln.Close()
		candidates = append(candidates, p)
		if len(candidates) >= 8 {
			break
		}
	}
	if len(candidates) == 0 {
		return NetworkEntry{}, false, ErrPortRangeExhausted
	}

	for _, port := range candidates {
		entry, created, err := s.insertEntry(ctx, siteID, buildID, kind, port)
		if err == nil {
			return entry, created, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.SQLState() == "23505" {
			continue // lost the race on this port; try the next candidate
		}
		return NetworkEntry{}, false, err
	}
	return NetworkEntry{}, false, ErrPortRangeExhausted
}

func (s *NetworkEntryStore) insertEntry(ctx context.Context, siteID uuid.UUID, buildID *uuid.UUID, kind string, port int) (NetworkEntry, bool, error) {
	id := newUUID()
	var e NetworkEntry
	err := s.pool.QueryRow(ctx,
		`INSERT INTO site_network_entries (id, site_id, kind, build_id, port)
		 SELECT $1, $2, $3, $4, $5
		 WHERE $3 = 'site' OR EXISTS (
			SELECT 1 FROM site_builds WHERE id=$4 AND site_id=$2 AND status='succeeded'
			AND artifact_state='available' FOR SHARE
		 )
		 RETURNING id, site_id, kind, build_id, port, created_at`,
		id, siteID, kind, buildID, port,
	).Scan(&e.ID, &e.SiteID, &e.Kind, &e.BuildID, &e.Port, &e.CreatedAt)
	if err != nil {
		return NetworkEntry{}, false, err
	}
	return e, true, nil
}

func (s *NetworkEntryStore) existing(ctx context.Context, siteID uuid.UUID, buildID *uuid.UUID, kind string) (NetworkEntry, error) {
	if kind == NetworkKindSite {
		return s.GetSiteEntry(ctx, siteID)
	}
	return s.GetPreviewEntry(ctx, *buildID)
}

func joinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
