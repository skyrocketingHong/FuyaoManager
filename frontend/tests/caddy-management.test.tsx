// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import CaddySitePage from '../src/pages/sites/CaddySitePage';
import { ApiError } from '../src/lib/api/client';
import { setLocale } from '../src/lib/i18n';
import * as service from '../src/lib/api/caddy';

const runtime = vi.hoisted(() => ({
	role: 'owner',
	state: { status: 'authenticated', user: { id: 'admin', platformRole: 'admin' } }
}));
vi.mock('../src/lib/session', () => ({
	sessionSnapshot: () => runtime.state,
	subscribeSession: () => () => undefined,
	loadSession: vi.fn()
}));
vi.mock('../src/components/layout/SiteBoundary', () => ({
	useCurrentSite: () => ({ siteId: 'site', name: 'Managed website', role: runtime.role })
}));
vi.mock('../src/lib/sites/site-list', () => ({
	loadSiteList: vi.fn().mockResolvedValue(undefined)
}));
vi.mock('../src/lib/api/caddy', async (original) => ({
	...(await original<typeof service>()),
	getCaddyConfig: vi.fn(),
	changeCaddyConfig: vi.fn()
}));

const settings: service.CaddySettings = {
	compression: 'preserve',
	cacheControl: 'preserve',
	securityHeaders: 'preserve'
};
function record(
	revision = 1,
	status: service.CaddyHistory['status'] = 'applied',
	values = settings
): service.CaddyHistory {
	return { revision, settings: { ...values }, status, createdAt: '2026-01-01T00:00:00Z' };
}
function config(
	revision = 1,
	state: 'applied' | 'draft' | 'validated' = 'applied',
	values = settings
): service.CaddyEnvelope {
	return {
		etag: `"${revision}-${state}"`,
		data: {
			siteId: 'site',
			sourceKey: 'source',
			kind: 'static',
			managementMode: 'platform',
			definition: { hosts: ['test.invalid'], listen: [':443'], root: '/srv/test' },
			present: true,
			syncedAt: '2026-01-01T00:00:00Z',
			capabilities: { convert: false, edit: true, validate: true, deploy: true },
			settingsCapabilities: { compression: true, cacheControl: true, securityHeaders: true },
			management: {
				revision,
				appliedRevision: state === 'applied' ? revision : 1,
				validatedRevision: state === 'validated' ? revision : null,
				settings: { ...values },
				sourceDigest: 'source-digest',
				updatedAt: '2026-01-01T00:00:00Z',
				history: [record(revision, state, values)]
			}
		}
	};
}
function Header() {
	const header = usePageHeaderContent();
	return (
		<div>
			{header?.content}
			{header?.actions}
		</div>
	);
}
function mount(search = '') {
	const router = createMemoryRouter(
		[
			{
				path: '/sites/:siteId/caddy',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope="caddy">
								<Header />
								<CaddySitePage />
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <div>route-crash</div>
			}
		],
		{ initialEntries: [`/sites/site/caddy${search}`] }
	);
	render(<RouterProvider router={router} />);
	return router;
}
async function choose(label: string, value: string) {
	fireEvent.mouseDown(await screen.findByRole('combobox', { name: label }));
	fireEvent.click(await screen.findByText(value, { selector: '.ant-select-item-option-content' }));
}
function button(label: string) {
	return screen.getByRole('button', { name: label }) as HTMLButtonElement;
}
async function confirm(title: string) {
	const dialog = await screen.findByRole('dialog', { name: title });
	fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
	await waitFor(() => expect(screen.queryByRole('dialog', { name: title })).toBeNull());
}
beforeEach(() => {
	vi.stubEnv('NODE_ENV', 'development');
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	runtime.role = 'owner';
	runtime.state = { status: 'authenticated', user: { id: 'admin', platformRole: 'admin' } };
	vi.mocked(service.getCaddyConfig).mockResolvedValue(config());
});
afterEach(() => {
	cleanup();
	vi.unstubAllEnvs();
	vi.unstubAllGlobals();
});

it('saves a conditional draft, revalidates it and requires confirmation before deployment', async () => {
	const changed = { ...settings, compression: 'gzip' as const };
	vi.mocked(service.changeCaddyConfig)
		.mockResolvedValueOnce(config(2, 'draft', changed))
		.mockResolvedValueOnce(config(2, 'validated', changed))
		.mockResolvedValueOnce(config(2, 'validated', changed))
		.mockResolvedValueOnce(config(2, 'applied', changed));
	mount();
	await choose('Compression', 'gzip');
	fireEvent.click(button('Save draft'));
	await waitFor(() =>
		expect(service.changeCaddyConfig).toHaveBeenNthCalledWith(1, 'site', 'draft', '"1-applied"', {
			settings: changed
		})
	);
	await waitFor(() => expect(button('Validate').disabled).toBe(false));
	expect(button('Deploy configuration').disabled).toBe(true);
	fireEvent.click(button('Validate'));
	await waitFor(() =>
		expect(service.changeCaddyConfig).toHaveBeenNthCalledWith(
			2,
			'site',
			'validate',
			'"2-draft"',
			{}
		)
	);
	await waitFor(() => expect(button('Deploy configuration').disabled).toBe(false));
	expect(button('Validate').disabled).toBe(false);
	fireEvent.click(button('Validate'));
	await waitFor(() =>
		expect(service.changeCaddyConfig).toHaveBeenNthCalledWith(
			3,
			'site',
			'validate',
			'"2-validated"',
			{}
		)
	);
	await waitFor(() => expect(button('Deploy configuration').disabled).toBe(false));
	fireEvent.click(button('Deploy configuration'));
	expect(service.changeCaddyConfig).toHaveBeenCalledTimes(3);
	await confirm('Deploy Caddy configuration');
	expect(service.changeCaddyConfig).toHaveBeenNthCalledWith(
		4,
		'site',
		'deploy',
		'"2-validated"',
		{}
	);
	await waitFor(() => expect(button('Save draft').disabled).toBe(true));
	expect(button('Deploy configuration').disabled).toBe(true);
	expect(screen.queryByText('route-crash')).toBeNull();
});

it('retains the local draft and original ETag when saving conflicts', async () => {
	vi.mocked(service.changeCaddyConfig).mockRejectedValue(
		new ApiError(412, 'precondition_failed', '', '"new"')
	);
	mount();
	await choose('Compression', 'gzip');
	fireEvent.click(button('Save draft'));
	expect(
		await screen.findByText(
			'The settings changed on the server after you loaded them. Your edits are still in the form.'
		)
	).toBeDefined();
	expect(button('Save draft').disabled).toBe(false);
	fireEvent.click(button('Save draft'));
	await waitFor(() => expect(service.changeCaddyConfig).toHaveBeenCalledTimes(2));
	expect(service.changeCaddyConfig).toHaveBeenLastCalledWith('site', 'draft', '"1-applied"', {
		settings: { ...settings, compression: 'gzip' }
	});
});

it('converts only with a backend capability and preserves a separate deployment step', async () => {
	const imported = config();
	imported.data.managementMode = 'caddy';
	imported.data.management = null;
	imported.data.capabilities = { convert: true, edit: false, validate: false, deploy: false };
	vi.mocked(service.getCaddyConfig).mockResolvedValue(imported);
	vi.mocked(service.changeCaddyConfig).mockResolvedValue(config());
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Convert to platform management' }));
	expect(service.changeCaddyConfig).not.toHaveBeenCalled();
	expect(
		await screen.findByText(/Current running content and existing rules are retained/)
	).toBeDefined();
	await confirm('Convert configuration management');
	expect(service.changeCaddyConfig).toHaveBeenCalledExactlyOnceWith(
		'site',
		'convert',
		'"1-applied"',
		{}
	);
	expect(button('Save draft').disabled).toBe(true);
	expect(button('Deploy configuration').disabled).toBe(true);
});

it('keeps an unregistered static source read-only rather than inferring conversion from kind', async () => {
	const imported = config();
	imported.data.management = null;
	imported.data.managementMode = 'caddy';
	imported.data.capabilities = { convert: false, edit: false, validate: false, deploy: false };
	imported.data.unavailableReason = 'operator_registration_required';
	vi.mocked(service.getCaddyConfig).mockResolvedValue(imported);
	mount();
	expect(
		await screen.findByText('An operator must register configuration-write access for this site.')
	).toBeDefined();
	expect(screen.queryByRole('button', { name: 'Convert to platform management' })).toBeNull();
	expect(screen.queryByRole('combobox')).toBeNull();
	expect(service.changeCaddyConfig).not.toHaveBeenCalled();
});

it.each([
	['viewer', 'admin'],
	['owner', 'member']
])(
	'prevents changes for site %s and platform %s despite stale capabilities',
	async (siteRole, platformRole) => {
		runtime.role = siteRole;
		runtime.state = { ...runtime.state, user: { ...runtime.state.user, platformRole } };
		mount();
		expect(
			await screen.findByText(
				'Configuration changes require a platform administrator with the site owner role.'
			)
		).toBeDefined();
		expect(
			(screen.getByRole('combobox', { name: 'Compression' }) as HTMLInputElement).disabled
		).toBe(true);
		expect(screen.queryByRole('button', { name: 'Save draft' })).toBeNull();
		expect(screen.queryByRole('button', { name: 'Deploy configuration' })).toBeNull();
	}
);

it('honors each registered setting capability', async () => {
	const limited = config();
	limited.data.settingsCapabilities = {
		compression: true,
		cacheControl: false,
		securityHeaders: false
	};
	vi.mocked(service.getCaddyConfig).mockResolvedValue(limited);
	mount();
	expect(
		((await screen.findByRole('combobox', { name: 'Compression' })) as HTMLInputElement).disabled
	).toBe(false);
	expect(
		(screen.getByRole('combobox', { name: 'Cache-Control' }) as HTMLInputElement).disabled
	).toBe(true);
	expect(
		(screen.getByRole('combobox', { name: 'Security response headers' }) as HTMLInputElement)
			.disabled
	).toBe(true);
});

it('confirms a historical applied revision before rolling back with the latest ETag', async () => {
	const current = config(3);
	current.data.management!.history = [record(1), record(2, 'draft'), record(3)];
	vi.mocked(service.getCaddyConfig).mockResolvedValue(current);
	vi.mocked(service.changeCaddyConfig).mockResolvedValue(config(4));
	mount('?tab=history');
	fireEvent.click(await screen.findByRole('button', { name: 'Roll back and deploy' }));
	expect(service.changeCaddyConfig).not.toHaveBeenCalled();
	await confirm('Restore applied configuration');
	expect(service.changeCaddyConfig).toHaveBeenCalledExactlyOnceWith(
		'site',
		'rollback',
		'"3-applied"',
		{ revision: 1 }
	);
});

it('shows an uncertain runtime state without exposing write actions', async () => {
	const unavailable = config(2, 'validated');
	unavailable.data.capabilities = { convert: false, edit: false, validate: false, deploy: false };
	unavailable.data.unavailableReason = 'caddy_runtime_uncertain';
	vi.mocked(service.getCaddyConfig).mockResolvedValue(unavailable);
	mount();
	expect(
		await screen.findByText(
			'The reload state is uncertain. An operator must verify the active Caddy configuration.'
		)
	).toBeDefined();
	expect(screen.queryByRole('button', { name: 'Deploy configuration' })).toBeNull();
});

it('dismisses a deployment confirmation when navigating to a different site', async () => {
	// Node's Request rejects jsdom's AbortSignal during router navigation.
	const NativeRequest = globalThis.Request;
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init ? { ...init, signal: undefined } : init);
			}
		}
	);
	vi.mocked(service.getCaddyConfig).mockResolvedValue(config(2, 'validated'));
	const router = mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Deploy configuration' }));
	await screen.findByRole('dialog', { name: 'Deploy Caddy configuration' });
	const next = config();
	next.data.siteId = 'other';
	vi.mocked(service.getCaddyConfig).mockResolvedValue(next);
	await act(async () => {
		await router.navigate('/sites/other/caddy');
	});
	await waitFor(() =>
		expect(screen.queryByRole('dialog', { name: 'Deploy Caddy configuration' })).toBeNull()
	);
	expect(service.changeCaddyConfig).not.toHaveBeenCalled();
});
