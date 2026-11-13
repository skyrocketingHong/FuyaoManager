import { api, type ApiResult } from './client';
import type { MessageKey } from '../i18n';
import type { components } from './schema';

export type CaddySettings = components['schemas']['CaddySettings'];
export const preservedCaddySettings: CaddySettings = {
	compression: 'preserve',
	cacheControl: 'preserve',
	securityHeaders: 'preserve'
};
type WireConfig = components['schemas']['CaddySite'];
export type CaddyHistory = NonNullable<WireConfig['management']>['history'][number];
interface Definition {
	hosts?: string[];
	listen?: string[];
	root?: string;
	readOnly?: boolean;
	reason?: string;
	redirect?: { location?: string; statusCode?: number };
}
export type CaddyConfig = Omit<WireConfig, 'definition'> & { definition: Definition };
export type CaddyEnvelope = ApiResult<CaddyConfig>;
function present(response: ApiResult<WireConfig>): CaddyEnvelope {
	const source = response.data.definition;
	const redirect =
		source.redirect && typeof source.redirect === 'object'
			? (source.redirect as Record<string, unknown>)
			: null;
	const definition: Definition = {
		hosts: Array.isArray(source.hosts)
			? source.hosts.filter((value): value is string => typeof value === 'string')
			: [],
		listen: Array.isArray(source.listen)
			? source.listen.filter((value): value is string => typeof value === 'string')
			: [],
		root: typeof source.root === 'string' ? source.root : undefined,
		redirect: redirect
			? {
					location: typeof redirect.location === 'string' ? redirect.location : undefined,
					statusCode: typeof redirect.statusCode === 'number' ? redirect.statusCode : undefined
				}
			: undefined
	};
	return { ...response, data: { ...response.data, definition } };
}
export async function getCaddyConfig(siteId: string, signal?: AbortSignal): Promise<CaddyEnvelope> {
	return present(
		await api<WireConfig>('GET', `/api/v1/sites/${encodeURIComponent(siteId)}/caddy`, { signal })
	);
}
export async function changeCaddyConfig(
	siteId: string,
	action: 'draft' | 'convert' | 'validate' | 'deploy' | 'rollback',
	etag: string,
	body: { settings?: CaddySettings; revision?: number } = {}
): Promise<CaddyEnvelope> {
	return present(
		await api<WireConfig>(
			action === 'draft' ? 'PUT' : 'POST',
			`/api/v1/sites/${encodeURIComponent(siteId)}/caddy/${action}`,
			{ etag, body }
		)
	);
}
export function caddyReasonKey(code: string | undefined): MessageKey {
	return (
		(
			{
				operator_registration_required: 'caddy.operatorRegistration',
				site_kind_unsupported: 'caddy.kindUnsupported',
				deployment_disabled: 'error.deploymentDisabled',
				caddy_deployment_disabled: 'error.deploymentDisabled',
				source_config_changed: 'caddy.sourceChanged',
				caddy_validation_failed: 'caddy.validationFailed',
				caddy_capability_denied: 'error.forbidden',
				caddy_runtime_uncertain: 'caddy.runtimeUncertain',
				caddy_unavailable: 'caddy.managementUnavailable',
				source_missing: 'caddy.missing'
			} as Record<string, MessageKey>
		)[code ?? ''] ?? 'caddy.managementUnavailable'
	);
}
