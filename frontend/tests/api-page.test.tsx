// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { MemoryRouter } from 'react-router-dom';
import ApiPage from '../src/pages/ApiPage';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import { api } from '../src/lib/api/client';
import { setLocale } from '../src/lib/i18n';

vi.mock('../src/lib/api/client', async (original) => ({
	...(await original<typeof import('../src/lib/api/client')>()),
	api: vi.fn()
}));
vi.mock('../src/lib/session', () => {
	const session = {
		status: 'authenticated',
		user: { id: 'admin', username: 'admin', platformRole: 'admin' }
	};
	return { sessionSnapshot: () => session, subscribeSession: () => () => {}, loadSession: vi.fn() };
});
const contract = {
	openapi: '3.0.3',
	info: { title: 'Manager API', version: '1.0.0' },
	security: [{ sessionCookie: [] }],
	components: { securitySchemes: { sessionCookie: { in: 'cookie', name: 'manager-session' } } },
	paths: {
		'/api/v1/sites/{siteId}': {
			get: {
				summary: 'Read site',
				tags: ['Sites'],
				parameters: [{ name: 'siteId', in: 'path', required: true, schema: { type: 'string' } }],
				responses: { 200: { description: 'Site response' } }
			}
		},
		'/api/v1/auth/login': {
			post: {
				summary: 'Sign-in',
				tags: ['Auth'],
				security: [],
				responses: { 204: { description: 'Signed in' } }
			}
		}
	}
};
function Header() {
	const header = usePageHeaderContent();
	return (
		<div>
			{header?.content}
			{header?.actions}
		</div>
	);
}
function mount() {
	render(
		<MemoryRouter>
			<ConfigProvider>
				<AntApp>
					<PageHeaderProvider scope="api">
						<Header />
						<ApiPage />
					</PageHeaderProvider>
				</AntApp>
			</ConfigProvider>
		</MemoryRouter>
	);
}
beforeEach(() => {
	vi.stubEnv('NODE_ENV', 'development');
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	vi.mocked(api).mockResolvedValue({ data: contract, etag: null });
});
afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.unstubAllEnvs();
	vi.unstubAllGlobals();
});

it('loads the authenticated contract, opens inline parameter/response details and filters operations', async () => {
	mount();
	fireEvent.click(await screen.findByRole('button', { name: /Read site/ }));
	expect(screen.getByRole('heading', { name: 'Request parameters' })).toBeDefined();
	expect(screen.getByText('siteId')).toBeDefined();
	expect(screen.getByRole('heading', { name: '200 Site response' })).toBeDefined();
	fireEvent.change(screen.getByRole('textbox', { name: 'Search API' }), {
		target: { value: 'sign-in' }
	});
	expect(screen.queryByText('/api/v1/sites/{siteId}')).toBeNull();
	expect(screen.getByRole('button', { name: /Sign-in/ })).toBeDefined();
	fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
	expect(screen.getByText('/api/v1/sites/{siteId}')).toBeDefined();
	expect(api).toHaveBeenCalledWith(
		'GET',
		'/api/v1/openapi',
		expect.objectContaining({ signal: expect.any(AbortSignal) })
	);
});
it('retries errors and provides real copy/download actions with cleanup', async () => {
	vi.mocked(api).mockRejectedValueOnce(new TypeError('offline'));
	const writeText = vi.fn().mockResolvedValue(undefined);
	Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
	const NativeURL = URL;
	const createObjectURL = vi.fn().mockReturnValue('blob:contract');
	const revokeObjectURL = vi.fn();
	vi.stubGlobal(
		'URL',
		class extends NativeURL {
			static createObjectURL = createObjectURL;
			static revokeObjectURL = revokeObjectURL;
		}
	);
	const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	fireEvent.click(await screen.findByRole('button', { name: /Read site/ }));
	fireEvent.click(screen.getByRole('button', { name: 'Copy path' }));
	await waitFor(() => expect(writeText).toHaveBeenCalledWith('/api/v1/sites/{siteId}'));
	expect(await screen.findByRole('button', { name: 'Copied' })).toBeDefined();
	fireEvent.click(screen.getByRole('button', { name: 'Download OpenAPI' }));
	expect(createObjectURL).toHaveBeenCalledTimes(1);
	expect((click.mock.contexts[0] as HTMLAnchorElement | undefined)?.download).toBe(
		'fuyao-manager-openapi.json'
	);
	cleanup();
	expect(revokeObjectURL).toHaveBeenCalledWith('blob:contract');
});
