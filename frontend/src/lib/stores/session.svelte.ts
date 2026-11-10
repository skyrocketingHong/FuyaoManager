/** App-wide session state backed by GET /api/v1/session. */
import { setCsrfToken } from '$lib/api/client';
import { fetchSession, logout as apiLogout, type User } from '$lib/api/management';

export type SessionStatus = 'loading' | 'authenticated' | 'anonymous';

export const session = $state<{ status: SessionStatus; user: User | null }>({
	status: 'loading',
	user: null
});

export async function loadSession(): Promise<void> {
	try {
		const info = await fetchSession();
		session.status = info.authenticated ? 'authenticated' : 'anonymous';
		session.user = info.user ?? null;
	} catch {
		session.status = 'anonymous';
		session.user = null;
		setCsrfToken(null);
	}
}

export function markSignedIn(user: User): void {
	session.status = 'authenticated';
	session.user = user;
}

export async function signOut(): Promise<void> {
	try {
		await apiLogout();
	} finally {
		session.status = 'anonymous';
		session.user = null;
		setCsrfToken(null);
	}
}
