/**
 * Document title: "Fuyao Manager - <page>". Page names reuse the navigation
 * metadata so the tab title, the sidebar menu, and the route table cannot
 * drift; the auth screens and the session error screen map to their own
 * message keys. The hook re-runs on route, session-status, and locale
 * changes, so language switches retitle the tab without a reload.
 */
import { useEffect } from 'react';
import { useSyncExternalStore } from 'react';
import { useLocation } from 'react-router-dom';
import { i18nSnapshot, subscribeI18n, t, type MessageKey } from '../i18n';
import { matchRoute } from '../navigation';
import type { SessionStatus } from '../session';

/** The brand stays locale-independent; only the page segment translates. */
const BRAND = 'Fuyao Manager';

const authPageKeys: Record<string, MessageKey> = {
	'/login': 'login.title',
	'/register-passkey': 'bind.title'
};

/** Resolves a pathname to its page-name message key, or null when the
 * location has no page name (loading, redirects). */
export function pageTitleKey(pathname: string): MessageKey | null {
	return authPageKeys[pathname] ?? matchRoute(pathname)?.item.titleKey ?? null;
}

/** Composes the full document title from an already-translated page name. */
export function documentTitle(page: string | null): string {
	return page ? `${BRAND} - ${page}` : BRAND;
}

/** Keeps document.title in sync with the route and the locale. The error
 * status covers the session-failure screen rendered before any route. */
export function useDocumentTitle(status: SessionStatus): void {
	const { pathname } = useLocation();
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	useEffect(() => {
		const key = status === 'error' ? 'session.errorTitle' : pageTitleKey(pathname);
		document.title = documentTitle(key ? t(key) : null);
	}, [status, pathname, locale]);
}
