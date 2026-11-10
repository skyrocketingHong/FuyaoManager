/**
 * Thin fetch wrapper for the management API. Requests are same-origin (the
 * dev server proxies /api to the loopback backend), carry the session cookie,
 * and attach the stable CSRF token on unsafe methods. Errors are normalized
 * into {@link ApiError}; the ETag of every response is surfaced for
 * optimistic concurrency.
 */

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		public detail: string,
		public etag: string | null
	) {
		super(`${status} ${code}${detail ? `: ${detail}` : ''}`);
		this.name = 'ApiError';
	}
}

export interface ApiResult<T> {
	data: T;
	etag: string | null;
}

let csrfToken: string | null = null;

/** Stores the session's stable CSRF token (from ceremony responses or GET /api/v1/session). */
export function setCsrfToken(token: string | null): void {
	csrfToken = token;
}

export function currentCsrfToken(): string | null {
	return csrfToken;
}

interface RequestOptions {
	body?: unknown;
	/** Exact ETag from a prior GET, sent as If-Match. */
	etag?: string;
}

export async function api<T>(
	method: string,
	path: string,
	opts: RequestOptions = {}
): Promise<ApiResult<T>> {
	const headers: Record<string, string> = {};
	if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
	if (opts.etag !== undefined) headers['If-Match'] = opts.etag;
	if (csrfToken !== null && method !== 'GET' && method !== 'HEAD') {
		headers['X-CSRF-Token'] = csrfToken;
	}

	const res = await fetch(path, {
		method,
		headers,
		credentials: 'same-origin',
		body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined
	});
	const etag = res.headers.get('ETag');

	if (!res.ok) {
		let code = 'internal_error';
		let detail = '';
		try {
			const problem = (await res.json()) as { code?: string; detail?: string };
			if (problem.code) code = problem.code;
			if (problem.detail) detail = problem.detail;
		} catch {
			// Non-JSON error body; keep the defaults.
		}
		throw new ApiError(res.status, code, detail, etag);
	}
	if (res.status === 204) {
		return { data: undefined as T, etag };
	}
	return { data: (await res.json()) as T, etag };
}
