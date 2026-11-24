/**
 * Desktop sidebar mode tests (robot-platform's 208 / 52 / hidden sidebar):
 * the mode control cycles expanded → icons → hidden → expanded, persists per
 * browser, moves between the sidebar brand row and the top bar with focus
 * following it, and the icon rail keeps every entry's accessible name plus a
 * native tooltip. The rail's flyout dismiss state follows Escape while the
 * rail is hovered or holds keyboard focus, and clears on pointer leave or
 * focus entering from outside. Narrow screens keep the drawer trigger only.
 * The antd chain is loaded after NODE_ENV is set to development (restored
 * afterwards), matching the shell suites: rc-util then selects real layout
 * effects as in production.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import zh from '../src/lib/i18n/locales/zh-CN.json';

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
const { navItems } = await import('../src/lib/navigation');
const { sidebarModeStorageKey } = await import('../src/components/layout/sidebarMode');

const alice = {
	id: '11111111-1111-1111-1111-111111111111',
	username: 'alice',
	displayName: 'Alice',
	platformRole: 'admin'
};
const siteAbc = {
	kind: 'theme' as const,
	siteId: 'abc',
	slug: 'abc',
	visibility: 'private' as const,
	provisionState: 'ready' as const,
	name: 'ABC Site',
	description: '',
	defaultLocale: 'zh-CN',
	role: 'owner' as const,
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

// External-store snapshots must keep one reference across calls, or
// useSyncExternalStore re-renders without end.
const sessionState = { status: 'authenticated', user: alice, platformInitialized: true };
const siteListState = { status: 'ready', sites: [siteAbc], errorKey: null };
vi.mock('../src/lib/session', () => ({
	subscribeSession: () => () => undefined,
	sessionSnapshot: () => sessionState,
	signInWithCode: vi.fn(),
	signInWithPasskey: vi.fn(),
	bindPasskey: vi.fn(),
	signOut: vi.fn(),
	loadSession: vi.fn()
}));
vi.mock('../src/lib/sites/site-list', () => ({
	subscribeSiteList: () => () => undefined,
	siteListSnapshot: () => siteListState,
	loadSiteList: vi.fn(),
	upsertSite: vi.fn()
}));
const apiRequest = vi.fn();
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
// Marker doubles keep the suite on the shell, not page internals.
vi.mock('../src/pages/SitesPage', () => ({ default: () => <div>page-sites</div> }));
vi.mock('../src/pages/content/posts/PostsPage', () => ({ default: () => <div>page-posts</div> }));

const originalMatchMedia = window.matchMedia;

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

function shell() {
	return document.querySelector<HTMLElement>('.app-shell');
}
function sider() {
	return document.querySelector<HTMLElement>('.app-sider');
}
function modeButton() {
	return document.querySelector<HTMLButtonElement>('.sidebar-mode-button');
}
function entryTitle(id: string) {
	const item = navItems.find((entry) => entry.id === id);
	if (!item) throw new Error(`unknown navigation item ${id}`);
	return zh[item.titleKey];
}
// The first platform menu entry with a group, so the rail shows an icon item
// under a group title.
const platformMenuEntry = navItems.find((item) => item.scope === 'platform' && item.menu !== false);

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
	apiRequest.mockResolvedValue({ data: {}, etag: null });
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	Object.defineProperty(window, 'matchMedia', {
		configurable: true,
		writable: true,
		value: originalMatchMedia
	});
});

afterAll(() => {
	process.env.NODE_ENV = originalNodeEnv;
});

describe('sidebar modes', () => {
	it('starts expanded with the control in the brand row and nothing persisted', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		expect(shell()?.dataset.sidebar).toBe('expanded');
		expect(sider()?.dataset.mode).toBe('expanded');
		expect(sider()?.style.flexBasis).toBe('208px');
		const button = screen.getByRole('button', { name: zh['app.collapseSidebar'] });
		expect(sider()?.querySelector('.sidebar-brand')?.contains(button)).toBe(true);
		// The expanded sidebar shows its labels: no tooltip duplicates them.
		expect(
			screen.getByRole('menuitem', { name: entryTitle(platformMenuEntry!.id) }).getAttribute('title')
		).toBeNull();
		expect(screen.getByRole('button', { name: zh['nav.platformHome'] }).getAttribute('title')).toBeNull();
		// The top bar's navigation slot stays empty while the sidebar is expanded.
		expect(document.querySelector('.topbar-navigation button')).toBeNull();
		expect(document.querySelector('.topbar-brand-mark')).toBeNull();
		expect(localStorage.getItem(sidebarModeStorageKey)).toBeNull();
	});

	it('collapses to the 52px icon rail, keeps names and tooltips, and shows the brand mark', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');
		// The rail is the expanded tree cut down to its width: the same
		// regions, in the same order, with the same entries. Only CSS decides
		// what shows, so no entry moves when the sidebar narrows.
		const regions = () =>
			[...sider()!.querySelectorAll('.sidebar-brand, .sidebar-location, .sidebar-actions, nav, .sidebar-foot')].map(
				(node) => node.className.split(' ')[0] || node.tagName.toLowerCase()
			);
		const entries = () =>
			[
				...sider()!.querySelectorAll<HTMLElement>('.sidebar-actions button, .ant-menu-item, .sidebar-foot a, .sidebar-foot code')
			].map((node) => node.getAttribute('aria-label') ?? node.textContent);
		const expandedRegions = regions();
		const expandedEntries = entries();
		expect(expandedRegions).toEqual(['sidebar-brand', 'sidebar-actions', 'nav', 'sidebar-foot']);

		fireEvent.click(screen.getByRole('button', { name: zh['app.collapseSidebar'] }));

		expect(shell()?.dataset.sidebar).toBe('icons');
		expect(sider()?.dataset.mode).toBe('icons');
		expect(sider()?.style.flexBasis).toBe('52px');
		expect(localStorage.getItem(sidebarModeStorageKey)).toBe('icons');
		expect(JSON.stringify(regions())).toBe(JSON.stringify(expandedRegions));
		expect(JSON.stringify(entries())).toBe(JSON.stringify(expandedEntries));
		// Group titles keep their text box in both modes.
		expect(sider()?.querySelector('.ant-menu-item-group-title .sidebar-group-text')?.textContent).toBeTruthy();
		// The control stays in the brand row and keeps keyboard focus.
		const hide = screen.getByRole('button', { name: zh['app.hideSidebar'] });
		expect(sider()?.querySelector('.sidebar-brand')?.contains(hide)).toBe(true);
		expect(document.activeElement).toBe(hide);

		// Labels are hidden by CSS, so each entry keeps its name and gains a tooltip.
		const entry = screen.getByRole('menuitem', { name: entryTitle(platformMenuEntry!.id) });
		expect(entry.getAttribute('title')).toBe(entryTitle(platformMenuEntry!.id));
		expect(sider()?.querySelector('.ant-menu-item-group-title')?.textContent).toBeTruthy();
		const home = screen.getByRole('button', { name: zh['nav.platformHome'] });
		expect(home.getAttribute('title')).toBe(zh['nav.platformHome']);
		expect(sider()?.querySelector('.sidebar-brand a')).not.toBeNull();

		// The rail shows icons only, so the top bar carries the brand mark.
		const mark = document.querySelector<HTMLImageElement>('.topbar-brand-mark img, img.topbar-brand-mark');
		expect(mark?.getAttribute('alt')).toBe(zh['app.title']);
		expect(document.querySelector('.topbar-navigation .sidebar-mode-button')).toBeNull();
	});

	it('hides the sidebar and restores it from the top bar, then persists each step', async () => {
		localStorage.setItem(sidebarModeStorageKey, 'icons');
		renderAt('/sites');
		await screen.findByText('page-sites');
		expect(sider()?.dataset.mode).toBe('icons');

		fireEvent.click(screen.getByRole('button', { name: zh['app.hideSidebar'] }));
		expect(shell()?.dataset.sidebar).toBe('hidden');
		expect(sider()).toBeNull();
		expect(localStorage.getItem(sidebarModeStorageKey)).toBe('hidden');
		const show = screen.getByRole('button', { name: zh['app.expandSidebar'] });
		expect(document.querySelector('.topbar-navigation')?.contains(show)).toBe(true);
		expect(document.activeElement).toBe(show);
		expect(document.querySelector('img.topbar-brand-mark')).toBeNull();
		// The page stays reachable and no drawer appears on the desktop.
		expect(screen.getByText('page-sites')).toBeDefined();
		expect(document.querySelector('.ant-drawer')).toBeNull();

		fireEvent.click(show);
		expect(shell()?.dataset.sidebar).toBe('expanded');
		expect(sider()?.dataset.mode).toBe('expanded');
		expect(localStorage.getItem(sidebarModeStorageKey)).toBe('expanded');
		const collapse = screen.getByRole('button', { name: zh['app.collapseSidebar'] });
		expect(document.activeElement).toBe(collapse);
		expect(modeButton()).toBe(collapse);
	});

	it('restores a hidden or collapsed sidebar and ignores unknown saved values', async () => {
		localStorage.setItem(sidebarModeStorageKey, 'hidden');
		const first = renderAt('/sites');
		await screen.findByText('page-sites');
		expect(shell()?.dataset.sidebar).toBe('hidden');
		expect(sider()).toBeNull();
		expect(screen.getByRole('button', { name: zh['app.expandSidebar'] })).toBeDefined();
		first.unmount();
		localStorage.clear();

		localStorage.setItem(sidebarModeStorageKey, 'not-a-mode');
		renderAt('/sites');
		await screen.findByText('page-sites');
		expect(shell()?.dataset.sidebar).toBe('expanded');
	});

	it('dismisses the rail flyout on Escape while hovered or focused and re-enables it afterwards', async () => {
		localStorage.setItem(sidebarModeStorageKey, 'icons');
		renderAt('/sites');
		await screen.findByText('page-sites');
		const rail = sider();
		if (!rail) throw new Error('icon rail missing');
		const panel = rail.querySelector<HTMLElement>('.sidebar-panel');
		if (!panel) throw new Error('sidebar panel missing');

		// CSS decides whether the rail is hovered or holds keyboard focus; the
		// double stands in for jsdom, which does not evaluate those selectors.
		const matches = Element.prototype.matches;
		let peeking = false;
		vi.spyOn(Element.prototype, 'matches').mockImplementation(function (
			this: Element,
			selector: string
		) {
			return selector.includes(':hover') ? peeking : matches.call(this, selector);
		});

		// Neither hovered nor focused: Escape leaves the rail available.
		fireEvent.keyDown(window, { key: 'Escape' });
		expect(rail.hasAttribute('data-peek-dismissed')).toBe(false);

		// A dialog or menu that already handled Escape keeps the rail as it is:
		// a capturing window listener stands in for that earlier handler.
		peeking = true;
		const handled = (event: Event) => event.preventDefault();
		window.addEventListener('keydown', handled, true);
		fireEvent.keyDown(document.body, { key: 'Escape' });
		window.removeEventListener('keydown', handled, true);
		expect(rail.hasAttribute('data-peek-dismissed')).toBe(false);

		peeking = true;
		const control = modeButton();
		if (!control) throw new Error('mode control missing');
		control.focus();
		fireEvent.keyDown(window, { key: 'Escape' });
		expect(rail.dataset.peekDismissed).toBe('true');
		// Escape does not change the mode or move focus.
		expect(shell()?.dataset.sidebar).toBe('icons');
		expect(document.activeElement).toBe(control);

		// Leaving with the pointer makes the flyout available again.
		fireEvent.mouseLeave(rail);
		expect(rail.hasAttribute('data-peek-dismissed')).toBe(false);

		fireEvent.keyDown(window, { key: 'Escape' });
		expect(rail.dataset.peekDismissed).toBe('true');
		// Focus entering from outside the panel does too.
		fireEvent.focus(panel, { relatedTarget: document.body });
		expect(rail.hasAttribute('data-peek-dismissed')).toBe(false);

		fireEvent.keyDown(window, { key: 'Escape' });
		expect(rail.dataset.peekDismissed).toBe('true');
		// Focus moving between controls inside the panel keeps the dismissal.
		const entry = screen.getByRole('menuitem', { name: entryTitle(platformMenuEntry!.id) });
		fireEvent.focus(entry, { relatedTarget: control });
		expect(rail.dataset.peekDismissed).toBe('true');

		// Changing the mode leaves the rail; a later collapse starts with its
		// flyout available again (the rail host is remounted, so no dismissed
		// state survives).
		peeking = false;
		fireEvent.click(modeButton()!);
		expect(shell()?.dataset.sidebar).toBe('hidden');
		expect(sider()).toBeNull();
		fireEvent.click(screen.getByRole('button', { name: zh['app.expandSidebar'] }));
		fireEvent.click(screen.getByRole('button', { name: zh['app.collapseSidebar'] }));
		expect(sider()?.dataset.mode).toBe('icons');
		expect(sider()?.hasAttribute('data-peek-dismissed')).toBe(false);
	});

	it('offers only the drawer trigger on narrow screens, whatever the saved mode', async () => {
		localStorage.setItem(sidebarModeStorageKey, 'hidden');
		Object.defineProperty(window, 'matchMedia', {
			configurable: true,
			writable: true,
			value: (query: string) => ({
				matches: query === NARROW_MEDIA_QUERY,
				media: query,
				onchange: null,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				addListener: () => undefined,
				removeListener: () => undefined,
				dispatchEvent: () => false
			})
		});
		renderAt('/sites');
		await screen.findByText('page-sites');
		expect(sider()).toBeNull();
		expect(screen.getByRole('button', { name: zh['app.openNavigation'] })).toBeDefined();
		expect(modeButton()).toBeNull();
		expect(document.querySelector('img.topbar-brand-mark')).toBeNull();
		// The saved desktop mode is kept for the next desktop viewport.
		expect(localStorage.getItem(sidebarModeStorageKey)).toBe('hidden');
	});
});
