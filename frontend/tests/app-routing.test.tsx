/**
 * App-level routing tests: the three authentication states drive guards,
 * deep links render the right site scope with the right menu selection, and
 * an unknown site shows its own state instead of bouncing to the list.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const alice = {
	id: '11111111-1111-1111-1111-111111111111',
	username: 'alice',
	displayName: 'Alice',
	platformRole: 'admin'
};
const siteAbc = {
	kind: 'theme' as 'theme' | 'static' | 'redirect' | 'reverse_proxy' | 'unsupported',
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
const getPost = vi.fn();
const getPostTaxonomy = vi.fn();
const getPostVocabulary = vi.fn();
const listPostMedia = vi.fn();
const apiRequest = vi.fn();
vi.mock('../src/lib/api/management', () => ({
	getSite: (...args: unknown[]) => getSite(...args),
	getPost: (...args: unknown[]) => getPost(...args),
	getPostTaxonomy: (...args: unknown[]) => getPostTaxonomy(...args),
	getPostVocabulary: (...args: unknown[]) => getPostVocabulary(...args),
	listPostMedia: (...args: unknown[]) => listPostMedia(...args)
}));
vi.mock('../src/lib/api/client', () => ({
	currentAuthGeneration: () => 0,
	api: (...args: unknown[]) => apiRequest(...args),
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
vi.mock('../src/pages/ApiPage', () => ({ default: () => <div>page-api</div> }));
vi.mock('../src/pages/sites/SiteDetailPage', () => ({ default: () => <div>page-overview</div> }));
vi.mock('../src/pages/sites/SettingsPage', () => ({ default: () => <div>page-settings</div> }));
vi.mock('../src/pages/sites/SiteConfigPage', () => ({ default: () => <div>page-config</div> }));
vi.mock('../src/pages/content/posts/PostsPage', () => ({ default: () => <div>page-posts</div> }));
vi.mock('../src/pages/content/FriendsPage', () => ({ default: () => <div>page-friends</div> }));
vi.mock('../src/pages/content/AlbumsPage', () => ({ default: () => <div>page-albums</div> }));
vi.mock('../src/pages/content/FootprintsPage', () => ({ default: () => <div>page-footprints</div> }));
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
	listPostMedia.mockResolvedValue({ items: [], canEdit: true });
	getPostTaxonomy.mockResolvedValue({
		directories: [],
		categories: [],
		tags: [],
		postCount: 0,
		canEdit: true
	});
	getPostVocabulary.mockResolvedValue({
		vocabulary: { version: 1, categories: [], tags: [], canEdit: true, digest: 'sha256:' + 'a'.repeat(64) },
		etag: '"vocabulary"'
	});
	getPost.mockResolvedValue({
		post: {
			path: 'tech/hello.md',
			category: 'tech',
			slug: 'hello',
			title: 'Loaded article',
			date: '2026-01-01',
			draft: false,
			body: 'Body',
			digest: 'sha256:' + 'a'.repeat(64)
		},
		etag: '"sha256:' + 'a'.repeat(64) + '"'
	});
	apiRequest.mockImplementation(async (_method: string, path: string) => {
		if (path === '/api/v1/content')
			return {
				data: {
					canEdit: true,
					counts: { posts: 0, albums: 0, photos: 0, places: 0, friends: 0 },
					amap: {}
				},
				etag: null
			};
		if (path.endsWith('/content-sources'))
			return {
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
			};
		if (path.endsWith('/overview'))
			return {
				data: {
					counts: { posts: 0, albums: 0, photos: 0, places: 0, friends: 0 },
					sources: {},
					hasCaddy: false,
					analytics: {
						configured: false,
						available: false,
						provider: 'cloudflare',
						cache: 'unknown',
						latest: null
					},
					theme: { selected: false, theme: null }
				},
				etag: null
			};
		return { data: {}, etag: null };
	});
	window.history.pushState({}, '', '/');
	const NativeRequest = globalThis.Request;
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init ? { ...init, signal: undefined } : init);
			}
		}
	);
});

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
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
	const router = createMemoryRouter([{ path: '*', element: <App /> }], { initialEntries: [path] });
	return { ...render(
		<ConfigProvider>
			<RouterProvider router={router} />
		</ConfigProvider>
	), router };
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

	it.each([
		['/content/posts/vocabulary?view=categories', '/content/posts/categories'],
		['/content/posts/vocabulary?view=tags', '/content/posts/tags'],
		['/sites/abc/posts/vocabulary?view=categories', '/sites/abc/posts/categories'],
		['/sites/abc/posts/vocabulary?view=tags', '/sites/abc/posts/tags']
	])('redirects the legacy taxonomy URL to its route', async (legacy, expected) => {
		const { router } = renderAt(legacy);
		await waitFor(() => expect(router.state.location.pathname).toBe(expected));
		await waitFor(() => expect(getPostVocabulary).toHaveBeenCalled());
		expect(router.state.location.search).toBe('');
	});

	it('renders the real post editor inside the site shell and retries a failed read', async () => {
		getPost.mockRejectedValueOnce(new Error('offline'));
		renderAt('/sites/abc/posts/tech%2Fhello.md/edit');
		await waitFor(() => expect(getPost).toHaveBeenCalledWith('abc', 'tech/hello.md'));
		const retry = await screen.findByRole('button', {
			name: (name) => name.replace(/\s/g, '') === zh['common.retry']
		});
		fireEvent.click(retry);
		expect(((await screen.findByLabelText(zh['posts.fieldTitle'])) as HTMLInputElement).value).toBe(
			'Loaded article'
		);
		expect(getPost).toHaveBeenLastCalledWith('abc', 'tech/hello.md');
		const topbar = document.querySelector('.topbar');
		expect(topbar?.textContent?.replace(/\s/g, '')).toContain(zh['common.save']);
		expect(document.querySelector('.ant-menu-item-selected')?.textContent).toContain(
			zh['nav.posts']
		);
	});

	it('renders the platform post editor with platform scope and a single top-bar save action', async () => {
		renderAt('/content/posts/new');
		await screen.findByLabelText(zh['posts.fieldTitle']);
		await waitFor(() =>
			expect(getPostTaxonomy).toHaveBeenCalledWith({ type: 'platform' }, expect.anything())
		);
		expect(document.querySelectorAll('.topbar button').length).toBeGreaterThan(0);
		expect(
			screen.getAllByRole('button', {
				name: (name) => name.replace(/\s/g, '') === zh['common.save']
			})
		).toHaveLength(1);
	});

	it('opens the platform media workspace in the real shell', async () => {
		renderAt('/content/posts/media');
		await waitFor(() =>
			expect(listPostMedia).toHaveBeenCalledWith({ type: 'platform' }, expect.anything())
		);
		expect(screen.getByRole('button', { name: zh['media.importFolder'] })).toBeDefined();
		expect(document.querySelector('.ant-menu-item-selected')?.textContent).toContain(
			zh['nav.posts']
		);
	});

	it('opens shared site posts as read-only in the real editor', async () => {
		const fallback = apiRequest.getMockImplementation()!;
		apiRequest.mockImplementation(async (method: string, path: string, ...rest: unknown[]) => {
			if (path.endsWith('/content-sources'))
				return {
					data: {
						sources: {
							posts: 'platform',
							albums: 'independent',
							footprints: 'independent',
							friends: 'independent'
						},
						revision: 1
					},
					etag: '"sources-1"'
				};
			return fallback(method, path, ...rest);
		});
		renderAt('/sites/abc/posts/tech%2Fhello.md/edit');
		const title = (await screen.findByLabelText(zh['posts.fieldTitle'])) as HTMLInputElement;
		expect(title.value).toBe('Loaded article');
		expect(title.disabled).toBe(true);
		expect(
			(
				screen.getByRole('button', {
					name: (name) => name.replace(/\s/g, '') === zh['common.save']
				}) as HTMLButtonElement
			).disabled
		).toBe(true);
		expect(getPost).toHaveBeenLastCalledWith('abc', 'tech/hello.md');
	});

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

describe('site kind workspaces', () => {
	beforeEach(() => authenticated());
	it('does not guess a theme menu for an unknown type while the catalogue is loading', async () => {
		mockSiteListState = { status: 'loading', sites: [], errorKey: null };
		getSite.mockResolvedValue({ ...siteAbc, kind: 'static' });
		renderAt('/sites/abc/posts');
		expect(await screen.findByText(zh['caddy.unsupportedPage'])).toBeDefined();
		expect(document.querySelector('.app-sider nav')?.textContent).not.toContain(zh['nav.posts']);
	});
	it('offers theme selection and content sources for an adopted static site before a theme is selected', async () => {
		const site = {
			...siteAbc,
			kind: 'static' as const,
			managementMode: 'platform',
			workspace: {
				themeEnabled: false,
				themeSelectable: true,
				buildable: false,
				deployable: false,
				hasCaddy: true
			}
		};
		mockSiteListState = { status: 'ready', sites: [site], errorKey: null };
		getSite.mockResolvedValue(site);
		renderAt('/sites/abc/theme');
		expect(await screen.findByText('page-theme')).toBeDefined();
		const menu = document.querySelector('.app-sider nav')!;
		expect(menu.textContent).toContain(zh['nav.themeSelection']);
		expect(menu.textContent).toContain(zh['nav.contentSources']);
		expect(menu.textContent).toContain(zh['nav.caddy']);
		expect(menu.textContent).not.toContain(zh['nav.themeConfiguration']);
		expect(menu.textContent).not.toContain(zh['nav.builds']);
	});
	it('uses declared theme workspace capabilities while retaining the static source identity', async () => {
		const site = {
			...siteAbc,
			kind: 'static' as const,
			managementMode: 'platform',
			workspace: {
				themeEnabled: true,
				themeSelectable: true,
				buildable: true,
				deployable: false,
				hasCaddy: true
			}
		};
		mockSiteListState = { status: 'ready', sites: [site], errorKey: null };
		getSite.mockResolvedValue(site);
		renderAt('/sites/abc/builds');
		expect(await screen.findByText('page-builds')).toBeDefined();
		const menu = document.querySelector('.app-sider nav')!;
		expect(menu.textContent).toContain(zh['nav.themeConfiguration']);
		expect(menu.textContent).toContain(zh['nav.builds']);
		expect(menu.textContent).toContain(zh['nav.releases']);
		expect(menu.textContent).toContain(zh['nav.caddy']);
		expect(screen.queryByText(zh['caddy.unsupportedPage'])).toBeNull();
	});
	it.each(['static', 'redirect', 'reverse_proxy', 'unsupported'] as const)(
		'shows the %s configuration without theme-only menu entries',
		async (kind) => {
			const site = { ...siteAbc, kind };
			mockSiteListState = { status: 'ready', sites: [site], errorKey: null };
			getSite.mockResolvedValue(site);
			apiRequest.mockResolvedValue({
				data: {
					siteId: site.siteId,
					kind,
					managementMode: 'caddy',
					capabilities: { convert: false, edit: false, validate: false, deploy: false },
					unavailableReason: 'operator_registration_required',
					present: true,
					syncedAt: site.updatedAt,
					definition: {
						hosts: ['existing.example.com'],
						listen: [':443'],
						root: '/srv/existing-static-site',
						readOnly: true,
						...(kind === 'redirect'
							? { redirect: { location: 'https://destination.example.com{uri}', statusCode: 308 } }
							: {})
					}
				},
				etag: null
			});
			renderAt('/sites/abc/caddy');
			expect(await screen.findByText('/srv/existing-static-site')).toBeDefined();
			const menu = document.querySelector('.app-sider nav')!;
			for (const key of [
				'nav.posts',
				'nav.friends',
				'nav.albums',
				'nav.footprints',
				'nav.theme',
				'nav.builds',
				'nav.releases',
				'nav.config'
			])
				expect(menu.textContent).not.toContain(zh[key as keyof typeof zh]);
			expect(menu.textContent).toContain(zh['nav.caddy']);
			expect(screen.getByText(zh['caddy.operatorRegistration'])).toBeDefined();
			expect(screen.queryByText('page-config')).toBeNull();
			expect(screen.queryByText('page-theme')).toBeNull();
			if (kind === 'redirect')
				expect(
					screen.getByRole('link', { name: zh['caddy.openTarget'] }).getAttribute('href')
				).toBe('https://destination.example.com');
		}
	);
	it.each(['static', 'redirect'] as const)(
		'does not let a %s posts deep link enter a theme editor',
		async (kind) => {
			const site = { ...siteAbc, kind };
			mockSiteListState = { status: 'ready', sites: [site], errorKey: null };
			getSite.mockResolvedValue(site);
			renderAt('/sites/abc/posts');
			expect(await screen.findByText(zh['caddy.unsupportedPage'])).toBeDefined();
			expect(screen.queryByText('page-posts')).toBeNull();
			expect(apiRequest).not.toHaveBeenCalled();
		}
	);
	it('opens the registered Caddy route independently of content kind', async () => {
		apiRequest.mockResolvedValue({
			data: {
				siteId: siteAbc.siteId,
				sourceKey: 'existing-source',
				kind: 'static',
				managementMode: 'platform',
				management: null,
				capabilities: { convert: false, edit: false, validate: false, deploy: false },
				unavailableReason: 'operator_registration_required',
				present: true,
				syncedAt: siteAbc.updatedAt,
				definition: { hosts: ['existing.example.com'], listen: [':443'], root: '/srv/theme-site' }
			},
			etag: '"source"'
		});
		renderAt('/sites/abc/caddy');
		expect(await screen.findByText('/srv/theme-site')).toBeDefined();
		expect(document.querySelector('.app-sider nav')?.textContent).toContain(zh['nav.posts']);
		expect(document.querySelector('.app-sider nav')?.textContent).toContain(zh['nav.caddy']);
		expect(apiRequest).toHaveBeenCalledWith(
			'GET',
			'/api/v1/sites/abc/caddy',
			expect.objectContaining({ signal: expect.anything() })
		);
		expect(screen.queryByText(zh['caddy.unsupportedPage'])).toBeNull();
	});
	it('retries Caddy loading within the real route and top bar', async () => {
		const site = { ...siteAbc, kind: 'static' as const };
		mockSiteListState = { status: 'ready', sites: [site], errorKey: null };
		getSite.mockResolvedValue(site);
		apiRequest.mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce({
			data: {
				siteId: site.siteId,
				sourceKey: 'existing-source',
				kind: 'static',
				managementMode: 'caddy',
				management: null,
				capabilities: { convert: false, edit: false, validate: false, deploy: false },
				unavailableReason: 'operator_registration_required',
				present: true,
				syncedAt: site.updatedAt,
				definition: { hosts: ['existing.example.com'], listen: [':443'], root: '/srv/recovered' }
			},
			etag: '"source"'
		});
		renderAt('/sites/abc/caddy');
		expect(await screen.findByText(zh['error.network'])).toBeDefined();
		fireEvent.click(
			screen.getByRole('button', { name: (name) => name.replace(/\s/g, '') === zh['common.retry'] })
		);
		expect(await screen.findByText('/srv/recovered')).toBeDefined();
		expect(document.title).toBe(`Fuyao Manager - ${zh['nav.caddy']}`);
		expect(document.querySelector('.topbar')?.textContent).toContain(zh['sites.caddyManagement']);
	});
});
