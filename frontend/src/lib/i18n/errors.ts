/** Maps backend problem codes and browser ceremony errors to message keys. */
import type { MessageKey } from './index.svelte';
import { ApiError } from '$lib/api/client';
import { CeremonyAborted } from '$lib/api/passkey';

const codeToKey: Record<string, MessageKey> = {
	invalid_permit: 'error.invalidPermit',
	permit_used: 'error.permitUsed',
	permit_expired: 'error.permitExpired',
	bootstrap_closed: 'error.bootstrapClosed',
	challenge_invalid: 'error.challengeInvalid',
	credential_rejected: 'error.credentialRejected',
	unauthenticated: 'error.unauthenticated',
	forbidden: 'error.forbidden',
	csrf_token_invalid: 'error.csrf',
	precondition_required: 'error.preconditionRequired',
	validation_error: 'error.validation',
	not_found: 'error.notFound'
};

/** Returns the message key describing the error, or null for unknown errors. */
export function apiErrorKey(err: unknown): MessageKey | null {
	if (err instanceof CeremonyAborted) return 'error.aborted';
	if (err instanceof ApiError) {
		if (err.code === 'precondition_failed') {
			return err.code === 'precondition_failed' && err.status === 412 ? 'error.conflict' : null;
		}
		return codeToKey[err.code] ?? null;
	}
	if (err instanceof TypeError) return 'error.network'; // fetch failed
	return null;
}
