/**
 * Sign-out and the draft gate: the account menu asks the unsaved-changes
 * question before the session is cleared. Cancelling keeps the session and
 * the draft untouched (before this gate, sign-out ran first and the
 * confirmation only appeared once the session was already gone); confirming
 * signs out, and the router navigation that follows passes the page's
 * blocker once instead of asking a second time. A clean page signs out
 * without a confirmation. The sites marker runs the real useUnsavedGuard
 * over a draft input; the session double notifies its subscribers the way
 * the real store does. The antd chain is loaded after NODE_ENV is set to
 * development (restored afterwards), matching the other shell suites.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterAll, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import zh from '../src/lib/i18n/locales/zh-CN.json';

declare const process: { env: { NODE_ENV: string | undefined } };

const originalNodeEnv = process.env.NODE_ENV;
process.env.NODE_ENV = 'development';

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
type SessionState = {
	status: string;
	user: typeof alice | null;
	platformInitialized: boolean;
	passkeyBound: boolean;
	passkeyAvailable: boolean;
};
const sessionListeners = new Set<() => void>();
let sessionState: SessionState;
const signOutMock = vi.fn();
const siteListState = { status: 'ready', sites: [siteAbc], errorKey: null };

vi.mock('../src/lib/session', () => ({
	subscribeSession: (listener: () => void) => {
		sessionListeners.add(listener);
		return () => {
			sessionListeners.delete(listener);
		};
	},
	sessionSnapshot: () => sessionState,
	signInWithCode: vi.fn(),
	signInWithPasskey: vi.fn(),
	bindPasskey: vi.fn(),
	signOut: (...args: unknown[]) => signOutMock(...args),
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
vi.mock('../src/lib/api/management', () => ({ getSite: vi.fn() }));
// The sites marker runs the real useUnsavedGuard over a draft input.
vi.mock('../src/pages/SitesPage', async () => {
	const { useState } = await import('react');
	const { useUnsavedGuard } = await import('../src/lib/editing/useUnsavedGuard');
	return {
		default: function SitesMarker() {
			const [draft, setDraft] = useState('initial');
			useUnsavedGuard(draft !== 'initial');
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
vi.mock('../src/pages/LoginPage', () => ({ default: () => <div>page-login</div> }));

const [{ App: AntApp, ConfigProvider }, { default: App }, { setLocale }] = await Promise.all([
	import('antd'),
	import('../src/App'),
	import('../src/lib/i18n')
]);

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

// antd inserts a space between two CJK button characters; compare without it.
function buttonByName(text: string) {
	return screen.getByRole('button', { name: (name) => name.replace(/\s/g, '') === text });
}

function confirmation() {
	return document.querySelector('.ant-modal-confirm');
}

async function chooseSignOut() {
	fireEvent.click(screen.getByRole('button', { name: 'alice' }));
	const item = await screen.findByRole('menuitem', {
		name: (name) => name.replace(/\s/g, '') === zh['app.signOut']
	});
	fireEvent.click(item);
}

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
	sessionListeners.clear();
	sessionState = {
		status: 'authenticated',
		user: alice,
		platformInitialized: true,
		passkeyBound: true,
		passkeyAvailable: true
	};
	// The real store clears the local session and notifies its subscribers.
	signOutMock.mockImplementation(async () => {
		sessionState = { ...sessionState, status: 'anonymous', user: null };
		for (const listener of [...sessionListeners]) listener();
	});
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
	process.env.NODE_ENV = originalNodeEnv;
});

describe('sign-out and the draft gate', () => {
	it('asks before clearing the session and keeps everything when the operator stays', async () => {
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });

		await chooseSignOut();
		await waitFor(() => expect(confirmation()).not.toBeNull());
		expect(document.querySelector('.ant-modal-confirm-title')?.textContent).toBe(
			zh['draft.leaveTitle']
		);
		// The question comes first: nothing has been signed out yet.
		expect(signOutMock).not.toHaveBeenCalled();

		fireEvent.click(buttonByName(zh['draft.leaveCancel']));
		await waitFor(() => expect(confirmation()).toBeNull());
		expect(signOutMock).not.toHaveBeenCalled();
		expect(screen.getByText('page-sites')).toBeDefined();
		expect((screen.getByRole('textbox', { name: 'draft-value' }) as HTMLInputElement).value).toBe(
			'unsaved'
		);
	});

	it('signs out once confirmed, and the page blocker passes the navigation without asking twice', async () => {
		renderAt('/sites');
		const input = (await screen.findByRole('textbox', { name: 'draft-value' })) as HTMLInputElement;
		fireEvent.change(input, { target: { value: 'unsaved' } });

		await chooseSignOut();
		await waitFor(() => expect(confirmation()).not.toBeNull());
		fireEvent.click(buttonByName(zh['draft.leaveConfirm']));

		await waitFor(() => expect(signOutMock).toHaveBeenCalledTimes(1));
		expect(await screen.findByText('page-login')).toBeDefined();
		expect(screen.queryByText('page-sites')).toBeNull();
		// The confirmed consent let the router navigation through; the page is
		// gone and no second confirmation is open.
		await waitFor(() => expect(confirmation()).toBeNull());
	});

	it('signs out a clean page without a confirmation', async () => {
		renderAt('/sites');
		await screen.findByText('page-sites');

		await chooseSignOut();
		await waitFor(() => expect(signOutMock).toHaveBeenCalledTimes(1));
		expect(await screen.findByText('page-login')).toBeDefined();
		expect(confirmation()).toBeNull();
	});
});
