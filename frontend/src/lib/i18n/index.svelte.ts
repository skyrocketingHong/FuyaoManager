/**
 * Minimal typed i18n: two locales, flat dictionaries, rune-backed state.
 * Locale switches must be wrapped in <Crossfade key={locale}> per the
 * project rules; this module only provides the state and lookup.
 */
import en from './locales/en-US.json';
import zh from './locales/zh-CN.json';

const dictionaries = { 'en-US': en, 'zh-CN': zh } as const;

export type Locale = keyof typeof dictionaries;
export type MessageKey = keyof typeof en;

const STORAGE_KEY = 'fuyao-admin-locale';

export const i18n = $state({ locale: 'zh-CN' as Locale });

export function initLocale(): void {
	const saved = localStorage.getItem(STORAGE_KEY);
	if (saved === 'en-US' || saved === 'zh-CN') {
		i18n.locale = saved;
	} else {
		i18n.locale = navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US';
	}
	document.documentElement.lang = i18n.locale;
}

export function setLocale(locale: Locale): void {
	i18n.locale = locale;
	localStorage.setItem(STORAGE_KEY, locale);
	document.documentElement.lang = locale;
}

export function t(key: MessageKey): string {
	return dictionaries[i18n.locale][key] ?? en[key] ?? key;
}
