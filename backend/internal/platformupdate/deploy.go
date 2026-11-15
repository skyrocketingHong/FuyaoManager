package platformupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
	// Command output may contain database details; it goes only to the service
	// journal, never to the browser-facing task or log ring.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed", filepath.Base(name))
	}
	return nil
}

func (m *Manager) deploy(v Version, rollback bool, rollbackPath string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	candidate := rollbackPath
	if !rollback {
		if err := m.phase("downloading", ""); err != nil {
			m.finish("failed", err)
			return
		}
		work, err := os.MkdirTemp(filepath.Join(m.cfg.Root, "update"), "download-*")
		if err != nil {
			m.finish("failed", err)
			return
		}
		defer os.RemoveAll(work)
		archive := filepath.Join(work, "release.tar.gz")
		if err = m.github().download(ctx, v, archive); err != nil {
			m.finish("failed", err)
			return
		}
		stage, err := os.MkdirTemp(filepath.Join(m.cfg.Root, "releases"), ".staging-*")
		if err != nil {
			m.finish("failed", err)
			return
		}
		defer os.RemoveAll(stage)
		if err = extractArchive(archive, stage); err != nil {
			m.finish("failed", err)
			return
		}
		if _, err = verifyRelease(stage, m.cfg.Repository, v); err != nil {
			m.finish("failed", err)
			return
		}
		if err = os.Chmod(stage, 0755); err != nil {
			m.finish("failed", err)
			return
		}
		candidate = filepath.Join(m.cfg.Root, "releases", releaseTag(v)+"-"+v.Commit[:12])
		if _, err = os.Lstat(candidate); os.IsNotExist(err) {
			if err = os.Rename(stage, candidate); err != nil {
				m.finish("failed", err)
				return
			}
		} else if err != nil {
			m.finish("failed", err)
			return
		}
	}
	manifest, err := verifyRelease(candidate, m.cfg.Repository, v)
	if err != nil {
		m.finish("failed", err)
		return
	}
	previous, err := m.currentPath()
	if err != nil {
		m.finish("failed", err)
		return
	}
	if old, readErr := readManifest(previous); readErr != nil || old.DatabaseSchema != manifest.DatabaseSchema {
		m.finish("failed", errors.New("database schema change requires an operator migration"))
		return
	}
	if err = m.phase("preparing", candidate); err != nil {
		m.finish("failed", err)
		return
	}
	if err = m.run(ctx, m.cfg.PrepareCommand, candidate); err != nil {
		m.finish("failed", err)
		return
	}
	if err = m.phase("switching", candidate); err != nil {
		m.finish("failed", err)
		return
	}
	if err = m.activate(ctx, candidate, v.Commit); err != nil {
		if restoreErr := m.restore(previous); restoreErr != nil {
			m.finish("recovery_required", errors.New("activation failed; previous application could not be verified"))
			return
		}
		m.finish("rolled_back", err)
		return
	}
	if err = m.installSupervisor(candidate); err != nil {
		if restoreErr := m.restore(previous); restoreErr != nil {
			m.finish("recovery_required", restoreErr)
			return
		}
		m.finish("rolled_back", err)
		return
	}
	m.mu.Lock()
	m.state.Previous = previous
	saveErr := m.saveLocked()
	m.mu.Unlock()
	if saveErr != nil {
		if restoreErr := m.restore(previous); restoreErr != nil {
			m.finish("recovery_required", restoreErr)
			return
		}
		m.finish("rolled_back", saveErr)
		return
	}
	m.finish("succeeded", nil)
	if m.restartSelf != nil {
		m.restartSelf()
	}
}

func (m *Manager) activate(ctx context.Context, candidate, commit string) error {
	if err := m.run(ctx, "systemctl", "stop", "fuyao-admin.service"); err != nil {
		return err
	}
	if err := m.switchLink(candidate); err != nil {
		return err
	}
	if err := m.run(ctx, "systemctl", "start", "fuyao-admin.service"); err != nil {
		return err
	}
	return m.health(ctx, commit)
}

func (m *Manager) switchLink(target string) error {
	if !m.releasePath(target) {
		return errors.New("invalid managed release")
	}
	tmp := filepath.Join(m.cfg.Root, ".current-update")
	_ = os.Remove(tmp)
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, filepath.Join(m.cfg.Root, "current"))
}

func (m *Manager) restore(previous string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	manifest, err := readManifest(previous)
	if err != nil {
		return err
	}
	if err = m.run(ctx, "systemctl", "stop", "fuyao-admin.service"); err != nil {
		return err
	}
	if err = m.switchLink(previous); err != nil {
		return err
	}
	if err = m.run(ctx, "systemctl", "start", "fuyao-admin.service"); err != nil {
		return err
	}
	return m.health(ctx, manifest.Commit)
}

func (m *Manager) waitHealthy(ctx context.Context, commit string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("health redirect refused") }}
	for {
		req, _ := http.NewRequestWithContext(ctx, "GET", m.cfg.HealthURL, nil)
		res, err := client.Do(req)
		if err == nil {
			var info struct {
				Commit string `json:"commit"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&info)
			_ = res.Body.Close()
			if res.StatusCode == 200 && decodeErr == nil && info.Commit == commit {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("running commit did not pass health verification")
		case <-ticker.C:
		}
	}
}

func (m *Manager) installSupervisor(candidate string) error {
	dir := filepath.Join(m.cfg.Root, "updater")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	src, err := os.Open(filepath.Join(candidate, "fuyao-manager-updater"))
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.CreateTemp(dir, ".updater-*")
	if err != nil {
		return err
	}
	defer os.Remove(dst.Name())
	_, err = io.Copy(dst, src)
	if err == nil {
		err = dst.Chmod(0755)
	}
	if err == nil {
		err = dst.Sync()
	}
	closeErr := dst.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(dst.Name(), filepath.Join(dir, "fuyao-manager-updater"))
}

func (m *Manager) recover() error {
	task := m.state.Task
	if task == nil || task.Status != "running" {
		return nil
	}
	if task.Phase == "switching" {
		if !m.releasePath(task.Previous) {
			return errors.New("interrupted update has invalid rollback target")
		}
		if err := m.restore(task.Previous); err != nil {
			return fmt.Errorf("interrupted deployment recovery: %w", err)
		}
		m.finish("rolled_back", errors.New("interrupted deployment restored previous application"))
	} else {
		m.finish("failed", errors.New("update interrupted before application switch"))
	}
	return nil
}
