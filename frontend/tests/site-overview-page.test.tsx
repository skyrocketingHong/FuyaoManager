// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { App as AntApp, ConfigProvider } from 'antd';
import SiteBoundary from '../src/components/layout/SiteBoundary';
import SiteOverviewPage from '../src/pages/sites/SiteOverviewPage';
import { PageHeaderProvider } from '../src/components/PageHeader';
import { ApiError } from '../src/lib/api/client';
import * as service from '../src/lib/api/management';
import * as overviewService from '../src/lib/api/content';
import { setLocale } from '../src/lib/i18n';
import en from '../src/lib/i18n/locales/en-US.json';

const runtime = vi.hoisted(() => ({
	state: { status: 'authenticated', user: { id: 'admin', platformRole: 'admin' } }
}));
vi.mock('../src/lib/session', () => ({
	sessionSnapshot: () => runtime.state,
	subscribeSession: () => () => undefined,
	loadSession: vi.fn()
}));
vi.mock('../src/lib/api/management', async (original) => ({
	...(await original<typeof service>()),
	getSite: vi.fn(),
	getCurrentRelease: vi.fn(),
	getSiteEdge: vi.fn(),
	putSiteVisibility: vi.fn(),
	reapplySiteEdge: vi.fn()
}));
vi.mock('../src/lib/api/content', async (original) => ({
	...(await original<typeof overviewService>()),
	getSiteOverview: vi.fn()
}));

const SITE = '11111111-1111-1111-1111-111111111111';
const OTHER = '22222222-2222-2222-2222-222222222222';
function site(siteId = SITE, hasCaddy = true, kind: service.Site['kind'] = 'static'): service.Site {
	return {
		siteId,
		kind,
		managementMode: 'platform',
		slug: 'website',
		name: 'Website',
		role: 'owner',
		visibility: 'private',
		provisionState: 'ready',
		description: '',
		defaultLocale: 'en-US',
		settingsRevision: 1,
		createdAt: '2026-01-01T00:00:00Z',
		updatedAt: '2026-01-01T00:00:00Z',
		workspace: {
			hasCaddy,
			themeEnabled: true,
			themeSelectable: true,
			buildable: true,
			deployable: true
		}
	};
}
function overview(website: service.Site): overviewService.SiteOverview {
	return {
		counts: { posts: 1, albums: 2, photos: 3, places: 4, friends: 5 },
		sources: {
			posts: 'independent',
			albums: 'independent',
			footprints: 'independent',
			friends: 'independent'
		},
		theme: {
			siteId: website.siteId,
			selected: website.workspace!.themeEnabled,
			theme: { themeId: 'theme', version: '1.0.0', manifestDigest: 'digest' },
			themeConfig: {},
			revision: 1,
			updatedAt: website.updatedAt
		},
		hasCaddy: website.workspace!.hasCaddy,
		workspace: website.workspace!,
		analytics: {
			configured: false,
			available: false,
			provider: 'cloudflare',
			cache: 'unknown',
			latest: null
		}
	};
}
function release(siteId = SITE): Awaited<ReturnType<typeof service.getCurrentRelease>> {
	return {
		originUrl: 'https://website.example',
		release: {
			id: 'release',
			siteId,
			buildId: 'build',
			theme: { themeId: 'theme', version: '1.0.0', manifestDigest: 'digest' },
			status: 'live',
			inputDigests: null,
			routes: { spaFallback: null, urlConventions: [] },
			reasons: ['manual'],
			failure: null,
			createdBy: 'admin',
			createdAt: '2026-01-01T00:00:00Z',
			activatedAt: '2026-01-01T00:00:00Z',
			supersededAt: null
		}
	};
}
function mount() {
	const router = createMemoryRouter(
		[
			{
				path: '/sites/:siteId',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope="overview">
								<SiteBoundary>
									<SiteOverviewPage />
								</SiteBoundary>
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <div>route-crash</div>
			}
		],
		{ initialEntries: [`/sites/${SITE}`] }
	);
	render(<RouterProvider router={router} />);
	return router;
}
beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	vi.mocked(service.getSite).mockImplementation(async (id) => site(id));
	vi.mocked(overviewService.getSiteOverview).mockImplementation(async (id) => overview(site(id)));
	vi.mocked(service.getCurrentRelease).mockResolvedValue(release());
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

it.each(['static', 'theme'] as const)(
	'uses Caddy publication facts for a %s source without legacy visibility or edge controls',
	async (kind) => {
		vi.mocked(service.getSite).mockResolvedValue(site(SITE, true, kind));
		mount();
		expect(await screen.findByText('Published by the platform')).toBeDefined();
		expect(screen.getByRole('link', { name: 'https://website.example' }).getAttribute('href')).toBe(
			'https://website.example'
		);
		expect(screen.getByRole('link', { name: 'Caddy configuration' }).getAttribute('href')).toBe(
			`/sites/${SITE}/caddy`
		);
		expect(screen.queryByRole('switch')).toBeNull();
		expect(screen.queryByText(en['edge.isolatedNotice'])).toBeNull();
		expect(screen.queryByText(en['edge.reapply'])).toBeNull();
		expect(service.getSiteEdge).not.toHaveBeenCalled();
		expect(service.putSiteVisibility).not.toHaveBeenCalled();
		expect(service.reapplySiteEdge).not.toHaveBeenCalled();
	}
);

it('shows no platform release only for the explicit no_live_release response', async () => {
	const website = site();
	website.workspace!.themeEnabled = false;
	website.workspace!.buildable = false;
	vi.mocked(service.getSite).mockResolvedValue(website);
	vi.mocked(overviewService.getSiteOverview).mockResolvedValue(overview(website));
	vi.mocked(service.getCurrentRelease).mockRejectedValue(
		new ApiError(404, 'no_live_release', '', null)
	);
	mount();
	expect(await screen.findByText(en['overview.caddyNotPublished'])).toBeDefined();
	expect(service.getCurrentRelease).toHaveBeenCalledWith(SITE);
	expect(screen.queryByText(en['overview.platformPublished'])).toBeNull();
	expect(screen.queryByRole('switch')).toBeNull();
	expect(service.getSiteEdge).not.toHaveBeenCalled();
});

it('keeps request failure distinct from an unpublished site and recovers through retry', async () => {
	vi.mocked(service.getCurrentRelease)
		.mockRejectedValueOnce(new TypeError('offline'))
		.mockResolvedValueOnce(release());
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	expect(screen.queryByText(en['overview.caddyNotPublished'])).toBeNull();
	expect(await screen.findByText(en['overview.platformPublished'])).toBeDefined();
	expect(screen.queryByText('route-crash')).toBeNull();
});

it('retains the legacy edge and visibility presentation for a site without Caddy source management', async () => {
	const website = site(SITE, false, 'theme');
	vi.mocked(service.getSite).mockResolvedValue(website);
	vi.mocked(overviewService.getSiteOverview).mockResolvedValue(overview(website));
	vi.mocked(service.getCurrentRelease).mockRejectedValue(
		new ApiError(404, 'no_live_release', '', null)
	);
	vi.mocked(service.getSiteEdge).mockResolvedValue({
		site: { siteId: SITE, port: 41000, originUrl: 'http://127.0.0.1:41000', currentOk: false },
		enabled: true,
		isolated: true,
		inSync: false
	});
	mount();
	expect(await screen.findByText(en['edge.isolatedNotice'])).toBeDefined();
	expect((screen.getByRole('switch') as HTMLButtonElement).disabled).toBe(true);
	expect(service.getSiteEdge).toHaveBeenCalledWith(SITE);
	expect(screen.queryByText(en['overview.caddyNotPublished'])).toBeNull();
});

it('does not carry a late publication response into another Caddy site', async () => {
	const NativeRequest = globalThis.Request;
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init ? { ...init, signal: undefined } : init);
			}
		}
	);
	let resolve!: (value: Awaited<ReturnType<typeof service.getCurrentRelease>>) => void;
	const pending = new Promise<Awaited<ReturnType<typeof service.getCurrentRelease>>>((done) => {
		resolve = done;
	});
	vi.mocked(service.getCurrentRelease).mockImplementation(async (id) =>
		id === SITE ? pending : Promise.reject(new ApiError(404, 'no_live_release', '', null))
	);
	const router = mount();
	await waitFor(() => expect(service.getCurrentRelease).toHaveBeenCalledWith(SITE));
	await act(async () => {
		await router.navigate(`/sites/${OTHER}`);
	});
	await screen.findByText(en['overview.caddyNotPublished']);
	await act(async () => {
		resolve(release());
	});
	expect(screen.queryByRole('link', { name: 'https://website.example' })).toBeNull();
	expect(screen.getByText(en['overview.caddyNotPublished'])).toBeDefined();
	expect(service.getSiteEdge).not.toHaveBeenCalled();
});

type TrafficFixture = overviewService.SiteOverview['analytics'];
function traffic(overrides: Partial<TrafficFixture> = {}): TrafficFixture {
	return {
		configured: true,
		available: true,
		provider: 'cloudflare',
		cache: 'fresh',
		latest: { date: '2026-11-12', pageViews: 123, requests: 456, uniques: 78 },
		...overrides
	};
}
function withTraffic(analytics: TrafficFixture): overviewService.SiteOverview {
	const result = overview(site());
	result.analytics = analytics;
	return result;
}
async function trafficPanel() {
	return (await screen.findByText('Cloudflare traffic')).closest('.panel') as HTMLElement;
}
it('shows the latest single-day Cloudflare values with the server date', async () => {
	vi.mocked(overviewService.getSiteOverview).mockResolvedValue(withTraffic(traffic()));
	mount();
	const panel = within(await trafficPanel());
	expect(panel.getByText('2026-11-12')).toBeDefined();
	expect(panel.getByText('123')).toBeDefined();
	expect(panel.getByText('456')).toBeDefined();
	expect(panel.getByText('78')).toBeDefined();
	expect(panel.queryByText('Cached snapshot')).toBeNull();
});
it('identifies stale daily traffic as a cached snapshot', async () => {
	vi.mocked(overviewService.getSiteOverview).mockResolvedValue(
		withTraffic(traffic({ cache: 'stale' }))
	);
	mount();
	const panel = within(await trafficPanel());
	expect(panel.getByText('Cached snapshot')).toBeDefined();
	expect(panel.getByText('2026-11-12')).toBeDefined();
	expect(panel.getByText('78')).toBeDefined();
});
it('shows an unavailable traffic state without zero placeholders and retries the overview', async () => {
	vi.mocked(overviewService.getSiteOverview)
		.mockResolvedValueOnce(
			withTraffic(traffic({ available: false, latest: null, cache: 'unknown' }))
		)
		.mockResolvedValueOnce(withTraffic(traffic()));
	mount();
	const panel = within(await trafficPanel());
	expect(panel.getByText('Traffic statistics are temporarily unavailable.')).toBeDefined();
	expect(panel.queryByText('0')).toBeNull();
	expect(panel.queryByText('Unique visitors')).toBeNull();
	fireEvent.click(panel.getByRole('button', { name: 'Retry' }));
	await waitFor(() =>
		expect(
			within(screen.getByText('Cloudflare traffic').closest('.panel') as HTMLElement).getByText(
				'78'
			)
		).toBeDefined()
	);
	expect(overviewService.getSiteOverview).toHaveBeenCalledTimes(2);
});
it('omits traffic entirely for an unconfigured site even if a stale payload includes numbers', async () => {
	vi.mocked(overviewService.getSiteOverview).mockResolvedValue(
		withTraffic(traffic({ configured: false }))
	);
	mount();
	await screen.findByText('5');
	expect(screen.queryByText('Cloudflare traffic')).toBeNull();
	expect(screen.queryByText('123')).toBeNull();
	expect(screen.queryByText('Cached snapshot')).toBeNull();
});
