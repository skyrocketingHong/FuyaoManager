// Catalogue model and filtering follow
// robot-platform/modules/frontend-api-catalog/ui/src/apiCatalog.ts.
// The manager consumes its authenticated OpenAPI JSON instead of a second catalogue API.
import { t } from './i18n';

type ObjectValue = Record<string, unknown>;
export interface ApiParameter {
	name?: string;
	in?: string;
	description?: string;
	required?: boolean;
	schema?: ObjectValue;
}
export interface ApiOperation {
	id: string;
	method: string;
	path: string;
	tags: string[];
	summary: string;
	description: string;
	minBackendVersion?: string;
	versionNote?: string;
	security: Record<string, string[]>[];
	permissions: string[];
	permissionNote: string;
	parameters: ApiParameter[];
	requestBody: ObjectValue | null;
	responses: Record<string, ObjectValue>;
}
export interface ApiCatalog {
	title: string;
	version: string;
	openapi: string;
	operations: ApiOperation[];
	securitySchemes: Record<string, ObjectValue>;
	source: string;
}
const object = (value: unknown): ObjectValue =>
	value !== null && typeof value === 'object' && !Array.isArray(value)
		? (value as ObjectValue)
		: {};
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const strings = (value: unknown): string[] =>
	Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
function security(value: unknown): Record<string, string[]>[] {
	return Array.isArray(value)
		? value.map((item) =>
				Object.fromEntries(
					Object.entries(object(item)).map(([key, scopes]) => [key, strings(scopes)])
				)
			)
		: [];
}

// Only local references are resolved. Cycles remain references, and no document
// value can trigger a remote request, script, HTML interpretation or file read.
function resolve(
	value: unknown,
	document: ObjectValue,
	seen = new Set<string>(),
	depth = 0
): unknown {
	if (depth > 12) return value;
	if (Array.isArray(value)) return value.map((item) => resolve(item, document, seen, depth + 1));
	if (value === null || typeof value !== 'object') return value;
	const item = object(value);
	const ref = text(item.$ref);
	if (ref.startsWith('#/') && !seen.has(ref)) {
		let target: unknown = document;
		for (const segment of ref.slice(2).split('/')) {
			let key: string;
			try {
				key = decodeURIComponent(segment).replace(/~1/g, '/').replace(/~0/g, '~');
			} catch {
				return item;
			}
			const parent = object(target);
			if (!Object.prototype.hasOwnProperty.call(parent, key)) return item;
			target = parent[key];
		}
		const next = new Set(seen);
		next.add(ref);
		return resolve(target, document, next, depth + 1);
	}
	if (ref) return item;
	return Object.fromEntries(
		Object.entries(item).map(([key, child]) => [key, resolve(child, document, seen, depth + 1)])
	);
}

export function parseApiCatalog(value: unknown): ApiCatalog {
	const document = object(value);
	if (
		!text(document.openapi).startsWith('3.') ||
		!document.paths ||
		typeof document.paths !== 'object' ||
		Array.isArray(document.paths)
	)
		throw new Error('Invalid OpenAPI document');
	const info = object(document.info);
	const components = object(document.components);
	const operations: ApiOperation[] = [];
	for (const [path, rawPath] of Object.entries(object(document.paths))) {
		const pathItem = object(resolve(rawPath, document));
		for (const method of ['get', 'post', 'put', 'patch', 'delete', 'options', 'head', 'trace']) {
			if (!pathItem[method] || typeof pathItem[method] !== 'object') continue;
			const operation = object(pathItem[method]);
			const merged = new Map<string, ApiParameter>();
			for (const list of [pathItem.parameters, operation.parameters]) {
				if (!Array.isArray(list)) continue;
				for (const raw of list) {
					const parameter = object(resolve(raw, document)) as ApiParameter;
					merged.set(`${parameter.in}:${parameter.name}`, parameter);
				}
			}
			const permissions = strings(operation['x-permissions']);
			const role = text(operation['x-required-platform-role']);
			if (role) permissions.push(`platformRole:${role}`);
			operations.push({
				id: text(operation.operationId) || `${method.toUpperCase()} ${path}`,
				method: method.toUpperCase(),
				path,
				tags: strings(operation.tags),
				summary: text(operation.summary),
				description: text(operation.description),
				minBackendVersion: text(operation['x-min-backend-version']) || undefined,
				versionNote: text(operation['x-version-note']) || undefined,
				security: security(
					operation.security === undefined ? document.security : operation.security
				),
				permissions,
				permissionNote: text(operation['x-permission-note']),
				parameters: [...merged.values()],
				requestBody: operation.requestBody
					? object(resolve(operation.requestBody, document))
					: null,
				responses: object(resolve(operation.responses, document)) as Record<string, ObjectValue>
			});
		}
	}
	return {
		title: text(info.title),
		version: text(info.version),
		openapi: text(document.openapi),
		operations,
		securitySchemes: object(components.securitySchemes) as Record<string, ObjectValue>,
		source: JSON.stringify(document, null, 2)
	};
}
export function apiGroup(operation: ApiOperation): { id: string; name: string } {
	const tag = operation.tags[0];
	return tag ? { id: tag, name: tag } : { id: 'other', name: t('api.otherGroup') };
}
export function apiVersionLabel(operation: ApiOperation): string {
	return operation.minBackendVersion
		? t('api.minimumVersion', { version: operation.minBackendVersion })
		: t('api.versionUnknown');
}
export function apiVersionDescription(operation: ApiOperation): string {
	return (
		operation.versionNote ||
		t(operation.minBackendVersion ? 'api.versionNote' : 'api.versionMissing')
	);
}
export function filterApiOperations(
	operations: ApiOperation[],
	query: string,
	group: string,
	method: string
): ApiOperation[] {
	const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
	return operations.filter(
		(operation) =>
			(!group || apiGroup(operation).id === group) &&
			(!method || operation.method === method) &&
			terms.every((term) =>
				[
					operation.path,
					operation.method,
					operation.summary,
					operation.description,
					operation.minBackendVersion ?? '',
					operation.versionNote ?? '',
					...operation.tags,
					...operation.permissions,
					operation.permissionNote
				]
					.join(' ')
					.toLocaleLowerCase()
					.includes(term)
			)
	);
}
export function apiAuthentication(operation: ApiOperation, catalog: ApiCatalog): string {
	if (!operation.security.length) return t('api.anonymous');
	return operation.security
		.map((requirement) =>
			Object.keys(requirement).length
				? Object.keys(requirement)
						.map((name) => {
							const scheme = catalog.securitySchemes[name];
							return scheme?.in === 'cookie'
								? t('api.cookie', { name: text(scheme.name) || name })
								: text(scheme?.description) || text(scheme?.name) || name;
						})
						.join(t('api.and'))
				: t('api.anonymous')
		)
		.join(t('api.or'));
}
