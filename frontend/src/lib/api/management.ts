/**
 * Typed business module over the generated OpenAPI schema. Every endpoint of
 * the management contract used by the admin console lives here; components
 * never call fetch directly.
 */
import { api, setCsrfToken } from './client';
import type { components } from './schema';

export type User = components['schemas']['User'];
export type Site = components['schemas']['Site'];
export type SiteSettings = components['schemas']['SiteSettings'];
export type SiteSettingsPatch = components['schemas']['SiteSettingsPatch'];
export type SessionInfo = components['schemas']['SessionInfo'];
export type CeremonyDone = components['schemas']['CeremonyDone'];
export type CreationOptions = components['schemas']['PublicKeyCredentialCreationOptions'];
export type RequestOptions = components['schemas']['PublicKeyCredentialRequestOptions'];
export type CreationOptionsEnvelope = components['schemas']['CreationOptionsEnvelope'];
export type AssertionOptionsEnvelope = components['schemas']['AssertionOptionsEnvelope'];

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

export async function loginOptions(): Promise<RequestOptions> {
	const { data } = await api<AssertionOptionsEnvelope>('POST', '/api/v1/auth/login/options', {
		body: {}
	});
	return data.publicKey;
}

export async function loginVerify(responseJSON: unknown): Promise<CeremonyDone> {
	const { data } = await api<CeremonyDone>('POST', '/api/v1/auth/login/verify', {
		body: responseJSON
	});
	setCsrfToken(data.csrfToken);
	return data;
}

export async function registerOptions(permit: string): Promise<CreationOptions> {
	const { data } = await api<CreationOptionsEnvelope>('POST', '/api/v1/auth/register/options', {
		body: { permit }
	});
	return data.publicKey;
}

export async function registerVerify(responseJSON: unknown): Promise<CeremonyDone> {
	const { data } = await api<CeremonyDone>('POST', '/api/v1/auth/register/verify', {
		body: responseJSON
	});
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
