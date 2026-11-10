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
