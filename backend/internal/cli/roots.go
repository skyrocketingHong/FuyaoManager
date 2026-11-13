// Deterministic root resolution for serve: the explicit REPO_ROOT wins
// (an operator-configured installation directory — it must exist, but it
// does not need this repository's markers), otherwise the root is discovered
// upward from the working directory by this repository's own markers. An
// undiscoverable root fails loudly instead of guessing another project or
// website.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/config"
)

// isRepoRoot reports whether dir carries this repository's markers: the
// authoritative OpenAPI contract plus the backend module. Any other project
// or website fails the check.
func isRepoRoot(dir string) bool {
	return fileExists(filepath.Join(dir, "contracts", "openapi", "openapi.yaml")) &&
		fileExists(filepath.Join(dir, "backend", "go.mod"))
}

// resolveRepoRoot determines the platform root. An explicitly configured
// REPO_ROOT is operator-owned: any existing directory is accepted (a binary
// installation carries no markers), while a missing path is a configuration
// error. Without a configured root, the working directory and its ancestors
// are searched for the markers — only there the markers are required, so
// starting `go -C backend` resolves the repository root, not backend/.
func resolveRepoRoot(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("%s=%q does not exist: %w", config.EnvRepoRoot, explicit, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s=%q is not a directory", config.EnvRepoRoot, explicit)
		}
		return abs, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for {
		if isRepoRoot(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("this repository's root was not found from the working directory; set %s explicitly", config.EnvRepoRoot)
}

// resolveRealPath resolves a path through symlinks for overlap checking
// without creating anything. The input is normalized to an absolute, cleaned
// path first: relative roots must compare against absolute ones (and
// EvalSymlinks of a relative path would answer in relative form, hiding an
// overlap). A missing tail resolves against the deepest existing ancestor,
// preserving the uncreated remainder so two not-yet-built roots under one
// directory still compare correctly.
func resolveRealPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	path = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real, nil
	}
	probe := path
	rel := ""
	for {
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		rel = filepath.Join(filepath.Base(probe), rel)
		if found, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(found, rel), nil
		}
		probe = parent
	}
	return "", fmt.Errorf("resolve %s: no existing ancestor", path)
}

// ensureDistinctRoots verifies the runtime data roots are mutually
// independent after resolving real paths and symlinks: none may equal or
// live inside another, so a symlink alias can never make one subsystem
// write into another's tree. The check never creates directories — it runs
// before any root is built, resolving missing tails against their deepest
// existing ancestors.
func ensureDistinctRoots(labels []string, paths []string) error {
	resolved := make([]string, len(paths))
	for i, path := range paths {
		real, err := resolveRealPath(path)
		if err != nil {
			return fmt.Errorf("resolve %s root %s: %w", labels[i], path, err)
		}
		resolved[i] = real
	}
	for i := range resolved {
		for j := i + 1; j < len(resolved); j++ {
			if resolved[i] == resolved[j] || within(resolved[i], resolved[j]) || within(resolved[j], resolved[i]) {
				return fmt.Errorf("%s root and %s root overlap after resolving real paths: %s and %s must be independent directories", labels[i], labels[j], paths[i], paths[j])
			}
		}
	}
	return nil
}

// within reports whether child lies strictly inside parent. The upward
// forms are exactly ".." and "../"-prefixed rels — judged precisely, so a
// literal sibling directory named "..foo" is never treated as an escape.
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." {
		return false
	}
	upward := rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator))
	return !upward
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
