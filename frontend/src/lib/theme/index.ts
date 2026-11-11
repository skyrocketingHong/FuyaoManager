/**
 * Day/night theme state persisted per browser, defaulting to the system
 * preference. Exposed as an external store; the antd ConfigProvider binds
 * the algorithm through it.
 */

const STORAGE_KEY = 'fuyao-admin-theme';

interface ThemeState {
	dark: boolean;
}

let state: ThemeState = { dark: false };
const listeners = new Set<() => void>();

function emit(): void {
	for (const listener of listeners) listener();
}

export function subscribeTheme(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function themeSnapshot(): ThemeState {
	return state;
}

export function initTheme(): void {
	let dark = false;
	if (typeof localStorage !== 'undefined') {
		const saved = localStorage.getItem(STORAGE_KEY);
		dark =
			saved === 'dark' || saved === 'light'
				? saved === 'dark'
				: window.matchMedia('(prefers-color-scheme: dark)').matches;
	}
	state = { dark };
	emit();
}

export function toggleTheme(): void {
	state = { dark: !state.dark };
	if (typeof localStorage !== 'undefined') {
		localStorage.setItem(STORAGE_KEY, state.dark ? 'dark' : 'light');
	}
	emit();
}
