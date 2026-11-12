/**
 * Session-store unit tests: the five-state model behind the passkey flow —
 * restricted vs. full sessions, error vs. anonymous reads, and the ceremony
 * transitions (code exchange, binding, passkey sign-in).
 */
import { describe, expect, it, vi, beforeEach } from 'vitest';

const fetchSession = vi.fn();
const exchangeAuthCode = vi.fn();
const beginPasskeyBinding = vi.fn();
const finishPasskeyBinding = vi.fn();
const beginPasskeyLogin = vi.fn();
const finishPasskeyLogin = vi.fn();
const logout = vi.fn();
const setCsrfToken = vi.fn();

vi.mock('../src/lib/api/management', () => ({
	fetchSession: (...args: unknown[]) => fetchSession(...args),
	exchangeAuthCode: (...args: unknown[]) => exchangeAuthCode(...args),
	beginPasskeyBinding: (...args: unknown[]) => beginPasskeyBinding(...args),
	finishPasskeyBinding: (...args: unknown[]) => finishPasskeyBinding(...args),
	beginPasskeyLogin: (...args: unknown[]) => beginPasskeyLogin(...args),
	finishPasskeyLogin: (...args: unknown[]) => finishPasskeyLogin(...args),
	logout: (...args: unknown[]) => logout(...args)
}));
vi.mock('../src/lib/api/client', () => ({
	setCsrfToken: (...args: unknown[]) => setCsrfToken(...args)
}));
const createPasskey = vi.fn();
const getPasskeyAssertion = vi.fn();
vi.mock('../src/lib/webauthn', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/webauthn')>();
	return {
		...actual,
		createPasskey: (...args: unknown[]) => createPasskey(...args),
		getPasskeyAssertion: (...args: unknown[]) => getPasskeyAssertion(...args)
	};
});

beforeEach(() => {
	vi.clearAllMocks();
	vi.resetModules();
});

const alice = { id: '11111111-1111-1111-1111-111111111111', username: 'alice', displayName: 'Alice' };

function fullDone() {
	return { user: alice, csrfToken: 'csrf-2', sessionScope: 'full', passkeyBound: true };
}

describe('session store', () => {
	it('maps a restricted server session to the restricted status', async () => {
		fetchSession.mockResolvedValue({
			authenticated: true,
			platformInitialized: true,
			user: alice,
			csrfToken: 'csrf-1',
			sessionScope: 'restricted',
			passkeyBound: false,
			passkeyAvailable: true
		});
		const session = await import('../src/lib/session');
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('restricted');
		expect(session.sessionSnapshot().passkeyBound).toBe(false);
		expect(session.sessionSnapshot().user?.username).toBe('alice');
	});

	it('maps a full server session to authenticated', async () => {
		fetchSession.mockResolvedValue({
			authenticated: true,
			platformInitialized: true,
			user: alice,
			csrfToken: 'csrf-2',
			sessionScope: 'full',
			passkeyBound: true,
			passkeyAvailable: true
		});
		const session = await import('../src/lib/session');
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('authenticated');
		expect(session.sessionSnapshot().passkeyBound).toBe(true);
	});

	it('keeps a failed session read as an explicit error, never anonymous', async () => {
		fetchSession.mockRejectedValue(new TypeError('fetch failed'));
		const session = await import('../src/lib/session');
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('error');
	});

	it('marks an anonymous probe anonymous with platformInitialized intact', async () => {
		fetchSession.mockResolvedValue({
			authenticated: false,
			platformInitialized: false,
			passkeyBound: false,
			passkeyAvailable: true
		});
		const session = await import('../src/lib/session');
		await session.loadSession();
		expect(session.sessionSnapshot().status).toBe('anonymous');
		expect(session.sessionSnapshot().platformInitialized).toBe(false);
	});

	it('resolves a code exchange to restricted when the account is unbound', async () => {
		exchangeAuthCode.mockResolvedValue({
			user: alice,
			csrfToken: 'csrf-1',
			sessionScope: 'restricted',
			passkeyBound: false
		});
		const session = await import('../src/lib/session');
		const status = await session.signInWithCode('XXXX-XXXX-XXXX-XXXX');
		expect(status).toBe('restricted');
		expect(session.sessionSnapshot().status).toBe('restricted');
	});

	it('completes the binding ceremony into a full session', async () => {
		beginPasskeyBinding.mockResolvedValue({ publicKey: { challenge: 'Y2hhbGxlbmdl' } });
		createPasskey.mockResolvedValue({ id: 'cred' });
		finishPasskeyBinding.mockResolvedValue(fullDone());
		const session = await import('../src/lib/session');
		const status = await session.bindPasskey();
		expect(status).toBe('authenticated');
		expect(session.sessionSnapshot().passkeyBound).toBe(true);
		expect(finishPasskeyBinding).toHaveBeenCalledTimes(1);
		expect(createPasskey).toHaveBeenCalledTimes(1);
	});

	it('signs in with a passkey assertion into a full session', async () => {
		beginPasskeyLogin.mockResolvedValue({ publicKey: { challenge: 'Y2hhbGxlbmdl' } });
		getPasskeyAssertion.mockResolvedValue({ id: 'cred' });
		finishPasskeyLogin.mockResolvedValue(fullDone());
		const session = await import('../src/lib/session');
		const status = await session.signInWithPasskey();
		expect(status).toBe('authenticated');
	});
});
