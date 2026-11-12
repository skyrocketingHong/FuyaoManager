/**
 * App-wide session state backed by GET /api/v1/session, exposed as an
 * external store for React binding. The model separates four facts the UI
 * must never collapse into one boolean: whether the platform has an active
 * administrator (platformInitialized), whether this browser confirmed an
 * account identity (anonymous vs. authenticated), whether the account owns
 * a credential for the current relying party (passkeyBound), and whether
 * the session may use business APIs (status 'authenticated' = full session;
 * 'restricted' = identity confirmed, binding still required). A failed
 * session read is an explicit 'error' state — never disguised as anonymous.
 */
import { setCsrfToken } from '../api/client';
import {
	beginPasskeyBinding,
	beginPasskeyLogin,
	exchangeAuthCode,
	fetchSession,
	finishPasskeyBinding,
	finishPasskeyLogin,
	logout as apiLogout,
	type CeremonyDone,
	type User
} from '../api/management';
import { createPasskey, getPasskeyAssertion } from '../webauthn';
import type { CreationOptionsJSON, RequestOptionsJSON } from '../webauthn';

export type SessionStatus = 'loading' | 'error' | 'anonymous' | 'restricted' | 'authenticated';

export interface SessionState {
	status: SessionStatus;
	user: User | null;
	platformInitialized: boolean;
	passkeyBound: boolean;
	passkeyAvailable: boolean;
}

let state: SessionState = {
	status: 'loading',
	user: null,
	platformInitialized: true,
	passkeyBound: false,
	passkeyAvailable: false
};
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
		if (info.authenticated && info.user) {
			state = {
				status: info.sessionScope === 'full' ? 'authenticated' : 'restricted',
				user: info.user,
				platformInitialized: info.platformInitialized,
				passkeyBound: info.passkeyBound,
				passkeyAvailable: info.passkeyAvailable
			};
		} else {
			setCsrfToken(null);
			state = {
				status: 'anonymous',
				user: null,
				platformInitialized: info.platformInitialized,
				passkeyBound: false,
				passkeyAvailable: info.passkeyAvailable
			};
		}
	} catch {
		// A network/server failure is not "signed out": keep the error
		// explicit so the shell can offer a retry instead of rendering the
		// login or initialization screens on a transient outage.
		setCsrfToken(null);
		state = { ...state, status: 'error', user: null };
	}
	emit();
}

function applyCeremony(done: CeremonyDone): SessionStatus {
	state = {
		status: done.sessionScope === 'full' ? 'authenticated' : 'restricted',
		user: done.user,
		platformInitialized: true,
		passkeyBound: done.passkeyBound,
		passkeyAvailable: true
	};
	emit();
	return state.status;
}

/**
 * Redeems a CLI-issued one-time code. The result is 'authenticated' for an
 * already-bound account and 'restricted' when a passkey bind must follow.
 */
export async function signInWithCode(code: string): Promise<SessionStatus> {
	return applyCeremony(await exchangeAuthCode(code));
}

/** Routine sign-in: discoverable passkey assertion, then a full session. */
export async function signInWithPasskey(): Promise<SessionStatus> {
	const options = await beginPasskeyLogin();
	const assertion = await getPasskeyAssertion(
		options.publicKey as unknown as RequestOptionsJSON
	);
	return applyCeremony(
		await finishPasskeyLogin(assertion as unknown as Parameters<typeof finishPasskeyLogin>[0])
	);
}

/** Binds a new passkey for the current session's account and rotates to a full session. */
export async function bindPasskey(): Promise<SessionStatus> {
	const options = await beginPasskeyBinding();
	const attestation = await createPasskey(options.publicKey as unknown as CreationOptionsJSON);
	return applyCeremony(
		await finishPasskeyBinding(attestation as unknown as Parameters<typeof finishPasskeyBinding>[0])
	);
}

export async function signOut(): Promise<void> {
	try {
		await apiLogout();
	} finally {
		setCsrfToken(null);
		state = { ...state, status: 'anonymous', user: null, passkeyBound: false };
		emit();
	}
}
