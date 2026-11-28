package httpapi

import "net/http"

func (a *API) routesPublishing() []routeSpec {
	return []routeSpec{
		// Build-preview tasks: creation freezes the version view; the runner
		// executes and persists state transitions that stay queryable here.
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/artifacts/delete", "deleteBuildArtifacts", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleDeleteBuildArtifacts)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds", "createBuild", a.requireFullScope(a.withCSRF(a.handleCreateBuild))},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds", "listBuilds", a.requireFullScope(a.handleListBuilds)},
		{http.MethodGet, "/api/v1/sites/{siteId}/builds/{buildId}", "getBuild", a.requireFullScope(a.handleGetBuild)},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-links", "createPreviewLink", a.requireFullScope(a.withCSRF(a.handleCreatePreviewLink))},
		{http.MethodPost, "/api/v1/sites/{siteId}/builds/{buildId}/preview-close", "closePreviewLink", a.requireFullScope(a.withCSRF(a.handleClosePreviewLink))},

		// Releases: publish freezes a succeeded build's artifacts into a
		// release directory and atomically switches the site's live version;
		// rollback re-points at an existing release without rebuilding.
		{http.MethodPost, "/api/v1/sites/{siteId}/releases/artifacts/delete", "deleteReleaseArtifacts", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleDeleteReleaseArtifacts)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/releases", "createRelease", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleCreateRelease)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases", "listReleases", a.requireFullScope(a.handleListReleases)},
		{http.MethodGet, "/api/v1/sites/{siteId}/releases/{releaseId}", "getRelease", a.requireFullScope(a.handleGetRelease)},
		{http.MethodGet, "/api/v1/sites/{siteId}/release", "getCurrentRelease", a.requireFullScope(a.handleGetCurrentRelease)},
		{http.MethodPost, "/api/v1/sites/{siteId}/releases/{releaseId}/rollback", "rollbackRelease", a.requireFullScope(a.withCSRF(a.deploymentRoute(a.handleRollbackRelease)))},
	}
}
