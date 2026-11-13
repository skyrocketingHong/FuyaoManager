import { api, type ApiResult } from './client';
import type { components } from './schema';

export type ContentSources = components['schemas']['ContentSourceSelection'];
export type ContentResource = keyof ContentSources;
export type ContentTarget = string | { readonly type: 'platform' };
export const platformContent: ContentTarget = Object.freeze({ type: 'platform' });
export const contentResources: readonly ContentResource[] = [
	'posts',
	'albums',
	'footprints',
	'friends'
];
export type ContentCounts = components['schemas']['ContentCounts'];
export type PlatformContentMeta = components['schemas']['PlatformContent'];
export type ContentSourcesDocument = components['schemas']['ContentSourceRecord'];
export type SiteOverview = components['schemas']['SiteOverview'];

export function contentResourcePath(target: ContentTarget, resource: ContentResource): string {
	if (typeof target !== 'string') return `/api/v1/content/${resource}`;
	const nested = resource === 'friends' || resource === 'footprints' ? '/content' : '';
	return `/api/v1/sites/${encodeURIComponent(target)}${nested}/${resource}`;
}
export async function getPlatformContent(signal?: AbortSignal): Promise<PlatformContentMeta> {
	return (await api<PlatformContentMeta>('GET', '/api/v1/content', { signal })).data;
}
export async function getContentSources(
	siteId: string,
	signal?: AbortSignal
): Promise<ApiResult<ContentSourcesDocument>> {
	const response = await api<ContentSourcesDocument>(
		'GET',
		`/api/v1/sites/${encodeURIComponent(siteId)}/content-sources`,
		{ signal }
	);
	if (!response.etag) throw new Error('Missing content sources ETag');
	return response;
}
export async function putContentSources(
	siteId: string,
	body: ContentSourcesDocument,
	etag: string
): Promise<ApiResult<ContentSourcesDocument>> {
	const response = await api<ContentSourcesDocument>(
		'PUT',
		`/api/v1/sites/${encodeURIComponent(siteId)}/content-sources`,
		{ body, etag }
	);
	if (!response.etag) throw new Error('Missing content sources ETag');
	return response;
}
export async function getSiteOverview(siteId: string, signal?: AbortSignal): Promise<SiteOverview> {
	return (
		await api<SiteOverview>('GET', `/api/v1/sites/${encodeURIComponent(siteId)}/overview`, {
			signal
		})
	).data;
}
