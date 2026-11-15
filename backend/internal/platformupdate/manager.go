package platformupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildinfo"
)

// Manager retains robot-platform's separate supervisor and asynchronous task
// admission. systemd releases replace its Docker slots; PostgreSQL and site
// publications remain outside both application slots.
type Manager struct {
	cfg         Config
	mu          sync.Mutex
	op          sync.Mutex
	state       diskState
	checking    bool
	versions    []Version
	run         func(context.Context, string, ...string) error
	health      func(context.Context, string) error
	restartSelf func()
}

func (m *Manager) SetRestart(restart func()) { m.restartSelf = restart }

func New(cfg Config) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(cfg.Root)
	if err != nil {
		return nil, err
	}
	cfg.Root = resolved
	if err := os.MkdirAll(filepath.Join(cfg.Root, "update"), 0700); err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, state: diskState{Channel: "main", Logs: []string{}}, versions: []Version{}, run: runCommand}
	m.health = m.waitHealthy
	raw, err := os.ReadFile(m.statePath())
	if err == nil {
		if err = json.Unmarshal(raw, &m.state); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if m.state.Channel != "main" && m.state.Channel != "dev" {
		return nil, errors.New("invalid persisted channel")
	}
	if m.state.Task != nil {
		m.state.Task.Previous = m.state.TaskPrevious
		m.state.Task.Candidate = m.state.TaskCandidate
	}
	if err := m.recover(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) statePath() string { return filepath.Join(m.cfg.Root, "update", "state.json") }
func (m *Manager) saveLocked() error {
	if m.state.Task != nil {
		m.state.TaskPrevious = m.state.Task.Previous
		m.state.TaskCandidate = m.state.Task.Candidate
	}
	return atomicJSON(m.statePath(), m.state)
}
func (m *Manager) logLocked(message string) {
	m.state.Logs = append(m.state.Logs, now()+" "+message)
	if len(m.state.Logs) > 200 {
		m.state.Logs = m.state.Logs[len(m.state.Logs)-200:]
	}
}
func (m *Manager) github() *githubClient {
	raw, _ := os.ReadFile(filepath.Join(m.cfg.Root, "update", "github-token"))
	return newGitHub(m.cfg.Repository, strings.TrimSpace(string(raw)))
}
func (m *Manager) currentPath() (string, error) {
	return filepath.EvalSymlinks(filepath.Join(m.cfg.Root, "current"))
}
func (m *Manager) releasePath(p string) bool {
	base := filepath.Join(m.cfg.Root, "releases")
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == "." || strings.Contains(rel, string(filepath.Separator)) || rel == ".." {
		return false
	}
	info, err := os.Lstat(p)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func (m *Manager) Info() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Status{Enabled: true, Repository: m.cfg.Repository, Channel: m.state.Channel, Auto: m.state.Auto, Pinned: m.state.Pinned, Checking: m.checking, ManagerCommit: buildinfo.Commit, Versions: append([]Version{}, m.versions...), Logs: append([]string{}, m.state.Logs...), LastCheck: m.state.LastCheck, LastError: m.state.LastError}
	if m.state.Task != nil {
		task := *m.state.Task
		s.Task = &task
		s.Updating = task.Status == "running"
	}
	currentSchema := 0
	if p, err := m.currentPath(); err == nil {
		if manifest, err := readManifest(p); err == nil {
			s.Current = manifest.Version
			currentSchema = manifest.DatabaseSchema
		}
	}
	if manifest, err := readManifest(m.state.Previous); err == nil && currentSchema > 0 && manifest.DatabaseSchema == currentSchema {
		v := manifest.Version
		s.Previous = &v
	}
	if info, err := os.Stat(filepath.Join(m.cfg.Root, "update", "github-token")); err == nil {
		s.TokenSet = info.Size() > 0
	}
	return s
}

func (m *Manager) Check(ctx context.Context) error {
	if !m.op.TryLock() {
		return errors.New("another update operation is in progress")
	}
	defer m.op.Unlock()
	m.mu.Lock()
	m.checking = true
	branch := m.state.Channel
	m.mu.Unlock()
	versions, err := m.github().versions(ctx, branch)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checking = false
	m.state.LastCheck = now()
	if err != nil {
		m.state.LastError = err.Error()
	} else {
		m.versions = versions
		m.state.LastError = ""
	}
	if saveErr := m.saveLocked(); saveErr != nil {
		return saveErr
	}
	return err
}

func (m *Manager) Action(ctx context.Context, action Action) error {
	if action.Action == "check" {
		return m.Check(ctx)
	}
	if !m.op.TryLock() {
		return errors.New("another update operation is in progress")
	}
	unlock := true
	defer func() {
		if unlock {
			m.op.Unlock()
		}
	}()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Task != nil && (m.state.Task.Status == "running" || m.state.Task.Status == "recovery_required") && action.Action != "pin" {
		return errors.New("update recovery requires operator attention")
	}
	switch action.Action {
	case "channel":
		if action.Channel != "main" && action.Channel != "dev" {
			return errors.New("invalid channel")
		}
		m.state.Channel = action.Channel
		m.versions = []Version{}
		m.state.LastCheck = ""
		m.state.LastError = ""
	case "auto", "pin":
		if action.On == nil {
			return errors.New("a boolean setting is required")
		}
		if action.Action == "auto" {
			m.state.Auto = *action.On
		} else {
			m.state.Pinned = *action.On
		}
	case "token":
		if action.Token == nil || len(*action.Token) > 512 || strings.ContainsAny(*action.Token, "\r\n\x00") {
			return errors.New("invalid GitHub token")
		}
		if err := atomicJSONToken(filepath.Join(m.cfg.Root, "update", "github-token"), strings.TrimSpace(*action.Token)); err != nil {
			return err
		}
		m.versions = []Version{}
		m.state.LastCheck = ""
	case "update", "deploy", "rollback":
		if m.state.Pinned {
			return errors.New("version is pinned")
		}
		var selected Version
		selectedSchema := 0
		if action.Action == "rollback" {
			manifest, err := readManifest(m.state.Previous)
			if err != nil || !m.releasePath(m.state.Previous) {
				return errors.New("no verified previous release")
			}
			selected = manifest.Version
			selectedSchema = manifest.DatabaseSchema
		} else {
			for _, v := range m.versions {
				if action.Action == "update" || (v.RunID == action.RunID && v.Commit == action.Commit) {
					selected = v
					break
				}
			}
			if selected.Commit == "" {
				return errors.New("check updates before selecting a release")
			}
		}
		previous, err := m.currentPath()
		if err != nil || !m.releasePath(previous) {
			return errors.New("current release is outside the managed installation")
		}
		current, err := readManifest(previous)
		if action.Action == "rollback" && (err != nil || selectedSchema != current.DatabaseSchema) {
			return errors.New("previous release uses an incompatible database schema")
		}
		if err == nil && current.Commit == selected.Commit {
			return errors.New("selected commit is already active")
		}
		task := &Task{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Status: "running", Phase: "accepted", Commit: selected.Commit, Previous: previous, StartedAt: now()}
		m.state.Task = task
		m.state.LastError = ""
		m.logLocked("Accepted " + action.Action + " " + selected.Commit)
		if err := m.saveLocked(); err != nil {
			m.state.Task = nil
			return err
		}
		rollbackPath := m.state.Previous
		unlock = false
		go func() { defer m.op.Unlock(); m.deploy(selected, action.Action == "rollback", rollbackPath) }()
		return nil
	default:
		return errors.New("unsupported update action")
	}
	m.logLocked("Changed " + action.Action)
	return m.saveLocked()
}

func atomicJSONToken(path, token string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(token); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (m *Manager) phase(phase, candidate string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Task.Phase = phase
	if candidate != "" {
		m.state.Task.Candidate = candidate
	}
	m.logLocked(phase)
	return m.saveLocked()
}

func (m *Manager) finish(status string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Task.Status = status
	m.state.Task.FinishedAt = now()
	if err != nil {
		m.state.Task.Error = err.Error()
		m.state.LastError = err.Error()
	}
	m.logLocked(status + " " + m.state.Task.Commit)
	if saveErr := m.saveLocked(); saveErr != nil {
		m.state.LastError = "cannot persist update result"
	}
}

func (m *Manager) Loop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info := m.Info()
			if !info.Auto || info.Pinned || info.Updating || (info.Task != nil && info.Task.Status == "recovery_required") {
				continue
			}
			checkCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
			err := m.Check(checkCtx)
			cancel()
			if err == nil {
				info = m.Info()
				if len(info.Versions) > 0 && info.Versions[0].Commit != info.Current.Commit {
					_ = m.Action(ctx, Action{Action: "update"})
				}
			}
		}
	}
}

func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(m.Info())
	})
	mux.HandleFunc("POST /action", func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		var action Action
		if err := decoder.Decode(&action); err != nil {
			http.Error(w, "invalid action", 400)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			http.Error(w, "invalid trailing input", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		if err := m.Action(ctx, action); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(m.Info())
	})
	return mux
}
