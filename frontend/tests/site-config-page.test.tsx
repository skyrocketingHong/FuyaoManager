// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { createMemoryRouter, Outlet, RouterProvider, useLocation } from 'react-router-dom';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import SiteConfigPage from '../src/pages/sites/SiteConfigPage';
import * as management from '../src/lib/api/management';
import { setLocale } from '../src/lib/i18n';

vi.mock('../src/lib/api/management', async (original) => ({
	...(await original<typeof management>()),
	getSite: vi.fn(),
	getSiteConfig: vi.fn(),
	putSiteConfig: vi.fn(),
	getSocialLinks: vi.fn()
}));

const site: management.Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Blog',
	description: '',
	defaultLocale: 'zh-CN',
	visibility: 'private',
	provisionState: 'ready',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

const NativeRequest = globalThis.Request;

function config(name: string): Awaited<ReturnType<typeof management.getSiteConfig>> {
	return {
		config: {
			site: { name: 'Blog', url: 'https://example.com', defaultLocale: 'zh-CN' },
			profile: { name, avatarUrl: '' },
			repository: { name: '', owner: '', url: '' },
			seo: { author: '', description: '' },
			services: {}
		},
		amapSecurityCodeSet: false,
		effectiveOrigin: '',
		digest: 'sha256:config'
	};
}

function pending<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}

function HeaderSlot() {
	const header = usePageHeaderContent();
	return <div data-testid="topbar">{header?.content}{header?.actions}</div>;
}

function Shell() {
	const { pathname } = useLocation();
	return (
		<PageHeaderProvider scope={pathname}>
			<HeaderSlot />
			<Outlet />
		</PageHeaderProvider>
	);
}

function mount() {
	const router = createMemoryRouter(
		[
			{
				element: <Shell />,
				errorElement: <div data-testid="route-crash">route crashed</div>,
				children: [{ path: '/sites/:siteId/config', element: <SiteConfigPage /> }]
			}
		],
		{ initialEntries: [`/sites/${site.siteId}/config`] }
	);
	render(
		<ConfigProvider>
			<AntApp>
				<RouterProvider router={router} />
			</AntApp>
		</ConfigProvider>
	);
	return router;
}

beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(management.getSiteConfig).mockReset();
	vi.mocked(management.putSiteConfig).mockReset();
	// These routes have no loaders; Node Request rejects jsdom's AbortSignal.
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, { ...init, signal: undefined });
			}
		}
	);
	setLocale('en-US');
	vi.mocked(management.getSite).mockResolvedValue(site);
	vi.mocked(management.getSocialLinks).mockResolvedValue({ links: [], digest: 'sha256:links' });
});
afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
});

it('keeps hooks and the real header registration stable from loading to ready', async () => {
	const request = pending<ReturnType<typeof config>>();
	vi.mocked(management.getSiteConfig).mockReturnValue(request.promise);
	mount();
	expect(within(screen.getByTestId('topbar')).queryByRole('button')).toBeNull();
	await act(async () => {
		request.resolve(config('Alice'));
	});
	expect(await screen.findByDisplayValue('Alice')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
	expect(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' })).toBeDefined();
});

it('removes old header actions while switching sites and registers the next page safely', async () => {
	const next = pending<ReturnType<typeof config>>();
	vi.mocked(management.getSiteConfig)
		.mockResolvedValueOnce(config('Alice'))
		.mockReturnValueOnce(next.promise);
	const router = mount();
	expect(await screen.findByDisplayValue('Alice')).toBeDefined();
	await act(async () => {
		await router.navigate('/sites/22222222-2222-2222-2222-222222222222/config');
	});
	expect(within(screen.getByTestId('topbar')).queryByRole('button')).toBeNull();
	await act(async () => {
		next.resolve(config('Bob'));
	});
	expect(await screen.findByDisplayValue('Bob')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
});

it('recovers from a failed load without changing hook order', async () => {
	vi.mocked(management.getSiteConfig)
		.mockRejectedValueOnce(new Error('offline'))
		.mockResolvedValueOnce(config('Recovered'));
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	expect(await screen.findByDisplayValue('Recovered')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
});

it('locks editing during a save and clears the draft after the canonical reply', async () => {
	vi.mocked(management.getSiteConfig).mockResolvedValue(config('Alice'));
	const save = pending<Awaited<ReturnType<typeof management.putSiteConfig>>>();
	vi.mocked(management.putSiteConfig).mockReturnValue(save.promise);
	mount();
	const input = (await screen.findByDisplayValue('Alice')) as HTMLInputElement;
	fireEvent.change(input, { target: { value: 'Edited' } });
	fireEvent.click(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(input.disabled).toBe(true));
	expect(management.putSiteConfig).toHaveBeenCalledWith(
		site.siteId,
		expect.objectContaining({ profile: expect.objectContaining({ name: 'Edited' }) }),
		'sha256:config',
		undefined
	);
	await act(async () => {
		save.resolve(config('Edited'));
	});
	await waitFor(() => expect(input.disabled).toBe(false));
	expect(
		(
			within(screen.getByTestId('topbar')).getByRole('button', {
				name: /Save$/
			}) as HTMLButtonElement
		).disabled
	).toBe(true);
});

function completeConfig(): ReturnType<typeof config> {
	const response = config('Alice');
	response.config = {
		schemaVersion: 1,
		site: {
			name: 'Settings-owned name',
			defaultLocale: 'zh-CN',
			url: 'https://old.example',
			startDate: '2019-03-01',
			startYear: 2019
		},
		profile: {
			name: 'Alice',
			birthDate: '2000-02-29',
			avatarUrl: 'https://images.example/avatar.png',
			roles: { 'zh-CN': '开发者', 'en-US': 'Developer' },
			quotes: { 'zh-CN': '签名', 'en-US': 'Quote' }
		},
		repository: { name: 'Repository', owner: 'owner', url: 'https://github.com/owner/repository' },
		seo: {
			author: 'Author',
			description: 'Description',
			keywords: ['one', 'two'],
			twitterId: 'account'
		},
		services: {
			wallpaper: {
				defaultUrl: 'https://images.example/default.jpg',
				apiUrl: '/api/wallpaper',
				metadataApiUrl: '/api/wallpaper-metadata'
			},
			amap: { browserKey: 'public-browser-key', serviceHost: '/_AMapService' },
			githubProxyUrl: '/api/github',
			analyticsProxyUrl: '/api/analytics',
			codingActivityProxyUrl: '/api/activity',
			codingLanguagesProxyUrl: '/api/languages'
		}
	};
	return response;
}

it('edits the canonical site URL and coding language service without losing other site configuration', async () => {
	const initial = completeConfig();
	const expected: management.SiteConfig = {
		...initial.config,
		site: { ...initial.config.site, url: 'https://new.example/blog' },
		services: { ...initial.config.services, codingLanguagesProxyUrl: '/services/languages' }
	};
	vi.mocked(management.getSiteConfig).mockResolvedValue(initial);
	vi.mocked(management.putSiteConfig).mockResolvedValue({
		config: expected,
		digest: 'sha256:saved',
		amapSecurityCodeSet: false
	});
	const router = mount();
	fireEvent.change(await screen.findByRole('textbox', { name: 'Site URL' }), {
		target: { value: 'https://new.example/blog' }
	});
	fireEvent.click(screen.getByRole('radio', { name: 'Services' }));
	await waitFor(() => expect(router.state.location.search).toBe('?tab=services'));
	fireEvent.change(await screen.findByRole('textbox', { name: 'Coding languages proxy URL' }), {
		target: { value: '/services/languages' }
	});
	fireEvent.click(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' }));
	await waitFor(() =>
		expect(management.putSiteConfig).toHaveBeenCalledExactlyOnceWith(
			site.siteId,
			expected,
			'sha256:config',
			undefined
		)
	);
	expect(screen.queryByRole('textbox', { name: 'Site name' })).toBeNull();
	fireEvent.click(screen.getByRole('radio', { name: 'General' }));
	expect(((await screen.findByRole('textbox', { name: 'Site URL' })) as HTMLInputElement).value).toBe(
		'https://new.example/blog'
	);
	expect(screen.queryByTestId('route-crash')).toBeNull();
});

it.each([
	['Site start date', '2026-02-30', 'Enter a valid date in YYYY-MM-DD format.'],
	['Birth date', '2025-02-29', 'Enter a valid date in YYYY-MM-DD format.'],
	['Site start year', '2026.5', 'The start year must be an integer from 1 to 9999.'],
	['Site URL', 'javascript:alert(1)', 'Enter a complete HTTP or HTTPS site URL.']
])('rejects an invalid %s before saving', async (label, value, error) => {
	vi.mocked(management.getSiteConfig).mockResolvedValue(completeConfig());
	mount();
	fireEvent.change(await screen.findByRole('textbox', { name: label }), { target: { value } });
	fireEvent.click(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' }));
	expect(await screen.findByText(error)).toBeDefined();
	expect(management.putSiteConfig).not.toHaveBeenCalled();
});

it('clears optional date, year and service values while preserving required settings-owned fields', async () => {
	const initial = completeConfig();
	vi.mocked(management.getSiteConfig).mockResolvedValue(initial);
	vi.mocked(management.putSiteConfig).mockImplementation(async (_siteId, submitted) => ({
		config: submitted,
		digest: 'sha256:cleared'
	}));
	mount();
	for (const label of ['Site start date', 'Site start year', 'Twitter ID'])
		fireEvent.change(await screen.findByRole('textbox', { name: label }), {
			target: { value: '' }
		});
	fireEvent.click(screen.getByRole('radio', { name: 'Services' }));
	fireEvent.change(await screen.findByRole('textbox', { name: 'Wallpaper API URL' }), {
		target: { value: '' }
	});
	fireEvent.click(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(management.putSiteConfig).toHaveBeenCalledTimes(1));
	const submitted = vi.mocked(management.putSiteConfig).mock.calls[0][1];
	expect(submitted.site).toEqual({ ...initial.config.site, startDate: '', startYear: undefined });
	expect(submitted.services.wallpaper).toEqual({
		...initial.config.services.wallpaper,
		apiUrl: ''
	});
	expect(submitted.seo.twitterId).toBe('');
	expect(submitted.services.codingActivityProxyUrl).toBe(
		initial.config.services.codingActivityProxyUrl
	);
});
