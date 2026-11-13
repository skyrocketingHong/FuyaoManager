package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/traffic"
)

func (a *API) canPublishManaged(ctx context.Context, site store.SiteRecord, auth store.AuthSession) bool {
	if !site.HasCaddy || !site.ThemeEnabled || site.Kind != "static" || site.ManagementMode != "platform" || site.Role != "owner" || auth.User.PlatformRole != "admin" || !a.cfg.CaddyManagementEnabled || a.cfg.CaddyControl == nil {
		return false
	}
	c, err := a.store.GetCaddySite(ctx, site.Site.ID, auth.User.ID)
	if err != nil || !c.Present {
		return false
	}
	m, err := a.store.GetCaddyManagement(ctx, site.Site.ID)
	if err != nil || m == nil {
		return false
	}
	root, err := a.cfg.CaddyControl(ctx, caddymanage.Request{Operation: "inspect", SiteKey: c.SourceKey})
	return err == nil && root.PublicationEnabled && root.Registered && !root.Drifted && !root.RuntimeUncertain && root.SourceDigest == m.SourceDigest && root.RootRevisionID == m.RootRevisionID
}
func (a *API) siteDTO(r *http.Request, site store.SiteRecord, auth store.AuthSession) siteDTO {
	dto := toSiteDTO(site)
	dto.Workspace.Deployable = (!site.HasCaddy && a.deployEnabled && (site.Role == "owner" || site.Role == "editor")) || a.canPublishManaged(r.Context(), site, auth)
	return dto
}

type contentCounts struct {
	Posts   int `json:"posts"`
	Albums  int `json:"albums"`
	Photos  int `json:"photos"`
	Places  int `json:"places"`
	Friends int `json:"friends"`
}

func countContent(c ContentStore, id uuid.UUID) (contentCounts, error) {
	var out contentCounts
	if c == nil {
		return out, nil
	}
	_, total, err := c.ListPosts(id, "", 1, 1)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return out, err
	}
	out.Posts = total
	album, _, err := c.GetAlbumIndex(id)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return out, err
	}
	out.Albums = len(album.Years)
	out.Photos = album.TotalPhotos
	friends, _, err := c.GetFriends(id)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return out, err
	}
	out.Friends = len(friends.Online) + len(friends.Offline)
	footprints, _, err := c.GetFootprints(id)
	if err != nil && !errors.Is(err, content.ErrNotFound) {
		return out, err
	}
	out.Places = len(footprints.Cities)
	for _, city := range footprints.Cities {
		out.Places += len(city.Places)
	}
	return out, nil
}
func (a *API) handlePlatformContent(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	if a.cfg.PlatformContent == nil {
		writeProblem(w, 503, "content_unavailable", "The platform content library is not configured.")
		return
	}
	counts, err := countContent(a.cfg.PlatformContent, content.PlatformScopeID)
	if err != nil {
		writeContentError(w, err, "Cannot read platform content statistics.")
		return
	}
	amap := map[string]string{}
	if config, _, _, err := a.cfg.PlatformContent.GetSiteConfig(content.PlatformScopeID); err == nil {
		amap["browserKey"] = config.Services.Amap.BrowserKey
		amap["serviceHost"] = config.Services.Amap.ServiceHost
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"canEdit": auth.User.PlatformRole == model.PlatformRoleAdmin, "counts": counts, "amap": amap})
}
func (a *API) handleGetContentSources(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	if _, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID); err != nil {
		writeSiteLookupError(w, err)
		return
	}
	record, err := a.store.GetContentSources(r.Context(), id)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	w.Header().Set("ETag", store.ContentSourcesETag(id, record.Revision))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, record)
}
func (a *API) handleSetContentSources(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	site, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	if site.Kind != "theme" && !(site.Kind == "static" && site.ManagementMode == "platform") {
		writeProblem(w, 409, "site_kind_mismatch", "Content sources are available only for managed websites.")
		return
	}
	if r.Header.Get("If-Match") == "" {
		writeProblem(w, 428, "precondition_required", "An exact content-sources ETag is required.")
		return
	}
	var body struct {
		Sources  store.ContentSources `json:"sources"`
		Revision *int64               `json:"revision,omitempty"`
	}
	if !decodeJSON(w, r, &body, false) {
		return
	}
	if !body.Sources.Valid() {
		writeProblem(w, 422, "validation_error", "Choose platform or independent for each content category.")
		return
	}
	if a.cfg.PlatformContent == nil && (body.Sources.Posts == "platform" || body.Sources.Albums == "platform" || body.Sources.Footprints == "platform" || body.Sources.Friends == "platform") {
		writeProblem(w, 503, "content_unavailable", "The platform content library is not configured.")
		return
	}
	if body.Revision != nil && store.ContentSourcesETag(id, *body.Revision) != r.Header.Get("If-Match") {
		writeProblem(w, 412, "precondition_failed", "The revision and ETag do not agree.")
		return
	}
	rec, err := a.store.SetContentSources(r.Context(), id, auth.User.ID, r.Header.Get("If-Match"), body.Sources)
	if err != nil {
		if errors.Is(err, store.ErrPreconditionFailed) {
			writeProblem(w, 412, "precondition_failed", "The content sources changed; reload before saving.")
		} else {
			a.adminError(w, r, err)
		}
		return
	}
	w.Header().Set("ETag", store.ContentSourcesETag(id, rec.Revision))
	writeJSON(w, 200, rec)
}
func (a *API) handleSiteOverview(w http.ResponseWriter, r *http.Request, auth store.AuthSession) {
	id, ok := parseSiteID(w, r)
	if !ok {
		return
	}
	site, err := a.store.Sites.GetForUser(r.Context(), id, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	counts, err := countContent(a.content, id)
	if err != nil {
		writeContentError(w, err, "Cannot read site statistics.")
		return
	}
	sources, err := a.store.GetContentSources(r.Context(), id)
	if err != nil {
		a.adminError(w, r, err)
		return
	}
	selection, err := a.store.Themes.GetSelectionForUser(r.Context(), id, auth.User.ID)
	if err != nil {
		writeSiteLookupError(w, err)
		return
	}
	theme, err := toSiteThemeDTO(id.String(), selection)
	if err != nil {
		writeProblem(w, 500, "internal_error", "Cannot read the site theme.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	analytics := traffic.Unconfigured()
	if id == a.cfg.TrafficStatsSiteID && a.cfg.TrafficStatsURL != "" {
		analytics = traffic.Fetch(r.Context(), a.cfg.TrafficStatsURL)
	}
	writeJSON(w, 200, map[string]any{"counts": counts, "sources": sources.Sources, "theme": theme, "hasCaddy": site.HasCaddy, "workspace": a.siteDTO(r, site, auth).Workspace, "analytics": analytics})
}
