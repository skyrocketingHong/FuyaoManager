/**
 * App-wide session state backed by GET /api/v1/session, exposed as an
 * external store for React binding. `platformInitialized` tells an anonymous
 * browser whether the CLI first-run binding already happened, driving the
 * initialization guidance on the login screen.
 */
import { setCsrfToken } from '../api/client';
import { exchangeAuthCode, fetchSession, logout as apiLogout, type User } from '../api/management';

export type SessionStatus = 'loading' | 'authenticated' | 'anonymous';

interface SessionState {
	status: SessionStatus;
	user: User | null;
	platformInitialized: boolean;
}

let state: SessionState = { status: 'loading', user: null, platformInitialized: true };
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
		state = {
			status: info.authenticated ? 'authenticated' : 'anonymous',
			user: info.user ?? null,
			platformInitialized: info.platformInitialized
		};
	} catch {
		state = { status: 'anonymous', user: null, platformInitialized: true };
		setCsrfToken(null);
	}
	emit();
}

/** Redeems a CLI one-time code and applies the resulting session. */
export async function signInWithCode(code: string): Promise<User> {
	const done = await exchangeAuthCode(code);
	state = { status: 'authenticated', user: done.user, platformInitialized: true };
	emit();
	return done.user;
}

export function markSignedIn(user: User): void {
	state = { ...state, status: 'authenticated', user };
	emit();
}

export async function signOut(): Promise<void> {
	try {
		await apiLogout();
	} finally {
		state = { ...state, status: 'anonymous', user: null };
		setCsrfToken(null);
		emit();
	}
}
