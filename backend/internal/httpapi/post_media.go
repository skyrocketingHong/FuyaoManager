package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// ASVS 8.2.1/8.2.2: resolve the caller's authorized posts source for every request.
func (a *API) handleListPostMedia(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	items, err := a.content.ListPostMedia(target.ID)
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
	data, kind, err := a.content.ReadPostMedia(target.ID, r.URL.Query().Get("path"))
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
	item, err := a.content.StorePostMedia(target.ID, r.FormValue("path"), file, header.Size)
	if errors.Is(err, content.ErrTooLarge) {
		writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The upload exceeds the size limit.")
		return
	}
	if err != nil {
		writeContentError(w, err, "Failed to import post media.")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
