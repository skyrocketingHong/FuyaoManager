/**
 * Browser passkey ceremonies (registration and discoverable login) composed
 * from the typed API module and @simplewebauthn/browser. The options JSON is
 * passed through verbatim; the server verifies every signature.
 */
import { startAuthentication, startRegistration } from '@simplewebauthn/browser';
import type { AuthenticationResponseJSON, RegistrationResponseJSON } from '@simplewebauthn/browser';
import {
	loginOptions,
	loginVerify,
	registerOptions,
	registerVerify,
	type CeremonyDone
} from './management';

export class CeremonyAborted extends Error {
	constructor() {
		super('passkey ceremony aborted');
		this.name = 'CeremonyAborted';
	}
}

function rethrowCancel(err: unknown): never {
	if (err instanceof Error && err.name === 'NotAllowedError') {
		throw new CeremonyAborted();
	}
	throw err;
}

/** Enrolls a passkey using a one-time bootstrap permit and signs in. */
export async function enrollPasskey(permit: string): Promise<CeremonyDone> {
	const options = await registerOptions(permit);
	let attestation: RegistrationResponseJSON;
	try {
		attestation = await startRegistration({ optionsJSON: options });
	} catch (err) {
		rethrowCancel(err);
	}
	return registerVerify(attestation);
}

/** Signs in with a discoverable passkey (user verification required). */
export async function loginWithPasskey(): Promise<CeremonyDone> {
	const options = await loginOptions();
	let assertion: AuthenticationResponseJSON;
	try {
		assertion = await startAuthentication({ optionsJSON: options });
	} catch (err) {
		rethrowCancel(err);
	}
	return loginVerify(assertion);
}
