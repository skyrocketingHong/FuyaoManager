/**
 * Typed business module over the generated OpenAPI schema. Every endpoint of
 * the management contract used by the admin console lives here; components
 * never call fetch directly.
 */
import { api, setCsrfToken } from './client';
import type { components } from './schema';

export type User = components['schemas']['User'];
export type Site = components['schemas']['Site'];
export type SiteCreate = components['schemas']['SiteCreate'];
export type SiteSettings = components['schemas']['SiteSettings'];
export type SiteSettingsPatch = components['schemas']['SiteSettingsPatch'];
export type SessionInfo = components['schemas']['SessionInfo'];
export type CeremonyDone = components['schemas']['CeremonyDone'];
export type AuthCodeExchangeRequest = components['schemas']['AuthCodeExchangeRequest'];
export type Theme = components['schemas']['Theme'];
export type SiteThemeSelection = components['schemas']['SiteThemeSelection'];
export type SiteThemeSelectionPut = components['schemas']['SiteThemeSelectionPut'];
export type BuildTask = components['schemas']['BuildTask'];
export type PreviewLink = components['schemas']['PreviewLink'];
export type Release = components['schemas']['Release'];
export type ReleaseCreate = components['schemas']['ReleaseCreate'];
export type Post = components['schemas']['Post'];
export type PostList = components['schemas']['PostList'];
export type Friend = components['schemas']['Friend'];
export type FriendsEnvelope = components['schemas']['FriendsEnvelope'];
export type SocialLink = components['schemas']['SocialLink'];
export type SocialLinksEnvelope = components['schemas']['SocialLinksEnvelope'];
export type SiteConfig = components['schemas']['SiteConfig'];
export type SiteConfigEnvelope = components['schemas']['SiteConfigEnvelope'];
export type AlbumIndex = components['schemas']['AlbumIndexEnvelope']['index'];
export type AlbumPhotoEntry = components['schemas']['AlbumPhotoEntry'];
export type AlbumYear = components['schemas']['AlbumYearEnvelope']['year'];
export type MediaTask = components['schemas']['MediaTask'];
export type FootprintCity = components['schemas']['FootprintCity'];
export type FootprintPlace = components['schemas']['FootprintPlace'];
export type FootprintsDoc = { cities: FootprintCity[] };

export function siteRole(site: Site): 'owner' | 'editor' | 'viewer' {
	return site.role;
}

export function canEditSite(site: Site): boolean {
	return site.role === 'owner' || site.role === 'editor';
}

export async function fetchSession(): Promise<SessionInfo> {
	const { data } = await api<SessionInfo>('GET', '/api/v1/session');
	setCsrfToken(data.csrfToken ?? null);
	return data;
}

/**
 * Redeems a CLI-issued one-time code (initialization or per-login) for a
 * fresh session; the CSRF token arrives with the signed-in user.
 */
export async function exchangeAuthCode(code: string): Promise<CeremonyDone> {
	const { data } = await api<CeremonyDone>('POST', '/api/v1/auth/exchange', { body: { code } });
	setCsrfToken(data.csrfToken);
	return data;
}

export async function logout(): Promise<void> {
	await api<void>('POST', '/api/v1/auth/logout', { body: {} });
	setCsrfToken(null);
}

export async function listSites(): Promise<Site[]> {
	const { data } = await api<{ sites: Site[] }>('GET', '/api/v1/sites');
	return data.sites;
}

export async function createSite(body: SiteCreate): Promise<Site> {
	const { data } = await api<Site>('POST', '/api/v1/sites', { body });
	return data;
}

export async function provisionSite(siteId: string): Promise<Site> {
	const { data } = await api<Site>('POST', `/api/v1/sites/${siteId}/provision`, { body: {} });
	return data;
}

export async function putSiteVisibility(
	siteId: string,
	visibility: 'public' | 'private'
): Promise<Site> {
	const { data } = await api<Site>('PUT', `/api/v1/sites/${siteId}/visibility`, {
		body: { visibility }
	});
	return data;
}

export async function getSite(siteId: string): Promise<Site> {
	const { data } = await api<Site>('GET', `/api/v1/sites/${siteId}`);
	return data;
}

export async function getSettings(
	siteId: string
): Promise<{ settings: SiteSettings; etag: string }> {
	const { data, etag } = await api<SiteSettings>('GET', `/api/v1/sites/${siteId}/settings`);
	if (etag === null) throw new Error('settings response missing ETag');
	return { settings: data, etag };
}

export async function updateSettings(
	siteId: string,
	patch: SiteSettingsPatch,
	etag: string
): Promise<{ settings: SiteSettings; etag: string }> {
	const { data, etag: newEtag } = await api<SiteSettings>(
		'PATCH',
		`/api/v1/sites/${siteId}/settings`,
		{ body: patch, etag }
	);
	if (newEtag === null) throw new Error('settings response missing ETag');
	return { settings: data, etag: newEtag };
}

// --- themes ---

export async function listThemes(): Promise<Theme[]> {
	const { data } = await api<{ themes: Theme[] }>('GET', '/api/v1/themes');
	return data.themes;
}

export async function getSiteTheme(
	siteId: string
): Promise<{ selection: SiteThemeSelection; etag: string }> {
	const { data, etag } = await api<SiteThemeSelection>('GET', `/api/v1/sites/${siteId}/theme`);
	if (etag === null) throw new Error('theme selection response missing ETag');
	return { selection: data, etag };
}

export async function putSiteTheme(
	siteId: string,
	body: SiteThemeSelectionPut,
	etag: string
): Promise<{ selection: SiteThemeSelection; etag: string }> {
	const { data, etag: newEtag } = await api<SiteThemeSelection>(
		'PUT',
		`/api/v1/sites/${siteId}/theme`,
		{ body, etag }
	);
	if (newEtag === null) throw new Error('theme selection response missing ETag');
	return { selection: data, etag: newEtag };
}

// --- build-preview tasks ---

export async function createBuild(siteId: string): Promise<BuildTask> {
	const { data } = await api<BuildTask>('POST', `/api/v1/sites/${siteId}/builds`, { body: {} });
	return data;
}

export async function listBuilds(siteId: string): Promise<BuildTask[]> {
	const { data } = await api<{ builds: BuildTask[] }>('GET', `/api/v1/sites/${siteId}/builds`);
	return data.builds;
}

export async function getBuild(siteId: string, buildId: string): Promise<BuildTask> {
	const { data } = await api<BuildTask>('GET', `/api/v1/sites/${siteId}/builds/${buildId}`);
	return data;
}

export async function createPreviewLink(siteId: string, buildId: string): Promise<PreviewLink> {
	const { data } = await api<PreviewLink>(
		'POST',
		`/api/v1/sites/${siteId}/builds/${buildId}/preview-links`,
		{ body: {} }
	);
	return data;
}

/** Closes the build's preview server-side: tokens revoked, listener and port released. */
export async function closePreviewLink(siteId: string, buildId: string): Promise<void> {
	await api<void>('POST', `/api/v1/sites/${siteId}/builds/${buildId}/preview-close`, { body: {} });
}

// --- releases ---

export async function createRelease(siteId: string, body: ReleaseCreate): Promise<Release> {
	const { data } = await api<Release>('POST', `/api/v1/sites/${siteId}/releases`, { body });
	return data;
}

export async function listReleases(siteId: string): Promise<Release[]> {
	const { data } = await api<{ releases: Release[] }>('GET', `/api/v1/sites/${siteId}/releases`);
	return data.releases;
}

export async function getCurrentRelease(
	siteId: string
): Promise<{ release: Release; originUrl: string }> {
	const { data } = await api<{ release: Release; originUrl: string }>(
		'GET',
		`/api/v1/sites/${siteId}/release`
	);
	return data;
}

export async function rollbackRelease(siteId: string, releaseId: string): Promise<Release> {
	const { data } = await api<Release>(
		'POST',
		`/api/v1/sites/${siteId}/releases/${releaseId}/rollback`,
		{ body: {} }
	);
	return data;
}

// --- site content (single authority for the frozen-snapshot builds) ---

export async function listPosts(
	siteId: string,
	query: string,
	page: number,
	pageSize: number
): Promise<PostList> {
	const { data } = await api<PostList>(
		'GET',
		`/api/v1/sites/${siteId}/posts?query=${encodeURIComponent(query)}&page=${page}&pageSize=${pageSize}`
	);
	return data;
}

export async function createPost(siteId: string, post: Partial<Post>): Promise<Post> {
	const { data } = await api<Post>('POST', `/api/v1/sites/${siteId}/posts`, { body: post });
	return data;
}

export async function getPost(
	siteId: string,
	postPath: string
): Promise<{ post: Post; etag: string }> {
	const { data, etag } = await api<Post>(
		'GET',
		`/api/v1/sites/${siteId}/posts/${encodeURIComponent(postPath)}`
	);
	if (etag === null) throw new Error('post response missing ETag');
	return { post: data, etag };
}

export async function putPost(
	siteId: string,
	postPath: string,
	post: Partial<Post>,
	etag: string
): Promise<Post> {
	const { data } = await api<Post>(
		'PUT',
		`/api/v1/sites/${siteId}/posts/${encodeURIComponent(postPath)}`,
		{
			body: post,
			etag
		}
	);
	return data;
}

export async function deletePost(siteId: string, postPath: string, etag: string): Promise<void> {
	await api<void>('DELETE', `/api/v1/sites/${siteId}/posts/${encodeURIComponent(postPath)}`, {
		etag
	});
}

export async function getFriends(
	siteId: string
): Promise<{ friends: FriendsEnvelope['friends']; digest: string }> {
	const { data } = await api<FriendsEnvelope>('GET', `/api/v1/sites/${siteId}/content/friends`);
	return { friends: data.friends, digest: data.digest };
}

export async function putFriends(
	siteId: string,
	friends: FriendsEnvelope['friends'],
	digest: string
): Promise<FriendsEnvelope> {
	const { data } = await api<FriendsEnvelope>('PUT', `/api/v1/sites/${siteId}/content/friends`, {
		body: { friends },
		etag: digest
	});
	return data;
}

export async function getSocialLinks(
	siteId: string
): Promise<{ links: SocialLink[]; digest: string }> {
	const { data } = await api<SocialLinksEnvelope>(
		'GET',
		`/api/v1/sites/${siteId}/content/social-links`
	);
	return { links: data.links, digest: data.digest };
}

export async function putSocialLinks(
	siteId: string,
	links: SocialLink[],
	digest: string
): Promise<SocialLinksEnvelope> {
	const { data } = await api<SocialLinksEnvelope>(
		'PUT',
		`/api/v1/sites/${siteId}/content/social-links`,
		{ body: { links }, etag: digest }
	);
	return data;
}

export async function getSiteConfig(
	siteId: string
): Promise<{ config: SiteConfig; amapSecurityCodeSet: boolean; digest: string }> {
	const { data } = await api<SiteConfigEnvelope>('GET', `/api/v1/sites/${siteId}/site-config`);
	return {
		config: data.config,
		amapSecurityCodeSet: data.amapSecurityCodeSet ?? false,
		digest: data.digest
	};
}

/**
 * Saves the site configuration. secrets.amap.securityCode is write-only:
 * undefined keeps the stored value; an empty string clears it.
 */
export async function putSiteConfig(
	siteId: string,
	config: SiteConfig,
	digest: string,
	securityCode?: string
): Promise<SiteConfigEnvelope> {
	const body: Record<string, unknown> = { config };
	if (securityCode !== undefined) {
		body.secrets = { amap: { securityCode } };
	}
	const { data } = await api<SiteConfigEnvelope>('PUT', `/api/v1/sites/${siteId}/site-config`, {
		body: body as never,
		etag: digest
	});
	return data;
}

// --- albums ---

export async function getAlbums(siteId: string): Promise<{ index: AlbumIndex; digest: string }> {
	const { data } = await api<{ index: AlbumIndex; digest: string }>(
		'GET',
		`/api/v1/sites/${siteId}/albums`
	);
	return data;
}

export async function getAlbumYear(
	siteId: string,
	year: string
): Promise<{ year: AlbumYear; digest: string }> {
	const { data } = await api<{ year: AlbumYear; digest: string }>(
		'GET',
		`/api/v1/sites/${siteId}/albums/${year}`
	);
	return data;
}

export async function putAlbumYear(
	siteId: string,
	year: string,
	photos: Array<{ filename: string; title?: string; description?: string; alt?: string }>,
	digest: string
): Promise<{ year: AlbumYear; digest: string }> {
	const { data } = await api<{ year: AlbumYear; digest: string }>(
		'PUT',
		`/api/v1/sites/${siteId}/albums/${year}`,
		{ body: { year: { photos } } as never, etag: digest }
	);
	return data;
}

/** Uploads one original; the returned task id tracks EXIF/thumbnail processing. */
export async function uploadAlbumPhoto(
	siteId: string,
	file: File
): Promise<{ path: string; taskId: string }> {
	const form = new FormData();
	form.append('file', file);
	const { data } = await api<{ path: string; taskId: string }>(
		'POST',
		`/api/v1/sites/${siteId}/albums/photos`,
		{ body: form, multipart: true }
	);
	return data;
}

export async function processAlbums(siteId: string): Promise<{ taskId: string }> {
	const { data } = await api<{ taskId: string }>('POST', `/api/v1/sites/${siteId}/albums/process`, {
		body: {}
	});
	return data;
}

export async function listMediaTasks(siteId: string): Promise<MediaTask[]> {
	const { data } = await api<{ tasks: MediaTask[] }>('GET', `/api/v1/sites/${siteId}/albums/tasks`);
	return data.tasks;
}

export async function deleteAlbumPhoto(
	siteId: string,
	year: string,
	filename: string,
	digest: string
): Promise<void> {
	await api<void>(
		'DELETE',
		`/api/v1/sites/${siteId}/albums/${year}/${encodeURIComponent(filename)}`,
		{
			etag: digest
		}
	);
}

// --- footprints ---

export async function getFootprints(
	siteId: string
): Promise<{ footprints: FootprintsDoc; digest: string }> {
	const { data } = await api<{ footprints: FootprintsDoc; digest: string }>(
		'GET',
		`/api/v1/sites/${siteId}/content/footprints`
	);
	return data;
}

export async function putFootprints(
	siteId: string,
	footprints: FootprintsDoc,
	digest: string
): Promise<{ footprints: FootprintsDoc; digest: string }> {
	const { data } = await api<{ footprints: FootprintsDoc; digest: string }>(
		'PUT',
		`/api/v1/sites/${siteId}/content/footprints`,
		{ body: { footprints }, etag: digest }
	);
	return data;
}
