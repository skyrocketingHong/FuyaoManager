package publish

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

var ErrArtifactProtected = errors.New("artifact is live, in use, or unavailable for deletion")

// ArtifactCleaner retires generated files while retaining build and release records.
type ArtifactCleaner struct {
	Store        *store.Store
	BuildsRoot   string
	ReleasesRoot string
	Activation   ActivationBridge
	PreviewInUse func(uuid.UUID, uuid.UUID) bool
}

func (c *ArtifactCleaner) Delete(ctx context.Context, siteID, actor uuid.UUID, ids []uuid.UUID, releases bool) error {
	// First make the whole batch unservable durably. A crash during filesystem
	// cleanup leaves a retryable record, and prevents reusing the same directory
	// for another publication until its previous contents have been removed.
	if err := c.cleanupPhase(ctx, siteID, actor, ids, releases, false); err != nil {
		return err
	}
	return c.cleanupPhase(ctx, siteID, actor, ids, releases, true)
}

func (c *ArtifactCleaner) cleanupPhase(ctx context.Context, siteID, actor uuid.UUID, ids []uuid.UUID, releases, remove bool) error {
	if !releases && c.PreviewInUse != nil {
		for _, id := range ids {
			if c.PreviewInUse(siteID, id) {
				return ErrArtifactProtected
			}
		}
	}
	var session ActivationSession
	if releases && c.Activation != nil {
		var err error
		session, err = c.Activation.Begin(ctx, siteID, actor)
		if err != nil {
			return err
		}
	}
	var tx pgx.Tx
	if session != nil {
		tx = session.Tx()
		defer session.Abort(context.WithoutCancel(ctx))
	} else {
		var err error
		tx, err = c.Store.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, store.AccountLifecycleLockKey); err != nil {
			return err
		}
		if err := c.Store.Releases.AcquirePublishLock(ctx, tx, siteID); err != nil {
			return err
		}
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT m.role='owner' AND u.platform_role='admin' AND u.status='active'
		FROM site_memberships m JOIN users u ON u.id=m.user_id
		WHERE m.site_id=$1 AND m.user_id=$2`, siteID, actor).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !allowed {
		return store.ErrOperatorForbidden
	}

	table, base := "site_builds", c.BuildsRoot
	if releases {
		table, base = "site_releases", c.ReleasesRoot
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	targets := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		var state, name string
		if releases {
			state, name, err = releaseArtifact(ctx, tx, root, siteID, id)
		} else {
			state, name, err = c.buildArtifact(ctx, tx, siteID, id)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		if err != nil {
			return err
		}
		if state == "deleted" {
			continue
		}
		if err := verifyArtifactDirectory(root, name); err != nil {
			return err
		}
		targets[id] = name
	}
	for id, name := range targets {
		state := "deleting"
		if remove {
			if err := removeFrozenArtifact(root, name); err != nil {
				return err
			}
			state = "deleted"
		}
		if _, err := tx.Exec(ctx, `UPDATE `+table+` SET artifact_state=$2 WHERE id=$1`, id, state); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (c *ArtifactCleaner) buildArtifact(ctx context.Context, tx pgx.Tx, siteID, id uuid.UUID) (string, string, error) {
	name := filepath.Join(id.String(), "artifacts")
	var state, status string
	var path *string
	err := tx.QueryRow(ctx, `SELECT artifact_state,status,artifact_path
		FROM site_builds WHERE site_id=$1 AND id=$2 FOR UPDATE`, siteID, id).Scan(&state, &status, &path)
	if err != nil || state == "deleted" {
		return state, name, err
	}
	var reserved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM site_network_entries WHERE build_id=$1 AND kind='preview')`, id).
		Scan(&reserved); err != nil {
		return "", "", err
	}
	if status != "succeeded" || path == nil || filepath.Clean(*path) != filepath.Join(c.BuildsRoot, name) || reserved {
		return "", "", ErrArtifactProtected
	}
	return state, name, nil
}

func releaseArtifact(ctx context.Context, tx pgx.Tx, root *os.Root, siteID, id uuid.UUID) (string, string, error) {
	var state, status, dir string
	var buildID uuid.UUID
	var current bool
	err := tx.QueryRow(ctx, `SELECT r.artifact_state,r.status,r.release_dir,r.build_id,(s.current_release_id IS NOT DISTINCT FROM r.id)
		FROM site_releases r JOIN sites s ON s.id=r.site_id WHERE r.site_id=$1 AND r.id=$2`,
		siteID, id).Scan(&state, &status, &dir, &buildID, &current)
	name := filepath.Join(siteID.String(), dir)
	if err != nil || state == "deleted" {
		return state, name, err
	}
	if current || status != "superseded" || dir != buildID.String() {
		return "", "", ErrArtifactProtected
	}
	// A legacy pointer can still serve the previous database release when edge
	// application failed. Edge writes share the same advisory lock.
	link, err := root.Readlink(filepath.Join(siteID.String(), "current"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", "", ErrArtifactProtected
	}
	if filepath.Base(filepath.Clean(link)) == dir {
		return "", "", ErrArtifactProtected
	}
	return state, name, nil
}

func verifyArtifactDirectory(root *os.Root, name string) error {
	for _, path := range []string{filepath.Dir(name), name} {
		info, err := root.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: unexpected directory type", ErrArtifactProtected)
		}
	}
	return nil
}

func removeFrozenArtifact(root *os.Root, name string) error {
	if err := fs.WalkDir(root.FS(), filepath.ToSlash(name), func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil || !entry.IsDir() {
			return err
		}
		// Only directory entries need write access for unlink. Never chmod links.
		return root.Chmod(filepath.FromSlash(path), 0o755)
	}); err != nil {
		return err
	}
	return root.RemoveAll(name)
}
