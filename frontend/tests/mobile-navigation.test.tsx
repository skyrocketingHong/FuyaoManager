/**
 * Narrow-screen navigation drawer tests: a controllable matchMedia double
 * drives the 991px breakpoint with the real antd Drawer in play. Covers the
 * Sider/Drawer switch, focus restoration after the native close paths,
 * route- and entry-driven closes, mask/scroll-lock cleanup when switching
 * back to desktop, stable page state across breakpoints, and scope/permission
 * parity with the desktop sidebar. The matchMedia replacement is installed
 * per test and restored afterwards so other suites keep the shared stub.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';
import { NARROW_MEDIA_QUERY } from '../src/components/layout/AppShell';

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
const apiRequest = vi.fn();
vi.mock('../src/lib/api/management', () => ({
	getSite: (...args: unknown[]) => getSite(...args)
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

// The sites marker publishes real top-bar slot content and hosts a draft
// input so breakpoint switches must not remount the page.
vi.mock('../src/pages/SitesPage', async () => {
	const { useState } = await import('react');
	const { usePageHeader } = await import('../src/components/PageHeader');
	return {
		default: function SitesMarker() {
			const [draft, setDraft] = useState('initial');
			usePageHeader({
				content: <span>marker-slot-content</span>,
				actions: <button type="button">marker-slot-action</button>
			});
			return (
				<div>
					<span>page-sites</span>
					<label>
						{zh['common.save']}
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
vi.mock('../src/pages/sites/SiteDetailPage', () => ({ default: () => <div>page-overview</div> }));
vi.mock('../src/pages/sites/PostsPage', () => ({ default: () => <div>page-posts</div> }));

import App from '../src/App';
import { setLocale } from '../src/lib/i18n';

// --- switchable matchMedia double -------------------------------------------
const originalMatchMedia = window.matchMedia;
let narrowMatches = false;
const mediaListeners = new Set<(event: { matches: boolean }) => void>();

function useNarrowViewport(initial: boolean) {
	narrowMatches = initial;
	mediaListeners.clear();
	Object.defineProperty(window, 'matchMedia', {
		configurable: true,
		writable: true,
		value: (query: string) => ({
			matches: query === NARROW_MEDIA_QUERY ? narrowMatches : false,
			media: query,
			onchange: null,
			addEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => {
				mediaListeners.add(listener);
			},
			removeEventListener: (_type: string, listener: (event: { matches: boolean }) => void) => {
				mediaListeners.delete(listener);
			},
			addListener: () => undefined,
			removeListener: () => undefined,
			dispatchEvent: () => false
		})
	});
}

function switchViewport(narrow: boolean) {
	narrowMatches = narrow;
	for (const listener of [...mediaListeners]) listener({ matches: narrow } as MediaQueryListEvent);
}

// --- drawer motion helpers ---------------------------------------------------
// jsdom never fires real transitions; re-dispatching transitionend until the
// expected condition holds drives the rc-motion enter/leave steps to
// completion no matter which frame they are on. The motion ref lives on the
// drawer section, so events are dispatched there (the content wrapper keeps
// the transition styles, so both targets are covered). The portal root stays
// mounted after a normal close (rc-drawer autoDestroy=false): a closed drawer
// is the open-class/mask/wrapper-hidden state, not a removed node.
async function settleDrawerMotion() {
	document.querySelector('.ant-drawer-section')?.dispatchEvent(new Event('transitionend'));
	document.querySelector('.ant-drawer-content-wrapper')?.dispatchEvent(new Event('transitionend'));
}

async function openNavigationDrawer() {
	const button = screen.getByRole('button', { name: zh['app.openNavigation'] });
	button.focus();
	fireEvent.click(button);
	await waitFor(() => expect(document.querySelector('.ant-drawer-open')).not.toBeNull());
	await waitFor(
		() => {
			settleDrawerMotion();
			expect(button.getAttribute('aria-expanded')).toBe('true');
		},
		{ timeout: 5000 }
	);
	return button;
}

async function expectDrawerClosed(button?: HTMLElement) {
	await waitFor(
		() => {
			settleDrawerMotion();
			expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(
				false
			);
			expect(document.querySelector('.ant-drawer-mask')).toBeNull();
			expect(
				document
					.querySelector('.ant-drawer-content-wrapper')
					?.classList.contains('ant-drawer-content-wrapper-hidden')
			).toBe(true);
			if (button) expect(document.activeElement).toBe(button);
		},
		{ timeout: 5000 }
	);
}

function bodyHasScrollLockStyle() {
	return Array.from(document.querySelectorAll('style')).some((tag) =>
		tag.textContent?.includes('overflow-y: hidden')
	);
}

// --- shared fixtures ---------------------------------------------------------
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
	return render(
		<ConfigProvider>
			<RouterProvider router={router} />
		</ConfigProvider>
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
	mockSiteListState = { status: 'ready', sites: [siteAbc], errorKey: null };
	getSite.mockResolvedValue(siteAbc);
	apiRequest.mockImplementation(async (_method: string, path: string) => {
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
	// jsdom's Request rejects AbortSignal values; real pages construct one.
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
	Object.defineProperty(window, 'matchMedia', {
		configurable: true,
		writable: true,
		value: originalMatchMedia
	});
	mediaListeners.clear();
});

describe('breakpoint switch', () => {
	it('renders the desktop Sider and swaps to the drawer trigger below 992px', async () => {
		useNarrowViewport(false);
		authenticated();
		renderAt('/sites');
		expect(await screen.findByText('page-sites')).toBeDefined();
		expect(document.querySelector('.app-sider')).not.toBeNull();
		expect(document.querySelector('.ant-drawer')).toBeNull();
		const desktopNavigationSlot = document.querySelector('.topbar-navigation');
		expect(desktopNavigationSlot?.querySelector('button')).toBeNull();

		await act(async () => {
			switchViewport(true);
		});
		expect(document.querySelector('.app-sider')).toBeNull();
		const button = screen.getByRole('button', { name: zh['app.openNavigation'] });
		expect(button.getAttribute('aria-expanded')).toBe('false');
		// A re-entered narrow viewport starts with the drawer closed.
		expect(document.querySelector('.ant-drawer')).toBeNull();

		await act(async () => {
			switchViewport(false);
		});
		expect(document.querySelector('.app-sider')).not.toBeNull();
		expect(screen.queryByRole('button', { name: zh['app.openNavigation'] })).toBeNull();
	});

	it('keeps the page draft and top-bar slots alive across breakpoint switches', async () => {
		useNarrowViewport(true);
		authenticated();
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		expect(screen.getByText('marker-slot-content')).toBeDefined();

		await act(async () => {
			switchViewport(false);
		});
		expect(
			(screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value
		).toBe('unsaved');
		expect(screen.getByText('marker-slot-content')).toBeDefined();
		expect(screen.getByText('marker-slot-action')).toBeDefined();

		await act(async () => {
			switchViewport(true);
		});
		// The drawer is closed by default and the page state survives again.
		expect(document.querySelector('.ant-drawer')).toBeNull();
		expect(
			(screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value
		).toBe('unsaved');
		expect(screen.getByText('marker-slot-content')).toBeDefined();
	});
});

describe('drawer interactions', () => {
	beforeEach(() => {
		useNarrowViewport(true);
		authenticated();
	});

	it('opens the navigation regions in order and closes via Escape back to the menu button', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		const button = await openNavigationDrawer();
		expect(document.querySelector('.app-sider')).toBeNull();
		const drawer = document.querySelector('.ant-drawer')!;
		expect(drawer.querySelector('.sidebar-brand a')).not.toBeNull();
		expect(drawer.querySelector('.sidebar-info')).not.toBeNull();
		expect(drawer.querySelector('.sidebar-actions')).not.toBeNull();
		expect(drawer.querySelector('nav')).not.toBeNull();
		expect(drawer.querySelector('.sidebar-foot')).not.toBeNull();
		expect(
			drawer.querySelector('.sidebar-actions')!.compareDocumentPosition(drawer.querySelector('nav')!) &
				Node.DOCUMENT_POSITION_FOLLOWING
		).not.toBe(0);
		expect(screen.getByRole('menuitem', { name: zh['nav.themesCatalog'] })).toBeDefined();

		fireEvent.keyDown(window, { key: 'Escape' });
		await expectDrawerClosed(button);
	}, 15000);

	it('closes via the native close button and restores focus', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		const button = await openNavigationDrawer();
		fireEvent.click(document.querySelector('.ant-drawer-close')!);
		await expectDrawerClosed(button);
	}, 15000);

	it('closes via the mask and restores focus', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		const button = await openNavigationDrawer();
		fireEvent.click(document.querySelector('.ant-drawer-mask')!);
		await expectDrawerClosed(button);
	}, 15000);

	it('closes after a successful menu navigation and after the brand link', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		const button = await openNavigationDrawer();
		fireEvent.click(screen.getByRole('menuitem', { name: zh['nav.themesCatalog'] }));
		expect(await screen.findByText('page-themes')).toBeDefined();
		await expectDrawerClosed(button);

		// The brand link navigates home and closes the drawer as well.
		const buttonAgain = await openNavigationDrawer();
		fireEvent.click(document.querySelector('.sidebar-brand a')!);
		expect(await screen.findByText('page-sites')).toBeDefined();
		await expectDrawerClosed(buttonAgain);
	}, 15000);

	it('closes when the current page entry is clicked without changing routes', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		const button = await openNavigationDrawer();
		fireEvent.click(screen.getByText(zh['nav.platformHome']).closest('button')!);
		await waitFor(() => expect(screen.getByText('page-sites')).toBeDefined());
		await expectDrawerClosed(button);
	}, 15000);

	it('cleans the mask and scroll lock when the viewport returns to desktop while open', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		await openNavigationDrawer();
		expect(document.querySelector('.ant-drawer-mask')).not.toBeNull();
		expect(bodyHasScrollLockStyle()).toBe(true);

		await act(async () => {
			switchViewport(false);
		});
		// The drawer portal is unmounted with the shell switch, the body scroll
		// lock style is removed, and the focus never lands on an unmounted node.
		expect(document.querySelector('.ant-drawer')).toBeNull();
		expect(bodyHasScrollLockStyle()).toBe(false);
		expect(document.activeElement === document.body || document.contains(document.activeElement)).toBe(
			true
		);
		expect(document.querySelector('.app-sider')).not.toBeNull();
	}, 15000);
});

describe('scopes and permissions in the drawer', () => {
	beforeEach(() => {
		useNarrowViewport(true);
		authenticated();
	});

	it('keeps site navigation, fixed actions, and the fixed-action route working', async () => {
		renderAt('/sites/abc/posts');
		expect(await screen.findByText('page-posts')).toBeDefined();
		const button = await openNavigationDrawer();
		const drawer = document.querySelector('.ant-drawer')!;
		expect(drawer.textContent).toContain(zh['nav.group.content']);
		expect(drawer.textContent).toContain(zh['nav.posts']);
		expect(drawer.textContent).not.toContain(zh['nav.themesCatalog']);
		expect(drawer.textContent).toContain(zh['nav.returnPlatform']);

		fireEvent.click(screen.getByText(zh['nav.returnPlatform']).closest('button')!);
		expect(await screen.findByText('page-sites')).toBeDefined();
		await expectDrawerClosed(button);
	}, 15000);

	it('filters admin-only entries out of the drawer for members', async () => {
		mockSession = { ...mockSession, user: { ...alice, platformRole: 'member' } };
		renderAt('/sites');
		await screen.findByText('page-sites');
		await openNavigationDrawer();
		expect(screen.queryByRole('menuitem', { name: zh['nav.users'] })).toBeNull();
		expect(screen.queryByRole('menuitem', { name: zh['nav.logs'] })).toBeNull();
		expect(screen.getByRole('menuitem', { name: zh['nav.appearance'] })).toBeDefined();
	}, 15000);
});
