/**
 * Document title tests: the tab title follows the route and the locale
 * ("Fuyao Manager - <page>"), the auth screens and the session-error screen
 * get their own page names, and the HTML entry references the local SVG
 * favicon with a pre-hydration fallback title.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';
import en from '../src/lib/i18n/locales/en-US.json';
import indexHtml from '../index.html?raw';
import faviconSvg from '../public/favicon.svg?raw';

const alice = {
	id: '11111111-1111-1111-1111-111111111111',
	username: 'alice',
	displayName: 'Alice',
	platformRole: 'admin'
};
const siteAbc = {
	siteId: 'abc',
	slug: 'abc',
	visibility: 'private',
	provisionState: 'ready',
	name: 'ABC Site',
	description: '',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

let mockSession: {
	status: string;
	user: typeof alice | null;
	platformInitialized: boolean;
	passkeyBound: boolean;
	passkeyAvailable: boolean;
};

vi.mock('../src/lib/session', () => ({
	subscribeSession: () => () => undefined,
	sessionSnapshot: () => mockSession,
	signInWithCode: vi.fn(),
	signInWithPasskey: vi.fn(),
	bindPasskey: vi.fn(),
	signOut: vi.fn(),
	loadSession: vi.fn()
}));

let mockSiteListState: { status: string; sites: (typeof siteAbc)[]; errorKey: null };
vi.mock('../src/lib/sites/site-list', () => ({
	subscribeSiteList: () => () => undefined,
	siteListSnapshot: () => mockSiteListState,
	loadSiteList: vi.fn(),
	upsertSite: vi.fn()
}));

const getSite = vi.fn();
vi.mock('../src/lib/api/management', () => ({
	getSite: (...args: unknown[]) => getSite(...args)
}));
vi.mock('../src/lib/api/client', () => ({
	currentAuthGeneration: () => 0,
	api: vi
		.fn()
		.mockResolvedValue({
			data: {
				sources: {
					posts: 'independent',
					albums: 'independent',
					footprints: 'independent',
					friends: 'independent'
				},
				revision: 1
			},
			etag: '"sources-1"'
		}),
	ApiError: class ApiError extends Error {
		constructor(
			public status: number,
			public code: string
		) {
			super(code);
		}
	}
}));

// Marker doubles keep the test focused on the tab title, not page internals.
vi.mock('../src/pages/LoginPage', () => ({ default: () => <div>page-login</div> }));
vi.mock('../src/pages/BindPasskeyPage', () => ({ default: () => <div>page-bind</div> }));
vi.mock('../src/pages/SitesPage', () => ({ default: () => <div>page-sites</div> }));
vi.mock('../src/pages/ThemesCatalogPage', () => ({ default: () => <div>page-themes</div> }));
vi.mock('../src/pages/UsersPage', () => ({ default: () => <div>page-users</div> }));
vi.mock('../src/pages/LogsPage', () => ({ default: () => <div>page-logs</div> }));
vi.mock('../src/pages/AppearancePage', () => ({ default: () => <div>page-appearance</div> }));
vi.mock('../src/pages/sites/SiteDetailPage', () => ({ default: () => <div>page-overview</div> }));
vi.mock('../src/pages/sites/SettingsPage', () => ({ default: () => <div>page-settings</div> }));
vi.mock('../src/pages/sites/SiteConfigPage', () => ({ default: () => <div>page-config</div> }));
vi.mock('../src/pages/sites/PostsPage', () => ({ default: () => <div>page-posts</div> }));
vi.mock('../src/pages/sites/FriendsPage', () => ({ default: () => <div>page-friends</div> }));
vi.mock('../src/pages/sites/AlbumsPage', () => ({ default: () => <div>page-albums</div> }));
vi.mock('../src/pages/sites/FootprintsPage', () => ({ default: () => <div>page-footprints</div> }));
vi.mock('../src/pages/sites/ThemePage', () => ({ default: () => <div>page-theme</div> }));
vi.mock('../src/pages/sites/BuildsPage', () => ({ default: () => <div>page-builds</div> }));
vi.mock('../src/pages/sites/ReleasesPage', () => ({ default: () => <div>page-releases</div> }));

import App from '../src/App';
import { setLocale } from '../src/lib/i18n';

beforeAll(() => {
	if (!window.matchMedia) {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			value: (query: string) => ({
				matches: false,
				media: query,
				onchange: null,
				addListener: () => undefined,
				removeListener: () => undefined,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				dispatchEvent: () => false
			})
		});
	}
});

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
	mockSiteListState = { status: 'ready', sites: [siteAbc], errorKey: null };
	getSite.mockResolvedValue(siteAbc);
});

afterEach(() => {
	cleanup();
});

function sessionWith(status: string) {
	mockSession = {
		status,
		user: status === 'anonymous' ? null : alice,
		platformInitialized: true,
		passkeyBound: status === 'authenticated',
		passkeyAvailable: true
	};
}

function renderAt(path: string) {
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[path]}>
				<App />
			</MemoryRouter>
		</ConfigProvider>
	);
}

describe('route titles', () => {
	it.each([
		['/users', 'nav.users'],
		['/logs', 'nav.logs'],
		['/appearance', 'nav.appearance']
	])('titles %s from the shared navigation metadata', async (path, key) => {
		sessionWith('authenticated');
		renderAt(path);
		await waitFor(() =>
			expect(document.title).toBe(`Fuyao Manager - ${zh[key as keyof typeof zh]}`)
		);
	});
	it('titles a platform route from its navigation metadata', async () => {
		sessionWith('authenticated');
		renderAt('/sites');
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['nav.sites']}`));
	});

	it('titles a site-scoped deep link with its navigation page name', async () => {
		sessionWith('authenticated');
		renderAt('/sites/abc/posts');
		await waitFor(() => expect(screen.getByText('page-posts')).toBeDefined());
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['nav.posts']}`));
	});

	it('titles the login page for an anonymous session', async () => {
		sessionWith('anonymous');
		renderAt('/login');
		await waitFor(() => expect(screen.getByText('page-login')).toBeDefined());
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['login.title']}`));
	});

	it('titles the binding page for a restricted session', async () => {
		sessionWith('restricted');
		renderAt('/register-passkey');
		await waitFor(() => expect(screen.getByText('page-bind')).toBeDefined());
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['bind.title']}`));
	});

	it('titles the session error screen while no route renders', async () => {
		sessionWith('error');
		renderAt('/sites');
		await waitFor(() => expect(screen.getByText(zh['session.errorTitle'])).toBeDefined());
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['session.errorTitle']}`));
	});
});

describe('locale switching', () => {
	it('retitles the current page when the locale changes', async () => {
		sessionWith('authenticated');
		renderAt('/sites/abc/posts');
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${zh['nav.posts']}`));
		act(() => setLocale('en-US'));
		await waitFor(() => expect(document.title).toBe(`Fuyao Manager - ${en['nav.posts']}`));
	});
});

describe('html entry', () => {
	it('references the local SVG favicon and a pre-hydration fallback title', () => {
		expect(indexHtml).toContain('rel="icon"');
		expect(indexHtml).toContain('href="/favicon.svg"');
		expect(indexHtml).toContain('<title>Fuyao Manager</title>');
		expect(faviconSvg).toContain('<svg');
	});
});
