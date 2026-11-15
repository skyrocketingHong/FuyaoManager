package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

type contentTarget struct {
	ID      uuid.UUID
	CanEdit bool
}

type postContentDTO struct {
	content.Post
	CanEdit bool `json:"canEdit"`
}

func (a *API) contentTarget(w http.ResponseWriter, r *http.Request, auth store.AuthSession, write bool) (contentTarget, bool) {
	var target contentTarget
	if a.platformScope {
		target = contentTarget{ID: content.PlatformScopeID, CanEdit: auth.User.PlatformRole == model.PlatformRoleAdmin}
	} else {
		id, ok := parseSiteID(w, r)
		if !ok {
			return target, false
		}
		rec, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
		if err != nil {
			writeSiteLookupError(w, err)
			return target, false
		}
		target = contentTarget{ID: id, CanEdit: rec.Role == model.RoleOwner || rec.Role == model.RoleEditor}
		if write && !target.CanEdit {
			writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may change site content.")
			return target, false
		}
		if category := contentCategory(r.URL.Path); category != "" {
			if guard, ok := a.content.(interface {
				RequireContentWrite(context.Context, uuid.UUID, string) error
			}); ok {
				if err := guard.RequireContentWrite(r.Context(), id, category); err != nil {
					if !errors.Is(err, content.ErrShared) || write {
						writeContentError(w, err, "Cannot resolve the content source.")
						return target, false
					}
					target.CanEdit = false
				}
			}
		}
	}
	if write && !target.CanEdit {
		writeProblem(w, http.StatusForbidden, "forbidden", "The current account may read this content but cannot change it.")
		return target, false
	}
	return target, true
}

func (a *API) platformContentRoute(handler func(*API, http.ResponseWriter, *http.Request, store.AuthSession)) authedHandler {
	return func(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
		if a.cfg.PlatformContent == nil {
			writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "The platform content library is not configured.")
			return
		}
		copy := *a
		copy.content = a.cfg.PlatformContent
		copy.platformScope = true
		handler(&copy, w, r, auth)
	}
}

func (a *API) platformContentRoutes() []routeSpec {
	read := func(h func(*API, http.ResponseWriter, *http.Request, store.AuthSession)) http.HandlerFunc {
		return a.requireFullScope(a.platformContentRoute(h))
	}
	write := func(h func(*API, http.ResponseWriter, *http.Request, store.AuthSession)) http.HandlerFunc {
		return a.requireFullScope(a.withCSRF(a.platformContentRoute(h)))
	}
	return []routeSpec{
		{"GET", "/api/v1/content/posts", "platformListPosts", read((*API).handleListPosts)},
		{"GET", "/api/v1/content/posts/taxonomy", "platformGetPostsTaxonomy", read((*API).handleGetPostsTaxonomy)},
		{"GET", "/api/v1/content/posts/vocabulary", "platformGetPostVocabulary", read((*API).handleGetPostVocabulary)},
		{"PUT", "/api/v1/content/posts/vocabulary", "platformPutPostVocabulary", write((*API).handlePutPostVocabulary)},
		{"GET", "/api/v1/content/posts/directories/{slug}", "platformGetPostDirectory", read((*API).handleGetPostDirectory)},
		{"PUT", "/api/v1/content/posts/directories/{slug}", "platformPutPostDirectory", write((*API).handlePutPostDirectory)},
		{"GET", "/api/v1/content/posts/media", "platformListPostMedia", read((*API).handleListPostMedia)},
		{"GET", "/api/v1/content/posts/media/file", "platformGetPostMedia", read((*API).handleGetPostMedia)},
		{"POST", "/api/v1/content/posts/media", "platformImportPostMedia", write((*API).handleImportPostMedia)},
		{"POST", "/api/v1/content/posts", "platformCreatePost", write((*API).handleCreatePost)},
		{"GET", "/api/v1/content/posts/{postPath}", "platformGetPost", read((*API).handleGetPost)},
		{"PUT", "/api/v1/content/posts/{postPath}", "platformPutPost", write((*API).handlePutPost)},
		{"DELETE", "/api/v1/content/posts/{postPath}", "platformDeletePost", write((*API).handleDeletePost)},
		{"GET", "/api/v1/content/friends", "platformGetFriends", read((*API).handleGetFriends)},
		{"PUT", "/api/v1/content/friends", "platformPutFriends", write((*API).handlePutFriends)},
		{"GET", "/api/v1/content/footprints", "platformGetFootprints", read((*API).handleGetFootprints)},
		{"PUT", "/api/v1/content/footprints", "platformPutFootprints", write((*API).handlePutFootprints)},
		{"GET", "/api/v1/content/albums", "platformGetAlbums", read((*API).handleGetAlbums)},
		{"POST", "/api/v1/content/albums/photos", "platformUploadAlbumPhoto", write((*API).handleUploadAlbumPhoto)},
		{"POST", "/api/v1/content/albums/process", "platformProcessAlbums", write((*API).handleEnqueueAlbumTask)},
		{"GET", "/api/v1/content/albums/tasks", "platformListMediaTasks", read((*API).handleListMediaTasks)},
		{"GET", "/api/v1/content/albums/{year}", "platformGetAlbumYear", read((*API).handleGetAlbumYear)},
		{"PUT", "/api/v1/content/albums/{year}", "platformPutAlbumYear", write((*API).handlePutAlbumYear)},
		{"DELETE", "/api/v1/content/albums/{year}/{filename}", "platformDeleteAlbumPhoto", write((*API).handleDeleteAlbumPhoto)},
	}
}

func (a *API) enqueueContentMediaTask(r *http.Request, id, actor uuid.UUID) (store.MediaTask, error) {
	if err := a.requireAlbumWrite(r, id); err != nil {
		return store.MediaTask{}, err
	}
	if a.platformScope {
		return a.mediaTasks.CreatePlatform(r.Context(), id, actor, a.cfg.PlatformContentThemeID, "album-index")
	}
	return a.mediaTasks.Create(r.Context(), id, actor, "album-index")
}

func (a *API) requireAlbumWrite(r *http.Request, id uuid.UUID) error {
	if guard, ok := a.content.(interface {
		RequireContentWrite(context.Context, uuid.UUID, string) error
	}); ok {
		return guard.RequireContentWrite(r.Context(), id, "albums")
	}
	return nil
}

func contentCategory(path string) string {
	for _, category := range []string{"posts", "friends", "footprints", "albums"} {
		if strings.Contains(path, "/"+category+"/") || strings.HasSuffix(path, "/"+category) {
			return category
		}
	}
	return ""
}
