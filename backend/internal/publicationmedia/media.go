// Package publicationmedia freezes the four externally served media trees
// beside a build. Its manifest binds those exact bytes to a site and build;
// publication never rereads a site's editable content.
package publicationmedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/google/uuid"
)

const ManifestName = "manifest.json"
const MaxManifestBytes = 8 << 20
const MaxFiles = 25000
const MaxTreeBytes int64 = 512 << 20
const MaxFileBytes int64 = 64 << 20

var ErrUnsafe = errors.New("publication media is unsafe or exceeds its limits")

var Paths = []string{"images", "wp-content", "albums/photos", "albums/thumbnails"}

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Version int    `json:"version"`
	SiteID  string `json:"siteId"`
	BuildID string `json:"buildId"`
	Files   []File `json:"files"`
}

func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func Freeze(contentRoot, target string, siteID, buildID uuid.UUID) (string, error) {
	if siteID == uuid.Nil || buildID == uuid.Nil {
		return "", ErrUnsafe
	}
	dir, err := OpenDirectory(contentRoot)
	if err != nil {
		return "", err
	}
	_ = dir.Close()
	if err := os.Mkdir(target, 0700); err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = Remove(target)
		}
	}()
	for _, relative := range Paths {
		destination := filepath.Join(target, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			return "", err
		}
		source := filepath.Join(contentRoot, filepath.FromSlash(relative))
		if _, err := os.Lstat(source); os.IsNotExist(err) {
			if err := os.Mkdir(destination, 0700); err != nil {
				return "", err
			}
			continue
		} else if err != nil {
			return "", err
		}
		if err := CopyTree(context.Background(), source, destination, uint32(os.Geteuid())); err != nil {
			return "", err
		}
	}
	manifest, err := Inventory(target, siteID.String(), buildID.String())
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if len(data) > MaxManifestBytes {
		return "", ErrUnsafe
	}
	if err := os.WriteFile(filepath.Join(target, ManifestName), data, 0444); err != nil {
		return "", err
	}
	if err := FreezeTree(target); err != nil {
		return "", err
	}
	complete = true
	return Hash(data), nil
}

// Inventory runs only over a private copied tree, never an untrusted live
// source. CopyTree performs the descriptor-based trust checks first.
func Inventory(root, siteID, buildID string) (Manifest, error) {
	manifest := Manifest{Version: 1, SiteID: siteID, BuildID: buildID, Files: []File{}}
	var total int64
	for _, relative := range Paths {
		base := filepath.Join(root, filepath.FromSlash(relative))
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return ErrUnsafe
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return ErrUnsafe
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Nlink != 1 || info.Size() > MaxFileBytes {
				return ErrUnsafe
			}
			total += info.Size()
			if total > MaxTreeBytes || len(manifest.Files) >= MaxFiles {
				return ErrUnsafe
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			manifest.Files = append(manifest.Files, File{filepath.ToSlash(name), int64(len(data)), Hash(data)})
			return nil
		})
		if err != nil {
			return Manifest{}, err
		}
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return manifest, nil
}

func FreezeTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrUnsafe
		}
		mode := os.FileMode(0444)
		if entry.IsDir() {
			mode = 0555
		} else if !entry.Type().IsRegular() {
			return ErrUnsafe
		}
		return os.Chmod(path, mode)
	})
}

func Remove(root string) error {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

func SafeName(value string) bool {
	return value != "" && value != "." && value != ".." && utf8.ValidString(value) && !strings.ContainsAny(value, "/\\\x00\r\n")
}
