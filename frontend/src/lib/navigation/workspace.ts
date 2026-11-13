import type { Site } from '../api/management';
import type { components } from '../api/schema';

export type SiteWorkspace = components['schemas']['SiteWorkspace'];
export function hasSiteWorkspace(site: Site): site is Site & { workspace: SiteWorkspace } {
	return !!site.workspace;
}
export function siteWorkspace(site: Site): SiteWorkspace {
	const declared = site.workspace;
	if (declared) return declared;
	const theme = !site.kind || site.kind === 'theme';
	return {
		themeEnabled: theme,
		themeSelectable: theme,
		buildable: theme,
		deployable: theme,
		hasCaddy: true
	};
}
export function allowSitePage(site: Site | null | undefined, page: string): boolean {
	if (!site) return false;
	const workspace = siteWorkspace(site);
	if (page === 'site.overview') return true;
	if (page === 'site.caddy') return workspace.hasCaddy;
	if (page === 'site.theme') return workspace.themeSelectable || workspace.themeEnabled;
	if (page === 'site.theme-config' || page === 'site.config' || page === 'site.settings')
		return workspace.themeEnabled;
	if (page === 'site.builds') return workspace.buildable;
	if (page === 'site.releases') return workspace.themeEnabled;
	if (
		page === 'site.sources' ||
		['site.posts', 'site.albums', 'site.footprints', 'site.friends'].includes(page)
	)
		return workspace.themeSelectable || workspace.themeEnabled;
	return false;
}
