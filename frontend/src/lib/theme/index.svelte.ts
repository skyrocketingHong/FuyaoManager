/** Day/night theme state persisted per browser, defaulting to the system preference. */

const STORAGE_KEY = 'fuyao-admin-theme';

export const theme = $state({ dark: false });

function apply(): void {
	document.documentElement.dataset.theme = theme.dark ? 'dark' : 'light';
}

export function initTheme(): void {
	const saved = localStorage.getItem(STORAGE_KEY);
	theme.dark =
		saved === 'dark' || saved === 'light'
			? saved === 'dark'
			: window.matchMedia('(prefers-color-scheme: dark)').matches;
	apply();
}

export function toggleTheme(): void {
	theme.dark = !theme.dark;
	localStorage.setItem(STORAGE_KEY, theme.dark ? 'dark' : 'light');
	apply();
}
