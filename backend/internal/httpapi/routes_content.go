package httpapi

import "net/http"

func (a *API) routesSiteContent() []routeSpec {
	return []routeSpec{
		// Per-site content (single authority for posts, friends, social
		// links, and the visual site configuration the builds consume).
		{http.MethodGet, "/api/v1/sites/{siteId}/posts", "listPosts", a.requireFullScope(a.contentRoute(a.handleListPosts))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/taxonomy", "getPostsTaxonomy", a.requireFullScope(a.contentRoute(a.handleGetPostsTaxonomy))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/vocabulary", "getPostVocabulary", a.requireFullScope(a.contentRoute(a.handleGetPostVocabulary))},
		{http.MethodPut, "/api/v1/sites/{siteId}/posts/vocabulary", "putPostVocabulary", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutPostVocabulary)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/posts/tags/rename", "renamePostTag", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleRenamePostTag)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/directories/{slug}", "getPostDirectory", a.requireFullScope(a.contentRoute(a.handleGetPostDirectory))},
		{http.MethodPut, "/api/v1/sites/{siteId}/posts/directories/{slug}", "putPostDirectory", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutPostDirectory)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/posts/directories/{slug}", "deletePostCategory", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleDeletePostCategory)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/media", "listPostMedia", a.requireFullScope(a.contentRoute(a.handleListPostMedia))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/media/file", "getPostMedia", a.requireFullScope(a.contentRoute(a.handleGetPostMedia))},
		{http.MethodPost, "/api/v1/sites/{siteId}/posts/media", "importPostMedia", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleImportPostMedia)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/posts", "createPost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleCreatePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/posts/{postPath}", "getPost", a.requireFullScope(a.contentRoute(a.handleGetPost))},
		{http.MethodPut, "/api/v1/sites/{siteId}/posts/{postPath}", "putPost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutPost)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/posts/{postPath}", "deletePost", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleDeletePost)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/friends", "getFriends", a.requireFullScope(a.contentRoute(a.handleGetFriends))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/friends", "putFriends", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutFriends)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/social-links", "getSocialLinks", a.requireFullScope(a.contentRoute(a.handleGetSocialLinks))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/social-links", "putSocialLinks", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutSocialLinks)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/site-config", "getSiteConfig", a.requireFullScope(a.contentRoute(a.handleGetSiteConfig))},
		{http.MethodPut, "/api/v1/sites/{siteId}/site-config", "putSiteConfig", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutSiteConfig)))},

		// Albums and footprints: uploads with async processing tasks,
		// metadata editing, reference-checked deletion, and the footprint
		// point document.
		{http.MethodGet, "/api/v1/sites/{siteId}/albums", "getAlbums", a.requireFullScope(a.contentRoute(a.handleGetAlbums))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/photos", "uploadAlbumPhoto", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleUploadAlbumPhoto)))},
		{http.MethodPost, "/api/v1/sites/{siteId}/albums/process", "processAlbums", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleEnqueueAlbumTask)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/tasks", "listMediaTasks", a.requireFullScope(a.contentRoute(a.handleListMediaTasks))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/thumbnail", "getAlbumThumbnail", a.requireFullScope(a.contentRoute(a.handleGetAlbumThumbnail))},
		{http.MethodGet, "/api/v1/sites/{siteId}/albums/{year}", "getAlbumYear", a.requireFullScope(a.contentRoute(a.handleGetAlbumYear))},
		{http.MethodPut, "/api/v1/sites/{siteId}/albums/{year}", "putAlbumYear", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutAlbumYear)))},
		{http.MethodDelete, "/api/v1/sites/{siteId}/albums/{year}/{filename}", "deleteAlbumPhoto", a.requireFullScope(a.withCSRF(a.contentRoute(a.handleDeleteAlbumPhoto)))},
		{http.MethodGet, "/api/v1/sites/{siteId}/content/footprints", "getFootprints", a.requireFullScope(a.contentRoute(a.handleGetFootprints))},
		{http.MethodPut, "/api/v1/sites/{siteId}/content/footprints", "putFootprints", a.requireFullScope(a.withCSRF(a.contentRoute(a.handlePutFootprints)))},
	}
}
