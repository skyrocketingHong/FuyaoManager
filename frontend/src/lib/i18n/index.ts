/**
 * Minimal typed i18n: two locales, flat dictionaries, and a framework-agnostic
 * external store. React binds through useSyncExternalStore; switching updates
 * every subscribed component and persists the choice per browser.
 */
import en from './locales/en-US.json';
import zh from './locales/zh-CN.json';

const dictionaries = { 'en-US': en, 'zh-CN': zh } as const;

export type Locale = keyof typeof dictionaries;
export type MessageKey = keyof typeof en;

const STORAGE_KEY = 'fuyao-admin-locale';

interface I18nState {
	locale: Locale;
}

let state: I18nState = { locale: 'zh-CN' };
const listeners = new Set<() => void>();

function emit(): void {
	for (const listener of listeners) listener();
}

export function subscribeI18n(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function i18nSnapshot(): I18nState {
	return state;
}

export function initLocale(): void {
	let next: Locale = 'zh-CN';
	const isBrowser = typeof localStorage !== 'undefined';
	if (isBrowser) {
		const saved = localStorage.getItem(STORAGE_KEY);
		if (saved === 'en-US' || saved === 'zh-CN') next = saved;
		else next = navigator.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en-US';
	}
	state = { locale: next };
	if (isBrowser) {
		localStorage.setItem(STORAGE_KEY, next);
		document.documentElement.lang = next;
	}
	emit();
}

export function setLocale(locale: Locale): void {
	state = { locale };
	if (typeof localStorage !== 'undefined') {
		localStorage.setItem(STORAGE_KEY, locale);
		document.documentElement.lang = locale;
	}
	emit();
}

export function t(key: MessageKey, vars: Record<string, string | number> = {}): string {
	const template = dictionaries[state.locale][key] ?? en[key] ?? key;
	return template.replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? `{${name}}`));
}
