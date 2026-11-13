/**
 * App-level routing tests: the three authentication states drive guards,
 * deep links render the right site scope with the right menu selection, and
 * an unknown site shows its own state instead of bouncing to the list.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';

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
	ApiError: class ApiError extends Error {
		constructor(
			public status: number,
			public code: string
		) {
			super(code);
		}
	}
}));

// Marker doubles keep the test focused on routing, not page internals.
vi.mock('../src/pages/LoginPage', () => ({ default: () => <div>page-login</div> }));
vi.mock('../src/pages/AccountSettingsPage', () => ({
	default: () => <div>page-account</div>
}));
vi.mock('../src/pages/BindPasskeyPage', () => ({ default: () => <div>page-bind</div> }));
vi.mock('../src/pages/SitesPage', async () => {
	const { useState } = await import('react');
	const { t } = await import('../src/lib/i18n');
	return {
		default: function SitesMarker() {
			const [draft, setDraft] = useState('initial');
			return (
				<div>
					<span>page-sites</span>
					<label>
						{t('common.save')}
						<input
							aria-label="draft-value"
							value={draft}
							onChange={(event) => setDraft(event.target.value)}
						/>
					</label>
				</div>
			);
		}
	};
});
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
	window.history.pushState({}, '', '/');
});

afterEach(() => {
	cleanup();
});

function authenticated() {
	mockSession = {
		status: 'authenticated',
		user: alice,
		platformInitialized: true,
		passkeyBound: true,
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

describe('guards', () => {
	it('redirects an anonymous browser from business routes to login', async () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		renderAt('/sites/abc/posts');
		await waitFor(() => expect(screen.getByText('page-login')).toBeDefined());
	});

	it('confines a restricted session to the binding page', async () => {
		mockSession = {
			status: 'restricted',
			user: alice,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		renderAt('/sites');
		await waitFor(() => expect(screen.getByText('page-bind')).toBeDefined());
	});

	it('sends an authenticated session away from /login to the sites list', async () => {
		authenticated();
		renderAt('/login');
		await waitFor(() => expect(screen.getByText('page-sites')).toBeDefined());
	});
});

describe('deep links and menu selection', () => {
	beforeEach(() => authenticated());

	it('renders the themes catalog from the menu-reachable route', async () => {
		renderAt('/themes');
		await waitFor(() => expect(screen.getByText('page-themes')).toBeDefined());
		await waitFor(() => {
			const selected = document.querySelector('.ant-menu-item-selected');
			expect(selected?.textContent).toContain(zh['nav.themesCatalog']);
		});
	});

	it.each([
		['/sites/abc/posts', 'page-posts', 'nav.posts'],
		['/sites/abc/albums', 'page-albums', 'nav.albums'],
		['/sites/abc/footprints', 'page-footprints', 'nav.footprints'],
		['/sites/abc/settings', 'page-settings', 'nav.settings'],
		['/sites/abc/config', 'page-config', 'nav.config'],
		['/sites/abc/theme', 'page-theme', 'nav.theme'],
		['/sites/abc/builds', 'page-builds', 'nav.builds'],
		['/sites/abc/releases', 'page-releases', 'nav.releases']
	])('deep link %s renders its page with menu selection', async (path, marker, titleKey) => {
		renderAt(path);
		await waitFor(() => expect(screen.getByText(marker)).toBeDefined());
		await waitFor(() => {
			const selected = document.querySelector('.ant-menu-item-selected');
			expect(selected?.textContent).toContain(zh[titleKey as keyof typeof zh]);
		});
	});

	it('deep link /sites/abc marks the overview in the fixed action region, not the menu', async () => {
		renderAt('/sites/abc');
		await waitFor(() => expect(screen.getByText('page-overview')).toBeDefined());
		await waitFor(() => {
			const active = document.querySelector('.sidebar-actions .is-active');
			expect(active?.textContent).toContain(zh['nav.overview']);
			expect(active?.getAttribute('aria-current')).toBe('page');
		});
		expect(document.querySelector('.ant-menu-item-selected')).toBeNull();
	});

	it('shows the current site block in site scope', async () => {
		renderAt('/sites/abc/posts');
		await waitFor(() => expect(screen.getByText('page-posts')).toBeDefined());
		expect(screen.getByText('ABC Site')).toBeDefined();
	});

	it('renders a not-found state for an unknown site instead of bouncing', async () => {
		const { ApiError } = await import('../src/lib/api/client');
		const notFound = new (ApiError as unknown as new (status: number, code: string) => Error)(
			404,
			'not_found'
		);
		getSite.mockRejectedValue(notFound);
		renderAt('/sites/ghost/posts');
		await waitFor(() => expect(screen.getByText(zh['site.boundary.notFoundTitle'])).toBeDefined());
		expect(screen.queryByText('page-sites')).toBeNull();
	});
});

describe('sidebar shell', () => {
	beforeEach(() => authenticated());

	it.each([
		['/users', 'page-users', 'nav.users'],
		['/logs', 'page-logs', 'nav.logs']
	])('allows administrators at %s', async (path, marker, title) => {
		renderAt(path);
		expect(await screen.findByText(marker)).toBeDefined();
		expect(document.querySelector('.ant-menu-item-selected')?.textContent).toContain(
			zh[title as keyof typeof zh]
		);
	});

	it.each(['/users', '/logs'])(
		'blocks a member deep link to %s and hides management menus',
		async (path) => {
			mockSession = { ...mockSession, user: { ...alice, platformRole: 'member' } };
			renderAt(path);
			expect(await screen.findByText(zh['error.forbidden'])).toBeDefined();
			expect(screen.queryByText('page-users')).toBeNull();
			expect(screen.queryByText('page-logs')).toBeNull();
			expect(screen.queryByRole('menuitem', { name: zh['nav.users'] })).toBeNull();
			expect(screen.queryByRole('menuitem', { name: zh['nav.logs'] })).toBeNull();
			expect(screen.getByRole('menuitem', { name: zh['nav.appearance'] })).toBeDefined();
		}
	);

	it('renders the favicon brand, the platform menu, and the fixed home action', async () => {
		renderAt('/sites');
		await waitFor(() => expect(screen.getByText('page-sites')).toBeDefined());
		expect(document.querySelector('.sidebar-brand img[src="/favicon.svg"]')).not.toBeNull();
		expect(screen.queryByText(zh['nav.sites'])).toBeNull();
		expect(screen.getByText(zh['nav.themesCatalog'])).toBeDefined();
		const home = screen.getByText(zh['nav.platformHome']).closest('button');
		expect(home).not.toBeNull();
		expect(home?.getAttribute('aria-current')).toBe('page');
		const sidebar = document.querySelector('.app-sider')!;
		const actions = sidebar.querySelector('.sidebar-actions')!;
		expect(
			actions.compareDocumentPosition(sidebar.querySelector('nav')!) &
				Node.DOCUMENT_POSITION_FOLLOWING
		).not.toBe(0);
		expect(sidebar.querySelectorAll('[aria-current="page"]')).toHaveLength(1);
		expect(screen.queryByText(zh['nav.overview'])).toBeNull();
	});

	it('keeps the site menu free of platform entries and puts overview in the actions', async () => {
		renderAt('/sites/abc/builds');
		await waitFor(() => expect(screen.getByText('page-builds')).toBeDefined());
		expect(screen.getByText(zh['nav.group.content'])).toBeDefined();
		expect(screen.queryByText(zh['nav.sites'])).toBeNull();
		expect(screen.queryByText(zh['nav.themesCatalog'])).toBeNull();
		expect(document.querySelector('.ant-menu-item-selected')?.textContent).toContain(
			zh['nav.builds']
		);
		const overview = screen.getByText(zh['nav.overview']).closest('button');
		expect(overview?.getAttribute('aria-current')).toBeNull();
		const home = screen.getByText(zh['nav.returnPlatform']).closest('button');
		expect(home?.getAttribute('aria-current')).toBeNull();
	});

	it('mounts /account from the navigation metadata but keeps it out of the menus', async () => {
		renderAt('/account');
		await waitFor(() => expect(screen.getByText('page-account')).toBeDefined());
		// The account page never mixes into either menu.
		expect(screen.queryByText(zh['nav.account'])).toBeNull();
	});

	it('navigates home from the fixed action region', async () => {
		renderAt('/themes');
		await waitFor(() => expect(screen.getByText('page-themes')).toBeDefined());
		await act(async () => {
			fireEvent.click(screen.getByText(zh['nav.platformHome']).closest('button')!);
		});
		await waitFor(() => expect(screen.getByText('page-sites')).toBeDefined());
	});

	it('updates the page locale without remounting its draft', async () => {
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		fireEvent.click(screen.getByRole('button', { name: 'en-US' }));
		await waitFor(() => expect(screen.getByText('Save')).toBeDefined());
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(document.title).toBe('Fuyao Manager - Sites');
	});
});
