// Package contentmigration imports trusted operator-selected content into
// independent scopes. It never rewrites source data or publication pointers.
package contentmigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

var ErrDestinationExists = errors.New("content destination already exists")
var ErrSourceChanged = errors.New("source content changed while importing")
var ErrUnsafePath = errors.New("content import requires independent directories without symlinks")

const maxImportBytes int64 = 10 << 30

type Options struct {
	SourceConfig  string
	SourceContent string
	SourceFavicon string
	PlatformRoot  string
	SitesRoot     string
	SiteID        uuid.UUID
}
type Target struct {
	ScopeID uuid.UUID `json:"scopeId"`
	Path    string    `json:"path"`
	Files   int       `json:"files"`
	Bytes   int64     `json:"bytes"`
	Digest  string    `json:"digest"`
}
type Report struct {
	Targets []Target `json:"targets"`
}
type source struct {
	path, relative string
	optional       bool
}
type manifest struct {
	hashes map[string]string
	bytes  int64
}

// Import creates complete, independent scope directories. Each destination
// installs by rename; if any later install fails, only this call's installs
// are removed. Existing directories, live symlinks and source trees stay intact.
func Import(ctx context.Context, options Options) (Report, error) {
	var report Report
	var roots []string
	for _, value := range []string{options.SourceConfig, options.SourceContent, options.SourceFavicon} {
		root, err := sourceRoot(value)
		if err != nil {
			return report, err
		}
		roots = append(roots, root)
	}
	inputs := []source{{filepath.Join(roots[0], "site.yaml"), "config/site.yaml", false}, {filepath.Join(roots[0], "content.yaml"), "config/content.yaml", false}, {roots[2], "config/favicon", false}}
	for _, name := range []string{"posts", "data", "albums", "images", "wp-content"} {
		inputs = append(inputs, source{filepath.Join(roots[1], name), "content/" + name, true})
	}
	var targets []Target
	if options.PlatformRoot != "" {
		targets = append(targets, Target{ScopeID: content.PlatformScopeID, Path: filepath.Join(options.PlatformRoot, content.PlatformScopeID.String())})
	}
	if options.SitesRoot != "" && options.SiteID != uuid.Nil && options.SiteID != content.PlatformScopeID {
		targets = append(targets, Target{ScopeID: options.SiteID, Path: filepath.Join(options.SitesRoot, options.SiteID.String())})
	} else if options.SitesRoot != "" || options.SiteID != uuid.Nil {
		return report, ErrUnsafePath
	}
	if len(targets) == 0 {
		return report, ErrUnsafePath
	}
	for i := range targets {
		absolute, err := canonicalDestination(targets[i].Path)
		if err != nil {
			return report, err
		}
		targets[i].Path = absolute
		for _, root := range roots {
			if overlaps(root, absolute) {
				return report, ErrUnsafePath
			}
		}
		for j := 0; j < i; j++ {
			if overlaps(targets[j].Path, absolute) {
				return report, ErrUnsafePath
			}
		}
		if _, err := os.Lstat(absolute); err == nil {
			return report, ErrDestinationExists
		} else if !os.IsNotExist(err) {
			return report, err
		}
		if err := ensureParents(filepath.Dir(absolute)); err != nil {
			return report, err
		}
	}
	stages := make([]string, len(targets))
	installed := []string{}
	success := false
	defer func() {
		for _, p := range stages {
			if p != "" {
				_ = os.RemoveAll(p)
			}
		}
		if !success {
			for _, p := range installed {
				_ = os.RemoveAll(p)
			}
		}
	}()
	var expected manifest
	for i, target := range targets {
		stage, err := os.MkdirTemp(filepath.Dir(target.Path), ".content-import-")
		if err != nil {
			return report, err
		}
		stages[i] = stage
		if err := os.Chmod(stage, 0750); err != nil {
			return report, err
		}
		m := manifest{hashes: map[string]string{}}
		for _, input := range inputs {
			if err := copySource(ctx, input, stage, &m); err != nil {
				return report, err
			}
		}
		if i == 0 {
			expected = m
		} else if m.digest() != expected.digest() {
			return report, ErrSourceChanged
		}
		targets[i].Files, targets[i].Bytes, targets[i].Digest = len(m.hashes), m.bytes, m.digest()
	}
	check := manifest{hashes: map[string]string{}}
	for _, input := range inputs {
		if err := copySource(ctx, input, "", &check); err != nil {
			return report, err
		}
	}
	if check.digest() != expected.digest() {
		return report, ErrSourceChanged
	}
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if _, err := os.Lstat(target.Path); err == nil {
			return report, ErrDestinationExists
		} else if !os.IsNotExist(err) {
			return report, err
		}
		if err := installDirectory(stages[i], target.Path); err != nil {
			if errors.Is(err, os.ErrExist) {
				return report, ErrDestinationExists
			}
			return report, err
		}
		stages[i] = ""
		installed = append(installed, target.Path)
	}
	success = true
	report.Targets = targets
	return report, nil
}

func sourceRoot(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrUnsafePath
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrUnsafePath
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return real, nil
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
func ensureParents(path string) error {
	if err := os.MkdirAll(path, 0750); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// System prefixes such as macOS /var may resolve before the supplied
	// root; directory-level escapes are still rejected by the caller's root.
	if real != absolute {
		return ErrUnsafePath
	}
	return nil
}
func copySource(ctx context.Context, input source, destination string, m *manifest) error {
	if _, err := os.Lstat(input.path); os.IsNotExist(err) && input.optional {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(input.path, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		rel, err := filepath.Rel(input.path, path)
		if err != nil {
			return err
		}
		name := input.relative
		if rel != "." {
			name = filepath.Join(name, rel)
		}
		if entry.IsDir() {
			if destination != "" {
				return os.MkdirAll(filepath.Join(destination, name), 0750)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return ErrUnsafePath
		}
		if info.Size() > maxImportBytes-m.bytes {
			return errors.New("content import exceeds the size limit")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) {
			return ErrSourceChanged
		}
		hash := sha256.New()
		writer := io.Writer(hash)
		var output *os.File
		if destination != "" {
			target := filepath.Join(destination, name)
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return err
			}
			output, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return err
			}
			defer output.Close()
			writer = io.MultiWriter(hash, output)
		}
		n, err := io.Copy(writer, io.LimitReader(file, maxImportBytes-m.bytes+1))
		if err != nil {
			return err
		}
		if n != info.Size() || n > maxImportBytes-m.bytes {
			return ErrSourceChanged
		}
		after, err := file.Stat()
		if err != nil || !info.ModTime().Equal(after.ModTime()) || info.Size() != after.Size() {
			return ErrSourceChanged
		}
		if output != nil {
			if err := output.Sync(); err != nil {
				return err
			}
			if err := output.Close(); err != nil {
				return err
			}
		}
		m.hashes[filepath.ToSlash(name)] = hex.EncodeToString(hash.Sum(nil))
		m.bytes += n
		return nil
	})
}
func (m manifest) digest() string {
	keys := make([]string, 0, len(m.hashes))
	for key := range m.hashes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", key, m.hashes[key])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func canonicalDestination(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrUnsafePath
	}
	current := path
	tail := []string{}
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", ErrUnsafePath
			}
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(tail) - 1; i >= 0; i-- {
				real = filepath.Join(real, tail[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		tail = append(tail, filepath.Base(current))
		current = filepath.Dir(current)
	}
}
