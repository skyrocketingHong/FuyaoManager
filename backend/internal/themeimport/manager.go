package themeimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/buildrunner"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themecontract"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeregistry"
)

type Candidate struct {
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	ManifestDigest string `json:"manifestDigest"`
	State          string `json:"state"`
}

type Task struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	State   string `json:"state"`
	Error   string `json:"error,omitempty"`
}

type SourceStatus struct {
	Source
	Task *Task `json:"task,omitempty"`
}

type Manager struct {
	root, contractsDir string
	ctx                context.Context
	store              *store.Store
	contracts          *themecontract.Set
	sources            []Source
	logger             *slog.Logger
	client             *http.Client
	resolve            func(context.Context, Source) (string, error)
	mu                 sync.Mutex
	busy               bool
	tasks              map[string]Task
}

func New(ctx context.Context, st *store.Store, contracts *themecontract.Set, contractsDir, root, config string, logger *slog.Logger) (*Manager, error) {
	if config == "" {
		return nil, nil
	}
	sources, err := parseSources(config)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	if contracts == nil {
		return nil, errors.New("GitHub theme installation requires theme contracts")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &Manager{
		root: root, contractsDir: contractsDir, ctx: ctx, store: st, contracts: contracts, sources: sources, logger: logger,
		client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		resolve: githubCommit, tasks: make(map[string]Task),
	}, nil
}

func (m *Manager) List() []SourceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SourceStatus, 0, len(m.sources))
	for _, source := range m.sources {
		item := SourceStatus{Source: source}
		if task, ok := m.tasks[source.ID]; ok {
			item.Task = &task
		}
		out = append(out, item)
	}
	return out
}

func (m *Manager) Check(ctx context.Context, id string) (Candidate, error) {
	for _, source := range m.sources {
		if source.ID == id {
			return m.check(ctx, source)
		}
	}
	return Candidate{}, store.ErrNotFound
}

func (m *Manager) check(ctx context.Context, source Source) (Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	commit, err := m.resolve(ctx, source)
	if err != nil {
		return Candidate{}, err
	}
	candidate, err := m.readCandidate(ctx, source, commit)
	if err != nil {
		return Candidate{}, err
	}
	theme, err := m.store.Themes.Get(ctx, source.ID, candidate.Version)
	if errors.Is(err, store.ErrNotFound) {
		return candidate, nil
	}
	if err != nil {
		return Candidate{}, err
	}
	if theme.SourcePath != m.themePath(source, commit) || !m.matchesCandidate(theme, candidate, source) {
		candidate.State = "conflict"
	} else if _, err := os.Stat(theme.SourcePath); err == nil {
		candidate.State = "installed"
	}
	return candidate, nil
}

func (m *Manager) Install(ctx context.Context, id, commit string, actor uuid.UUID) (Task, error) {
	var source Source
	for _, configured := range m.sources {
		if configured.ID == id {
			source = configured
			break
		}
	}
	if source.ID == "" {
		return Task{}, store.ErrNotFound
	}
	if !commitPattern.MatchString(commit) {
		return Task{}, ErrStale
	}
	candidate, err := m.check(ctx, source)
	if err != nil {
		return Task{}, err
	}
	if candidate.Commit != commit || candidate.State == "conflict" {
		return Task{}, ErrStale
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := Task{Version: candidate.Version, Commit: commit, State: "installing"}
	if candidate.State == "installed" {
		task.State = "installed"
		m.tasks[id] = task
		return task, nil
	}
	if m.busy {
		if previous, ok := m.tasks[id]; ok && previous.State == "installing" && previous.Commit == commit {
			return previous, nil
		}
		return Task{}, ErrBusy
	}
	m.busy = true
	m.tasks[id] = task
	go m.run(source, candidate, task, actor)
	return task, nil
}

func (m *Manager) themePath(source Source, commit string) string {
	return filepath.Join(m.root, source.ID, commit, filepath.FromSlash(source.Path))
}

func (m *Manager) run(source Source, candidate Candidate, task Task, actor uuid.UUID) {
	ctx, cancel := context.WithTimeout(m.ctx, 10*time.Minute)
	defer cancel()
	if err := m.install(ctx, source, candidate, actor); err != nil {
		task.State = "failed"
		switch {
		case errors.Is(err, ErrIncompatible):
			task.Error = "theme_source_incompatible"
		case errors.Is(err, ErrSource):
			task.Error = "theme_source_failed"
		case errors.Is(err, store.ErrConflict):
			task.Error = "theme_source_conflict"
		case errors.Is(err, store.ErrOperatorForbidden):
			task.Error = "forbidden"
		default:
			task.Error = "theme_install_failed"
		}
		m.logger.Error("install GitHub theme", "themeId", source.ID, "commit", task.Commit, "error", err)
	} else {
		task.State = "installed"
	}
	m.mu.Lock()
	m.tasks[source.ID] = task
	m.busy = false
	m.mu.Unlock()
}

func (m *Manager) install(ctx context.Context, source Source, candidate Candidate, actor uuid.UUID) error {
	sourceDir := filepath.Join(m.root, source.ID)
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		return err
	}
	themePath := m.themePath(source, candidate.Commit)
	if _, err := os.Lstat(filepath.Join(sourceDir, candidate.Commit)); errors.Is(err, os.ErrNotExist) {
		if err := m.preparePackage(ctx, sourceDir, source, candidate); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	theme, err := themeregistry.LoadThemeRoot(ctx, m.contractsDir, themePath)
	if err != nil {
		return err
	}
	if theme.SourcePath != themePath || !m.matchesCandidate(theme, candidate, source) {
		return ErrIncompatible
	}
	return m.store.RegisterImportedTheme(ctx, actor, theme)
}

func (m *Manager) preparePackage(ctx context.Context, sourceDir string, source Source, candidate Candidate) error {
	stage, err := os.MkdirTemp(sourceDir, ".incoming-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	checkout := filepath.Join(stage, "checkout")
	home := filepath.Join(stage, "home")
	for _, dir := range []string{checkout, home} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return err
		}
	}
	body, err := m.get(ctx, "codeload.github.com", source.repo+"/tar.gz/"+candidate.Commit)
	if err != nil {
		return err
	}
	extractErr := extractGitHub(body, checkout)
	body.Close()
	if extractErr != nil {
		return fmt.Errorf("%w: %v", ErrIncompatible, extractErr)
	}
	themeRoot := filepath.Join(checkout, filepath.FromSlash(source.Path))
	theme, err := themeregistry.LoadThemeRoot(ctx, m.contractsDir, themeRoot)
	if err != nil || !m.matchesCandidate(theme, candidate, source) {
		return fmt.Errorf("%w: archived manifest or schema does not match: %v", ErrIncompatible, err)
	}
	dependencyRoot, err := npmRoot(checkout, themeRoot)
	if err != nil {
		return err
	}
	if err := buildrunner.InstallThemeDependencies(ctx, dependencyRoot, home); err != nil {
		return fmt.Errorf("install locked npm dependencies: %w", err)
	}
	dest := filepath.Join(sourceDir, candidate.Commit)
	if err := os.Rename(checkout, dest); err != nil {
		// Another process may have placed the exact, fully prepared commit.
		if info, statErr := os.Lstat(dest); statErr != nil || !info.IsDir() {
			return err
		}
	}
	return nil
}

// npmRoot accepts the theme's own lockfile or one from an npm workspace that
// hoists its dependencies. The build runner resolves that same workspace root.
func npmRoot(checkout, themeRoot string) (string, error) {
	manifest, err := os.ReadFile(filepath.Join(themeRoot, "package.json"))
	var pkg struct{ Scripts map[string]string }
	if err != nil || json.Unmarshal(manifest, &pkg) != nil || pkg.Scripts["build"] == "" {
		return "", fmt.Errorf("%w: theme does not provide an npm build script", ErrIncompatible)
	}
	dir := buildrunner.FindWorkspaceRoot(themeRoot)
	rel, err := filepath.Rel(checkout, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("%w: npm workspace must be in the downloaded tree", ErrIncompatible)
	}
	data, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return "", fmt.Errorf("%w: npm package-lock.json is required", ErrIncompatible)
	}
	if err := validateNPMLock(data, dir != themeRoot); err != nil {
		return "", err
	}
	return dir, nil
}
