import { beforeEach, expect, it, vi } from 'vitest';
import type { Site } from '../src/lib/api/management';

const { auth, api } = vi.hoisted(() => ({ auth: { generation: 0 }, api: { listSites: vi.fn() } }));
vi.mock('../src/lib/api/client', () => ({ currentAuthGeneration: () => auth.generation }));
vi.mock('../src/lib/api/management', () => ({ listSites: api.listSites }));
let cache: typeof import('../src/lib/sites/site-list');

function site(id: string): Site {
	return {
		siteId: id,
		slug: id,
		name: id,
		description: '',
		defaultLocale: 'zh-CN',
		visibility: 'private',
		provisionState: 'ready',
		role: 'owner',
		settingsRevision: 1,
		createdAt: '2026-01-01T00:00:00Z',
		updatedAt: '2026-01-01T00:00:00Z'
	};
}

function pending() {
	let resolve!: (sites: Site[]) => void;
	const promise = new Promise<Site[]>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}

beforeEach(async () => {
	vi.resetModules();
	api.listSites.mockReset();
	auth.generation++;
	cache = await import('../src/lib/sites/site-list');
});

it('hides old-account data immediately and discards its late response', async () => {
	const previous = pending();
	api.listSites.mockReturnValueOnce(previous.promise).mockResolvedValueOnce([site('bob')]);
	const first = cache.loadSiteList();
	auth.generation++;
	expect(cache.siteListSnapshot().sites).toEqual([]);
	expect(cache.siteListSnapshot()).toBe(cache.siteListSnapshot());
	await cache.loadSiteList();
	previous.resolve([site('alice')]);
	await first;
	expect(cache.siteListSnapshot().sites.map((item) => item.siteId)).toEqual(['bob']);
});

it('rejects a previous-account mutation response', async () => {
	api.listSites.mockResolvedValue([site('alice')]);
	await cache.loadSiteList();
	const previous = auth.generation++;
	api.listSites.mockResolvedValue([site('bob')]);
	await cache.loadSiteList();
	cache.upsertSite(site('alice-new'), previous);
	expect(cache.siteListSnapshot().sites.map((item) => item.siteId)).toEqual(['bob']);
});

it('preserves a committed mutation when an older list query finishes', async () => {
	const query = pending();
	api.listSites.mockReturnValue(query.promise);
	const loading = cache.loadSiteList();
	cache.upsertSite(site('new'), auth.generation);
	query.resolve([site('old')]);
	await loading;
	expect(cache.siteListSnapshot().sites.map((item) => item.siteId)).toEqual(['new', 'old']);
});
