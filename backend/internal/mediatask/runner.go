// Package mediatask executes asynchronous media processing: the album
// indexer (EXIF extraction plus thumbnail generation) runs as the site's
// selected theme's own Node script against the site's content directories,
// never writing the registered theme source. Tasks are persisted so
// failures carry reasons and restarts recover interrupted rows.
package mediatask

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// ContentRoots resolves a site's album directories.
type ContentRoots func(siteID uuid.UUID) (photosRoot, thumbnailsRoot, metadataRoot string, err error)

// Config assembles the runner.
type Config struct {
	Tasks        *store.MediaTaskStore
	Themes       *store.ThemeStore
	ContentRoots ContentRoots
	Logger       *slog.Logger
}

// Runner polls queued tasks and executes them one at a time.
type Runner struct {
	cfg    Config
	logger *slog.Logger
	stop   chan struct{}
	done   chan struct{}
}

// NewRunner assembles the runner.
func NewRunner(cfg Config) *Runner {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Runner{cfg: cfg, logger: cfg.Logger, stop: make(chan struct{}), done: make(chan struct{})}
}

// Start recovers interrupted rows and launches the worker.
func (r *Runner) Start(ctx context.Context) error {
	if err := r.cfg.Tasks.RecoverInterrupted(ctx); err != nil {
		return err
	}
	go r.loop()
	return nil
}

// Stop waits for the worker to drain.
func (r *Runner) Stop() {
	close(r.stop)
	select {
	case <-r.done:
	case <-time.After(30 * time.Second):
	}
}

func (r *Runner) loop() {
	defer close(r.done)
	for {
		select {
		case <-r.stop:
			return
		case <-time.After(5 * time.Second):
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		task, ok, err := r.cfg.Tasks.ClaimNext(ctx)
		if err != nil {
			r.logger.Error("media task claim", "error", err)
			cancel()
			continue
		}
		if !ok {
			cancel()
			continue
		}
		r.run(ctx, task)
		cancel()
	}
}

func (r *Runner) run(ctx context.Context, task store.MediaTask) {
	started := time.Now()
	err := r.runAlbumIndex(ctx, task)
	if err == nil {
		if err := r.cfg.Tasks.MarkSucceeded(ctx, task.ID); err != nil {
			r.logger.Error("media task mark succeeded", "taskId", task.ID, "error", err)
		}
		r.logger.Info("media task succeeded", "taskId", task.ID, "siteId", task.SiteID, "elapsed", time.Since(started).Round(time.Millisecond))
		return
	}
	r.logger.Error("media task failed", "taskId", task.ID, "siteId", task.SiteID, "error", err)
	failure := model.MediaTaskFailure{Stage: "album-index", Message: err.Error()}
	if err := r.cfg.Tasks.MarkFailed(ctx, task.ID, failure); err != nil {
		r.logger.Error("media task mark failed", "taskId", task.ID, "error", err)
	}
}

// runAlbumIndex executes the site theme's indexer with the site's album
// directories. The script reads original photos and writes thumbnails plus
// metadata into the content tree; the theme source itself is only read.
func (r *Runner) runAlbumIndex(ctx context.Context, task store.MediaTask) error {
	photos, thumbs, meta, err := r.cfg.ContentRoots(task.SiteID)
	if err != nil {
		return err
	}
	// Resolve the script through the site's selected theme so the executed
	// code is exactly what the catalog registered.
	selection, err := r.cfg.Themes.GetSelectionForUser(ctx, task.SiteID, task.RequestedBy)
	if err != nil {
		return fmt.Errorf("the site has no theme selection to provide the media scripts: %w", err)
	}
	if selection.Selection == nil {
		return fmt.Errorf("the site has no theme selection to provide the media scripts")
	}
	theme, err := r.cfg.Themes.GetByRowID(ctx, selection.Selection.ThemeID)
	if err != nil {
		return fmt.Errorf("the site's theme is no longer registered: %w", err)
	}
	script := filepath.Join(theme.SourcePath, "scripts", "generate-album-index.js")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("album indexer missing from the theme source: %w", err)
	}

	pathValue, _ := os.LookupEnv("PATH")
	bin := filepath.Join(filepath.Dir(theme.SourcePath), "node_modules", ".bin")
	if _, statErr := os.Stat(bin); statErr == nil {
		pathValue = bin + string(os.PathListSeparator) + pathValue
	}
	env := []string{
		"FUYAO_ALBUM_PHOTOS_DIR=" + photos,
		"FUYAO_ALBUM_THUMBNAILS_DIR=" + thumbs,
		"FUYAO_ALBUM_METADATA_DIR=" + meta,
		"FUYAO_ALBUM_COORD_TYPE=wgs84",
		"PATH=" + pathValue,
	}
	for _, name := range []string{"HOME", "TMPDIR", "LANG", "NODE_OPTIONS"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}

	cmd := exec.CommandContext(ctx, "node", script, "--no-rename")
	cmd.Dir = theme.SourcePath
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 2000 {
			tail = tail[len(tail)-2000:]
		}
		return fmt.Errorf("%v: %s", err, tail)
	}
	return nil
}
