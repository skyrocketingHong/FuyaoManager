import { beforeEach, expect, it } from 'vitest';
import { apiAuthentication, filterApiOperations, parseApiCatalog } from '../src/lib/apiCatalog';
import { setLocale } from '../src/lib/i18n';

export const document = {
	openapi: '3.0.3',
	info: { title: 'Manager API', version: '1.0.0' },
	security: [{ sessionCookie: [] }],
	components: {
		securitySchemes: {
			sessionCookie: { type: 'apiKey', in: 'cookie', name: 'manager-session' },
			csrf: { type: 'apiKey', in: 'header', name: 'X-CSRF-Token' }
		},
		parameters: {
			SiteId: {
				name: 'siteId',
				in: 'path',
				required: true,
				schema: { type: 'string', format: 'uuid' }
			}
		},
		schemas: {
			Node: {
				type: 'object',
				properties: { name: { type: 'string' }, child: { $ref: '#/components/schemas/Node' } }
			}
		},
		responses: {
			Ok: {
				description: 'Saved',
				content: { 'application/json': { schema: { $ref: '#/components/schemas/Node' } } }
			}
		}
	},
	paths: {
		'/api/v1/sites/{siteId}': {
			parameters: [{ $ref: '#/components/parameters/SiteId' }],
			get: {
				operationId: 'getSite',
				tags: ['Sites'],
				summary: 'Read site',
				responses: { 200: { $ref: '#/components/responses/Ok' } }
			},
			put: {
				operationId: 'putSite',
				tags: ['Sites'],
				summary: 'Save site',
				'x-min-backend-version': '0.3.0',
				'x-required-platform-role': 'admin',
				security: [{ sessionCookie: [], csrf: [] }],
				parameters: [
					{ name: 'siteId', in: 'path', description: 'Override', schema: { type: 'string' } }
				],
				requestBody: {
					content: { 'application/json': { schema: { $ref: '#/components/schemas/Node' } } }
				},
				responses: {}
			}
		},
		'/api/v1/auth/login': {
			post: {
				operationId: 'login',
				tags: ['Auth'],
				summary: 'Sign-in',
				security: [],
				responses: { 204: { description: 'Signed in' } }
			}
		}
	}
};
beforeEach(() => setLocale('en-US'));
it('resolves local parameters, request bodies and responses while preserving the source contract', () => {
	const catalog = parseApiCatalog(document);
	expect(catalog.operations).toHaveLength(3);
	expect(catalog.operations[0].parameters[0]).toMatchObject({
		name: 'siteId',
		required: true,
		schema: { format: 'uuid' }
	});
	expect(catalog.operations[1].parameters).toHaveLength(1);
	expect(catalog.operations[1].parameters[0].description).toBe('Override');
	expect(catalog.operations[0].responses['200'].description).toBe('Saved');
	expect(JSON.stringify(catalog.operations[1].requestBody)).toContain('#/components/schemas/Node');
	expect(JSON.parse(catalog.source)).toEqual(document);
});
it('inherits security, preserves anonymous overrides and filters every search term', () => {
	const catalog = parseApiCatalog(document);
	expect(apiAuthentication(catalog.operations[0], catalog)).toBe(
		'Session cookie (manager-session)'
	);
	expect(apiAuthentication(catalog.operations[1], catalog)).toContain('X-CSRF-Token');
	expect(apiAuthentication(catalog.operations[2], catalog)).toBe('No account session required');
	expect(
		filterApiOperations(catalog.operations, 'sites 0.3 admin', 'Sites', 'PUT').map((op) => op.id)
	).toEqual(['putSite']);
	expect(filterApiOperations(catalog.operations, 'sign-in', '', '').map((op) => op.id)).toEqual([
		'login'
	]);
});
it('rejects malformed documents and never follows external references', () => {
	expect(() => parseApiCatalog({ openapi: '3.0.3', paths: 'invalid' })).toThrow();
	const catalog = parseApiCatalog({
		openapi: '3.1.0',
		paths: { '/x': { get: { responses: { 200: { $ref: 'https://example.invalid/schema' } } } } }
	});
	expect(catalog.operations[0].responses['200']).toEqual({
		$ref: 'https://example.invalid/schema'
	});
});
