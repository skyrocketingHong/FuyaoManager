package contentbind

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

var postMediaReference = regexp.MustCompile(`/wp-content/uploads/[^\s<>"']+`)

// ReadPostMedia keeps generated WordPress-size links working when the
// original is present but that particular derivative is absent.
func ReadPostMedia(ctx context.Context, legacy *content.Store, media *store.PostMediaStore, scope uuid.UUID, path string) ([]byte, string, error) {
	if err := content.CheckPostMediaPath(path); err != nil {
		return nil, "", err
	}
	data, kind, err := media.Get(ctx, scope, path)
	if !errors.Is(err, content.ErrNotFound) {
		return data, kind, err
	}
	if legacy.Posts != nil {
		if original, ok := content.WordPressOriginalPath(path); ok {
			return media.Get(ctx, scope, original)
		}
		return nil, "", content.ErrNotFound
	}
	data, kind, err = legacy.ReadPostMedia(scope, path)
	if errors.Is(err, content.ErrNotFound) {
		if original, ok := content.WordPressOriginalPath(path); ok {
			data, kind, err = media.Get(ctx, scope, original)
			if errors.Is(err, content.ErrNotFound) {
				data, kind, err = legacy.ReadPostMedia(scope, original)
				if err == nil {
					err = media.Put(ctx, scope, original, data)
				}
			}
		}
	}
	if err != nil {
		return nil, "", err
	}
	if err := media.Put(ctx, scope, path, data); err != nil {
		return nil, "", err
	}
	return data, kind, nil
}

// MaterializeWithMedia resolves the posts scope and freezes its database media
// alongside the selected content sources in the task-private input directory.
func MaterializeWithMedia(ctx context.Context, local, platform *content.Store, media *store.PostMediaStore, id uuid.UUID, sources store.ContentSources, target string) (string, string, string, error) {
	if !sources.Valid() {
		return "", "", "", fmt.Errorf("invalid frozen content source selection")
	}
	legacy, scope := local, id
	if sources.Posts == "platform" {
		legacy, scope = platform, content.PlatformScopeID
	}
	if legacy == nil {
		return "", "", "", content.ErrNotFound
	}
	if err := SyncPostMedia(ctx, legacy, media, scope); err != nil {
		return "", "", "", err
	}
	var mediaPaths map[string]struct{}
	if legacy.Posts != nil {
		items, err := media.List(ctx, scope)
		if err != nil {
			return "", "", "", err
		}
		mediaPaths = make(map[string]struct{}, len(items))
		for _, item := range items {
			mediaPaths[item.Path] = struct{}{}
		}
	}
	root, config, favicon, err := materialize(local, platform, id, sources, target, legacy.Posts != nil, mediaPaths)
	if err != nil {
		return "", "", "", err
	}
	if legacy.Posts != nil {
		if err := legacy.Posts.ExportPosts(ctx, scope, filepath.Join(root, "posts")); err != nil {
			_ = os.RemoveAll(target)
			return "", "", "", err
		}
	}
	if err := ExportPostMedia(ctx, media, scope, root); err != nil {
		_ = os.RemoveAll(target)
		return "", "", "", err
	}
	return root, config, favicon, nil
}

// SyncPostMedia imports legacy scoped images before cutover and materializes
// database-backed WordPress size aliases for referenced images. Repeated calls
// never overwrite different database bytes.
func SyncPostMedia(ctx context.Context, legacy *content.Store, media *store.PostMediaStore, scope uuid.UUID) error {
	existing, err := media.List(ctx, scope)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		seen[item.Path] = struct{}{}
	}
	if legacy.Posts == nil {
		items, err := legacy.ListPostMedia(scope)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, ok := seen[item.Path]; ok {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			data, _, err := legacy.ReadPostMedia(scope, item.Path)
			if err != nil {
				return fmt.Errorf("read post media %s: %w", item.Path, err)
			}
			if err := media.Put(ctx, scope, item.Path, data); err != nil {
				return fmt.Errorf("store post media %s: %w", item.Path, err)
			}
			seen[item.Path] = struct{}{}
		}
	}
	posts, _, err := legacy.ListPosts(scope, "", 1, int(^uint(0)>>1))
	if errors.Is(err, content.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, post := range posts {
		for _, reference := range postMediaReference.FindAllString(post.Body+" "+post.Cover, -1) {
			reference = strings.TrimRight(reference, ").],;")
			reference, _, _ = strings.Cut(reference, "?")
			reference, _, _ = strings.Cut(reference, "#")
			decoded, err := url.PathUnescape(reference)
			if err != nil {
				continue
			}
			if _, exists := seen[decoded]; exists {
				continue
			}
			original, ok := content.WordPressOriginalPath(decoded)
			if !ok {
				continue
			}
			data, _, err := media.Get(ctx, scope, original)
			if errors.Is(err, content.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := media.Put(ctx, scope, decoded, data); err != nil {
				return err
			}
			seen[decoded] = struct{}{}
		}
	}
	return nil
}

// VerifyLegacyPostMedia detects conflicting bytes before the old files cease
// to be the authority. Existing database images are never overwritten.
func VerifyLegacyPostMedia(ctx context.Context, legacy *content.Store, media *store.PostMediaStore, scope uuid.UUID) error {
	items, err := legacy.ListPostMedia(scope)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		original, _, err := legacy.ReadPostMedia(scope, item.Path)
		if err != nil {
			return err
		}
		stored, _, err := media.Get(ctx, scope, item.Path)
		if err != nil {
			return err
		}
		if !bytes.Equal(original, stored) {
			return fmt.Errorf("legacy post media conflicts with database: %s", item.Path)
		}
	}
	return nil
}

// ExportPostMedia writes database-owned images into a private build snapshot.
func ExportPostMedia(ctx context.Context, media *store.PostMediaStore, scope uuid.UUID, root string) error {
	return media.Visit(ctx, scope, func(path string, data []byte) error {
		if _, err := content.ValidatePostMedia(path, data); err != nil {
			return err
		}
		filename := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
		if err := os.MkdirAll(filepath.Dir(filename), 0750); err != nil {
			return err
		}
		file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if errors.Is(err, os.ErrExist) {
			info, statErr := os.Lstat(filename)
			if statErr != nil || !info.Mode().IsRegular() {
				return content.ErrInvalidPath
			}
			previous, readErr := os.ReadFile(filename)
			if readErr != nil {
				return readErr
			}
			if !bytes.Equal(previous, data) {
				return fmt.Errorf("post media path conflict: %s", path)
			}
			return nil
		}
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	})
}
