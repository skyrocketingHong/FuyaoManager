/**
 * Reactive-view verification: mounts the real SiteSettingsView component in
 * jsdom (no browser) and asserts that UI derivations — the save button
 * state and the status notices — actually update through Svelte reactivity
 * when the editor's baseline or form changes.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import type { Site, SiteSettings } from '../src/lib/api/management';

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
import SiteSettingsView from '../src/lib/components/sites/SiteSettingsView.svelte';
import { initLocale } from '../src/lib/i18n/index.svelte';

const baseSite: Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 3,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

const baseSettings: SiteSettings = {
	siteId: baseSite.siteId,
	name: 'Baseline Name',
	description: 'Baseline description',
	defaultLocale: 'zh-CN',
	revision: 3,
	updatedAt: '2026-01-01T00:00:00Z'
};

async function settled(): Promise<void> {
	for (let i = 0; i < 12; i++) await Promise.resolve();
	flushSync();
}

let target: HTMLElement | null = null;

function mountView(siteId = baseSite.siteId) {
	target = document.createElement('div');
	document.body.appendChild(target);
	return mount(SiteSettingsView, { target, props: { siteId } });
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(getSettings).mockReset();
	vi.mocked(updateSettings).mockReset();
	localStorage.clear();
	localStorage.setItem('fuyao-admin-locale', 'zh-CN');
	initLocale();
});

afterEach(() => {
	if (target) {
		document.body.removeChild(target);
		target = null;
	}
});

function saveButton(): HTMLButtonElement {
	const button = document.querySelector('button[type="submit"]');
	if (!(button instanceof HTMLButtonElement)) throw new Error('save button missing');
	return button;
}

function nameInput(): HTMLInputElement {
	const input = document.querySelector('input:not([type="hidden"])');
	if (!(input instanceof HTMLInputElement)) throw new Error('name input missing');
	return input;
}

function descriptionInput(): HTMLTextAreaElement {
	const input = document.querySelector('textarea');
	if (!(input instanceof HTMLTextAreaElement)) throw new Error('description textarea missing');
	return input;
}

async function readyView() {
	vi.mocked(getSite).mockResolvedValue(baseSite);
	vi.mocked(getSettings).mockResolvedValue({ settings: baseSettings, etag: '"e-r3"' });
	mountView();
	await settled();
}

describe('SiteSettingsView reactive derivations', () => {
	it('save button stays disabled while nothing is dirty', async () => {
		await readyView();
		expect(saveButton().disabled).toBe(true);
	});

	it('enables the save button on the first edit and re-disables after a clean save', async () => {
		await readyView();
		const name = nameInput();
		name.value = 'New Name';
		name.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(saveButton().disabled).toBe(false);

		vi.mocked(updateSettings).mockResolvedValue({
			settings: { ...baseSettings, name: 'New Name', revision: 4 },
			etag: '"e-r4"'
		});
		saveButton().click();
		await settled();

		// Adopted baseline recomputes the derivation through real reactivity:
		// no clickable-but-inert button, and a saved notice is shown.
		expect(saveButton().disabled).toBe(true);
		expect(document.body.textContent).toContain('已保存为修订号 4');
	});

	it('never labels in-flight typing as saved', async () => {
		await readyView();
		const name = nameInput();
		name.value = 'New Name';
		name.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		let resolveSave!: (v: { settings: SiteSettings; etag: string }) => void;
		vi.mocked(updateSettings).mockReturnValue(
			new Promise((resolve) => {
				resolveSave = resolve;
			})
		);
		saveButton().click();
		await settled();

		const description = descriptionInput();
		description.value = 'Typed during flight';
		description.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		resolveSave({
			settings: { ...baseSettings, name: 'New Name', revision: 4 },
			etag: '"e-r4"'
		});
		await settled();

		// The view must report the save plus the surviving unsaved edit, not
		// a plain "saved", and the button must remain actionable for it.
		expect(document.body.textContent).toContain('你还有新的未保存修改');
		expect(saveButton().disabled).toBe(false);
		expect(descriptionInput().value).toBe('Typed during flight');
	});

	it('adopts server-side normalization when nothing was typed after submitting', async () => {
		await readyView();
		const name = nameInput();
		name.value = '  Spaced Name  ';
		name.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();
		expect(saveButton().disabled).toBe(false);

		// The server trims the name; with no in-flight input the form must
		// adopt the normalized value and converge to clean — otherwise the
		// raw whitespace would pin the form dirty across repeated saves.
		vi.mocked(updateSettings).mockResolvedValue({
			settings: { ...baseSettings, name: 'Spaced Name', revision: 4 },
			etag: '"e-r4"'
		});
		saveButton().click();
		await settled();

		expect(nameInput().value).toBe('Spaced Name');
		expect(saveButton().disabled).toBe(true);
		expect(document.body.textContent).toContain('已保存为修订号 4');
		expect(document.body.textContent).not.toContain('你还有新的未保存修改');
	});

	it('keeps in-flight edits dirty while untouched fields adopt normalization', async () => {
		await readyView();
		const name = nameInput();
		name.value = '  Spaced Name  ';
		name.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		let resolveSave!: (v: { settings: SiteSettings; etag: string }) => void;
		vi.mocked(updateSettings).mockReturnValue(
			new Promise((resolve) => {
				resolveSave = resolve;
			})
		);
		saveButton().click();
		await settled();

		// The user edits the description while the save is in flight; that
		// newer input must survive the response and stay dirty, while the
		// untouched (submitted) name adopts the server's trimmed value.
		const description = descriptionInput();
		description.value = 'Edited during flight';
		description.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		resolveSave({
			settings: { ...baseSettings, name: 'Spaced Name', revision: 4 },
			etag: '"e-r4"'
		});
		await settled();

		expect(nameInput().value).toBe('Spaced Name');
		expect(descriptionInput().value).toBe('Edited during flight');
		expect(saveButton().disabled).toBe(false);
		expect(document.body.textContent).toContain('你还有新的未保存修改');
	});
});
