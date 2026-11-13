/**
 * Single navigation metadata source. The route table, the sidebar menu, the
 * selected state, and page positioning all consume this module — no
 * hand-maintained regexes or parallel lists elsewhere.
 */
import type { MessageKey } from '../i18n';

export type NavScope = 'platform' | 'site';

export interface NavItem {
	/** Stable route identifier, e.g. 'site.posts'. */
	id: string;
	/** Route pattern, e.g. '/sites/:siteId/posts'. */
	path: string;
	scope: NavScope;
	/** Menu group within the scope. */
	group: 'platform' | 'administration' | 'overview' | 'content' | 'settings' | 'delivery';
	titleKey: MessageKey;
	adminOnly?: boolean;
	menu?: boolean;
}

export const navItems: readonly NavItem[] = [
	{
		id: 'sites',
		path: '/sites',
		scope: 'platform',
		group: 'platform',
		titleKey: 'nav.sites',
		menu: false
	},
	{
		id: 'themes',
		path: '/themes',
		scope: 'platform',
		group: 'platform',
		titleKey: 'nav.themesCatalog'
	},
	{
		id: 'appearance',
		path: '/appearance',
		scope: 'platform',
		group: 'administration',
		titleKey: 'nav.appearance'
	},
	{
		id: 'users',
		path: '/users',
		scope: 'platform',
		group: 'administration',
		titleKey: 'nav.users',
		adminOnly: true
	},
	{
		id: 'logs',
		path: '/logs',
		scope: 'platform',
		group: 'administration',
		titleKey: 'nav.logs',
		adminOnly: true
	},
	// The account settings page lives outside the sidebar menus: it is
	// reached from the account dropdown in the top bar only.
	{
		id: 'account',
		path: '/account',
		scope: 'platform',
		group: 'platform',
		titleKey: 'nav.account',
		menu: false
	},
	{
		id: 'site.overview',
		path: '/sites/:siteId',
		scope: 'site',
		group: 'overview',
		titleKey: 'nav.overview'
	},
	{
		id: 'site.posts',
		path: '/sites/:siteId/posts',
		scope: 'site',
		group: 'content',
		titleKey: 'nav.posts'
	},
	{
		id: 'site.friends',
		path: '/sites/:siteId/friends',
		scope: 'site',
		group: 'content',
		titleKey: 'nav.friends'
	},
	{
		id: 'site.albums',
		path: '/sites/:siteId/albums',
		scope: 'site',
		group: 'content',
		titleKey: 'nav.albums'
	},
	{
		id: 'site.footprints',
		path: '/sites/:siteId/footprints',
		scope: 'site',
		group: 'content',
		titleKey: 'nav.footprints'
	},
	{
		id: 'site.settings',
		path: '/sites/:siteId/settings',
		scope: 'site',
		group: 'settings',
		titleKey: 'nav.settings'
	},
	{
		id: 'site.config',
		path: '/sites/:siteId/config',
		scope: 'site',
		group: 'settings',
		titleKey: 'nav.config'
	},
	{
		id: 'site.theme',
		path: '/sites/:siteId/theme',
		scope: 'site',
		group: 'settings',
		titleKey: 'nav.theme'
	},
	{
		id: 'site.builds',
		path: '/sites/:siteId/builds',
		scope: 'site',
		group: 'delivery',
		titleKey: 'nav.builds'
	},
	{
		id: 'site.releases',
		path: '/sites/:siteId/releases',
		scope: 'site',
		group: 'delivery',
		titleKey: 'nav.releases'
	}
];

export interface NavGroupMeta {
	id: NavItem['group'];
	titleKey: MessageKey;
}

/** Sidebar group order and labels (the overview entry stands alone above
 * the groups); groups render expanded, never flat. */
export const siteNavGroups: readonly NavGroupMeta[] = [
	{ id: 'content', titleKey: 'nav.group.content' },
	{ id: 'settings', titleKey: 'nav.group.settings' },
	{ id: 'delivery', titleKey: 'nav.group.delivery' }
];

export const platformNavGroups: readonly NavGroupMeta[] = [
	{ id: 'platform', titleKey: 'nav.group.resources' },
	{ id: 'administration', titleKey: 'nav.group.platformSettings' }
];

export interface RouteMatch {
	item: NavItem;
	siteId: string | null;
}

function patternToRegex(pattern: string): RegExp {
	const source = pattern
		.split('/')
		.map((segment) => (segment === ':siteId' ? '(?<siteId>[^/]+)' : segment))
		.join('\\/');
	return new RegExp(`^${source}\\/?$`);
}

const matchers = navItems.map((item) => ({ item, regex: patternToRegex(item.path) }));

/** Resolves a pathname to its navigation entry and site context. */
export function matchRoute(pathname: string): RouteMatch | null {
	for (const { item, regex } of matchers) {
		const match = regex.exec(pathname);
		if (match) {
			return { item, siteId: match.groups?.siteId ?? null };
		}
	}
	return null;
}

/** Fills an item's path pattern with a concrete site id. */
export function itemPath(item: NavItem, siteId: string | null): string {
	return siteId ? item.path.replace(':siteId', siteId) : item.path;
}

/** The menu selected key for a pathname (the item's concrete path). */
export function selectedPath(pathname: string): string | null {
	const match = matchRoute(pathname);
	return match ? itemPath(match.item, match.siteId) : null;
}
