/**
 * Draft-preservation regressions for the theme-selection store: input typed
 * during a save, and edits made before a 412 conflict sync, must survive;
 * the saved-baseline display must not follow the unsaved radio draft.
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

const site = (): Site => ({
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Site',
	description: '',
	defaultLocale: 'zh-CN',
	role: 'owner',
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

const selection = (selected: boolean, revision: number, config: unknown = {}): SiteThemeSelection => ({
	siteId: site().siteId,
	selected,
	theme: selected ? { themeId: 'fuyao-homepage', version: '27.1.1', manifestDigest: 'a'.repeat(64) } : null,
	themeConfig: selected ? (config as Record<string, never>) : null,
	revision,
	updatedAt: '2026-01-01T00:00:00Z'
});

async function flushed(rounds = 8): Promise<void> {
	for (let i = 0; i < rounds; i++) await Promise.resolve();
}

function readyStore(savedConfig: Record<string, unknown> = { flag: true }) {
	vi.mocked(getSite).mockResolvedValue(site());
	vi.mocked(listThemes).mockResolvedValue([theme]);
	vi.mocked(getSiteTheme).mockResolvedValue({
		selection: selection(true, 1, savedConfig),
		etag: '"s-t1"'
	});
	return new ThemeSelection({ onUnauthenticated: vi.fn() });
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(listThemes).mockReset();
	vi.mocked(getSiteTheme).mockReset();
	vi.mocked(putSiteTheme).mockReset();
});

describe('theme selection draft preservation', () => {
	it('keeps text typed during the save as an unsaved draft', async () => {
		const store = readyStore();
		await store.load(site().siteId);
		await flushed();
		expect(store.getSnapshot().configText).toBe('{\n  "flag": true\n}');

		let resolveSave!: (v: { selection: SiteThemeSelection; etag: string }) => void;
		vi.mocked(putSiteTheme).mockReturnValue(
			new Promise((resolve) => {
				resolveSave = resolve;
			})
		);
		const saving = store.save();
		// The user keeps editing while the request is in flight.
		store.setConfig('{\n  "flag": true,\n  "draft": "typed"\n}');
		resolveSave({ selection: selection(true, 2, { flag: true }), etag: '"s-t2"' });
		await saving;
		await flushed();

		const state = store.getSnapshot();
		expect(state.phase.kind).toBe('saved');
		// The typed draft survived; the baseline adopted the server value.
		expect(state.configText).toBe('{\n  "flag": true,\n  "draft": "typed"\n}');
		expect(store.dirty()).toBe(true);
	});

	it('keeps genuinely edited config through a 412 conflict sync', async () => {
		const store = readyStore();
		await store.load(site().siteId);
		await flushed();
		store.setConfig('{\n  "edited": "by user"\n}');

		vi.mocked(putSiteTheme).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'stale', '"s-t2"')
		);
		await store.save();
		await flushed();
		expect(store.getSnapshot().phase.kind).toBe('conflict');

		// The server meanwhile saved a different config.
		vi.mocked(getSiteTheme).mockResolvedValue({
			selection: selection(true, 2, { server: 'value' }),
			etag: '"s-t2"'
		});
		await store.syncAfterConflict(site().siteId);
		await flushed();

		const state = store.getSnapshot();
		// The user's edit survives; the baseline is the server value.
		expect(state.configText).toBe('{\n  "edited": "by user"\n}');
		expect(store.dirty()).toBe(true);
	});

	it('adopts server config on sync only when the draft was untouched', async () => {
		const store = readyStore();
		await store.load(site().siteId);
		await flushed();

		vi.mocked(putSiteTheme).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'stale', '"s-t2"')
		);
		await store.save();
		await flushed();

		vi.mocked(getSiteTheme).mockResolvedValue({
			selection: selection(true, 2, { server: 'value' }),
			etag: '"s-t2"'
		});
		await store.syncAfterConflict(site().siteId);
		await flushed();

		const state = store.getSnapshot();
		expect(state.configText).toBe('{\n  "server": "value"\n}');
		expect(store.dirty()).toBe(false);
	});

	it('shows the saved selection, not the unsaved radio draft', async () => {
		const store = readyStore();
		await store.load(site().siteId);
		await flushed();

		// The user flips the radio but has not saved.
		expect(store.getSnapshot().savedSelected).toBe('fuyao-homepage@27.1.1');
		expect(store.getSnapshot().hasSelection).toBe(true);
		store.select('other-theme@9.9.9');
		expect(store.getSnapshot().savedSelected).toBe('fuyao-homepage@27.1.1');
		expect(store.dirty()).toBe(true);
	});

	it('keeps the form editable after invalid JSON', async () => {
		const store = readyStore();
		await store.load(site().siteId);
		await flushed();
		store.setConfig('{broken');
		await store.save();
		expect(putSiteTheme).not.toHaveBeenCalled();
		const state = store.getSnapshot();
		expect(state.phase.kind).toBe('error');
		// The draft is intact and still editable.
		expect(state.configText).toBe('{broken');
		store.setConfig('{}');
		expect(store.getSnapshot().configText).toBe('{}');
	});
});
