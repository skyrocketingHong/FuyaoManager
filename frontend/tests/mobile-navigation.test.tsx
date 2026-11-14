/**
 * Narrow-screen navigation drawer tests: a controllable matchMedia double
 * drives the 991px breakpoint with the real antd Drawer in play, and the
 * real useUnsavedGuard (data-router blocker + AntApp modal) protects the
 * marker page's draft. Covers the Sider/Drawer switch, native close paths
 * with focus restoration across consecutive rounds, blocked/cancelled/
 * confirmed navigation against the drawer, current-page entries closing
 * without a leave confirmation, brand modifier-key passthrough, mask/
 * scroll-lock cleanup on the desktop switch, and scope/permission parity
 * with the desktop sidebar. The suite disables antd motion locally
 * (token.motion → rc-motion) so close callbacks are deterministic in jsdom,
 * and initializes NODE_ENV to development before the antd chain first loads
 * (restored afterwards) so rc-util's module-init selection uses real layout
 * effects like production; the matchMedia replacement is installed per test
 * and restored afterwards so other suites keep the shared stub.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import zh from '../src/lib/i18n/locales/zh-CN.json';

// rc-util chooses React.useLayoutEffect vs React.useEffect at module init
// from NODE_ENV ('test' degrades to passive effects, which reorders the
// drawer's opener capture after its autofocus). Set development BEFORE the
// antd chain is first loaded — static imports would evaluate too early, so
// the antd-backed modules are imported dynamically here. No module resets
// (no second React instance) and nothing outside this file is affected.
// The frontend tsconfig has no node types; vitest runs this file with a
// real process global.
declare const process: { env: { NODE_ENV: string | undefined } };

const originalNodeEnv = process.env.NODE_ENV;
process.env.NODE_ENV = 'development';
const [{ App: AntApp, ConfigProvider }, { default: App }, { NARROW_MEDIA_QUERY }, { setLocale }] =
	await Promise.all([
		import('antd'),
		import('../src/App'),
		import('../src/components/layout/AppShell'),
		import('../src/lib/i18n')
	]);

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

// The sites marker runs the real useUnsavedGuard over a draft input and
// publishes real top-bar slot content, so blocked navigation and breakpoint
// switches exercise the production protection instead of a stand-in.
vi.mock('../src/pages/SitesPage', async () => {
	const { useState } = await import('react');
	const { useUnsavedGuard } = await import('../src/lib/editing/useUnsavedGuard');
	const { usePageHeader } = await import('../src/components/PageHeader');
	return {
		default: function SitesMarker() {
			const [draft, setDraft] = useState('initial');
			const dirty = draft !== 'initial';
			useUnsavedGuard(dirty);
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
// The posts marker carries the real guard too, so the brand link's
// cross-page navigation (site page → platform home) can be blocked.
vi.mock('../src/pages/sites/PostsPage', async () => {
	const { useState } = await import('react');
	const { useUnsavedGuard } = await import('../src/lib/editing/useUnsavedGuard');
	return {
		default: function PostsMarker() {
			const [draft, setDraft] = useState('initial');
			useUnsavedGuard(draft !== 'initial');
			return (
				<div>
					<span>page-posts</span>
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

// --- drawer helpers -----------------------------------------------------------
// With token.motion: false the rc-motion steps complete synchronously, so
// the observable close state (and rc-drawer's native focus restoration) is
// deterministic. The portal root stays mounted after a normal close
// (rc-drawer autoDestroy=false): a closed drawer is the
// open-class/mask/wrapper-hidden state, not a removed node.
async function openNavigationDrawer() {
	const button = screen.getByRole('button', { name: zh['app.openNavigation'] });
	button.focus();
	fireEvent.click(button);
	await waitFor(() => expect(document.querySelector('.ant-drawer-open')).not.toBeNull());
	// The drawer's autofocus has moved focus into the real drawer surface.
	await waitFor(() =>
		expect(document.querySelector('.ant-drawer')?.contains(document.activeElement)).toBe(true)
	);
	expect(button.getAttribute('aria-expanded')).toBe('true');
	return button;
}

async function expectDrawerClosed(button?: HTMLElement) {
	await waitFor(() => {
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
	});
}

function bodyHasScrollLockStyle() {
	return Array.from(document.querySelectorAll('style')).some((tag) =>
		tag.textContent?.includes('overflow-y: hidden')
	);
}

// antd inserts a space between two CJK button characters; compare without
// whitespace.
function buttonByName(text: string) {
	return screen.getByRole('button', { name: (name) => name.replace(/\s/g, '') === text });
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
		<ConfigProvider theme={{ token: { motion: false } }}>
			<AntApp>
				<RouterProvider router={router} />
			</AntApp>
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

afterAll(() => {
	process.env.NODE_ENV = originalNodeEnv;
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
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(screen.getByText('marker-slot-content')).toBeDefined();
		expect(screen.getByText('marker-slot-action')).toBeDefined();

		await act(async () => {
			switchViewport(true);
		});
		// The drawer is closed by default and the page state survives again.
		expect(document.querySelector('.ant-drawer')).toBeNull();
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(screen.getByText('marker-slot-content')).toBeDefined();
	});
});

describe('native close paths and focus', () => {
	beforeEach(() => {
		useNarrowViewport(true);
		authenticated();
	});

	it('restores focus to the menu button across consecutive open-close rounds', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');

		// Round 1: Escape.
		const button = await openNavigationDrawer();
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
		fireEvent.keyDown(window, { key: 'Escape' });
		await expectDrawerClosed(button);

		// Round 2: native close button, straight after the first cycle.
		const buttonAgain = await openNavigationDrawer();
		fireEvent.click(document.querySelector('.ant-drawer-close')!);
		await expectDrawerClosed(buttonAgain);

		// Round 3: mask click.
		const buttonThird = await openNavigationDrawer();
		fireEvent.click(document.querySelector('.ant-drawer-mask')!);
		await expectDrawerClosed(buttonThird);
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

describe('unsaved-changes navigation', () => {
	beforeEach(() => {
		useNarrowViewport(true);
		authenticated();
	});

	async function openWithDirtyDraft() {
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		await openNavigationDrawer();
		return input;
	}

	it('keeps the drawer and draft intact when navigation is blocked, then closes after confirming', async () => {
		const input = await openWithDirtyDraft();
		const button = screen.getByRole('button', { name: zh['app.openNavigation'] });

		// Cross-page menu click with a dirty page: the blocker dialog appears
		// while the drawer stays open, and nothing steals the dialog's focus.
		fireEvent.click(screen.getByRole('menuitem', { name: zh['nav.themesCatalog'] }));
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).not.toBeNull());
		expect(document.querySelector('.ant-modal-confirm-title')?.textContent).toBe(
			zh['draft.leaveTitle']
		);
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect(
			(screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value
		).toBe('unsaved');
		// The drawer stays open behind the dialog and nothing steals the
		// dialog's focus.
		expect(document.querySelector('.ant-modal')?.contains(document.activeElement)).toBe(true);

		// Cancel: route, draft, and drawer state all survive.
		fireEvent.click(buttonByName(zh['draft.leaveCancel']));
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).toBeNull());
		expect(screen.getByText('page-sites')).toBeDefined();
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect(input.isConnected).toBe(true);

		// Confirm: the route changes, the drawer closes, focus returns.
		fireEvent.click(screen.getByRole('menuitem', { name: zh['nav.themesCatalog'] }));
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).not.toBeNull());
		fireEvent.click(buttonByName(zh['draft.leaveConfirm']));
		expect(await screen.findByText('page-themes')).toBeDefined();
		await expectDrawerClosed(button);
	}, 15000);

	it('closes the current-page entry directly without a leave confirmation', async () => {
		await openWithDirtyDraft();
		const button = screen.getByRole('button', { name: zh['app.openNavigation'] });

		fireEvent.click(screen.getByText(zh['nav.platformHome']).closest('button')!);
		await expectDrawerClosed(button);
		// No navigation attempt, so the leave confirmation never appears and
		// the draft survives.
		expect(screen.queryByText(zh['draft.leaveTitle'])).toBeNull();
		expect(screen.getByText('page-sites')).toBeDefined();
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
	}, 15000);

	it('keeps drawer state on a blocked fixed-action navigation from a dirty site page', async () => {
		renderAt('/sites/abc/posts');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		const button = await openNavigationDrawer();

		// The fixed action targets the platform home; the dirty page blocks it.
		fireEvent.click(screen.getByText(zh['nav.returnPlatform']).closest('button')!);
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).not.toBeNull());
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(document.querySelector('.ant-modal')?.contains(document.activeElement)).toBe(true);

		// Cancel: route, draft, and drawer state all survive.
		fireEvent.click(buttonByName(zh['draft.leaveCancel']));
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).toBeNull());
		expect(screen.getByText('page-posts')).toBeDefined();
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect(input.isConnected).toBe(true);

		// Confirm: the route changes, the drawer closes, focus returns.
		fireEvent.click(screen.getByText(zh['nav.returnPlatform']).closest('button')!);
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).not.toBeNull());
		fireEvent.click(buttonByName(zh['draft.leaveConfirm']));
		expect(await screen.findByText('page-sites')).toBeDefined();
		await expectDrawerClosed(button);
		expect(input.isConnected).toBe(false);
	}, 15000);

	it('passes modifier-key brand clicks to the browser and closes on the current-page plain click', async () => {
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		const button = await openNavigationDrawer();
		const link = document.querySelector('.sidebar-brand a')!;

		// Ctrl-click on the current page: the event stays uncancelled for the
		// browser (new tab), the drawer stays open, nothing navigates.
		let prevented: boolean | null = null;
		const record = (event: MouseEvent) => {
			prevented = event.defaultPrevented;
		};
		document.body.addEventListener('click', record);
		fireEvent.click(link, { ctrlKey: true });
		document.body.removeEventListener('click', record);
		expect(prevented).toBe(false);
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
		expect(screen.queryByText(zh['draft.leaveTitle'])).toBeNull();

		// Plain click on the current page: closed directly, no leave dialog.
		fireEvent.click(link);
		await expectDrawerClosed(button);
		expect(screen.getByText('page-sites')).toBeDefined();
		expect(screen.queryByText(zh['draft.leaveTitle'])).toBeNull();
		expect(
			(screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value
		).toBe('unsaved');
	}, 15000);

	it('closes only after the route changes when the brand link navigates away', async () => {
		// The brand link always targets the platform home; a dirty site page
		// makes the click a blocked cross-page navigation.
		renderAt('/sites/abc/posts');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });
		const button = await openNavigationDrawer();

		fireEvent.click(document.querySelector('.sidebar-brand a')!);
		await waitFor(() => expect(document.querySelector('.ant-modal-confirm')).not.toBeNull());
		expect(document.querySelector('.ant-drawer')?.classList.contains('ant-drawer-open')).toBe(true);
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);

		fireEvent.click(buttonByName(zh['draft.leaveConfirm']));
		expect(await screen.findByText('page-sites')).toBeDefined();
		await expectDrawerClosed(button);
		expect(input.isConnected).toBe(false);
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
