// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
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
	getSocialLinks: vi.fn()
}));

const site: management.Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog', name: 'Blog', description: '', defaultLocale: 'zh-CN',
	visibility: 'private', provisionState: 'ready', role: 'owner', settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z'
};

const NativeRequest = globalThis.Request;

function config(name: string): Awaited<ReturnType<typeof management.getSiteConfig>> {
	return {
		config: {
			site: { name: 'Blog', url: 'https://example.com', defaultLocale: 'zh-CN' },
			profile: { name, avatarUrl: '' }, repository: { name: '', owner: '', url: '' },
			seo: { author: '', description: '' }, services: {}
		},
		amapSecurityCodeSet: false, effectiveOrigin: '', digest: 'sha256:config'
	};
}

function pending<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => { resolve = done; });
	return { promise, resolve };
}

function HeaderSlot() {
	const header = usePageHeaderContent();
	return <div data-testid="topbar">{header?.actions}</div>;
}

function Shell() {
	const { pathname } = useLocation();
	return <PageHeaderProvider scope={pathname}><HeaderSlot /><Outlet /></PageHeaderProvider>;
}

function mount() {
	const router = createMemoryRouter([{
		element: <Shell />,
		errorElement: <div data-testid="route-crash">route crashed</div>,
		children: [{ path: '/sites/:siteId/config', element: <SiteConfigPage /> }]
	}], { initialEntries: [`/sites/${site.siteId}/config`] });
	render(<ConfigProvider><AntApp><RouterProvider router={router} /></AntApp></ConfigProvider>);
	return router;
}

beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(management.getSiteConfig).mockReset();
	// These routes have no loaders; Node Request rejects jsdom's AbortSignal.
	vi.stubGlobal('Request', class extends NativeRequest {
		constructor(input: RequestInfo | URL, init?: RequestInit) {
			super(input, { ...init, signal: undefined });
		}
	});
	setLocale('en-US');
	vi.mocked(management.getSite).mockResolvedValue(site);
	vi.mocked(management.getSocialLinks).mockResolvedValue({ links: [], digest: 'sha256:links' });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('keeps hooks and the real header registration stable from loading to ready', async () => {
	const request = pending<ReturnType<typeof config>>();
	vi.mocked(management.getSiteConfig).mockReturnValue(request.promise);
	mount();
	expect(within(screen.getByTestId('topbar')).queryByRole('button')).toBeNull();
	await act(async () => { request.resolve(config('Alice')); });
	expect(await screen.findByDisplayValue('Alice')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
	expect(within(screen.getByTestId('topbar')).getByRole('button', { name: 'Save' })).toBeDefined();
});

it('removes old header actions while switching sites and registers the next page safely', async () => {
	const next = pending<ReturnType<typeof config>>();
	vi.mocked(management.getSiteConfig).mockResolvedValueOnce(config('Alice')).mockReturnValueOnce(next.promise);
	const router = mount();
	expect(await screen.findByDisplayValue('Alice')).toBeDefined();
	await act(async () => { await router.navigate('/sites/22222222-2222-2222-2222-222222222222/config'); });
	expect(within(screen.getByTestId('topbar')).queryByRole('button')).toBeNull();
	await act(async () => { next.resolve(config('Bob')); });
	expect(await screen.findByDisplayValue('Bob')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
});

it('recovers from a failed load without changing hook order', async () => {
	vi.mocked(management.getSiteConfig).mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(config('Recovered'));
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	expect(await screen.findByDisplayValue('Recovered')).toBeDefined();
	expect(screen.queryByTestId('route-crash')).toBeNull();
});
