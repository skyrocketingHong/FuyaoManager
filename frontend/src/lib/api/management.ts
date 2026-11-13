/**
 * Typed business module over the generated OpenAPI schema. Every endpoint of
 * the management contract used by the admin console lives here; components
 * never call fetch directly.
 */
import { api, setCsrfToken } from './client';
import type { components } from './schema';
import { contentResourcePath, type ContentTarget } from './content';

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
export type SiteEdgeState = components['schemas']['SiteEdgeState'];
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
	// The CSRF write happens in the session store only after its generation
	// guard accepts the read — a stale response must never touch the token
	// a newer login installed.
	const { data } = await api<SessionInfo>('GET', '/api/v1/session');
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
	// The CSRF clear happens in the session store only when the sign-out's
	// generation guard accepts it — a late logout must not erase the token
	// of a session that raced past it.
	await api<void>('POST', '/api/v1/auth/logout', { body: {} });
}

export type CreationOptionsEnvelope = components['schemas']['CreationOptionsEnvelope'];
export type AssertionOptionsEnvelope = components['schemas']['AssertionOptionsEnvelope'];
export type CredentialCreationResponse = components['schemas']['CredentialCreationResponse'];
export type CredentialAssertionResponse = components['schemas']['CredentialAssertionResponse'];

/** Starts a discoverable-credential passkey login. */
export async function beginPasskeyLogin(): Promise<AssertionOptionsEnvelope> {
	const { data } = await api<AssertionOptionsEnvelope>('POST', '/api/v1/auth/login/options', {
		body: {}
	});
	return data;
}

/** Completes a passkey login; the server mints a fresh full session. */
export async function finishPasskeyLogin(
	assertion: CredentialAssertionResponse
): Promise<CeremonyDone> {
	const { data } = await api<CeremonyDone>('POST', '/api/v1/auth/login/verify', {
		body: assertion
	});
	setCsrfToken(data.csrfToken);
	return data;
}

/** Starts the session-bound passkey-binding ceremony (restricted or full session). */
export async function beginPasskeyBinding(): Promise<CreationOptionsEnvelope> {
	const { data } = await api<CreationOptionsEnvelope>(
		'POST',
		'/api/v1/auth/passkey/register/options',
		{ body: {} }
	);
	return data;
}

/** Completes the binding; the server rotates the session to a full one. */
export async function finishPasskeyBinding(
	attestation: CredentialCreationResponse
): Promise<CeremonyDone> {
	const { data } = await api<CeremonyDone>('POST', '/api/v1/auth/passkey/register/verify', {
		body: attestation
	});
	setCsrfToken(data.csrfToken);
	return data;
}

export async function updateAccountProfile(username: string): Promise<User> {
	const { data } = await api<User>('PATCH', '/api/v1/account/profile', { body: { username } });
	return data;
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
	siteId: ContentTarget,
	query: string,
	page: number,
	pageSize: number
): Promise<PostList> {
	const { data } = await api<PostList>(
		'GET',
		`${contentResourcePath(siteId, 'posts')}?query=${encodeURIComponent(query)}&page=${page}&pageSize=${pageSize}`
	);
	return data;
}

export async function createPost(siteId: ContentTarget, post: Partial<Post>): Promise<Post> {
	const { data } = await api<Post>('POST', contentResourcePath(siteId, 'posts'), { body: post });
	return data;
}

export async function getPost(
	siteId: ContentTarget,
	postPath: string
): Promise<{ post: Post; etag: string }> {
	const { data, etag } = await api<Post>(
		'GET',
		`${contentResourcePath(siteId, 'posts')}/${encodeURIComponent(postPath)}`
	);
	if (etag === null) throw new Error('post response missing ETag');
	return { post: data, etag };
}

export async function putPost(
	siteId: ContentTarget,
	postPath: string,
	post: Partial<Post>,
	etag: string
): Promise<Post> {
	const { data } = await api<Post>(
		'PUT',
		`${contentResourcePath(siteId, 'posts')}/${encodeURIComponent(postPath)}`,
		{
			body: post,
			etag
		}
	);
	return data;
}

export async function deletePost(
	siteId: ContentTarget,
	postPath: string,
	etag: string
): Promise<void> {
	await api<void>(
		'DELETE',
		`${contentResourcePath(siteId, 'posts')}/${encodeURIComponent(postPath)}`,
		{
			etag
		}
	);
}

export async function getFriends(
	siteId: ContentTarget
): Promise<{ friends: FriendsEnvelope['friends']; digest: string }> {
	const { data } = await api<FriendsEnvelope>('GET', contentResourcePath(siteId, 'friends'));
	return { friends: data.friends, digest: data.digest };
}

export async function putFriends(
	siteId: ContentTarget,
	friends: FriendsEnvelope['friends'],
	digest: string
): Promise<FriendsEnvelope> {
	const { data } = await api<FriendsEnvelope>('PUT', contentResourcePath(siteId, 'friends'), {
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

export async function getSiteConfig(siteId: string): Promise<{
	config: SiteConfig;
	amapSecurityCodeSet: boolean;
	effectiveOrigin: string;
	digest: string;
}> {
	const { data } = await api<SiteConfigEnvelope>('GET', `/api/v1/sites/${siteId}/site-config`);
	return {
		config: data.config,
		amapSecurityCodeSet: data.amapSecurityCodeSet ?? false,
		effectiveOrigin: data.effectiveOrigin ?? '',
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

export async function getAlbums(
	siteId: ContentTarget
): Promise<{ index: AlbumIndex; digest: string }> {
	const { data } = await api<{ index: AlbumIndex; digest: string }>(
		'GET',
		contentResourcePath(siteId, 'albums')
	);
	return data;
}

export async function getAlbumYear(
	siteId: ContentTarget,
	year: string
): Promise<{ year: AlbumYear; digest: string }> {
	const { data } = await api<{ year: AlbumYear; digest: string }>(
		'GET',
		`${contentResourcePath(siteId, 'albums')}/${encodeURIComponent(year)}`
	);
	return data;
}

export async function putAlbumYear(
	siteId: ContentTarget,
	year: string,
	photos: Array<{ filename: string; title?: string; description?: string; alt?: string }>,
	digest: string,
	title = '',
	cover = ''
): Promise<{ year: AlbumYear; digest: string }> {
	const { data } = await api<{ year: AlbumYear; digest: string }>(
		'PUT',
		`${contentResourcePath(siteId, 'albums')}/${encodeURIComponent(year)}`,
		{ body: { year: { title, cover, photos } } as never, etag: digest }
	);
	return data;
}

/** Uploads one original; the returned task id tracks EXIF/thumbnail processing. */
export async function uploadAlbumPhoto(
	siteId: ContentTarget,
	file: File
): Promise<{ path: string; taskId: string }> {
	const form = new FormData();
	form.append('file', file);
	const { data } = await api<{ path: string; taskId: string }>(
		'POST',
		`${contentResourcePath(siteId, 'albums')}/photos`,
		{ body: form, multipart: true }
	);
	return data;
}

export async function processAlbums(siteId: ContentTarget): Promise<{ taskId: string }> {
	const { data } = await api<{ taskId: string }>(
		'POST',
		`${contentResourcePath(siteId, 'albums')}/process`,
		{
			body: {}
		}
	);
	return data;
}

export async function listMediaTasks(siteId: ContentTarget): Promise<MediaTask[]> {
	const { data } = await api<{ tasks: MediaTask[] }>(
		'GET',
		`${contentResourcePath(siteId, 'albums')}/tasks`
	);
	return data.tasks;
}

export async function deleteAlbumPhoto(
	siteId: ContentTarget,
	year: string,
	filename: string,
	digest: string,
	mode: 'delete' | 'detach' = 'delete'
): Promise<void> {
	await api<void>(
		'DELETE',
		`${contentResourcePath(siteId, 'albums')}/${encodeURIComponent(year)}/${encodeURIComponent(filename)}?mode=${mode}`,
		{
			etag: digest
		}
	);
}

// --- footprints ---

export async function getFootprints(
	siteId: ContentTarget
): Promise<{ footprints: FootprintsDoc; digest: string }> {
	const { data } = await api<{ footprints: FootprintsDoc; digest: string }>(
		'GET',
		contentResourcePath(siteId, 'footprints')
	);
	return data;
}

export async function putFootprints(
	siteId: ContentTarget,
	footprints: FootprintsDoc,
	digest: string
): Promise<{ footprints: FootprintsDoc; digest: string }> {
	const { data } = await api<{ footprints: FootprintsDoc; digest: string }>(
		'PUT',
		contentResourcePath(siteId, 'footprints'),
		{ body: { footprints }, etag: digest }
	);
	return data;
}

// --- managed edge entry ---

export async function getSiteEdge(siteId: string): Promise<SiteEdgeState> {
	const { data } = await api<SiteEdgeState>('GET', `/api/v1/sites/${siteId}/edge`);
	return data;
}

/** Re-runs render + validate + reload for the managed snippet. */
export async function reapplySiteEdge(siteId: string): Promise<void> {
	await api<void>('POST', `/api/v1/sites/${siteId}/edge/reapply`, { body: {} });
}
