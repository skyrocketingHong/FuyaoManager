// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { App as AntApp, ConfigProvider } from 'antd';
import ThemePage from '../src/pages/sites/ThemePage';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import { setLocale } from '../src/lib/i18n';
import * as service from '../src/lib/api/management';
import { loadSiteList } from '../src/lib/sites/site-list';

vi.mock('../src/lib/api/management', async (original) => ({
	...(await original<typeof service>()),
	getSite: vi.fn(),
	listThemes: vi.fn(),
	getSiteTheme: vi.fn(),
	putSiteTheme: vi.fn()
}));
vi.mock('../src/lib/sites/site-list', () => ({
	loadSiteList: vi.fn().mockResolvedValue(undefined)
}));
const theme: service.Theme = {
	id: 'theme-row',
	themeId: 'theme',
	version: '1.0.0',
	name: 'Test theme',
	adapter: 'node22-sveltekit-static',
	inputProtocol: 'fuyao.theme.build-input/v1',
	manifestDigest: 'digest',
	capabilities: ['blog'],
	outputs: { spaFallback: null, requiredArtifacts: [], urlConventions: [] },
	configSchema: {
		type: 'object',
		properties: { heading: { type: 'string', title: 'Heading' } }
	} as unknown as service.Theme['configSchema'],
	registeredAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};
const site = { siteId: 'site', role: 'owner', kind: 'static' } as service.Site;
function selection(
	selected: boolean,
	config: Record<string, unknown> = {}
): service.SiteThemeSelection {
	return {
		siteId: 'site',
		selected,
		theme: selected ? { themeId: 'theme', version: '1.0.0', manifestDigest: 'digest' } : null,
		themeConfig: selected ? (config as Record<string, never>) : null,
		revision: selected ? 1 : 0,
		updatedAt: '2026-01-01T00:00:00Z'
	};
}
function Header() {
	const header = usePageHeaderContent();
	return (
		<div>
			{header?.content}
			{header?.actions}
		</div>
	);
}
function mount(view: 'selection' | 'configuration') {
	const router = createMemoryRouter(
		[
			{
				path: '/sites/:siteId/*',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope={view}>
								<Header />
								<ThemePage view={view} />
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <div>route-crash</div>
			}
		],
		{ initialEntries: [`/sites/site/${view === 'selection' ? 'theme' : 'theme-config'}`] }
	);
	render(<RouterProvider router={router} />);
}
beforeEach(() => {
	vi.stubEnv('NODE_ENV', 'development');
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	vi.mocked(service.getSite).mockResolvedValue(site);
	vi.mocked(service.listThemes).mockResolvedValue([theme]);
});
afterEach(() => {
	cleanup();
	vi.unstubAllEnvs();
});

it('selects a theme without presenting its parameter editor and refreshes workspace capabilities', async () => {
	vi.mocked(service.getSiteTheme).mockResolvedValue({
		selection: selection(false),
		etag: '"theme-0"'
	});
	vi.mocked(service.putSiteTheme).mockResolvedValue({
		selection: selection(true),
		etag: '"theme-1"'
	});
	mount('selection');
	fireEvent.click(await screen.findByRole('radio', { name: /Test theme/ }));
	expect(screen.queryByRole('textbox', { name: 'Heading' })).toBeNull();
	expect(screen.queryByRole('textbox', { name: 'Theme config JSON' })).toBeNull();
	fireEvent.click(screen.getByRole('button', { name: 'Save selection' }));
	await waitFor(() =>
		expect(service.putSiteTheme).toHaveBeenCalledExactlyOnceWith(
			'site',
			{ themeId: 'theme', version: '1.0.0', themeConfig: {} },
			'"theme-0"'
		)
	);
	await waitFor(() => expect(loadSiteList).toHaveBeenCalledWith(true));
	expect(screen.queryByText('route-crash')).toBeNull();
});

it('edits schema parameters without changing the selected theme or dropping undeclared JSON keys', async () => {
	vi.mocked(service.getSiteTheme).mockResolvedValue({
		selection: selection(true, { heading: 'Original', extra: 7 }),
		etag: '"theme-1"'
	});
	vi.mocked(service.putSiteTheme).mockResolvedValue({
		selection: selection(true, { heading: 'Updated', extra: 7 }),
		etag: '"theme-2"'
	});
	mount('configuration');
	fireEvent.change(await screen.findByRole('textbox', { name: 'Heading' }), {
		target: { value: 'Updated' }
	});
	expect(screen.queryByRole('radio', { name: /Test theme/ })).toBeNull();
	fireEvent.click(screen.getByRole('button', { name: 'Save configuration' }));
	await waitFor(() =>
		expect(service.putSiteTheme).toHaveBeenCalledExactlyOnceWith(
			'site',
			{ themeId: 'theme', version: '1.0.0', themeConfig: { heading: 'Updated', extra: 7 } },
			'"theme-1"'
		)
	);
	expect((screen.getByRole('textbox', { name: 'Heading' }) as HTMLInputElement).value).toBe(
		'Updated'
	);
});
