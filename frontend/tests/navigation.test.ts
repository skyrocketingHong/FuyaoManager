/**
 * Navigation metadata tests: every business route resolves to exactly one
 * item with the right scope, group, and site context; the menu selection is
 * derived from the same source, so drift shows up here.
 */
import { describe, expect, it } from 'vitest';
import { itemPath, matchRoute, navItems, selectedPath } from '../src/lib/navigation';
import { allowSitePage, type SiteWorkspace } from '../src/lib/navigation/workspace';
import type { Site } from '../src/lib/api/management';

describe('navigation metadata', () => {
	it('covers every business path exactly once', () => {
		const paths = navItems.map((item) => item.path);
		expect(new Set(paths).size).toBe(paths.length);
		expect(paths).toContain('/sites');
		expect(paths).toContain('/themes');
		for (const sub of [
			'',
			'/caddy',
			'/settings',
			'/config',
			'/posts',
			'/friends',
			'/albums',
			'/footprints',
			'/theme',
			'/builds',
			'/releases'
		]) {
			expect(paths).toContain(`/sites/:siteId${sub}`);
		}
	});

	it('resolves platform routes', () => {
		expect(matchRoute('/sites')?.item.id).toBe('sites');
		expect(matchRoute('/themes')?.item.id).toBe('themes');
		expect(matchRoute('/themes')?.siteId).toBeNull();
		for (const resource of ['posts', 'albums', 'footprints', 'friends'])
			expect(matchRoute(`/content/${resource}`)?.item.scope).toBe('platform');
	});
	it('uses declared workspace capabilities independently of a static site kind', () => {
		const website: Site & { workspace: SiteWorkspace } = {
			siteId: 'website',
			slug: 'website',
			name: 'Website',
			visibility: 'private',
			provisionState: 'ready',
			role: 'owner',
			settingsRevision: 1,
			description: '',
			defaultLocale: 'en-US',
			createdAt: '2026-01-01T00:00:00Z',
			updatedAt: '2026-01-01T00:00:00Z',
			kind: 'static',
			workspace: {
				themeEnabled: false,
				themeSelectable: true,
				buildable: false,
				deployable: false,
				hasCaddy: true
			}
		};
		expect(allowSitePage(website, 'site.theme')).toBe(true);
		expect(allowSitePage(website, 'site.sources')).toBe(true);
		expect(allowSitePage(website, 'site.posts')).toBe(true);
		expect(allowSitePage(website, 'site.theme-config')).toBe(false);
		expect(allowSitePage(website, 'site.builds')).toBe(false);
		expect(
			allowSitePage(
				{
					...website,
					workspace: { ...website.workspace, themeEnabled: true, buildable: true }
				} as Site,
				'site.builds'
			)
		).toBe(true);
		expect(allowSitePage(null, 'site.theme')).toBe(false);
	});

	it('resolves every site-scoped route with its site id', () => {
		const cases: [string, string][] = [
			['/sites/abc', 'site.overview'],
			['/sites/abc/caddy', 'site.caddy'],
			['/sites/abc/settings', 'site.settings'],
			['/sites/abc/config', 'site.config'],
			['/sites/abc/posts', 'site.posts'],
			['/sites/abc/friends', 'site.friends'],
			['/sites/abc/albums', 'site.albums'],
			['/sites/abc/footprints', 'site.footprints'],
			['/sites/abc/theme', 'site.theme'],
			['/sites/abc/builds', 'site.builds'],
			['/sites/abc/releases', 'site.releases']
		];
		for (const [path, id] of cases) {
			const match = matchRoute(path);
			expect(match?.item.id, path).toBe(id);
			expect(match?.siteId, path).toBe('abc');
			expect(match?.item.scope, path).toBe('site');
		}
	});

	it('assigns groups per the information architecture', () => {
		const groupOf = (id: string) => navItems.find((item) => item.id === id)?.group;
		expect(groupOf('sites')).toBe('platform');
		expect(groupOf('themes')).toBe('platform');
		expect(groupOf('site.overview')).toBe('overview');
		for (const id of ['site.posts', 'site.friends', 'site.albums', 'site.footprints']) {
			expect(groupOf(id), id).toBe('content');
		}
		for (const id of ['site.caddy', 'site.settings', 'site.config', 'site.theme']) {
			expect(groupOf(id), id).toBe('settings');
		}
		for (const id of ['site.builds', 'site.releases']) {
			expect(groupOf(id), id).toBe('delivery');
		}
	});

	it('derives the selected menu path from the same source', () => {
		expect(selectedPath('/sites/abc/albums')).toBe('/sites/abc/albums');
		expect(selectedPath('/sites/abc')).toBe('/sites/abc');
		expect(selectedPath('/themes')).toBe('/themes');
		expect(selectedPath('/nowhere')).toBeNull();
		expect(selectedPath('/content/posts/new')).toBe('/content/posts');
		expect(selectedPath('/content/posts/tech%2Fhello.md/edit')).toBe('/content/posts');
		expect(selectedPath('/sites/abc/posts/tech%2Fhello.md/edit')).toBe('/sites/abc/posts');
		expect(selectedPath('/sites/abc/posts/vocabulary')).toBe('/sites/abc/posts');
	});

	it('fills concrete paths', () => {
		const albums = navItems.find((item) => item.id === 'site.albums');
		expect(albums && itemPath(albums, 'xyz')).toBe('/sites/xyz/albums');
	});

	it('does not let a site id swallow another segment', () => {
		expect(matchRoute('/sites/abc/extra/segments')).toBeNull();
		expect(matchRoute('/sites/abc/albums/edit')).toBeNull();
	});
});
