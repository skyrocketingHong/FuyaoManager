package httpapi

import "net/http"

func (a *API) routesPlatform() []routeSpec {
	return []routeSpec{
		{http.MethodGet, "/api/v1/version", "getApplicationVersion", a.handleApplicationVersion},
		{http.MethodGet, "/api/v1/admin/updates", "getPlatformUpdates", a.requireFullScope(a.adminRoute(a.handlePlatformUpdates))},
		{http.MethodPost, "/api/v1/admin/updates", "managePlatformUpdates", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleUpdateAction)))},
		{http.MethodGet, "/{previewHost}/{path...}", "getPreviewWrapper", a.handlePreviewWrapper},
		{http.MethodGet, "/api/v1/openapi", "getOpenAPI", a.requireFullScope(a.handleOpenAPI)},
	}
}
