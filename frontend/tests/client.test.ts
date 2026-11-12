import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { api, ApiError, setCsrfToken } from '../src/lib/api/client';

function jsonResponse(body: unknown, init?: ResponseInit & { etag?: string }): Response {
	const headers = new Headers({ 'Content-Type': 'application/json' });
	if (init?.etag) headers.set('ETag', init.etag);
	const { etag: _etag, ...rest } = init ?? {};
	return new Response(body === undefined ? null : JSON.stringify(body), { ...rest, headers });
}

const fetchMock = vi.fn();

beforeEach(() => {
	fetchMock.mockReset();
	vi.stubGlobal('fetch', fetchMock);
	setCsrfToken(null);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('api client', () => {
	it('parses successful JSON responses and surfaces the ETag', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ sites: [] }, { etag: '"x-r1"' }));
		const result = await api<{ sites: unknown[] }>('GET', '/api/v1/sites');
		expect(result.data.sites).toEqual([]);
		expect(result.etag).toBe('"x-r1"');
	});

	it('sends CSRF token and If-Match on unsafe requests', async () => {
		setCsrfToken('csrf-1');
		fetchMock.mockResolvedValueOnce(jsonResponse({ name: 'n' }, { etag: '"x-r2"' }));
		await api('PATCH', '/api/v1/sites/abc/settings', { body: { name: 'n' }, etag: '"x-r1"' });

		const [path, init] = fetchMock.mock.calls[0] as [string, RequestInit];
		expect(path).toBe('/api/v1/sites/abc/settings');
		expect(init.method).toBe('PATCH');
		const headers = init.headers as Record<string, string>;
		expect(headers['X-CSRF-Token']).toBe('csrf-1');
		expect(headers['If-Match']).toBe('"x-r1"');
		expect(headers['Content-Type']).toBe('application/json');
	});

	it('keeps CSRF off GET requests', async () => {
		setCsrfToken('csrf-2');
		fetchMock.mockResolvedValueOnce(jsonResponse({ authenticated: false }));
		await api('GET', '/api/v1/session');
		const headers = (fetchMock.mock.calls[0] as [string, RequestInit])[1].headers as Record<
			string,
			string
		>;
		expect(headers['X-CSRF-Token']).toBeUndefined();
	});

	it('normalizes problem bodies into ApiError and keeps the response ETag', async () => {
		fetchMock.mockResolvedValueOnce(
			jsonResponse(
				{
					type: 'about:blank',
					title: 'Precondition Failed',
					status: 412,
					code: 'precondition_failed',
					detail: 'stale'
				},
				{ status: 412, etag: '"x-r9"' }
			)
		);
		const err = await api('PATCH', '/api/v1/sites/abc/settings', {
			body: {},
			etag: '"x-r1"'
		}).catch((e) => e);
		expect(err).toBeInstanceOf(ApiError);
		expect((err as ApiError).status).toBe(412);
		expect((err as ApiError).code).toBe('precondition_failed');
		expect((err as ApiError).detail).toBe('stale');
		expect((err as ApiError).etag).toBe('"x-r9"');
	});

	it('falls back to a generic code for non-JSON error bodies', async () => {
		fetchMock.mockResolvedValueOnce(new Response('gateway panic', { status: 502 }));
		const err = await api('GET', '/api/v1/sites').catch((e) => e);
		expect(err).toBeInstanceOf(ApiError);
		expect((err as ApiError).status).toBe(502);
		expect((err as ApiError).code).toBe('internal_error');
	});

	it('returns undefined data for 204 responses', async () => {
		fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
		const result = await api<void>('POST', '/api/v1/auth/logout', { body: {} });
		expect(result.data).toBeUndefined();
	});
});

describe('unified business-401 handling', () => {
	it('invokes the registered handler for a business 401 at the same generation and still throws', async () => {
		const client = await import('../src/lib/api/client');
		const handler = vi.fn();
		client.setUnauthorizedHandler(handler);
		fetchMock.mockResolvedValueOnce(jsonResponse({ code: 'unauthenticated' }, { status: 401 }));
		await expect(api('GET', '/api/v1/sites')).rejects.toBeInstanceOf(ApiError);
		expect(handler).toHaveBeenCalledTimes(1);

		client.setUnauthorizedHandler(null);
	});

	it('never invokes the handler for ceremony 401s', async () => {
		const client = await import('../src/lib/api/client');
		const handler = vi.fn();
		client.setUnauthorizedHandler(handler);
		fetchMock.mockResolvedValueOnce(
			jsonResponse({ code: 'challenge_invalid' }, { status: 401 })
		);
		await expect(api('POST', '/api/v1/auth/login/verify', { body: {} })).rejects.toBeInstanceOf(
			ApiError
		);
		expect(handler).not.toHaveBeenCalled();

		client.setUnauthorizedHandler(null);
	});

	it('never invokes the handler when a newer session migration landed first', async () => {
		const client = await import('../src/lib/api/client');
		const handler = vi.fn();
		client.setUnauthorizedHandler(handler);
		// The request starts, then a login/sign-out/expiry bumps the
		// generation; the late 401 describes a replaced session.
		const pending = api('GET', '/api/v1/sites').catch((e) => e);
		client.bumpAuthGeneration();
		fetchMock.mockResolvedValueOnce(jsonResponse({ code: 'unauthenticated' }, { status: 401 }));
		await pending;
		expect(handler).not.toHaveBeenCalled();

		client.setUnauthorizedHandler(null);
	});

	it('never invokes the handler for a network failure', async () => {
		const client = await import('../src/lib/api/client');
		const handler = vi.fn();
		client.setUnauthorizedHandler(handler);
		fetchMock.mockRejectedValueOnce(new TypeError('fetch failed'));
		await expect(api('GET', '/api/v1/sites')).rejects.toBeInstanceOf(TypeError);
		expect(handler).not.toHaveBeenCalled();

		client.setUnauthorizedHandler(null);
	});
});

describe('business-401 session integration', () => {
	it('expires an authenticated session so routing can reach the login screen', async () => {
		const client = await import('../src/lib/api/client');
		const session = await import('../src/lib/session');
		fetchMock.mockResolvedValueOnce(
			jsonResponse({
				authenticated: true,
				user: { id: 'u1', username: 'alice', displayName: 'Alice' },
				sessionScope: 'full',
				platformInitialized: true,
				passkeyBound: true,
				passkeyAvailable: true
			})
		);
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('authenticated');

		fetchMock.mockResolvedValueOnce(jsonResponse({ code: 'unauthenticated' }, { status: 401 }));
		await expect(api('GET', '/api/v1/sites')).rejects.toBeInstanceOf(ApiError);
		expect(session.sessionSnapshot().status).toBe('anonymous');
		expect(session.sessionSnapshot().user).toBeNull();
		expect(client.currentCsrfToken()).toBeNull();
	});
});

describe('session CSRF writes behind the generation guard', () => {
	// These tests import the real management wrappers over the real client —
	// only fetch is stubbed — so the CSRF side effects are observed exactly
	// where they happen, not through mocks of the wrapper layer.
	function sessionBody(csrfToken: string | null): unknown {
		return {
			authenticated: true,
			user: { id: 'u1', username: 'alice', displayName: 'Alice' },
			sessionScope: 'full',
			platformInitialized: true,
			passkeyBound: true,
			passkeyAvailable: true,
			csrfToken
		};
	}

	function ceremonyBody(csrfToken: string): unknown {
		return {
			user: { id: 'u1', username: 'alice', displayName: 'Alice' },
			csrfToken,
			sessionScope: 'full',
			passkeyBound: true
		};
	}

	it('a successful session load installs its CSRF token', async () => {
		const client = await import('../src/lib/api/client');
		const session = await import('../src/lib/session');
		fetchMock.mockImplementation((input: RequestInfo | URL) => {
			const path = String(input);
			if (path === '/api/v1/session') {
				return Promise.resolve(jsonResponse(sessionBody('csrf-load')));
			}
			return Promise.reject(new Error(`unexpected fetch ${path}`));
		});
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('authenticated');
		expect(client.currentCsrfToken()).toBe('csrf-load');
	});

	it('a stale session load never erases the CSRF token of a newer login', async () => {
		const client = await import('../src/lib/api/client');
		const management = await import('../src/lib/api/management');
		const session = await import('../src/lib/session');
		let resolveSessionGet!: (v: Response) => void;
		fetchMock.mockImplementation((input: RequestInfo | URL) => {
			const path = String(input);
			if (path === '/api/v1/session') {
				return new Promise<Response>((resolve) => {
					resolveSessionGet = resolve;
				});
			}
			if (path === '/api/v1/auth/exchange') {
				return Promise.resolve(jsonResponse(ceremonyBody('csrf-new')));
			}
			return Promise.reject(new Error(`unexpected fetch ${path}`));
		});

		// The stale read starts first and hangs on the wire.
		const stale = session.loadSession();

		// A fresh login completes and installs the new token.
		await management.exchangeAuthCode('code-1');
		await session.signInWithCode('code-1');
		expect(session.sessionSnapshot().status).toBe('authenticated');
		expect(client.currentCsrfToken()).toBe('csrf-new');

		// The stale read lands carrying its own CSRF token; the store drops
		// the whole result, so both the session and the token survive.
		resolveSessionGet(jsonResponse(sessionBody('csrf-stale')));
		await stale;
		expect(session.sessionSnapshot().status).toBe('authenticated');
		expect(client.currentCsrfToken()).toBe('csrf-new');
	});

	it('a logout finishing after a newer ceremony never clears the fresh CSRF token', async () => {
		let resolveLogout!: (v: Response) => void;
		const client = await import('../src/lib/api/client');
		const session = await import('../src/lib/session');
		fetchMock.mockImplementation((input: RequestInfo | URL) => {
			const path = String(input);
			if (path === '/api/v1/auth/exchange') {
				return Promise.resolve(jsonResponse(ceremonyBody('csrf-new')));
			}
			if (path === '/api/v1/auth/logout') {
				return new Promise<Response>((resolve) => {
					resolveLogout = resolve;
				});
			}
			return Promise.reject(new Error(`unexpected fetch ${path}`));
		});

		// The first login migrates the store; the sign-out starts afterwards.
		await session.signInWithCode('code-1');
		expect(session.sessionSnapshot().status).toBe('authenticated');
		const pending = session.signOut();
		// A second login races past the sign-out and installs a newer token.
		await session.signInWithCode('code-2');
		resolveLogout(new Response(null, { status: 204 }));
		await pending;

		expect(session.sessionSnapshot().status).toBe('authenticated');
		expect(session.sessionSnapshot().user?.username).toBe('alice');
		expect(client.currentCsrfToken()).toBe('csrf-new');
	});
});
