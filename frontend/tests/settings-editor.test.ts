/**
 * Behavior tests for the settings editor state machine. The API module is
 * mocked; assertions cover the concurrency semantics: dirty-field-only
 * patches, save-in-flight typing, 412 sync convergence, and cross-site load
 * races.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { ApiError } from '../src/lib/api/client';
import type { Site, SiteSettings } from '../src/lib/api/management';

const management = await vi.importActual<typeof import('../src/lib/api/management')>(
	'../src/lib/api/management'
);
vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		getSettings: vi.fn(),
		updateSettings: vi.fn()
	};
});

import { getSite, getSettings, updateSettings } from '../src/lib/api/management';
import { SettingsEditor } from '../src/lib/sites/settings-editor';

const site = (overrides: Partial<Site> = {}): Site => ({
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	visibility: 'private',
	provisionState: 'ready',
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 3,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z',
	...overrides
});

const settings = (overrides: Partial<SiteSettings> = {}): SiteSettings => ({
	siteId: '11111111-1111-1111-1111-111111111111',
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	revision: 3,
	updatedAt: '2026-01-01T00:00:00Z',
	...overrides
});

async function flushed(): Promise<void> {
	// Let pending microtasks (mock resolutions) run.
	for (let i = 0; i < 8; i++) await Promise.resolve();
}

function makeEditor() {
	return new SettingsEditor({ onUnauthenticated: vi.fn() });
}

async function readyEditor() {
	vi.mocked(getSite).mockResolvedValue(site());
	vi.mocked(getSettings).mockResolvedValue({ settings: settings(), etag: '"e-r3"' });
	const editor = makeEditor();
	await editor.load(site().siteId);
	await flushed();
	return editor;
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(getSettings).mockReset();
	vi.mocked(updateSettings).mockReset();
});

describe('settings editor', () => {
	it('loads the site and settings into the form', async () => {
		const editor = await readyEditor();
		expect(editor.getSnapshot().phase.kind).toBe('ready');
		expect(editor.getSnapshot().name).toBe('Baseline Name');
		expect(editor.dirtyFields()).toEqual([]);
	});

	it('submits only the field the user actually changed', async () => {
		const editor = await readyEditor();
		editor.setField('name', 'New Name');
		expect(editor.dirtyFields()).toEqual(['name']);

		vi.mocked(updateSettings).mockResolvedValue({
			settings: settings({ name: 'New Name', revision: 4 }),
			etag: '"e-r4"'
		});
		await editor.save();
		await flushed();

		expect(updateSettings).toHaveBeenCalledWith(site().siteId, { name: 'New Name' }, '"e-r3"');
		expect(editor.getSnapshot().phase).toEqual({ kind: 'saved', revision: 4 });
	});

	it('refuses to save when nothing is dirty', async () => {
		const editor = await readyEditor();
		await editor.save();
		expect(updateSettings).not.toHaveBeenCalled();
	});

	it('keeps typing during a save visible as unsaved afterwards', async () => {
		const editor = await readyEditor();
		editor.setField('name', 'New Name');

		let resolveSave!: (v: { settings: SiteSettings; etag: string }) => void;
		vi.mocked(updateSettings).mockReturnValue(
			new Promise((resolve) => {
				resolveSave = resolve;
			})
		);
		const saving = editor.save();
		expect(editor.getSnapshot().phase.kind).toBe('saving');

		// The user keeps typing while the request is in flight.
		editor.setField('description', 'Typed during flight');
		resolveSave({ settings: settings({ name: 'New Name', revision: 4 }), etag: '"e-r4"' });
		await saving;
		await flushed();

		// Baseline adopted the server merge; the typed description stays in
		// the form and is correctly reported as an unsaved edit.
		expect(editor.getSnapshot().phase).toEqual({ kind: 'saved', revision: 4 });
		expect(editor.getSnapshot().description).toBe('Typed during flight');
		expect(editor.dirtyFields()).toEqual(['description']);
	});

	it('on 412, sync adopts server values for untouched fields and keeps user edits dirty', async () => {
		const editor = await readyEditor();
		editor.setField('name', 'My Rename');

		vi.mocked(updateSettings).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'stale', '"e-r5"')
		);
		await editor.save();
		await flushed();
		expect(editor.getSnapshot().phase.kind).toBe('conflict');

		// Meanwhile someone else changed the description server-side.
		vi.mocked(getSettings).mockResolvedValue({
			settings: settings({
				name: 'Baseline Name',
				description: 'Changed elsewhere',
				revision: 5
			}),
			etag: '"e-r5"'
		});
		await editor.syncAfterConflict();
		await flushed();

		const phase = editor.getSnapshot().phase;
		expect(phase.kind).toBe('conflict');
		if (phase.kind === 'conflict') {
			expect(phase.synced).toBe(true);
			expect(phase.serverRevision).toBe(5);
		}

		// Untouched field converged to the server value; the user's edit
		// stays dirty. The stale description must NOT be re-sent.
		expect(editor.getSnapshot().description).toBe('Changed elsewhere');
		expect(editor.dirtyFields()).toEqual(['name']);

		vi.mocked(updateSettings).mockResolvedValue({
			settings: settings({ name: 'My Rename', description: 'Changed elsewhere', revision: 6 }),
			etag: '"e-r6"'
		});
		await editor.save();
		await flushed();
		expect(updateSettings).toHaveBeenCalledWith(site().siteId, { name: 'My Rename' }, '"e-r5"');
	});

	it('keeps a same-field conflict explicitly dirty after sync', async () => {
		const editor = await readyEditor();
		editor.setField('name', 'My Rename');

		vi.mocked(updateSettings).mockRejectedValue(
			new ApiError(412, 'precondition_failed', 'stale', '"e-r5"')
		);
		await editor.save();
		await flushed();

		// The server also changed the same field.
		vi.mocked(getSettings).mockResolvedValue({
			settings: settings({ name: 'Server Rename', revision: 5 }),
			etag: '"e-r5"'
		});
		await editor.syncAfterConflict();
		await flushed();

		// The user's diverging edit survives and is the only dirty field; the
		// next save explicitly overwrites after the user saw the conflict.
		expect(editor.getSnapshot().name).toBe('My Rename');
		expect(editor.dirtyFields()).toEqual(['name']);
	});

	it('ignores out-of-order loads for a different site', async () => {
		const siteA = site();
		const siteB = site({
			siteId: '22222222-2222-2222-2222-222222222222',
			slug: 'b',
			visibility: 'private',
			provisionState: 'ready',
			name: 'Site B',
			description: 'Site B description'
		});

		let resolveA!: (v: Site) => void;
		let resolveB!: (v: Site) => void;
		vi.mocked(getSite)
			.mockReturnValueOnce(new Promise((r) => (resolveA = r)))
			.mockReturnValueOnce(new Promise((r) => (resolveB = r)));
		vi.mocked(getSettings).mockImplementation(async () => ({
			settings: settings(),
			etag: '"e-r3"'
		}));

		const editor = makeEditor();
		const loadA = editor.load(siteA.siteId);
		const loadB = editor.load(siteB.siteId);

		resolveB(siteB);
		await loadB;
		await flushed();
		resolveA(siteA); // the stale response arrives last
		await loadA;
		await flushed();

		expect(editor.getSnapshot().site?.siteId).toBe(siteB.siteId);
		expect(editor.getSnapshot().name).toBe('Baseline Name');
		expect(editor.dirtyFields()).toEqual([]);
	});

	it('routes a 401 load to the unauthenticated handler', async () => {
		vi.mocked(getSite).mockRejectedValue(new ApiError(401, 'unauthenticated', '', null));
		const onUnauthenticated = vi.fn();
		const editor = new SettingsEditor({
			onUnauthenticated: async () => {
				onUnauthenticated();
			}
		});
		await editor.load(site().siteId);
		await flushed();
		expect(onUnauthenticated).toHaveBeenCalledTimes(1);
		expect(management.getSite).toBeDefined();
	});
});
