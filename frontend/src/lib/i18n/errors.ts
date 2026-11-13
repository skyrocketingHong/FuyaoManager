/** Maps backend problem codes and browser errors to message keys. */
import type { MessageKey } from './index';
import { ApiError } from '../api/client';

const codeToKey: Record<string, MessageKey> = {
	preview_closed: 'error.previewClosed',
	caddy_unavailable: 'error.caddyUnavailable',
	caddy_invalid: 'error.caddyInvalid',
	site_kind_mismatch: 'caddy.unsupportedPage',
	invalid_permit: 'error.invalidPermit',
	permit_used: 'error.permitUsed',
	permit_expired: 'error.permitExpired',
	bootstrap_closed: 'error.bootstrapClosed',
	challenge_invalid: 'error.challengeInvalid',
	credential_rejected: 'error.credentialRejected',
	invalid_code: 'error.invalidCode',
	code_used: 'error.codeUsed',
	code_expired: 'error.codeExpired',
	code_locked: 'error.codeLocked',
	code_purpose_mismatch: 'error.codePurposeMismatch',
	rate_limited: 'error.rateLimited',
	passkey_binding_required: 'error.passkeyBindingRequired',
	credential_duplicate: 'error.credentialDuplicate',
	session_rotated: 'error.sessionRotated',
	passkey_unavailable: 'error.passkeyUnavailable',
	slug_taken: 'error.slugTaken',
	content_unavailable: 'error.contentUnavailable',
	content_exists: 'error.contentExists',
	invalid_content_path: 'error.validation',
	photo_in_use: 'error.photoInUse',
	provisioning_unavailable: 'error.provisioningUnavailable',
	unauthenticated: 'error.unauthenticated',
	forbidden: 'error.forbidden',
	csrf_token_invalid: 'error.csrf',
	precondition_required: 'error.preconditionRequired',
	validation_error: 'error.validation',
	not_found: 'error.notFound',
	no_theme_selected: 'error.noThemeSelected',
	theme_not_found: 'error.themeNotFound',
	invalid_theme_config: 'error.invalidThemeConfig',
	theme_contracts_unavailable: 'error.themeContractsUnavailable',
	build_not_found: 'error.buildNotFound',
	build_not_succeeded: 'error.buildNotSucceeded',
	build_runner_unavailable: 'error.buildRunnerUnavailable',
	preview_unavailable: 'error.previewUnavailable',
	publish_unavailable: 'error.publishUnavailable',
	build_not_publishable: 'error.buildNotPublishable',
	release_build_conflict: 'error.releaseBuildConflict',
	release_not_found: 'error.releaseNotFound',
	release_not_rollbackable: 'error.releaseNotRollbackable',
	no_live_release: 'error.noLiveRelease',
	deployment_disabled: 'error.deploymentDisabled',
	username_taken: 'error.usernameTaken',
	last_admin: 'error.lastAdmin',
	last_owner: 'error.lastOwner',
	last_passkey: 'error.lastPasskey',
	self_management: 'error.selfManagement',
	account_conflict: 'error.accountConflict',
	operator_registration_required: 'caddy.operatorRegistration',
	source_missing: 'caddy.missing',
	source_config_changed: 'caddy.sourceChanged',
	caddy_validation_failed: 'caddy.validationFailed',
	caddy_capability_denied: 'error.forbidden',
	caddy_runtime_uncertain: 'caddy.runtimeUncertain',
	site_kind_unsupported: 'caddy.kindUnsupported',
	caddy_deployment_disabled: 'error.deploymentDisabled',
	caddy_state_conflict: 'caddy.stateConflict'
};

/** Returns the message key describing the error, or null for unknown errors. */
export function apiErrorKey(err: unknown): MessageKey | null {
	if (err instanceof ApiError) {
		if (err.code === 'precondition_failed') {
			return err.code === 'precondition_failed' && err.status === 412 ? 'error.conflict' : null;
		}
		return codeToKey[err.code] ?? null;
	}
	if (err instanceof TypeError) return 'error.network'; // fetch failed
	return null;
}
