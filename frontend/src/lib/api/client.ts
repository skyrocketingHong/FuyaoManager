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
		public etag: string | null,
		/** Parsed body of a non-problem error response (for example the failed
		 * release record a publish 422 carries as application/json). */
		public body?: unknown
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

// Auth generation: bumped by the session store on every state migration
// (load, ceremony, sign-out, expiry). A request captures the generation at
// its start; a 401 that lands after a newer migration is a stale answer
// about an already-replaced session and must not expire the fresh one.
let authGeneration = 0;
let unauthorizedHandler: (() => void) | null = null;

export function bumpAuthGeneration(): void {
	authGeneration++;
}

export function currentAuthGeneration(): number {
	return authGeneration;
}

/** Registers the handler the client calls for business-request 401s: the
 * session store clears the stale session and CSRF token there, so routing
 * can reach the login screen without the authenticated guard bouncing back.
 * Ceremony 401s and network failures never invoke it, and the client layer
 * stays free of session imports (no dependency cycle). */
export function setUnauthorizedHandler(handler: (() => void) | null): void {
	unauthorizedHandler = handler;
}

interface RequestOptions {
	body?: unknown;
	/** Exact ETag from a prior GET, sent as If-Match. */
	etag?: string;
	/** Send the body as multipart/form-data (fetch sets the boundary). */
	multipart?: boolean;
}

export async function api<T>(
	method: string,
	path: string,
	opts: RequestOptions = {}
): Promise<ApiResult<T>> {
	const myGen = currentAuthGeneration();
	const headers: Record<string, string> = {};
	if (opts.body !== undefined && !opts.multipart) headers['Content-Type'] = 'application/json';
	if (opts.etag !== undefined) headers['If-Match'] = opts.etag;
	if (csrfToken !== null && method !== 'GET' && method !== 'HEAD') {
		headers['X-CSRF-Token'] = csrfToken;
	}

	const res = await fetch(path, {
		method,
		headers,
		credentials: 'same-origin',
		body:
			opts.body !== undefined
				? opts.multipart
					? (opts.body as FormData)
					: JSON.stringify(opts.body)
				: undefined
	});
	const etag = res.headers.get('ETag');

	if (!res.ok) {
		// A business-request 401 means the session cookie no longer backs this
		// request: clear the stale session so routing reaches the login screen
		// instead of bouncing off the authenticated guard. Ceremony 401s
		// (/api/v1/auth/*) are ordinary failures of the rite itself, and a 401
		// that lands after a newer session migration describes a session that
		// no longer exists — neither may touch the current one. Network errors
		// never reach this branch, so an outage is never disguised as a
		// sign-out.
		if (
			res.status === 401 &&
			!path.startsWith('/api/v1/auth/') &&
			myGen === currentAuthGeneration() &&
			unauthorizedHandler
		) {
			unauthorizedHandler();
		}
		let code = 'internal_error';
		let detail = '';
		let body: unknown;
		try {
			const parsed = (await res.json()) as { code?: string; detail?: string };
			if (parsed && typeof parsed.code === 'string' && parsed.code) {
				code = parsed.code;
			} else {
				// Not an RFC 9457 problem document (e.g. the failed release
				// record returned as application/json): keep the parsed body
				// so callers can surface its structured content.
				body = parsed;
			}
			if (parsed && typeof parsed.detail === 'string') detail = parsed.detail;
		} catch {
			// Non-JSON error body; keep the defaults.
		}
		throw new ApiError(res.status, code, detail, etag, body);
	}
	if (res.status === 204) {
		return { data: undefined as T, etag };
	}
	return { data: (await res.json()) as T, etag };
}
