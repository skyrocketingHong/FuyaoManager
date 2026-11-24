/**
 * PageBoundary tests: a page that throws while rendering is reported inside
 * the workspace — the sidebar, the top bar and the session stay mounted and
 * the router's full-screen error page never appears. The page slot retries in
 * place, keeps the failure while the retry throws again, and clears it when
 * navigation moves to another page. The users page marker throws on demand.
 * The antd chain is loaded after NODE_ENV is set to development (restored
 * afterwards), matching the other shell suites.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import zh from '../src/lib/i18n/locales/zh-CN.json';

declare const process: { env: { NODE_ENV: string | undefined } };

const originalNodeEnv = process.env.NODE_ENV;
process.env.NODE_ENV = 'development';

// The users page marker throws on demand; the marker below only reads it
// while rendering, after this module's top level has run.
const usersMarker = { fail: false };

const [{ App: AntApp, ConfigProvider }, { default: App }, { setLocale }] = await Promise.all([
	import('antd'),
	import('../src/App'),
	import('../src/lib/i18n')
]);
const { navItems } = await import('../src/lib/navigation');

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

// External-store snapshots keep one reference across calls.
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
vi.mock('../src/lib/api/client', () => ({
	currentAuthGeneration: () => 0,
	api: async () => ({ data: {}, etag: null }),
	ApiError: class ApiError extends Error {
		constructor(
			public status: number,
			public code: string
		) {
			super(code);
		}
	}
}));
vi.mock('../src/pages/UsersPage', () => ({
	default: function UsersMarker() {
		if (usersMarker.fail) throw new Error('users page failed to render');
		return <div>page-users</div>;
	}
}));
vi.mock('../src/pages/ThemesCatalogPage', () => ({ default: () => <div>page-themes</div> }));

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

function entryTitle(id: string) {
	const item = navItems.find((entry) => entry.id === id);
	if (!item) throw new Error(`unknown navigation item ${id}`);
	return zh[item.titleKey];
}

// antd inserts a space between two CJK button characters; compare without it.
function buttonByName(text: string) {
	return screen.getByRole('button', { name: (name) => name.replace(/\s/g, '') === text });
}

const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined);

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
	usersMarker.fail = false;
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
});

afterAll(() => {
	consoleError.mockRestore();
	process.env.NODE_ENV = originalNodeEnv;
});

describe('PageBoundary', () => {
	it('reports a failed page in the workspace and keeps the shell mounted', async () => {
		usersMarker.fail = true;
		renderAt('/users');
		const alert = await screen.findByRole('alert');
		expect(alert.textContent).toContain(zh['app.pageError']);
		// The shell — sidebar, top bar and navigation — stays mounted.
		expect(document.querySelector('.app-shell')).not.toBeNull();
		expect(document.querySelector('.app-sider')).not.toBeNull();
		expect(document.querySelector('.topbar')).not.toBeNull();
		expect(screen.getByRole('menuitem', { name: entryTitle('users') })).toBeDefined();
		// The router's full-screen error page is not used for a page fault.
		expect(document.querySelector('.auth-screen')).toBeNull();
		expect(screen.queryByText('page-users')).toBeNull();
		expect(consoleError).toHaveBeenCalledWith(
			'Page render failed',
			expect.objectContaining({ message: 'users page failed to render' }),
			expect.anything()
		);
	});

	it('retries the failed page in place and keeps the failure while it throws again', async () => {
		usersMarker.fail = true;
		renderAt('/users');
		await screen.findByRole('alert');

		fireEvent.click(buttonByName(zh['common.retry']));
		expect(await screen.findByRole('alert')).toBeDefined();
		expect(screen.queryByText('page-users')).toBeNull();

		usersMarker.fail = false;
		fireEvent.click(buttonByName(zh['common.retry']));
		expect(await screen.findByText('page-users')).toBeDefined();
		expect(screen.queryByRole('alert')).toBeNull();
		// The shell was never replaced during either attempt.
		expect(document.querySelector('.app-shell')).not.toBeNull();
	});

	it('clears the failure when navigation moves to another page', async () => {
		usersMarker.fail = true;
		renderAt('/users');
		await screen.findByRole('alert');

		fireEvent.click(screen.getByRole('menuitem', { name: entryTitle('themes') }));
		expect(await screen.findByText('page-themes')).toBeDefined();
		expect(screen.queryByRole('alert')).toBeNull();

		// Returning to the still failing page reports it again.
		usersMarker.fail = true;
		fireEvent.click(screen.getByRole('menuitem', { name: entryTitle('users') }));
		expect(await screen.findByRole('alert')).toBeDefined();
	});
});
