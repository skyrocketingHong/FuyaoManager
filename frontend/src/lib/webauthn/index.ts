/**
 * Browser WebAuthn (passkey) support for the management console.
 *
 * Centralizes capability detection, base64url <-> binary conversion,
 * complete response serialization, and error mapping so pages never touch
 * `navigator.credentials` directly. Native JSON conversion
 * (PublicKeyCredential.parseCreationOptionsFromJSON / .toJSON) is used when
 * the browser provides it; otherwise the required fields are converted
 * manually. A PublicKeyCredential is never JSON.stringify'd blindly — the
 * prototype fields would be lost.
 */

export type WebAuthnCapability = 'available' | 'unsupported' | 'insecure-context';

/** Detects whether this browser context can run passkey ceremonies at all. */
export function detectCapability(): WebAuthnCapability {
	if (typeof window === 'undefined' || !window.isSecureContext) return 'insecure-context';
	if (
		typeof window.PublicKeyCredential === 'undefined' ||
		typeof navigator === 'undefined' ||
		!navigator.credentials ||
		typeof navigator.credentials.create !== 'function' ||
		typeof navigator.credentials.get !== 'function'
	) {
		return 'unsupported';
	}
	return 'available';
}

export type WebAuthnErrorKind =
	| 'cancelled' // user cancelled or the authenticator timed out
	| 'duplicate' // the authenticator already holds this credential
	| 'rp-mismatch' // RP ID / origin misconfiguration
	| 'insecure-context' // not a secure context (https / localhost required)
	| 'unsupported'
	| 'unknown';

function capabilityError(): WebAuthnError {
	return detectCapability() === 'insecure-context'
		? new WebAuthnError('insecure-context')
		: new WebAuthnError('unsupported');
}

/** Maps a browser ceremony failure to a stable, translatable kind. */
export class WebAuthnError extends Error {
	readonly kind: WebAuthnErrorKind;

	constructor(kind: WebAuthnErrorKind, cause?: unknown) {
		super(`webauthn ${kind}`);
		this.kind = kind;
		this.cause = cause;
	}
}

export function mapCeremonyError(err: unknown): WebAuthnError {
	if (err instanceof WebAuthnError) return err;
	if (err instanceof DOMException) {
		switch (err.name) {
			case 'NotAllowedError':
			case 'AbortError':
				return new WebAuthnError('cancelled', err);
			case 'InvalidStateError':
				return new WebAuthnError('duplicate', err);
			case 'SecurityError':
				return new WebAuthnError('rp-mismatch', err);
			case 'NotSupportedError':
				return new WebAuthnError('unsupported', err);
			default:
				return new WebAuthnError('unknown', err);
		}
	}
	return new WebAuthnError('unknown', err);
}

// ---------------------------------------------------------------------------
// JSON representations (subset of the WebAuthn Level 3 JSON types the
// management API exchanges; binary fields are base64url strings).
// ---------------------------------------------------------------------------

export interface CreationOptionsJSON {
	rp: { id: string; name: string };
	user: { id: string; name: string; displayName: string };
	challenge: string;
	pubKeyCredParams: PublicKeyCredentialParameters[];
	timeout?: number;
	excludeCredentials?: { id: string; type: 'public-key'; transports?: AuthenticatorTransport[] }[];
	authenticatorSelection?: AuthenticatorSelectionCriteria;
	attestation?: string;
	extensions?: Record<string, unknown>;
}

export interface RequestOptionsJSON {
	challenge: string;
	timeout?: number;
	rpId?: string;
	allowCredentials?: { id: string; type: 'public-key'; transports?: AuthenticatorTransport[] }[];
	userVerification?: UserVerificationRequirement;
	extensions?: Record<string, unknown>;
}

export interface RegistrationResponseJSON {
	id: string;
	rawId: string;
	type: 'public-key';
	response: {
		attestationObject: string;
		clientDataJSON: string;
		transports?: string[];
	};
	clientExtensionResults: AuthenticationExtensionsClientOutputs;
}

export interface AuthenticationResponseJSON {
	id: string;
	rawId: string;
	type: 'public-key';
	response: {
		authenticatorData: string;
		clientDataJSON: string;
		signature: string;
		userHandle: string | null;
	};
	clientExtensionResults: AuthenticationExtensionsClientOutputs;
}

export function base64urlToBuffer(value: string): ArrayBuffer {
	const base64 = value.replace(/-/g, '+').replace(/_/g, '/');
	const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4);
	const binary = atob(padded);
	const bytes = new Uint8Array(binary.length);
	for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
	return bytes.buffer;
}

export function bufferToBase64url(buffer: ArrayBuffer): string {
	const bytes = new Uint8Array(buffer);
	let binary = '';
	for (const byte of bytes) binary += String.fromCharCode(byte);
	return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

type JSONCapableCredential = PublicKeyCredential & {
	toJSON?: () => unknown;
};

type StaticJSON = {
	parseCreationOptionsFromJSON?: (json: CreationOptionsJSON) => PublicKeyCredentialCreationOptions;
	parseRequestOptionsFromJSON?: (json: RequestOptionsJSON) => PublicKeyCredentialRequestOptions;
};

function creationFromJSON(json: CreationOptionsJSON): PublicKeyCredentialCreationOptions {
	const nativeParse = (PublicKeyCredential as unknown as StaticJSON).parseCreationOptionsFromJSON;
	if (typeof nativeParse === 'function') return nativeParse(json);
	return {
		...json,
		challenge: base64urlToBuffer(json.challenge),
		user: { ...json.user, id: base64urlToBuffer(json.user.id) },
		excludeCredentials: json.excludeCredentials?.map((entry) => ({
			...entry,
			id: base64urlToBuffer(entry.id)
		}))
	} as PublicKeyCredentialCreationOptions;
}

function requestFromJSON(json: RequestOptionsJSON): PublicKeyCredentialRequestOptions {
	const nativeParse = (PublicKeyCredential as unknown as StaticJSON).parseRequestOptionsFromJSON;
	if (typeof nativeParse === 'function') return nativeParse(json);
	return {
		...json,
		challenge: base64urlToBuffer(json.challenge),
		allowCredentials: json.allowCredentials?.map((entry) => ({
			...entry,
			id: base64urlToBuffer(entry.id)
		}))
	} as PublicKeyCredentialRequestOptions;
}

/** Creates a credential from server-issued creation options. */
export async function createPasskey(options: CreationOptionsJSON): Promise<RegistrationResponseJSON> {
	if (detectCapability() !== 'available') throw capabilityError();
	let credential: Credential | null;
	try {
		credential = await navigator.credentials.create({ publicKey: creationFromJSON(options) });
	} catch (err) {
		throw mapCeremonyError(err);
	}
	if (!credential || credential.type !== 'public-key') throw new WebAuthnError('unknown');
	const pk = credential as JSONCapableCredential;
	if (typeof pk.toJSON === 'function') return pk.toJSON() as RegistrationResponseJSON;
	const response = pk.response as AuthenticatorAttestationResponse;
	return {
		id: pk.id,
		rawId: bufferToBase64url(pk.rawId),
		type: 'public-key',
		response: {
			attestationObject: bufferToBase64url(response.attestationObject),
			clientDataJSON: bufferToBase64url(response.clientDataJSON),
			transports: typeof response.getTransports === 'function' ? response.getTransports() : []
		},
		clientExtensionResults: pk.getClientExtensionResults()
	};
}

/** Requests an assertion for a discoverable-credential login. */
export async function getPasskeyAssertion(
	options: RequestOptionsJSON
): Promise<AuthenticationResponseJSON> {
	if (detectCapability() !== 'available') throw capabilityError();
	let credential: Credential | null;
	try {
		credential = await navigator.credentials.get({ publicKey: requestFromJSON(options) });
	} catch (err) {
		throw mapCeremonyError(err);
	}
	if (!credential || credential.type !== 'public-key') throw new WebAuthnError('unknown');
	const pk = credential as JSONCapableCredential;
	if (typeof pk.toJSON === 'function') return pk.toJSON() as AuthenticationResponseJSON;
	const response = pk.response as AuthenticatorAssertionResponse;
	return {
		id: pk.id,
		rawId: bufferToBase64url(pk.rawId),
		type: 'public-key',
		response: {
			authenticatorData: bufferToBase64url(response.authenticatorData),
			clientDataJSON: bufferToBase64url(response.clientDataJSON),
			signature: bufferToBase64url(response.signature),
			userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : null
		},
		clientExtensionResults: pk.getClientExtensionResults()
	};
}
