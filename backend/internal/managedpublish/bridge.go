// Package managedpublish coordinates the registered Caddy capability with
// release activation. An uncertain cross-process commit blocks further writes.
package managedpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publish"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

var ErrDisabled = errors.New("registered Caddy publication is disabled")
var ErrStateConflict = errors.New("registered publication does not match the database")
var ErrUncertain = errors.New("publication outcome is uncertain; reconcile the registered root state before continuing")
var rootIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Config struct {
	Store        *store.Store
	Control      func(context.Context, caddymanage.Request) (caddymanage.Result, error)
	Enabled      bool
	ReleasesRoot string
	BuildsRoot   string
}
type Bridge struct{ cfg Config }

func New(cfg Config) *Bridge { return &Bridge{cfg: cfg} }

func (b *Bridge) Begin(ctx context.Context, siteID, actor uuid.UUID) (publish.ActivationSession, error) {
	state, err := b.cfg.Store.BeginManagedPublication(ctx, siteID, actor)
	if err != nil || state == nil {
		return nil, err
	}
	close := true
	defer func() {
		if close {
			_ = state.Tx.Rollback(context.Background())
		}
	}()
	if !b.cfg.Enabled || b.cfg.Control == nil {
		return nil, ErrDisabled
	}
	result, err := b.cfg.Control(ctx, caddymanage.Request{Operation: "inspect", SiteKey: state.SourceKey})
	if err != nil {
		return nil, err
	}
	if !matches(state, result) || !result.PublicationEnabled {
		return nil, ErrStateConflict
	}
	close = false
	return &session{bridge: b, before: state, actor: actor}, nil
}

type session struct {
	bridge    *Bridge
	before    *store.ManagedPublication
	actor     uuid.UUID
	release   model.Release
	applied   *caddymanage.Result
	attempted bool
	closed    bool
}

func (s *session) Tx() pgx.Tx { return s.before.Tx }
func (s *session) Activate(ctx context.Context, release model.Release) error {
	if s.closed || s.attempted || release.SiteID != s.before.SiteID {
		return ErrStateConflict
	}
	if id, err := uuid.Parse(release.ReleaseDir); err != nil || id == uuid.Nil || id.String() != release.ReleaseDir {
		return ErrStateConflict
	}
	manifest, err := fileDigest(filepath.Join(s.bridge.cfg.ReleasesRoot, release.SiteID.String(), release.ReleaseDir, "release.json"), 64<<10)
	if err != nil {
		return err
	}
	media, err := fileDigest(filepath.Join(s.bridge.cfg.BuildsRoot, release.ReleaseDir, "publication-media", publicationmedia.ManifestName), publicationmedia.MaxManifestBytes)
	if err != nil {
		return err
	}
	s.release = release
	s.attempted = true
	result, err := s.bridge.cfg.Control(ctx, caddymanage.Request{Operation: "release", SiteKey: s.before.SourceKey, ExpectedDigest: s.before.Management.SourceDigest, SiteID: release.SiteID.String(), ReleaseID: release.ReleaseDir, ManifestDigest: manifest, MediaManifestDigest: media})
	if err != nil {
		return err
	}
	if !validResult(result) || !result.PublicationEnabled || result.PublishedReleaseID != release.ReleaseDir || result.RootRevisionID == s.before.Management.RootRevisionID {
		return ErrUncertain
	}
	s.applied = &result
	if err := s.bridge.cfg.Store.UpdateManagedPublicationRoot(ctx, s.before, result.RootRevisionID, result.SourceDigest); err != nil {
		return err
	}
	return s.bridge.cfg.Store.AuditManagedPublication(ctx, s.before.Tx, s.actor, release.SiteID, release.ID, "activateManagedRelease")
}
func (s *session) Commit(ctx context.Context) error {
	if s.closed || s.applied == nil {
		return ErrStateConflict
	}
	err := s.before.Tx.Commit(ctx)
	s.closed = true
	if err == nil {
		markAuditCommitted(ctx)
		return nil
	}
	committed, recoveryErr := s.reconcile(ctx)
	if recoveryErr != nil {
		return recoveryErr
	}
	if committed {
		markAuditCommitted(ctx)
		return nil
	}
	return err
}
func (s *session) Abort(ctx context.Context) error {
	if s.closed {
		return nil
	}
	s.closed = true
	_ = s.before.Tx.Rollback(context.Background())
	if !s.attempted {
		return nil
	}
	_, err := s.reconcile(ctx)
	return err
}

func (s *session) reconcile(original context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(original), 40*time.Second)
	defer cancel()
	current, err := s.bridge.cfg.Store.BeginManagedPublicationRecovery(ctx, s.before.SiteID)
	if err != nil || current == nil {
		return false, ErrUncertain
	}
	defer current.Tx.Rollback(context.Background())
	root, err := s.bridge.cfg.Control(ctx, caddymanage.Request{Operation: "inspect", SiteKey: current.SourceKey})
	if err != nil {
		return false, ErrUncertain
	}
	if matches(current, root) {
		var committed bool
		if s.release.ID != uuid.Nil {
			if err := current.Tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM site_releases WHERE id=$1 AND site_id=$2 AND status<>'failed')`, s.release.ID, s.before.SiteID).Scan(&committed); err != nil {
				return false, ErrUncertain
			}
		}
		return committed, nil
	}
	// Restore only our exact root result, after proving the database still
	// points to the pre-activation release. Never undo another actor's update.
	if s.applied == nil || current.SourceKey != s.before.SourceKey || current.CurrentReleaseID != s.before.CurrentReleaseID || current.Management.RootRevisionID != s.before.Management.RootRevisionID || current.Management.SourceDigest != s.before.Management.SourceDigest || !validResult(root) || root.RootRevisionID != s.applied.RootRevisionID || root.SourceDigest != s.applied.SourceDigest || root.PublishedReleaseID != s.release.ReleaseDir {
		return false, ErrUncertain
	}
	restored, err := s.bridge.cfg.Control(ctx, caddymanage.Request{Operation: "restore", SiteKey: current.SourceKey, ExpectedDigest: root.SourceDigest, RevisionID: s.before.Management.RootRevisionID})
	if err != nil || !validResult(restored) || restored.SourceDigest != s.before.Management.SourceDigest || restored.PublishedReleaseID != s.before.PublishedReleaseDir {
		return false, ErrUncertain
	}
	if err := s.bridge.cfg.Store.UpdateManagedPublicationRoot(ctx, current, restored.RootRevisionID, restored.SourceDigest); err != nil {
		return false, ErrUncertain
	}
	if err := s.bridge.cfg.Store.AuditManagedPublication(ctx, current.Tx, s.actor, s.before.SiteID, s.release.ID, "compensateManagedRelease"); err != nil {
		return false, ErrUncertain
	}
	if err := current.Tx.Commit(ctx); err != nil {
		return false, ErrUncertain
	}
	return false, nil
}
func validResult(root caddymanage.Result) bool {
	return root.Registered && root.Adopted && !root.Drifted && !root.RuntimeUncertain && rootIDPattern.MatchString(root.RootRevisionID) && digestPattern.MatchString(root.SourceDigest)
}
func matches(state *store.ManagedPublication, root caddymanage.Result) bool {
	return validResult(root) && state.Management != nil && root.RootRevisionID == state.Management.RootRevisionID && root.SourceDigest == state.Management.SourceDigest && root.PublishedReleaseID == state.PublishedReleaseDir
}
func fileDigest(path string, limit int64) (string, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return "", ErrStateConflict
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return "", ErrStateConflict
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func markAuditCommitted(ctx context.Context) {
	if request := store.AuditRequestFrom(ctx); request != nil {
		request.Committed = true
	}
}
