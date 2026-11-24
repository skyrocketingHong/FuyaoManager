// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import AppearanceProvider from '../src/components/AppearanceProvider';
import AppearancePage from '../src/pages/AppearancePage';
import { setLocale } from '../src/lib/i18n';
import {
	accentPresets,
	appearanceStorageKey,
	defaultAppearance,
	initTheme,
	normalizeAppearance,
	setAppearance,
	themeSnapshot,
	toggleTheme
} from '../src/lib/theme';
import { appearanceTheme, contrastRatio } from '../src/lib/theme/appearance';

let stop: (() => void) | undefined;
let dark = false;
let onSystem: (() => void) | undefined;
beforeEach(() => {
	localStorage.clear();
	setLocale('en-US');
	dark = false;
	vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
		get matches() {
			return dark;
		},
		media: query,
		onchange: null,
		addEventListener: (_name: string, listener: EventListenerOrEventListenerObject) => {
			onSystem = listener as () => void;
		},
		removeEventListener: () => {
			onSystem = undefined;
		},
		addListener: () => {},
		removeListener: () => {},
		dispatchEvent: () => true
	}));
	stop = initTheme();
});
afterEach(() => {
	cleanup();
	stop?.();
	vi.restoreAllMocks();
	localStorage.clear();
});

it('validates stored preferences and preserves legacy day/night choices', () => {
	expect(normalizeAppearance({ mode: 'other', accent: 'url(https://example.com)' })).toEqual(
		defaultAppearance
	);
	expect(normalizeAppearance({ mode: 'dark', accent: '#AABBCC' })).toEqual({
		mode: 'dark',
		accent: '#aabbcc',
		contentBackground: null
	});
	localStorage.setItem('fuyao-admin-theme', 'dark');
	stop = initTheme();
	expect(themeSnapshot().dark).toBe(true);
	setAppearance({ accent: '#16856b' });
	expect(JSON.parse(localStorage.getItem(appearanceStorageKey)!)).toEqual({
		mode: 'dark',
		accent: '#16856b',
		contentBackground: null
	});
	stop = initTheme();
	expect(themeSnapshot()).toEqual({ mode: 'dark', dark: true, accent: '#16856b', contentBackground: null });
});

it('validates, persists and applies the personal content background', async () => {
	// Only a six-digit hex color is accepted; anything else follows the theme.
	expect(normalizeAppearance({ contentBackground: '#1C6E67' }).contentBackground).toBe('#1c6e67');
	expect(normalizeAppearance({ contentBackground: 'url(example)' }).contentBackground).toBeNull();
	expect(normalizeAppearance({ contentBackground: '#fff' }).contentBackground).toBeNull();
	expect(appearanceTheme('light').variables['--content-surface']).toBe('var(--background)');
	expect(appearanceTheme('dark', '#1677ff', 'not-a-color').variables['--content-surface']).toBe(
		'var(--background)'
	);
	const tinted = 'color-mix(in srgb, var(--background) 90%, #1c6e67)';
	expect(appearanceTheme('light', '#1677ff', '#1C6E67').variables['--content-surface']).toBe(tinted);

	setAppearance({ contentBackground: '#1c6e67' });
	expect(JSON.parse(localStorage.getItem(appearanceStorageKey)!)).toEqual({
		mode: 'system',
		accent: '#1677ff',
		contentBackground: '#1c6e67'
	});
	stop = initTheme();
	expect(themeSnapshot().contentBackground).toBe('#1c6e67');

	render(
		<MemoryRouter>
			<AppearanceProvider>
				<AppearancePage />
			</AppearanceProvider>
		</MemoryRouter>
	);
	await waitFor(() =>
		expect(document.documentElement.style.getPropertyValue('--content-surface')).toBe(tinted)
	);
	expect(screen.getByRole('heading', { name: 'Content background' })).toBeDefined();
	expect(screen.getByRole('button', { name: 'Choose a tint' }).textContent).toContain('Choose a tint');

	const follow = screen.getByRole('button', { name: 'Follow theme' }) as HTMLButtonElement;
	expect(follow.disabled).toBe(false);
	fireEvent.click(follow);
	await waitFor(() => expect(themeSnapshot().contentBackground).toBeNull());
	expect(document.documentElement.style.getPropertyValue('--content-surface')).toBe('var(--background)');
	// With no tint the control has nothing to follow besides the theme.
	expect((screen.getByRole('button', { name: 'Follow theme' }) as HTMLButtonElement).disabled).toBe(true);

	await act(async () => {
		setAppearance({ contentBackground: '#16856b' });
	});
	fireEvent.click(screen.getByRole('button', { name: 'Reset appearance' }));
	await waitFor(() => expect(themeSnapshot()).toEqual({ ...defaultAppearance, dark: false }));
});

it('tracks system changes, preserves accent on quick toggles, and synchronizes other tabs', () => {
	dark = true;
	onSystem?.();
	expect(themeSnapshot().dark).toBe(true);
	setAppearance({ accent: '#722ed1' });
	toggleTheme();
	expect(themeSnapshot()).toEqual({
		mode: 'light',
		dark: false,
		accent: '#722ed1',
		contentBackground: null
	});
	onSystem?.();
	expect(themeSnapshot().dark).toBe(false);
	localStorage.setItem(
		appearanceStorageKey,
		JSON.stringify({ mode: 'system', accent: '#d4380d', contentBackground: '#030e1f' })
	);
	window.dispatchEvent(new StorageEvent('storage', { key: appearanceStorageKey }));
	expect(themeSnapshot()).toEqual({
		mode: 'system',
		dark: true,
		accent: '#d4380d',
		contentBackground: '#030e1f'
	});
});

it('keeps preset and extreme custom colors readable on both palettes without changing status colors', () => {
	for (const mode of ['light', 'dark'] as const)
		for (const accent of [...accentPresets.map((p) => p.color), '#ffffff', '#000000', '#eeeeee', '#ff00ff']) {
			const { variables: vars, config } = appearanceTheme(mode, accent);
			for (const surface of ['--card', '--surface', '--muted', '--sidebar-bg', '--accent']) {
				expect(contrastRatio(vars['--primary'], vars[surface])).toBeGreaterThanOrEqual(4.5);
			}
			for (const ink of ['--sidebar-icon-detail', '--sidebar-icon-body']) {
				for (const surface of ['--sidebar-bg', '--muted', '--accent']) {
					expect(contrastRatio(vars[ink], vars[surface])).toBeGreaterThanOrEqual(3);
				}
			}
			expect(contrastRatio(vars['--sidebar-icon-detail'], vars['--sidebar-icon-body'])).toBeGreaterThanOrEqual(2);
			expect(contrastRatio(String(config.token?.colorPrimary), '#ffffff')).toBeGreaterThanOrEqual(
				4.5
			);
			expect(vars['--status-danger']).toBe(appearanceTheme(mode).variables['--status-danger']);
		}
});

it('applies the real mode and palette controls to both antd and CSS and resets them', async () => {
	render(
		<MemoryRouter>
			<AppearanceProvider>
				<AppearancePage />
			</AppearanceProvider>
		</MemoryRouter>
	);
	fireEvent.click(screen.getByRole('radio', { name: 'Dark' }));
	await waitFor(() => expect(document.documentElement.classList.contains('dark')).toBe(true));
	fireEvent.click(screen.getByRole('button', { name: 'Purple' }));
	await waitFor(() => expect(themeSnapshot().accent).toBe('#722ed1'));
	expect(document.documentElement.style.getPropertyValue('--accent')).toBe(
		appearanceTheme('dark', '#722ed1').variables['--accent']
	);
	await act(async () => {
		setAppearance({ accent: '#ffffff' });
	});
	expect(screen.getByRole('button', { name: 'Custom color' }).textContent).toContain('#FFFFFF');
	fireEvent.click(screen.getByRole('button', { name: 'Reset appearance' }));
	await waitFor(() => expect(themeSnapshot()).toEqual({ ...defaultAppearance, dark: false }));
	expect(document.documentElement.classList.contains('dark')).toBe(false);
});
