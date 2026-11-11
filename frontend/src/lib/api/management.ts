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
