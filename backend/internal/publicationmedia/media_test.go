package publicationmedia

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestFreezeBindsMediaBytesToSiteAndBuild(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := filepath.Join(root, "content")
	if err := os.MkdirAll(filepath.Join(content, "albums", "photos"), 0700); err != nil {
		t.Fatal(err)
	}
	photo := filepath.Join(content, "albums", "photos", "photo.jpg")
	if err := os.WriteFile(photo, []byte("frozen-photo"), 0600); err != nil {
		t.Fatal(err)
	}
	siteID, buildID := uuid.New(), uuid.New()
	target := filepath.Join(root, "publication-media")
	digest, err := Freeze(content, target, siteID, buildID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Remove(target) })
	data, err := os.ReadFile(filepath.Join(target, ManifestName))
	if err != nil || digest != Hash(data) {
		t.Fatal("manifest digest mismatch", err)
	}
	var manifest Manifest
	if json.Unmarshal(data, &manifest) != nil || manifest.SiteID != siteID.String() || manifest.BuildID != buildID.String() || len(manifest.Files) != 1 || manifest.Files[0].Path != "albums/photos/photo.jpg" || manifest.Files[0].SHA256 != Hash([]byte("frozen-photo")) {
		t.Fatal("unbound manifest")
	}
	if err := os.WriteFile(photo, []byte("edited-after-build"), 0600); err != nil {
		t.Fatal(err)
	}
	frozen, _ := os.ReadFile(filepath.Join(target, "albums", "photos", "photo.jpg"))
	if string(frozen) != "frozen-photo" {
		t.Fatal("snapshot follows editable source")
	}
	for _, path := range Paths {
		if info, err := os.Stat(filepath.Join(target, filepath.FromSlash(path))); err != nil || !info.IsDir() || info.Mode().Perm()&0222 != 0 {
			t.Fatal("missing or writable media directory", path, err)
		}
	}
	if _, err := Freeze(content, target, siteID, buildID); err == nil {
		t.Fatal("overwrote frozen snapshot")
	}
}

func TestFreezeRefusesSymlinksAndHardlinks(t *testing.T) {
	for _, kind := range []string{"file-symlink", "directory-symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			content := filepath.Join(root, "content")
			if err := os.MkdirAll(filepath.Join(content, "images"), 0700); err != nil {
				t.Fatal(err)
			}
			secret := filepath.Join(root, "outside.txt")
			if err := os.WriteFile(secret, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(content, "images", "input")
			switch kind {
			case "file-symlink":
				err = os.Symlink(secret, target)
			case "directory-symlink":
				err = os.Symlink(root, target)
			case "hardlink":
				err = os.Link(secret, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "frozen")
			if _, err := Freeze(content, output, uuid.New(), uuid.New()); err == nil {
				t.Fatal("unsafe source accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed snapshot left output")
			}
		})
	}
}

func TestDescriptorCopyRefusesSymlinkAncestor(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(t.Context(), alias, filepath.Join(root, "copy"), uint32(os.Geteuid())); err == nil {
		t.Fatal("ancestor symlink followed")
	}
}
