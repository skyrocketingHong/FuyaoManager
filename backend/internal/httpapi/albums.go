package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
)

// handleGetAlbums returns the album index (years, totals, devices).
func (a *API) handleGetAlbums(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	index, digest, err := a.content.GetAlbumIndex(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read the album index.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"index": index, "digest": digest})
}

// handleGetAlbumYear returns one year's photos plus digest.
func (a *API) handleGetAlbumYear(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	doc, digest, err := a.content.GetAlbumYear(siteID, r.PathValue("year"))
	if err != nil {
		writeContentError(w, err, "Failed to read the album year.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"year": doc, "digest": digest})
}

// yearEditBody is the editable projection of one year document.
type yearEditBody struct {
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
	rec, ok := a.requireSiteEditor(w, r, auth)
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
	saved, digest, err := a.content.SaveAlbumYear(rec.Site.ID, r.PathValue("year"), expected, func(doc *content.AlbumYear) error {
		if len(body.Photos) != len(doc.Photos) {
			return errors.New("the photo list changed after it was read; fetch again and retry")
		}
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
	writeJSON(w, http.StatusOK, map[string]any{"year": saved, "digest": digest})
}

// handleUploadAlbumPhoto accepts one original (multipart form field
// "file"), stores it under a server-controlled path, and queues the album
// indexing task whose status is queryable.
func (a *API) handleUploadAlbumPhoto(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
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
	rel, err := a.content.StoreUpload(rec.Site.ID, file, 0)
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
	task, err := a.mediaTasks.Create(r.Context(), rec.Site.ID, auth.User.ID, "album-index")
	if err != nil {
		a.logger.Error("enqueue media task", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "The upload was stored but processing could not be queued.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"path": rel, "taskId": task.ID})
}

// handleEnqueueAlbumTask re-queues the album indexing task (retry).
func (a *API) handleEnqueueAlbumTask(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	if a.mediaTasks == nil {
		writeProblem(w, http.StatusServiceUnavailable, "content_unavailable", "Media processing is not configured on this server.")
		return
	}
	task, err := a.mediaTasks.Create(r.Context(), rec.Site.ID, auth.User.ID, "album-index")
	if err != nil {
		a.logger.Error("enqueue media task", "error", err)
		writeProblem(w, http.StatusInternalServerError, "internal_error", "Failed to queue the task.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": task.ID})
}

// handleListMediaTasks returns the site's recent media tasks.
func (a *API) handleListMediaTasks(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	tasks, err := a.store.MediaTasks.ListForSite(r.Context(), siteID, auth.User.ID)
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
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

// handleDeleteAlbumPhoto removes a photo. The default (hard) mode deletes
// the original, thumbnails, and metadata entry, but only after proving that
// no live or rollback-able release still references the photo; a failed or
// unreadable reference scan refuses the deletion. The `mode=detach` form
// removes the photo from the next build's metadata only, keeping files for
// releases that still reference them.
func (a *API) handleDeleteAlbumPhoto(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	if r.URL.Query().Get("mode") == "detach" {
		if err := a.content.DetachAlbumPhoto(rec.Site.ID, r.PathValue("year"), r.PathValue("filename"), expected); err != nil {
			writeContentError(w, err, "Failed to detach the photo.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	releaseJSONs, err := content.ReleaseMetadataForSite(a.releasesRoot, rec.Site.ID)
	if err != nil {
		a.logger.Error("scan release references for photo deletion", "siteId", rec.Site.ID, "error", err)
		writeProblem(w, http.StatusInternalServerError, "reference_scan_failed", "The release reference scan failed; deletion is refused until the references can be verified.")
		return
	}
	err = a.content.DeleteAlbumPhoto(rec.Site.ID, r.PathValue("year"), r.PathValue("filename"), releaseJSONs, expected)
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
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	doc, digest, err := a.content.GetFootprints(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read footprints.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"footprints": doc, "digest": digest})
}

// handlePutFootprints replaces the footprints document under If-Match
// semantics with full schema validation.
func (a *API) handlePutFootprints(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
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
	saved, digest, err := a.content.SaveFootprints(rec.Site.ID, body.Footprints, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save footprints.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"footprints": saved, "digest": digest})
}
