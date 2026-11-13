// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { MemoryRouter } from 'react-router-dom';
import SitesPage from '../src/pages/SitesPage';
import { setLocale } from '../src/lib/i18n';
import type { Site } from '../src/lib/api/management';

const runtime = vi.hoisted(() => ({
	state: { status: 'authenticated', user: { id: 'admin', platformRole: 'admin' } },
	sites: { status: 'ready', sites: [] as Site[] }
}));
vi.mock('../src/lib/session', () => ({
	sessionSnapshot: () => runtime.state,
	subscribeSession: () => () => undefined,
	loadSession: vi.fn()
}));
vi.mock('../src/lib/sites/site-list', () => ({
	siteListSnapshot: () => runtime.sites,
	subscribeSiteList: () => () => undefined,
	loadSiteList: vi.fn().mockResolvedValue(undefined),
	upsertSite: vi.fn()
}));
function site(siteId: string, kind: Site['kind'], managementMode: Site['managementMode']): Site {
	return {
		siteId,
		kind,
		managementMode,
		slug: siteId,
		name: `${siteId} website`,
		role: 'owner',
		visibility: 'private',
		provisionState: 'ready',
		settingsRevision: 1,
		description: '',
		defaultLocale: 'en-US',
		createdAt: '2026-01-01T00:00:00Z',
		updatedAt: '2026-01-01T00:00:00Z'
	};
}
beforeEach(() => {
	localStorage.clear();
	setLocale('en-US');
	vi.clearAllMocks();
});
afterEach(cleanup);

it('groups by configuration ownership independently of site kind and links to the proper workspace', async () => {
	runtime.sites = {
		status: 'ready',
		sites: [
			site('theme', 'theme', 'platform'),
			site('adopted-static', 'static', 'platform'),
			site('redirect', 'redirect', 'caddy'),
			site('missing-mode', 'static', undefined)
		]
	};
	render(
		<MemoryRouter>
			<ConfigProvider theme={{ token: { motion: false } }}>
				<AntApp>
					<SitesPage />
				</AntApp>
			</ConfigProvider>
		</MemoryRouter>
	);
	const platform = screen.getByRole('region', { name: 'Platform deployment management' });
	const caddy = screen.getByRole('region', { name: 'Caddy configuration management' });
	const unknown = screen.getByRole('region', { name: 'Unclassified sites' });
	expect(within(platform).getByRole('link', { name: 'theme website' })).toBeDefined();
	expect(within(platform).getByRole('link', { name: 'adopted-static website' })).toBeDefined();
	expect(within(platform).queryByText('redirect website')).toBeNull();
	expect(within(caddy).getByRole('link', { name: 'redirect website' })).toBeDefined();
	expect(within(unknown).getByRole('link', { name: 'missing-mode website' })).toBeDefined();
	const themeRow = within(platform).getByText('theme website').closest('tr')!;
	const staticRow = within(platform).getByText('adopted-static website').closest('tr')!;
	expect(within(themeRow).getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe(
		'/sites/theme'
	);
	expect(within(staticRow).getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe(
		'/sites/adopted-static/caddy'
	);
	expect(within(caddy).getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe(
		'/sites/redirect/caddy'
	);
	expect(screen.queryByRole('button', { name: 'Convert to platform management' })).toBeNull();
});
