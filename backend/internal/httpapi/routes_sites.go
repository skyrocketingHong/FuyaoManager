package httpapi

import "net/http"

func (a *API) routesSites() []routeSpec {
	return []routeSpec{
		// Sites.
		{http.MethodGet, "/api/v1/sites", "listSites", a.requireFullScope(a.handleListSites)},
		{http.MethodPost, "/api/v1/sites", "createSite", a.requireFullScope(a.withCSRF(a.handleCreateSite))},
		{http.MethodGet, "/api/v1/sites/{siteId}", "getSite", a.requireFullScope(a.handleGetSite)},
		{http.MethodGet, "/api/v1/content", "getPlatformContent", a.requireFullScope(a.handlePlatformContent)},
		{http.MethodGet, "/api/v1/sites/{siteId}/overview", "getSiteOverview", a.requireFullScope(a.handleSiteOverview)},
		{http.MethodGet, "/api/v1/sites/{siteId}/content-sources", "getContentSources", a.requireFullScope(a.handleGetContentSources)},
		{http.MethodPut, "/api/v1/sites/{siteId}/content-sources", "setContentSources", a.requireFullScope(a.withCSRF(a.handleSetContentSources))},
		{http.MethodPost, "/api/v1/sites/{siteId}/provision", "provisionSite", a.requireFullScope(a.withCSRF(a.handleProvisionSite))},
		{http.MethodPut, "/api/v1/sites/{siteId}/visibility", "putSiteVisibility", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handlePutVisibility)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/settings", "getSiteSettings", a.requireFullScope(a.handleGetSettings)},
		{http.MethodPatch, "/api/v1/sites/{siteId}/settings", "updateSiteSettings", a.requireFullScope(a.withCSRF(a.handlePatchSettings))},

		// Theme catalog and per-site theme selection. Registration itself is
		// operator-driven (fuyao-admin register-theme); the HTTP surface only
		// reads the catalog and saves selections.
		{http.MethodGet, "/api/v1/themes/sources", "listThemeSources", a.requireFullScope(a.adminRoute(a.handleListThemeSources))},
		{http.MethodGet, "/api/v1/themes/sources/{sourceId}/candidate", "checkThemeSource", a.requireFullScope(a.adminRoute(a.handleCheckThemeSource))},
		{http.MethodPost, "/api/v1/themes/sources/{sourceId}/install", "installThemeSource", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleInstallThemeSource)))},
		{http.MethodGet, "/api/v1/themes", "listThemes", a.requireFullScope(a.handleListThemes)},
		{http.MethodGet, "/api/v1/sites/{siteId}/theme", "getSiteTheme", a.requireFullScope(a.handleGetSiteTheme)},
		{http.MethodPut, "/api/v1/sites/{siteId}/theme", "putSiteTheme", a.requireFullScope(a.withCSRF(a.handlePutSiteTheme))},

		// Reverse proxy configuration and deployment status.
		{http.MethodPost, "/api/v1/admin/caddy/sync", "syncCaddySites", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleSyncCaddy)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/caddy", "getCaddySite", a.requireFullScope(a.handleGetCaddySite)},
		{http.MethodPost, "/api/v1/sites/{siteId}/caddy/convert", "convertCaddySite", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCaddyConvert)))},
		{http.MethodPut, "/api/v1/sites/{siteId}/caddy/draft", "saveCaddyDraft", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCaddyDraft)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/caddy/validate", "validateCaddyDraft", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCaddyValidate)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/caddy/deploy", "deployCaddyDraft", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCaddyDeploy)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/caddy/rollback", "rollbackCaddyConfiguration", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCaddyRollback)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/edge", "getSiteEdge", a.requireFullScope(a.handleGetSiteEdge)},
		{http.MethodPost, "/api/v1/sites/{siteId}/edge/reapply", "reapplySiteEdge", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleReapplySiteEdge)))},
	}
}
