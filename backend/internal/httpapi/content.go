package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// contentProblem maps content-store errors onto stable problem codes.
func writeContentError(w http.ResponseWriter, err error, writeContext string) {
	switch {
	case errors.Is(err, content.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "not_found", "The content does not exist.")
	case errors.Is(err, content.ErrInvalidPath):
		writeProblem(w, http.StatusBadRequest, "invalid_content_path", "The content path is not a valid editable location.")
	case errors.Is(err, content.ErrConflict):
		writeProblem(w, http.StatusPreconditionFailed, "precondition_failed", "The content changed after it was read; fetch again and retry with the new ETag.")
	case errors.Is(err, content.ErrExists):
		writeProblem(w, http.StatusConflict, "content_exists", "Content already exists at this path.")
	default:
		var field *content.FieldError
		if errors.As(err, &field) {
			writeProblem(w, http.StatusBadRequest, "validation_error", field.Error())
			return
		}
		writeProblem(w, http.StatusInternalServerError, "internal_error", writeContext)
	}
}

// requireSiteEditor loads the site record and rejects non-editors.
func (a *API) requireSiteEditor(w http.ResponseWriter, r *http.Request, auth store.AuthSession) (rec store.SiteRecord, ok bool) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return store.SiteRecord{}, false
	}
	rec, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return store.SiteRecord{}, false
	}
	if rec.Role != model.RoleOwner && rec.Role != model.RoleEditor {
		writeProblem(w, http.StatusForbidden, "forbidden", "Only owners and editors may change site content.")
		return store.SiteRecord{}, false
	}
	return rec, true
}

// handleListPosts returns a paginated, searchable projection of the site's
// markdown posts (any member may read).
func (a *API) handleListPosts(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if pageSize > 100 {
		pageSize = 100
	}
	posts, total, err := a.content.ListPosts(siteID, r.URL.Query().Get("query"), page, pageSize)
	if err != nil {
		writeContentError(w, err, "Failed to list posts.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"posts": posts, "total": total})
}

// handleCreatePost writes a new markdown post.
func (a *API) handleCreatePost(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	var body content.Post
	if !decodeJSON(w, r, &body, false) {
		return
	}
	created, err := a.content.CreatePost(rec.Site.ID, body)
	if err != nil {
		writeContentError(w, err, "Failed to create the post.")
		return
	}
	w.Header().Set("ETag", `"`+created.Digest+`"`)
	writeJSON(w, http.StatusCreated, created)
}

// handleGetPost returns one editable post plus its digest ETag.
func (a *API) handleGetPost(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	post, err := a.content.GetPost(siteID, r.PathValue("postPath"))
	if err != nil {
		writeContentError(w, err, "Failed to read the post.")
		return
	}
	w.Header().Set("ETag", `"`+post.Digest+`"`)
	writeJSON(w, http.StatusOK, post)
}

// handlePutPost saves a post under read-modify-write concurrency: If-Match
// carries the digest returned by the previous read.
func (a *API) handlePutPost(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	var body content.Post
	if !decodeJSON(w, r, &body, false) {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	if postPath := r.PathValue("postPath"); postPath != "" {
		body.Category, body.Slug = splitPostPath(postPath)
	}
	saved, err := a.content.SavePost(rec.Site.ID, body, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save the post.")
		return
	}
	w.Header().Set("ETag", `"`+saved.Digest+`"`)
	writeJSON(w, http.StatusOK, saved)
}

// handleDeletePost removes a post after digest verification.
func (a *API) handleDeletePost(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	if err := a.content.DeletePost(rec.Site.ID, r.PathValue("postPath"), expected); err != nil {
		writeContentError(w, err, "Failed to delete the post.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func splitPostPath(postPath string) (category, slug string) {
	trimmed := strings.TrimSuffix(postPath, ".md")
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return "", trimmed
	}
	return trimmed[:idx], trimmed[idx+1:]
}

// requireDigestIfMatch parses the exact-digest If-Match header used by the
// content endpoints (`"sha256:<hex>"`).
func requireDigestIfMatch(w http.ResponseWriter, r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("If-Match"))
	if value == "" {
		writeProblem(w, http.StatusPreconditionRequired, "precondition_required", "This change requires an If-Match header carrying the current digest ETag.")
		return "", errAbort
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		writeProblem(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be the quoted digest ETag returned by the read.")
		return "", errAbort
	}
	inner := value[1 : len(value)-1]
	if !strings.HasPrefix(inner, "sha256:") || len(inner) != len("sha256:")+64 {
		writeProblem(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be the quoted digest ETag returned by the read.")
		return "", errAbort
	}
	return inner, nil
}

var errAbort = errors.New("response written")

// handleGetFriends returns the whole friends document plus its digest.
func (a *API) handleGetFriends(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	doc, digest, err := a.content.GetFriends(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read friends.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"friends": doc, "digest": digest})
}

// handlePutFriends replaces the friends document under If-Match semantics.
func (a *API) handlePutFriends(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	var body struct {
		Friends content.FriendsDoc `json:"friends"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	saved, digest, err := a.content.SaveFriends(rec.Site.ID, body.Friends, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save friends.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"friends": saved, "digest": digest})
}

// handleGetSocialLinks returns the site's social links plus digest.
func (a *API) handleGetSocialLinks(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	links, digest, err := a.content.GetSocialLinks(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read social links.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "digest": digest})
}

// handlePutSocialLinks replaces social-links.yaml under If-Match semantics.
func (a *API) handlePutSocialLinks(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	var body struct {
		Links []content.SocialLink `json:"links"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	saved, digest, err := a.content.SaveSocialLinks(rec.Site.ID, body.Links, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save social links.")
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"links": saved, "digest": digest})
}

// handleGetSiteConfig returns the editable site.yaml projection with the
// amap security code redacted (only whether one is set is reported).
func (a *API) handleGetSiteConfig(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	siteID, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	doc, securitySet, digest, err := a.content.GetSiteConfig(siteID)
	if err != nil {
		writeContentError(w, err, "Failed to read the site configuration.")
		return
	}
	effectiveOrigin := ""
	if a.edge != nil && a.store.NetworkEntries != nil {
		if site, err := a.store.Sites.GetForUser(r.Context(), siteID, auth.User.ID); err == nil {
			if entry, err := a.store.NetworkEntries.GetSiteEntry(r.Context(), siteID); err == nil {
				effectiveOrigin = a.edge.EntryURL(site.Site.Visibility, entry.Port)
			}
		}
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"config": doc, "amapSecurityCodeSet": securitySet, "effectiveOrigin": effectiveOrigin, "digest": digest})
}

// handlePutSiteConfig saves site.yaml under If-Match semantics. The amap
// security code is write-only: omitted keeps the stored value, an empty
// string clears it, and it is never returned.
func (a *API) handlePutSiteConfig(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	rec, ok := a.requireSiteEditor(w, r, auth)
	if !ok {
		return
	}
	var body struct {
		Config  content.SiteConfigDoc `json:"config"`
		Secrets struct {
			Amap *struct {
				SecurityCode *string `json:"securityCode"`
			} `json:"amap"`
		} `json:"secrets"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	var securityCode *string
	if body.Secrets.Amap != nil {
		securityCode = body.Secrets.Amap.SecurityCode
	}
	expected, err := requireDigestIfMatch(w, r)
	if err != nil {
		return
	}
	saved, digest, err := a.content.SaveSiteConfig(rec.Site.ID, body.Config, securityCode, expected)
	if err != nil {
		writeContentError(w, err, "Failed to save the site configuration.")
		return
	}
	_, securitySet, _, err := a.content.GetSiteConfig(rec.Site.ID)
	if err != nil {
		a.logger.Error("reread site config after save", "error", err)
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"config": saved, "amapSecurityCodeSet": securitySet, "digest": digest})
}
