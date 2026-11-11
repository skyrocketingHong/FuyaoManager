/**
 * App-wide session state backed by GET /api/v1/session, exposed as an
 * external store for React binding.
 */
import { setCsrfToken } from '../api/client';
import { fetchSession, logout as apiLogout, type User } from '../api/management';

export type SessionStatus = 'loading' | 'authenticated' | 'anonymous';

interface SessionState {
	status: SessionStatus;
	user: User | null;
}

let state: SessionState = { status: 'loading', user: null };
const listeners = new Set<() => void>();

function emit(): void {
	for (const listener of listeners) listener();
}

export function subscribeSession(listener: () => void): () => void {
	listeners.add(listener);
	return () => listeners.delete(listener);
}

export function sessionSnapshot(): SessionState {
	return state;
}

export async function loadSession(): Promise<void> {
	try {
		const info = await fetchSession();
		state = { status: info.authenticated ? 'authenticated' : 'anonymous', user: info.user ?? null };
	} catch {
		state = { status: 'anonymous', user: null };
		setCsrfToken(null);
	}
	emit();
}

export function markSignedIn(user: User): void {
	state = { status: 'authenticated', user };
	emit();
}

export async function signOut(): Promise<void> {
	try {
		await apiLogout();
	} finally {
		state = { status: 'anonymous', user: null };
		setCsrfToken(null);
		emit();
	}
}
