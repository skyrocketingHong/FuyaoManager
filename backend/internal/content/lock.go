package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
)

// Cross-process compare-and-swap coordination. Every mutation of an
// authority file takes an exclusive flock on a sibling lock file for the
// duration of "read current → compare digest → write", so concurrent
// requests (and the Node media indexer, when wrapped by the Go runner) see
// a consistent sequence instead of racing a bare temp-file rename. Lock
// files live outside the content/config trees that snapshot preparation
// copies, so they never leak into frozen builds.

// withFileLock runs fn while holding the exclusive lock for one authority
// file. The lock file is <siteRoot>/.locks/<scope>-<hash>.lock.
func (s *Store) withFileLock(siteID uuid.UUID, scope, rel string, fn func() error) error {
	dir := filepath.Join(s.siteRoot(siteID), ".locks", scope)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(rel))
	name := filepath.Join(dir, hex.EncodeToString(sum[:8])+".lock")
	return withFLock(name, fn)
}

// WithAlbumMetaLock runs fn while holding the site-wide album metadata lock.
// The media indexer (Node) runs inside this critical section from the Go
// task runner, serializing index regeneration against human edits of the
// same files.
func (s *Store) WithAlbumMetaLock(ctx context.Context, siteID uuid.UUID, fn func() error) error {
	return s.withFileLock(siteID, "album-meta", "album-metadata", fn)
}

// withFLock opens (creating if needed) the lock file and holds an exclusive
// flock for the duration of fn.
func withFLock(path string, fn func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o640)
	if err != nil {
		return fmt.Errorf("open lock %s: %w", path, err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock %s: %w", path, err)
	}
	defer func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }()
	return fn()
}
