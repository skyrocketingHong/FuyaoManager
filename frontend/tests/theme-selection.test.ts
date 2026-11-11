/**
 * Behavior tests for the theme-selection store: catalog + selection load,
 * If-Match save with configuration, 412 conflict sync, and viewer
 * read-only enforcement.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { ApiError } from '../src/lib/api/client';
import type { Site, SiteThemeSelection, Theme } from '../src/lib/api/management';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		listThemes: vi.fn(),
		getSiteTheme: vi.fn(),
		putSiteTheme: vi.fn()
	};
});

import { getSite, listThemes, getSiteTheme, putSiteTheme } from '../src/lib/api/management';
import { ThemeSelection } from '../src/lib/themes/theme-selection';

const site = (role: 'owner' | 'viewer' = 'owner'): Site => ({
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Site',
	description: '',
	defaultLocale: 'zh-CN',
	role,
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
});

const theme: Theme = {
	id: 'row-1',
	themeId: 'fuyao-homepage',
	version: '27.1.1',
	adapter: 'node22-sveltekit-static',
	inputProtocol: 'fuyao.theme.build-input/v1',
	capabilities: ['blog'],
	outputs: { spaFallback: '200.html', urlConventions: ['posts'], requiredArtifacts: [] },
	configSchema: null,
	manifestDigest: 'a'.repeat(64),
	registeredAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

const selection = (selected: boolean, revision: number): SiteThemeSelection => ({
	siteId: site().siteId,
	selected,
	theme: selected ? { themeId: 'fuyao-homepage', version: '27.1.1', manifestDigest: 'a'.repeat(64) } : null,
	themeConfig: selected ? {} : null,
	revision,
	updatedAt: '2026-01-01T00:00:00Z'
});

async function flushed(): Promise<void> {
	for (let i = 0; i < 8; i++) await Promise.resolve();
}

function makeStore() {
	return new ThemeSelection({ onUnauthenticated: vi.fn() });
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(listThemes).mockReset();
	vi.mocked(getSiteTheme).mockReset();
	vi.mocked(putSiteTheme).mockReset();
});

describe('theme selection', () => {
	it('loads catalog, site, and the unselected state', async () => {
		vi.mocked(getSite).mockResolvedValue(site());
		vi.mocked(listThemes).mockResolvedValue([theme]);
		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(false, 0), etag: '"s-t0"' });
		const store = makeStore();
		await store.load(site().siteId);
		await flushed();
		expect(store.getSnapshot().phase.kind).toBe('ready');
		expect(store.getSnapshot().hasSelection).toBe(false);
		expect(store.getSnapshot().themes).toHaveLength(1);
	});

	it('saves the selection with If-Match and adopts the response', async () => {
		vi.mocked(getSite).mockResolvedValue(site());
		vi.mocked(listThemes).mockResolvedValue([theme]);
		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(false, 0), etag: '"s-t0"' });
		const store = makeStore();
		await store.load(site().siteId);
		await flushed();

		store.select('fuyao-homepage@27.1.1');
		store.setConfig('{"flag":true}');
		vi.mocked(putSiteTheme).mockResolvedValue({
			selection: selection(true, 1),
			etag: '"s-t1"'
		});
		await store.save();
		await flushed();

		expect(putSiteTheme).toHaveBeenCalledWith(
			site().siteId,
			{ themeId: 'fuyao-homepage', version: '27.1.1', themeConfig: { flag: true } },
			'"s-t0"'
		);
		expect(store.getSnapshot().phase.kind).toBe('saved');
		expect(store.getSnapshot().hasSelection).toBe(true);
	});

	it('rejects invalid configuration JSON before sending', async () => {
		vi.mocked(getSite).mockResolvedValue(site());
		vi.mocked(listThemes).mockResolvedValue([theme]);
		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(false, 0), etag: '"s-t0"' });
		const store = makeStore();
		await store.load(site().siteId);
		await flushed();
		store.select('fuyao-homepage@27.1.1');
		store.setConfig('{not json');
		await store.save();
		expect(putSiteTheme).not.toHaveBeenCalled();
		expect(store.getSnapshot().phase.kind).toBe('error');
	});

	it('offers a conflict sync on 412', async () => {
		vi.mocked(getSite).mockResolvedValue(site());
		vi.mocked(listThemes).mockResolvedValue([theme]);
		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(false, 0), etag: '"s-t0"' });
		const store = makeStore();
		await store.load(site().siteId);
		await flushed();
		store.select('fuyao-homepage@27.1.1');

		vi.mocked(putSiteTheme).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'stale', '"s-t1"')
		);
		await store.save();
		await flushed();
		expect(store.getSnapshot().phase.kind).toBe('conflict');

		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(true, 1), etag: '"s-t1"' });
		await store.syncAfterConflict(site().siteId);
		await flushed();
		const phase = store.getSnapshot().phase;
		expect(phase.kind).toBe('conflict');
		if (phase.kind === 'conflict') expect(phase.synced).toBe(true);
		expect(store.getSnapshot().hasSelection).toBe(true);
	});

	it('refuses saves for viewer-role members', async () => {
		vi.mocked(getSite).mockResolvedValue(site('viewer'));
		vi.mocked(listThemes).mockResolvedValue([theme]);
		vi.mocked(getSiteTheme).mockResolvedValue({ selection: selection(false, 0), etag: '"s-t0"' });
		const store = makeStore();
		await store.load(site().siteId);
		await flushed();
		expect(store.editable()).toBe(false);
		await store.save();
		expect(putSiteTheme).not.toHaveBeenCalled();
	});
});
