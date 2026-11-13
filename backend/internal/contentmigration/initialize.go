package contentmigration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

// InitializePlatform creates an empty library on a fresh installation. An
// existing scope is retained without seeding or overwriting any of its files.
func InitializePlatform(root string) error {
	target, err := canonicalDestination(filepath.Join(root, content.PlatformScopeID.String()))
	if err != nil {
		return err
	}
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ensureParents(filepath.Dir(target)); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".content-initialize-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0750); err != nil {
		return err
	}
	for _, dir := range []string{"content/posts", "content/data", "content/albums/photos", "content/albums/thumbnails", "content/albums/metadata", "content/images", "content/wp-content", "config/favicon"} {
		if err := os.MkdirAll(filepath.Join(stage, dir), 0750); err != nil {
			return err
		}
	}
	index, err := json.Marshal(map[string]any{"schemaVersion": 1, "generatedAt": time.Now().UTC().Format(time.RFC3339), "totalPhotos": 0, "years": []int{}, "devices": []any{}})
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"content/data/friends.yaml": []byte("online: []\noffline: []\n"), "content/data/footprints.yaml": []byte("cities: []\n"), "content/data/social-links.yaml": []byte("[]\n"), "content/data/payments.yaml": []byte("[]\n"), "content/albums/metadata/index.json": index, "config/content.yaml": []byte("schemaVersion: 1\npaths:\n  posts: posts\n  data: data\n  albumPhotos: albums/photos\n  albumThumbnails: albums/thumbnails\n  albumMetadata: albums/metadata\nmedia:\n  mode: external\n  albumPublicBase: /media/albums\n")} {
		if err := os.WriteFile(filepath.Join(stage, name), data, 0600); err != nil {
			return err
		}
	}
	if err := installDirectory(stage, target); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return nil
}
