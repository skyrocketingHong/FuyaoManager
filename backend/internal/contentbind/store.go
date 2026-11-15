// Package contentbind resolves each site's explicit content-source choices.
// Configuration stays site-owned; shared content is edited only through the
// platform surface. Builds materialize independent, immutable input copies.
package contentbind

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type Store struct {
	*content.Store
	Platform *content.Store
	Resolve  func(context.Context, uuid.UUID) (store.ContentSourceRecord, error)
}

func (s *Store) target(id uuid.UUID, category string, write bool) (*content.Store, uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rec, err := s.Resolve(ctx, id)
	if err != nil {
		return nil, id, err
	}
	if rec.Sources.Source(category) == "platform" {
		if write {
			return nil, id, content.ErrShared
		}
		if s.Platform == nil {
			return nil, id, content.ErrNotFound
		}
		return s.Platform, content.PlatformScopeID, nil
	}
	return s.Store, id, nil
}
func (s *Store) RequireContentWrite(ctx context.Context, id uuid.UUID, category string) error {
	rec, err := s.Resolve(ctx, id)
	if err != nil {
		return err
	}
	if rec.Sources.Source(category) == "platform" {
		return content.ErrShared
	}
	return nil
}
func (s *Store) ListPosts(id uuid.UUID, q string, p, n int, order ...content.PostSort) ([]content.Post, int, error) {
	t, k, e := s.target(id, "posts", false)
	if e != nil {
		return nil, 0, e
	}
	return t.ListPosts(k, q, p, n, order...)
}

// GetPostTaxonomy forwards the blog taxonomy read to the scope bound to
// the site's posts source. The explicit method overrides the embedded
// content.Store promotion: without it, a site sourcing its posts from the
// platform would read its own (nonexistent) local directories instead of
// the platform library.
func (s *Store) GetPostTaxonomy(id uuid.UUID) (content.PostTaxonomy, error) {
	t, k, e := s.target(id, "posts", false)
	if e != nil {
		return content.PostTaxonomy{}, e
	}
	return t.GetPostTaxonomy(k)
}

func (s *Store) GetPostVocabulary(id uuid.UUID) (content.PostVocabulary, string, error) {
	t, k, e := s.target(id, "posts", false)
	if e != nil {
		return content.PostVocabulary{}, "", e
	}
	return t.GetPostVocabulary(k)
}

func (s *Store) SavePostVocabulary(id uuid.UUID, doc content.PostVocabulary, digest string) (content.PostVocabulary, string, error) {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return content.PostVocabulary{}, "", e
	}
	return t.SavePostVocabulary(k, doc, digest)
}

func (s *Store) RenamePostTag(id uuid.UUID, from string, to *string, digest string) (int, content.PostVocabulary, string, error) {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return 0, content.PostVocabulary{}, "", e
	}
	return t.RenamePostTag(k, from, to, digest)
}

func (s *Store) GetPostDirectory(id uuid.UUID, slug string) (content.PostDirectoryDocument, string, error) {
	t, k, e := s.target(id, "posts", false)
	if e != nil {
		return content.PostDirectoryDocument{}, "", e
	}
	return t.GetPostDirectory(k, slug)
}

func (s *Store) SavePostDirectory(id uuid.UUID, slug, title, digest string) (content.PostDirectoryDocument, string, error) {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return content.PostDirectoryDocument{}, "", e
	}
	return t.SavePostDirectory(k, slug, title, digest)
}

func (s *Store) DeletePostCategory(id uuid.UUID, slug, digest string) error {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return e
	}
	return t.DeletePostCategory(k, slug, digest)
}

func (s *Store) ListPostMedia(id uuid.UUID) ([]content.PostMedia, error) {
	t, k, err := s.target(id, "posts", false)
	if err != nil {
		return nil, err
	}
	return t.ListPostMedia(k)
}

// PostMediaTarget resolves the physical legacy source and database scope together.
func (s *Store) PostMediaTarget(id uuid.UUID, write bool) (*content.Store, uuid.UUID, error) {
	return s.target(id, "posts", write)
}

func (s *Store) ReadPostMedia(id uuid.UUID, path string) ([]byte, string, error) {
	t, k, err := s.target(id, "posts", false)
	if err != nil {
		return nil, "", err
	}
	return t.ReadPostMedia(k, path)
}

func (s *Store) StorePostMedia(id uuid.UUID, path string, reader io.Reader, size int64) (content.PostMedia, error) {
	t, k, err := s.target(id, "posts", true)
	if err != nil {
		return content.PostMedia{}, err
	}
	return t.StorePostMedia(k, path, reader, size)
}
func (s *Store) GetPost(id uuid.UUID, path string) (content.Post, error) {
	t, k, e := s.target(id, "posts", false)
	if e != nil {
		return content.Post{}, e
	}
	return t.GetPost(k, path)
}
func (s *Store) CreatePost(id uuid.UUID, p content.Post) (content.Post, error) {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return content.Post{}, e
	}
	return t.CreatePost(k, p)
}
func (s *Store) SavePost(id uuid.UUID, p content.Post, d string) (content.Post, error) {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return content.Post{}, e
	}
	return t.SavePost(k, p, d)
}
func (s *Store) DeletePost(id uuid.UUID, p, d string) error {
	t, k, e := s.target(id, "posts", true)
	if e != nil {
		return e
	}
	return t.DeletePost(k, p, d)
}
func (s *Store) GetFriends(id uuid.UUID) (content.FriendsDoc, string, error) {
	t, k, e := s.target(id, "friends", false)
	if e != nil {
		return content.FriendsDoc{}, "", e
	}
	return t.GetFriends(k)
}
func (s *Store) SaveFriends(id uuid.UUID, p content.FriendsDoc, d string) (content.FriendsDoc, string, error) {
	t, k, e := s.target(id, "friends", true)
	if e != nil {
		return content.FriendsDoc{}, "", e
	}
	return t.SaveFriends(k, p, d)
}
func (s *Store) GetFootprints(id uuid.UUID) (content.FootprintsDoc, string, error) {
	t, k, e := s.target(id, "footprints", false)
	if e != nil {
		return content.FootprintsDoc{}, "", e
	}
	return t.GetFootprints(k)
}
func (s *Store) SaveFootprints(id uuid.UUID, p content.FootprintsDoc, d string) (content.FootprintsDoc, string, error) {
	t, k, e := s.target(id, "footprints", true)
	if e != nil {
		return content.FootprintsDoc{}, "", e
	}
	return t.SaveFootprints(k, p, d)
}
func (s *Store) GetAlbumIndex(id uuid.UUID) (content.AlbumIndex, string, error) {
	t, k, e := s.target(id, "albums", false)
	if e != nil {
		return content.AlbumIndex{}, "", e
	}
	return t.GetAlbumIndex(k)
}
func (s *Store) GetAlbumYear(id uuid.UUID, y string) (content.AlbumYear, string, error) {
	t, k, e := s.target(id, "albums", false)
	if e != nil {
		return content.AlbumYear{}, "", e
	}
	return t.GetAlbumYear(k, y)
}
func (s *Store) SaveAlbumYear(id uuid.UUID, y, d string, edit func(*content.AlbumYear) error) (content.AlbumYear, string, error) {
	t, k, e := s.target(id, "albums", true)
	if e != nil {
		return content.AlbumYear{}, "", e
	}
	return t.SaveAlbumYear(k, y, d, edit)
}
func (s *Store) StoreUpload(id uuid.UUID, r io.Reader, n int64) (string, error) {
	t, k, e := s.target(id, "albums", true)
	if e != nil {
		return "", e
	}
	return t.StoreUpload(k, r, n)
}
func (s *Store) DeleteAlbumPhoto(id uuid.UUID, y, f string, refs [][]byte, d string) error {
	t, k, e := s.target(id, "albums", true)
	if e != nil {
		return e
	}
	return t.DeleteAlbumPhoto(k, y, f, refs, d)
}
func (s *Store) DetachAlbumPhoto(id uuid.UUID, y, f, d string) error {
	t, k, e := s.target(id, "albums", true)
	if e != nil {
		return e
	}
	return t.DetachAlbumPhoto(k, y, f, d)
}
