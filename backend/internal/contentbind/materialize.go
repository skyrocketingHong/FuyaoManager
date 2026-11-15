package contentbind

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// Materialize copies the selected sources into one private task directory.
// Theme prepare may regenerate metadata only in this copy, never the library.
func Materialize(local, platform *content.Store, id uuid.UUID, sources store.ContentSources, target string) (string, string, string, error) {
	if !sources.Valid() {
		return "", "", "", fmt.Errorf("invalid frozen content source selection")
	}
	if _, err := os.Lstat(target); err == nil {
		return "", "", "", fmt.Errorf("input target exists")
	}
	if err := os.MkdirAll(target, 0750); err != nil {
		return "", "", "", err
	}
	contentRoot := filepath.Join(target, "content")
	configRoot := filepath.Join(target, "config")
	fail := func(err error) (string, string, string, error) { _ = os.RemoveAll(target); return "", "", "", err }
	if err := copyTree(local.ContentRoot(id), contentRoot, false); err != nil {
		return fail(err)
	}
	if err := copyTree(local.ConfigRoot(id), configRoot, false); err != nil {
		return fail(err)
	}
	for _, entry := range []struct{ category, path string }{{"posts", "posts"}, {"albums", "albums"}, {"friends", "data/friends.yaml"}, {"footprints", "data/footprints.yaml"}} {
		if sources.Source(entry.category) != "platform" {
			continue
		}
		if platform == nil {
			return fail(content.ErrNotFound)
		}
		dest := filepath.Join(contentRoot, entry.path)
		if err := os.RemoveAll(dest); err != nil {
			return fail(err)
		}
		if err := copyTree(filepath.Join(platform.ContentRoot(content.PlatformScopeID), entry.path), dest, false); err != nil {
			return fail(err)
		}
	}
	if platform != nil && (sources.Posts == "platform" || sources.Albums == "platform" || sources.Friends == "platform" || sources.Footprints == "platform") {
		if sources.Posts == "platform" {
			if err := checkPostMediaConflicts(filepath.Join(platform.ContentRoot(content.PlatformScopeID), "wp-content", "uploads"), filepath.Join(contentRoot, "wp-content", "uploads")); err != nil {
				return fail(err)
			}
		}
		for _, path := range []string{"images", "wp-content"} {
			if err := copyTree(filepath.Join(platform.ContentRoot(content.PlatformScopeID), path), filepath.Join(contentRoot, path), true); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fail(err)
			}
		}
	}
	return contentRoot, configRoot, filepath.Join(configRoot, "favicon"), nil
}

// Different bytes at a shared post's public URL must never silently resolve
// to a site-owned file during publication.
func checkPostMediaConflicts(source, target string) error {
	parent, err := os.Lstat(filepath.Dir(source))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !parent.IsDir() || parent.Mode()&os.ModeSymlink != 0 {
		return content.ErrInvalidPath
	}
	if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return content.ErrInvalidPath
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return content.ErrInvalidPath
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		other := filepath.Join(target, rel)
		info, err := os.Lstat(other)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return content.ErrInvalidPath
		}
		same, err := equalMediaFiles(current, other)
		if err != nil {
			return err
		}
		if !same {
			return fmt.Errorf("post media path conflict: /wp-content/uploads/%s", filepath.ToSlash(rel))
		}
		return nil
	})
}

func equalMediaFiles(a, b string) (bool, error) {
	first, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer first.Close()
	second, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer second.Close()
	left, right := sha256.New(), sha256.New()
	if _, err := io.Copy(left, first); err != nil {
		return false, err
	}
	if _, err := io.Copy(right, second); err != nil {
		return false, err
	}
	return string(left.Sum(nil)) == string(right.Sum(nil)), nil
}
func copyTree(source, target string, keep bool) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return content.ErrInvalidPath
	}
	if !info.IsDir() {
		return copyFile(source, target, keep)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel != "." && strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return content.ErrInvalidPath
		}
		dest := filepath.Join(target, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0750)
		}
		if !entry.Type().IsRegular() {
			return content.ErrInvalidPath
		}
		return copyFile(path, dest, keep)
	})
}
func copyFile(source, target string, keep bool) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return content.ErrInvalidPath
	}
	if keep {
		if _, err := os.Lstat(target); err == nil {
			return nil
		}
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0640)
}
