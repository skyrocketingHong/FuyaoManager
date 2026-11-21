package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// handleGetAlbums returns the album index (years, totals, devices).
func (a *API) handleGetAlbums(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	siteID := target.ID
	index, digest, err := a.content.GetAlbumIndex(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read the album index.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"index": index, "digest": digest, "canEdit": target.CanEdit})
}

// handleGetAlbumYear returns one year's photos plus digest.
func (a *API) handleGetAlbumYear(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	siteID := target.ID
	doc, digest, err := a.content.GetAlbumYear(siteID, r.PathValue("year"))
	if err != nil {
		writeContentError(w, err, "Failed to read the album year.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"year": doc, "digest": digest, "canEdit": target.CanEdit})
}

func (a *API) handleGetAlbumThumbnail(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	// ASVS 8.2.2: thumbnails inherit album scope and membership permissions.
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	paths := r.URL.Query()["path"]
	if len(paths) != 1 {
		writeContentError(w, content.ErrInvalidPath, "")
		return
	}
	data, kind, err := a.content.ReadAlbumThumbnail(target.ID, paths[0])
	if err != nil {
		writeContentError(w, err, "Failed to read the album thumbnail.")
		return
	}
	// ASVS 3.2.1: serve only sniffed image types with a fixed safe policy.
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// yearEditBody is the editable projection of one year document: the
// year-level managed title/cover plus the per-photo managed fields in the
// authoritative submitted order.
type yearEditBody struct {
	Title  string `json:"title"`
	Cover  string `json:"cover"`
	Photos []struct {
		Filename    string `json:"filename"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Alt         string `json:"alt"`
	} `json:"photos"`
}

// handlePutAlbumYear edits photo titles/descriptions/alt and the ordering.
// The submitted order is authoritative: entries are re-emitted in exactly
// the submitted sequence. Identity fields (path, variants) are
// importer-owned; the submitted filename set must be complete, unique, and
// identical to the stored set — no additions, removals (use delete/detach),
// or renames through this endpoint.
func (a *API) handlePutAlbumYear(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	var body yearEditBody
	if !decodeJSON(w, r, &body, false) {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	saved, digest, err := a.content.SaveAlbumYear(rec.ID, r.PathValue("year"), expected, func(doc *content.AlbumYear) error {
		if len(body.Photos) != len(doc.Photos) {
			return errors.New("the photo list changed after it was read; fetch again and retry")
		}
		// Year-level managed fields. The cover must name an existing photo
		// of this year (or be empty to fall back to the first thumbnail).
		if body.Cover != "" {
			found := false
			for _, photo := range doc.Photos {
				if photo.Filename == body.Cover {
					found = true
					break
				}
			}
			if !found {
				return errors.New("cover must be an existing photo filename of this year")
			}
		}
		doc.Title = body.Title
		doc.Cover = body.Cover
		edits := make(map[string]struct {
			title, description, alt string
		}, len(body.Photos))
		for _, edit := range body.Photos {
			if _, dup := edits[edit.Filename]; dup {
				return errors.New("duplicate photo " + edit.Filename + " in the submitted order")
			}
			edits[edit.Filename] = struct {
				title, description, alt string
			}{edit.Title, edit.Description, edit.Alt}
		}
		byFilename := make(map[string]content.AlbumPhoto, len(doc.Photos))
		for _, photo := range doc.Photos {
			if _, ok := edits[photo.Filename]; !ok {
				return errors.New("unknown photo " + photo.Filename)
			}
			byFilename[photo.Filename] = photo
		}
		if len(byFilename) != len(edits) {
			return errors.New("the submitted photo set does not match the stored photos")
		}
		// Emit in the submitted order, carrying the importer-owned identity
		// fields from the stored entry and the editable fields from the edit.
		next := make([]content.AlbumPhoto, 0, len(body.Photos))
		for _, edit := range body.Photos {
			photo := byFilename[edit.Filename]
			next = append(next, content.AlbumPhoto{
				Filename: photo.Filename, Path: photo.Path, Date: photo.Date, GPS: photo.GPS,
				CoordType: photo.CoordType, Make: photo.Make, Model: photo.Model,
				Width: photo.Width, Height: photo.Height, Variants: photo.Variants,
				Title: edit.Title, Description: edit.Description, Alt: edit.Alt,
			})
		}
		doc.Photos = next
		return nil
	})
	if err != nil {
		writeContentError(w, err, "Failed to save the album year.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"year": saved, "digest": digest, "canEdit": rec.CanEdit})
}

// handleUploadAlbumPhoto accepts one original (multipart form field
// "file"), stores it under a server-controlled path, and queues the album
// indexing task whose status is queryable.
func (a *API) handleUploadAlbumPhoto(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	if a.mediaTasks == nil || (a.platformScope && a.cfg.PlatformContentThemeID == uuid.Nil) {
		writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "A trusted media importer is not configured.")
		return
	}
	if err := a.requireAlbumWrite(r, rec.ID); err != nil {
		writeContentError(w, err, "The album content is read-only.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, content.MaxUploadBytes()+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", "The upload must be a multipart form with one file field.")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "validation_error", "The upload must contain a file field named file.")
		return
	}
	defer file.Close()
	rel, err := a.content.StoreUpload(rec.ID, file, 0)
	if err != nil {
		if errors.Is(err, content.ErrTooLarge) {
			writeProblem(w, http.StatusRequestEntityTooLarge, "request_too_large", "The upload exceeds the size limit.")
			return
		}
		writeContentError(w, err, "Failed to store the upload.")
		return
	}
	if a.mediaTasks == nil {
		writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "Media processing is not configured on this server.")
		return
	}
	task, err := a.enqueueContentMediaTask(r, rec.ID, auth.User.ID)
	if err != nil {
		a.logger.Error("enqueue media task", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "The upload was stored but processing could not be queued.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"path": rel, "taskId": task.ID})
}

// handleEnqueueAlbumTask re-queues the album indexing task (retry).
func (a *API) handleEnqueueAlbumTask(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	var body struct{}
	if !decodeJSON(w, r, &body, true) {
		return
	}
	if a.platformScope && a.cfg.PlatformContentThemeID == uuid.Nil {
		writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "A trusted media importer is not configured.")
		return
	}
	if a.mediaTasks == nil {
		writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "Media processing is not configured on this server.")
		return
	}
	task, err := a.enqueueContentMediaTask(r, rec.ID, auth.User.ID)
	if err != nil {
		a.logger.Error("enqueue media task", "error", err)
		writeContentError(w, err, "Failed to queue the task.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

// handleListMediaTasks returns the site's recent media tasks.
func (a *API) handleListMediaTasks(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	tasks, err := a.store.MediaTasks.ListForContentScope(r.Context(), target.ID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		entry := map[string]any{
			"id": task.ID.String(), "kind": task.Kind, "status": task.Status,
			"createdAt": task.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
		if task.Failure != nil {
			entry["failure"] = task.Failure
		}
		if task.FinishedAt != nil {
			entry["finishedAt"] = task.FinishedAt.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out, "canEdit": target.CanEdit})
}

// handleDeleteAlbumPhoto removes a photo. The default (hard) mode deletes
// the original, thumbnails, and metadata entry, but only after proving that
// no live or rollback-able release still references the photo; a failed or
// unreadable reference scan refuses the deletion. The `mode=detach` form
// removes the photo from the next build's metadata only, keeping files for
// releases that still reference them.
func (a *API) handleDeleteAlbumPhoto(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	if r.URL.Query().Get("mode") == "detach" {
		if err := a.content.DetachAlbumPhoto(rec.ID, r.PathValue("year"), r.PathValue("filename"), expected); err != nil {
			writeContentError(w, err, "Failed to detach the photo.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	releaseJSONs, err := content.ReleaseMetadataForScope(a.releasesRoot, rec.ID, a.platformScope)
	if err != nil {
		a.logger.Error("scan release references for photo deletion", "siteId", rec.ID, "error", err)
		writeProblem(w, http.StatusInternalServerError, "reference_scan_failed", "The release reference scan failed; deletion is refused until the references can be verified.")
		return
	}
	err = a.content.DeleteAlbumPhoto(rec.ID, r.PathValue("year"), r.PathValue("filename"), releaseJSONs, expected)
	if err != nil {
		var inUse *content.PhotoInUseError
		if errors.As(err, &inUse) {
			writeProblem(w, http.StatusConflict, "photo_in_use", "This photo is still referenced by a live or rollback-able release; detach it from future builds now and hard-delete after those releases are retired.")
			return
		}
		writeContentError(w, err, "Failed to delete the photo.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetFootprints returns the footprints document plus digest.
func (a *API) handleGetFootprints(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	target, ok := a.contentTarget(w, r, auth, false)
	if !ok {
		return
	}
	siteID := target.ID
	doc, digest, err := a.content.GetFootprints(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read footprints.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"footprints": doc, "digest": digest, "canEdit": target.CanEdit})
}

// handlePutFootprints replaces the footprints document under If-Match
// semantics with full schema validation.
func (a *API) handlePutFootprints(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.contentTarget(w, r, auth, true)
	if !ok {
		return
	}
	var body struct {
		Footprints content.FootprintsDoc `json:"footprints"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	saved, digest, err := a.content.SaveFootprints(rec.ID, body.Footprints, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save footprints.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"footprints": saved, "digest": digest, "canEdit": rec.CanEdit})
}
