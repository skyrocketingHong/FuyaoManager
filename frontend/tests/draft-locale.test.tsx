/**
 * Draft continuity tests: switching the UI language or the day/night theme
 * must not remount the editor or drop unsaved input.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act, waitFor, cleanup } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { ConfigProvider } from 'antd';
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

import { getSite, getSettings } from '../src/lib/api/management';
import SettingsPage from '../src/pages/sites/SettingsPage';
import { initLocale, setLocale } from '../src/lib/i18n';
import { initTheme, toggleTheme } from '../src/lib/theme';

const baseSite: Site = {
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

beforeAll(() => {
	if (!window.matchMedia) {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			value: (query: string) => ({
				matches: false,
				media: query,
				onchange: null,
				addListener: () => undefined,
				removeListener: () => undefined,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				dispatchEvent: () => false
			})
		});
	}
});

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	initLocale();
	initTheme();
	vi.mocked(getSite).mockResolvedValue(baseSite);
	vi.mocked(getSettings).mockResolvedValue({ settings: baseSettings, etag: '"e-r3"' });
});

afterEach(cleanup);

function mountPage() {
	const router = createMemoryRouter(
		[{ path: '/sites/:siteId/settings', element: <SettingsPage /> }],
		{ initialEntries: [`/sites/${baseSite.siteId}/settings`] }
	);
	return render(
		<ConfigProvider>
			<RouterProvider router={router} />
		</ConfigProvider>
	);
}

async function settled(): Promise<void> {
	for (let i = 0; i < 12; i++) await Promise.resolve();
}

describe('editor draft continuity', () => {
	it('keeps unsaved input across a locale switch', async () => {
		mountPage();
		const input = await screen.findByDisplayValue('Baseline Name');
		fireEvent.change(input, { target: { value: 'Edited Name' } });
		await settled();
		act(() => setLocale('en-US'));
		await settled();
		expect(screen.getByDisplayValue('Edited Name')).toBeDefined();
		act(() => setLocale('zh-CN'));
		await settled();
		expect(screen.getByDisplayValue('Edited Name')).toBeDefined();
	});

	it('keeps unsaved input across a day/night switch', async () => {
		mountPage();
		const input = await screen.findByDisplayValue('Baseline Name');
		fireEvent.change(input, { target: { value: 'Night Draft' } });
		await settled();
		act(() => toggleTheme());
		await settled();
		expect(screen.getByDisplayValue('Night Draft')).toBeDefined();
		act(() => toggleTheme());
		await settled();
		await waitFor(() => {
			expect(screen.getByDisplayValue('Night Draft')).toBeDefined();
		});
	});
});
