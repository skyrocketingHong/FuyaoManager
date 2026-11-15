package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

func (a *API) postMediaTarget(id uuid.UUID, write bool) (*content.Store, uuid.UUID, error) {
	if resolver, ok := a.content.(interface {
		PostMediaTarget(uuid.UUID, bool) (*content.Store, uuid.UUID, error)
	}); ok {
		return resolver.PostMediaTarget(id, write)
	}
	if legacy, ok := a.content.(*content.Store); ok {
		return legacy, id, nil
	}
	return nil, id, content.ErrNotFound
}

// Resolve the caller's authorized posts source for every request.
func (a *API) handleListPostMedia(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	legacy, scope, err := a.postMediaTarget(target.ID, false)
	if err == nil && legacy.Posts == nil {
		err = contentbind.SyncPostMedia(r.Context(), legacy, a.store.PostMedia, scope)
	}
	if err != nil {
		writeContentError(w, err, "Failed to read post media.")
		return
	}
	items, err := a.store.PostMedia.List(r.Context(), scope)
	if err != nil {
		writeContentError(w, err, "Failed to list post media.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "canEdit": target.CanEdit})
}

func (a *API) handleGetPostMedia(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	legacy, scope, err := a.postMediaTarget(target.ID, false)
	if err != nil {
		writeContentError(w, err, "Failed to read post media.")
		return
	}
	data, kind, err := contentbind.ReadPostMedia(r.Context(), legacy, a.store.PostMedia, scope, path)
	if err != nil {
		writeContentError(w, err, "Failed to read post media.")
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (a *API) handleImportPostMedia(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	// ASVS 5.2.1: bound multipart uploads and clean temporary form files.
	r.Body = http.MaxBytesReader(w, r.Body, content.MaxUploadBytes()+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The upload exceeds the size limit.")
			return
		}
		writeProblem(w, http.StatusBadRequest, "validation_error", "The upload must contain one image file.")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", "The upload must contain a file field named file.")
		return
	}
	defer file.Close()
	item, data, _, err := content.PreparePostMedia(r.FormValue("path"), file, header.Size)
	if errors.Is(err, content.ErrTooLarge) {
		writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The upload exceeds the size limit.")
		return
	}
	if err != nil {
		writeContentError(w, err, "Failed to import post media.")
		return
	}
	legacy, scope, err := a.postMediaTarget(target.ID, true)
	if err == nil {
		var existing []byte
		existing, _, err = legacy.ReadPostMedia(scope, item.Path)
		if errors.Is(err, content.ErrNotFound) {
			err = nil
		} else if err == nil && !bytes.Equal(existing, data) {
			err = content.ErrExists
		}
	}
	if err == nil {
		err = a.store.PostMedia.Put(r.Context(), scope, item.Path, data)
	}
	if err != nil {
		writeContentError(w, err, "Failed to store post media.")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
