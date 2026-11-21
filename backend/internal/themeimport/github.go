// Package themeimport installs explicitly trusted GitHub sources into the
// existing theme catalog. A published source version is tied to its commit.
package themeimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/themeregistry"
)

const manifestLimit = 64 << 10

var (
	ErrIncompatible = errors.New("incompatible theme")
	ErrSource       = errors.New("unable to read the configured GitHub source")
	ErrStale        = errors.New("theme candidate changed or its version is already registered")
	ErrBusy         = errors.New("a theme installation is running")
	idPattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}[a-z0-9]$`)
	repoPattern     = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*/[a-zA-Z0-9_.-]+$`)
	branchPattern   = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$`)
	commitPattern   = regexp.MustCompile(`^[a-f0-9]{40}$`)
)

type Source struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
	repo   string
}

func parseSources(raw string) ([]Source, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var sources []Source
	if err := decoder.Decode(&sources); err != nil || len(sources) > 16 {
		return nil, errors.New("invalid GitHub theme source configuration")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("invalid GitHub theme source configuration")
	}
	seen := make(map[string]bool, len(sources))
	for i := range sources {
		source := &sources[i]
		source.repo = strings.TrimSuffix(strings.TrimPrefix(source.URL, "https://github.com/"), ".git")
		_, repo, hasOwner := strings.Cut(source.repo, "/")
		if !strings.HasPrefix(source.URL, "https://github.com/") || !repoPattern.MatchString(source.repo) || !hasOwner || repo == "." || repo == ".." ||
			!idPattern.MatchString(source.ID) || seen[source.ID] {
			return nil, errors.New("theme sources need distinct IDs and public https://github.com/owner/repo links")
		}
		source.URL = "https://github.com/" + source.repo
		if source.Branch == "" {
			source.Branch = "main"
		}
		if source.Path == "" {
			source.Path = "."
		}
		if !branchPattern.MatchString(source.Branch) || strings.Contains(source.Branch, "..") ||
			!validPath(source.Path) {
			return nil, errors.New("invalid GitHub theme branch or theme path")
		}
		seen[source.ID] = true
	}
	return sources, nil
}

func validPath(name string) bool {
	return name == "." || (name != "" && !strings.ContainsAny(name, "\\:\x00") &&
		!strings.HasPrefix(name, "/") && path.Clean(name) == name && name != ".." && !strings.HasPrefix(name, "../"))
}

func githubCommit(ctx context.Context, source Source) (string, error) {
	ref := "refs/heads/" + source.Branch
	cmd := exec.CommandContext(ctx, "git", "-c", "http.followRedirects=false", "-c", "credential.helper=",
		"ls-remote", "--exit-code", "--refs", source.URL+".git", ref)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_PROTOCOL_FROM_USER=0"}
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.Output()
	fields := strings.Fields(string(output))
	if err != nil || len(fields) != 2 || fields[1] != ref || !commitPattern.MatchString(fields[0]) {
		return "", fmt.Errorf("%w: cannot resolve branch (%v)", ErrSource, err)
	}
	return fields[0], nil
}

func (m *Manager) get(ctx context.Context, host, resource string) (io.ReadCloser, error) {
	location := url.URL{Scheme: "https", Host: host, Path: "/" + resource}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := m.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSource, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		if host == "raw.githubusercontent.com" && response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: theme.json is missing", ErrIncompatible)
		}
		return nil, fmt.Errorf("%w: HTTP %d", ErrSource, response.StatusCode)
	}
	return response.Body, nil
}

func (m *Manager) readCandidate(ctx context.Context, source Source, commit string) (Candidate, error) {
	body, err := m.get(ctx, "raw.githubusercontent.com", source.repo+"/"+commit+"/"+path.Join(source.Path, "theme.json"))
	if err != nil {
		return Candidate{}, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, manifestLimit+1))
	if err != nil {
		return Candidate{}, fmt.Errorf("%w: %v", ErrSource, err)
	}
	if len(data) > manifestLimit || m.contracts.ValidateManifest(data) != nil {
		return Candidate{}, ErrIncompatible
	}
	view, err := model.ParseThemeManifest(data)
	if err != nil || view.ID != source.ID || themeregistry.CheckTrustedAdapter(view.Adapter) != nil {
		return Candidate{}, ErrIncompatible
	}
	digest := sha256.Sum256(data)
	return Candidate{Commit: commit, Version: view.Version, ManifestDigest: hex.EncodeToString(digest[:]), State: "available"}, nil
}

func (m *Manager) matchesCandidate(theme model.Theme, candidate Candidate, source Source) bool {
	digest, err := hex.DecodeString(candidate.ManifestDigest)
	return err == nil && theme.ThemeID == source.ID && theme.Version == candidate.Version && bytes.Equal(theme.ManifestDigest, digest)
}

func validateNPMLock(data []byte, workspace bool) error {
	var lock struct {
		LockfileVersion int
		Packages        map[string]struct {
			Resolved  string
			Integrity string
			Link      bool
			InBundle  bool
		}
	}
	if json.Unmarshal(data, &lock) != nil || lock.LockfileVersion != 3 || lock.Packages == nil {
		return fmt.Errorf("%w: npm lockfile v3 is required", ErrIncompatible)
	}
	for name, pkg := range lock.Packages {
		if pkg.Link {
			if !workspace || pkg.Resolved == "." || !validPath(pkg.Resolved) {
				return fmt.Errorf("%w: invalid workspace link", ErrIncompatible)
			}
			if _, ok := lock.Packages[pkg.Resolved]; !ok {
				return fmt.Errorf("%w: undeclared workspace link", ErrIncompatible)
			}
			continue
		}
		if pkg.Resolved == "" {
			if !strings.Contains(name, "node_modules/") {
				continue
			}
			// Bundled entries carry no own digest. Walk to the enclosing
			// dependency whose registry archive carries their bytes.
			parent := name
			for lock.Packages[parent].InBundle && lock.Packages[parent].Resolved == "" {
				cut := strings.LastIndex(parent, "/node_modules/")
				if cut < 0 {
					break
				}
				parent = parent[:cut]
			}
			if parent != name && lock.Packages[parent].Resolved != "" {
				continue
			}
		}
		target, err := url.Parse(pkg.Resolved)
		if err != nil || target.Scheme != "https" || target.Host != "registry.npmjs.org" ||
			target.User != nil || !strings.HasPrefix(pkg.Integrity, "sha512-") {
			return fmt.Errorf("%w: dependency must be integrity-locked to the npm registry", ErrIncompatible)
		}
	}
	return nil
}
