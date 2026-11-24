// Preference validation and mode resolution adapted from
// robot-platform/libraries/platform-ui/lib/appearance.ts.
export type ThemeMode = 'light' | 'dark' | 'system';
export interface AppearancePreferences {
	mode: ThemeMode;
	accent: string;
	/** Personal tint mixed into the content surface; null follows the theme. */
	contentBackground: string | null;
}
export interface ThemeState extends AppearancePreferences {
	dark: boolean;
}
export const appearanceStorageKey = 'fuyao-manager-appearance:v1';
export const defaultAppearance: AppearancePreferences = {
	mode: 'system',
	accent: '#1677ff',
	contentBackground: null
};
export const accentPresets = [
	{ name: 'blue', color: '#1677ff' },
	{ name: 'indigo', color: '#2f54eb' },
	{ name: 'purple', color: '#722ed1' },
	{ name: 'magenta', color: '#a02eb3' },
	{ name: 'rose', color: '#c43d78' },
	{ name: 'red', color: '#d4380d' },
	{ name: 'orange', color: '#d46b08' },
	{ name: 'gold', color: '#ad8b00' },
	{ name: 'green', color: '#389e0d' },
	{ name: 'emerald', color: '#16856b' },
	{ name: 'cyan', color: '#08979c' },
	{ name: 'graphite', color: '#64748b' }
] as const;

export function normalizeAppearance(value: unknown): AppearancePreferences {
	const input = value && typeof value === 'object' ? (value as Partial<AppearancePreferences>) : {};
	return {
		mode:
			input.mode === 'light' || input.mode === 'dark' || input.mode === 'system'
				? input.mode
				: defaultAppearance.mode,
		accent:
			typeof input.accent === 'string' && /^#[\da-f]{6}$/i.test(input.accent)
				? input.accent.toLowerCase()
				: defaultAppearance.accent,
		contentBackground:
			typeof input.contentBackground === 'string' && /^#[\da-f]{6}$/i.test(input.contentBackground)
				? input.contentBackground.toLowerCase()
				: null
	};
}
function readPreferences(): AppearancePreferences {
	try {
		const saved = localStorage.getItem(appearanceStorageKey);
		if (saved !== null) return normalizeAppearance(JSON.parse(saved));
		const legacy = localStorage.getItem('fuyao-admin-theme');
		if (legacy === 'dark' || legacy === 'light') return { ...defaultAppearance, mode: legacy };
	} catch {
		/* Storage can be disabled by browser policy. */
	}
	return { ...defaultAppearance };
}
function systemDark(): boolean {
	return (
		typeof window !== 'undefined' &&
		typeof window.matchMedia === 'function' &&
		window.matchMedia('(prefers-color-scheme: dark)').matches
	);
}
function resolved(preferences: AppearancePreferences): ThemeState {
	return {
		...preferences,
		dark: preferences.mode === 'system' ? systemDark() : preferences.mode === 'dark'
	};
}
let state = resolved(readPreferences());
const listeners = new Set<() => void>();
let dispose: (() => void) | undefined;
function emit() {
	for (const listener of listeners) listener();
}
export function subscribeTheme(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}
export function themeSnapshot(): ThemeState {
	return state;
}
export function initTheme(): () => void {
	dispose?.();
	state = resolved(readPreferences());
	const media = window.matchMedia('(prefers-color-scheme: dark)');
	const onSystem = () => {
		if (state.mode === 'system') {
			state = resolved(state);
			emit();
		}
	};
	const onStorage = (event: StorageEvent) => {
		if (event.key === appearanceStorageKey || event.key === null) {
			state = resolved(readPreferences());
			emit();
		}
	};
	media.addEventListener('change', onSystem);
	window.addEventListener('storage', onStorage);
	const stop = () => {
		media.removeEventListener('change', onSystem);
		window.removeEventListener('storage', onStorage);
	};
	dispose = stop;
	emit();
	return stop;
}
export function setAppearance(patch: Partial<AppearancePreferences>): void {
	state = resolved(normalizeAppearance({ ...state, ...patch }));
	try {
		localStorage.setItem(
			appearanceStorageKey,
			JSON.stringify({
				mode: state.mode,
				accent: state.accent,
				contentBackground: state.contentBackground
			})
		);
	} catch {
		/* Keep the in-memory preference. */
	}
	emit();
}
export function resetAppearance(): void {
	setAppearance(defaultAppearance);
}
export function toggleTheme(): void {
	setAppearance({ mode: state.dark ? 'light' : 'dark' });
}
