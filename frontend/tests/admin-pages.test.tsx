// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import TopBar from '../src/components/layout/TopBar';
import UsersPage from '../src/pages/UsersPage';
import LogsPage from '../src/pages/LogsPage';
import { setLocale } from '../src/lib/i18n';
import * as service from '../src/lib/api/admin';
import { getVisibleButton } from './visible-button';

const NativeRequest = globalThis.Request;

const runtime = vi.hoisted(() => ({
	generation: 0,
	state: {
		status: 'authenticated',
		user: {
			id: '11111111-1111-1111-1111-111111111111',
			username: 'admin',
			displayName: 'Admin',
			platformRole: 'admin'
		},
		platformInitialized: true,
		passkeyBound: true,
		passkeyAvailable: true
	},
	listeners: new Set<() => void>()
}));
vi.mock('../src/lib/session', () => ({
	sessionSnapshot: () => runtime.state,
	subscribeSession: (listener: () => void) => {
		runtime.listeners.add(listener);
		return () => runtime.listeners.delete(listener);
	},
	loadSession: vi.fn()
}));
vi.mock('../src/lib/api/client', async (original) => ({
	...(await original<typeof import('../src/lib/api/client')>()),
	currentAuthGeneration: () => runtime.generation
}));
vi.mock('../src/lib/sites/site-list', () => ({
	loadSiteList: vi.fn().mockResolvedValue(undefined)
}));
vi.mock('../src/lib/api/admin', async (original) => ({
	...(await original<typeof service>()),
	listUsers: vi.fn(),
	getUser: vi.fn(),
	createUser: vi.fn(),
	updateUser: vi.fn(),
	setUserRole: vi.fn(),
	setUserStatus: vi.fn(),
	deleteUser: vi.fn(),
	issueUserCode: vi.fn(),
	setSiteAccess: vi.fn(),
	revokeSessions: vi.fn(),
	revokePasskey: vi.fn(),
	listAudit: vi.fn()
}));

const member: service.AdminUser = {
	id: '22222222-2222-2222-2222-222222222222',
	username: 'member',
	displayName: 'Member',
	platformRole: 'member',
	status: 'active',
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};
function detail(): service.AdminUserDetail {
	return {
		user: { ...member },
		access: {
			sites: [
				{
					siteId: '33333333-3333-3333-3333-333333333333',
					name: 'Demo',
					slug: 'demo',
					role: 'viewer'
				}
			],
			passkeys: [
				{
					id: '44444444-4444-4444-4444-444444444444',
					rpId: 'localhost',
					currentRp: true,
					createdAt: member.createdAt,
					lastUsedAt: null,
					backupEligible: false,
					backupState: false
				}
			],
			sessions: [
				{
					id: '55555555-5555-5555-5555-555555555555',
					kind: 'full',
					current: false,
					createdAt: member.createdAt,
					lastSeenAt: member.createdAt,
					expiresAt: '2027-01-01T00:00:00Z'
				}
			]
		}
	};
}
function pending<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}
function Header() {
	const header = usePageHeaderContent();
	return (
		<div data-testid="header">
			{header?.content}
			{header?.actions}
		</div>
	);
}
function mount(Page = UsersPage, search = '') {
	const router = createMemoryRouter([
		{
			path: '*',
			element: (
				<ConfigProvider theme={{ token: { motion: false } }}>
					<AntApp>
						<PageHeaderProvider scope="admin">
							<Header />
							<Page />
						</PageHeaderProvider>
					</AntApp>
				</ConfigProvider>
			),
			errorElement: <div>route-crash</div>
		}
	], { initialEntries: [`/users${search}`] });
	render(<RouterProvider router={router} />);
	return router;
}
async function openCreate() {
	fireEvent.click(await screen.findByRole('button', { name: 'Create user' }));
	return screen.findByRole('dialog', { name: 'Create user' });
}
async function openMember() {
	fireEvent.click(await screen.findByRole('button', { name: 'Manage' }));
	return screen.findByRole('dialog', { name: 'member' });
}
async function choose(input: HTMLElement, label: string) {
	fireEvent.mouseDown(input);
	const option = await screen.findByText(label, { selector: '.ant-select-item-option-content' });
	fireEvent.click(option);
}

beforeEach(() => {
	// rc-util returns the same "test-id" for every control in test mode,
	// breaking dialog aria-labelledby when selects share the document.
	vi.stubEnv('NODE_ENV', 'development');
	vi.stubGlobal('Request', class extends NativeRequest {
		constructor(input: RequestInfo | URL, init?: RequestInit) {
			super(input, { ...init, signal: undefined });
		}
	});
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	runtime.generation++;
	runtime.state = {
		...runtime.state,
		user: { ...runtime.state.user, id: '11111111-1111-1111-1111-111111111111' }
	};
	vi.mocked(service.listUsers).mockResolvedValue({
		users: [{ ...member }],
		total: 1,
		page: 1,
		pageSize: 50
	});
	vi.mocked(service.getUser).mockResolvedValue(detail());
	vi.mocked(service.listAudit).mockResolvedValue({
		entries: [],
		total: 0,
		page: 1,
		pageSize: 50,
		operations: []
	});
});
afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.unstubAllEnvs();
	vi.unstubAllGlobals();
});

it('recovers a failed user list through the real header and error controls', async () => {
	vi.mocked(service.listUsers).mockRejectedValueOnce(new TypeError('offline'));
	mount();
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	expect(await screen.findByRole('button', { name: 'Manage' })).toBeDefined();
	expect(screen.queryByText('route-crash')).toBeNull();
});

it('creates an account once and clears the once-returned code when closed', async () => {
	const response = pending<service.AccountCode>();
	vi.mocked(service.createUser).mockReturnValue(response.promise);
	mount();
	const dialog = await openCreate();
	fireEvent.change(within(dialog).getByRole('textbox', { name: 'Username' }), {
		target: { value: 'new-user' }
	});
	fireEvent.change(within(dialog).getByRole('textbox', { name: 'Display name' }), {
		target: { value: 'New User' }
	});
	fireEvent.click(within(dialog).getByRole('button', { name: 'Create user' }));
	await waitFor(() => expect(service.createUser).toHaveBeenCalledTimes(1));
	expect(service.createUser).toHaveBeenCalledWith({
		username: 'new-user',
		displayName: 'New User'
	});
	await act(async () => {
		response.resolve({ user: member, code: 'ONE-TIME-CODE', expiresAt: '2027-01-01T00:00:00Z' });
	});
	const code = await screen.findByRole('dialog', { name: 'Registration or recovery code' });
	expect(within(code).getByText('ONE-TIME-CODE')).toBeDefined();
	fireEvent.click(within(code).getAllByRole('button', { name: 'Close' }).at(-1)!);
	await waitFor(() => expect(screen.queryByText('ONE-TIME-CODE')).toBeNull());
});

it('keeps profile edits when cancelling close and installs the canonical saved name', async () => {
	vi.mocked(service.updateUser).mockResolvedValue({ ...member, displayName: 'Canonical' });
	mount();
	const drawer = await openMember();
	const input = within(drawer).getByRole('textbox', { name: 'Display name' }) as HTMLInputElement;
	fireEvent.change(input, { target: { value: 'Edited' } });
	fireEvent.click(within(drawer).getByRole('button', { name: 'Close' }));
	const confirmation = await screen.findByRole('dialog', { name: 'Unsaved changes' });
	fireEvent.click(within(confirmation).getByRole('button', { name: 'Keep editing' }));
	expect(input.value).toBe('Edited');
	fireEvent.click(within(drawer).getByRole('button', { name: 'Save profile' }));
	await waitFor(() => expect(input.value).toBe('Canonical'));
	expect(service.updateUser).toHaveBeenCalledWith(member.id, {
		username: 'member',
		displayName: 'Edited'
	});
});

it('requires confirmation before elevating a platform role', async () => {
	vi.mocked(service.setUserRole).mockResolvedValue({ ...member, platformRole: 'admin' });
	mount();
	const drawer = await openMember();
	await choose(
		within(drawer).getByRole('combobox', { name: 'Platform role' }),
		'Platform administrator'
	);
	expect(service.setUserRole).not.toHaveBeenCalled();
	const confirmation = await screen.findByRole('dialog', { name: 'Change platform role · member' });
	fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
	await waitFor(() => expect(service.setUserRole).toHaveBeenCalledWith(member.id, 'admin'));
});

it('revokes a specific session only after confirmation', async () => {
	vi.mocked(service.revokeSessions).mockResolvedValue(undefined);
	mount();
	const drawer = await openMember();
	const sessions = within(drawer)
		.getByRole('heading', { name: 'Active sessions' })
		.closest('section')!;
	const row = within(sessions).getByText('Full access').closest('tr')!;
	fireEvent.click(getVisibleButton(row, 'Revoke'));
	expect(service.revokeSessions).not.toHaveBeenCalled();
	const confirmation = await screen.findByRole('dialog', { name: 'Revoke · member' });
	fireEvent.click(within(confirmation).getByRole('button', { name: 'Confirm' }));
	await waitFor(() =>
		expect(service.revokeSessions).toHaveBeenCalledWith(
			member.id,
			'55555555-5555-5555-5555-555555555555'
		)
	);
});

it('does not show a registration code returned after changing accounts', async () => {
	const response = pending<service.AccountCode>();
	vi.mocked(service.createUser).mockReturnValue(response.promise);
	mount();
	const dialog = await openCreate();
	fireEvent.change(within(dialog).getByRole('textbox', { name: 'Username' }), {
		target: { value: 'old-user' }
	});
	fireEvent.click(within(dialog).getByRole('button', { name: 'Create user' }));
	await waitFor(() => expect(service.createUser).toHaveBeenCalledTimes(1));
	await act(async () => {
		runtime.generation++;
		runtime.state = {
			...runtime.state,
			user: { ...runtime.state.user, id: '66666666-6666-6666-6666-666666666666' }
		};
		runtime.listeners.forEach((listener) => listener());
	});
	await act(async () => {
		response.resolve({ user: member, code: 'STALE-SECRET', expiresAt: '2027-01-01T00:00:00Z' });
	});
	expect(screen.queryByText('STALE-SECRET')).toBeNull();
	expect(screen.queryByRole('dialog', { name: 'Registration or recovery code' })).toBeNull();
});

it('never applies a prior account list after the account generation changes', async () => {
	const old = pending<service.AdminUserPage>();
	vi.mocked(service.listUsers)
		.mockReturnValueOnce(old.promise)
		.mockResolvedValue({ users: [], total: 0, page: 1, pageSize: 50 });
	mount();
	await waitFor(() => expect(service.listUsers).toHaveBeenCalledTimes(1));
	await act(async () => {
		runtime.generation++;
		runtime.state = {
			...runtime.state,
			user: { ...runtime.state.user, id: '77777777-7777-7777-7777-777777777777' }
		};
		runtime.listeners.forEach((listener) => listener());
	});
	expect(await screen.findByText('No matching accounts')).toBeDefined();
	await act(async () => {
		old.resolve({ users: [member], total: 1, page: 1, pageSize: 50 });
	});
	expect(screen.queryByRole('button', { name: 'Manage' })).toBeNull();
});

it('loads user batches without pagination and resets them when sorting changes', async () => {
	vi.mocked(service.listUsers).mockImplementation(async (query) => {
		const params = new URLSearchParams(query);
		const page = Number(params.get('page'));
		return { users: [page === 1 ? member : { ...member, id: 'second', username: 'second' }], total: 100, page, pageSize: 50 };
	});
	mount();
	await screen.findByText('member');
	expect(screen.queryByLabelText('Page 2')).toBeNull();
	fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
	expect(await screen.findByText('second')).toBeDefined();
	fireEvent.click(screen.getByRole('columnheader', { name: /Username/ }));
	await waitFor(() => {
		const params = new URLSearchParams(vi.mocked(service.listUsers).mock.calls.at(-1)?.[0]);
		expect(params.get('page')).toBe('1');
		expect(params.get('sort')).toBe('username');
		expect(params.get('order')).toBe('asc');
	});
	await waitFor(() => expect(screen.queryByText('second')).toBeNull());
});

it('requests another user batch when the last row enters view', async () => {
	let notify: IntersectionObserverCallback | undefined;
	const observe = vi.fn();
	vi.stubGlobal('IntersectionObserver', class {
		constructor(callback: IntersectionObserverCallback) { notify = callback; }
		observe = observe;
		disconnect = vi.fn();
	});
	vi.mocked(service.listUsers).mockImplementation(async (query) => {
		const page = Number(new URLSearchParams(query).get('page'));
		return { users: [{ ...member, id: `id-${page}`, username: `account-${page}` }], total: 100, page, pageSize: 50 };
	});
	mount();
	await screen.findByText('account-1');
	await waitFor(() => expect(observe).toHaveBeenCalledWith(screen.getByText('account-1').closest('tr')));
	act(() => notify?.([{ isIntersecting: true } as IntersectionObserverEntry], {} as IntersectionObserver));
	expect(await screen.findByText('account-2')).toBeDefined();
});

it('loads audit batches and preserves the selected order on subsequent requests', async () => {
	vi.mocked(service.listAudit).mockImplementation(async (query) => {
		const params = new URLSearchParams(query);
		const page = Number(params.get('page'));
		return { entries: [{
			id: `event-${page}`, createdAt: member.createdAt, actorId: member.id,
			actorName: `actor-${page}`, operation: 'setUserRole', resourceId: member.id,
			siteId: null, method: 'PUT', route: '/test', peerIp: '', status: 200,
			outcome: 'success', details: {}
		}], total: 100, page, pageSize: 50, operations: [] };
	});
	mount(LogsPage);
	await screen.findByText('actor-1');
	fireEvent.click(screen.getByRole('columnheader', { name: /Operator/ }));
	await waitFor(() => expect(new URLSearchParams(vi.mocked(service.listAudit).mock.calls.at(-1)?.[0]).get('sort')).toBe('actorName'));
	fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
	expect(await screen.findByText('actor-2')).toBeDefined();
	const params = new URLSearchParams(vi.mocked(service.listAudit).mock.calls.at(-1)?.[0]);
	expect(params.get('page')).toBe('2');
	expect(params.get('order')).toBe('asc');
	expect(screen.queryByLabelText('Page 2')).toBeNull();
});

it('filters logs by outcome and expands metadata without inventing record details', async () => {
	const event: service.AuditEvent = {
		id: '88888888-8888-8888-8888-888888888888',
		createdAt: member.createdAt,
		actorId: member.id,
		actorName: member.username,
		operation: 'setUserRole',
		resourceId: member.id,
		siteId: null,
		method: 'PUT',
		route: '/api/v1/admin/users/{userId}/role',
		peerIp: '127.0.0.1',
		status: 200,
		outcome: 'success',
		details: {
			targetUsername: 'target.member',
			field: 'platformRole',
			before: 'member',
			after: 'admin'
		}
	};
	vi.mocked(service.listAudit).mockResolvedValue({
		entries: [event],
		total: 1,
		page: 1,
		pageSize: 50,
		operations: ['setUserRole']
	});
	mount(LogsPage);
	expect(await screen.findByText('Change platform role')).toBeDefined();
	const disclosure = await screen.findByText('Record details', { selector: 'summary' });
	fireEvent.click(disclosure);
	expect((disclosure.closest('details') as HTMLDetailsElement).open).toBe(true);
	expect(await screen.findByText('/api/v1/admin/users/', { exact: false })).toBeDefined();
	expect(screen.getByText('Before')).toBeDefined();
	expect(screen.getByText('After')).toBeDefined();
	expect(screen.getByText('Platform administrator')).toBeDefined();
	vi.mocked(service.listAudit).mockResolvedValue({
		entries: [],
		total: 0,
		page: 1,
		pageSize: 50,
		operations: ['setUserRole']
	});
	await choose(screen.getByRole('combobox', { name: 'Result' }), 'Failed');
	await waitFor(() =>
		expect(
			new URLSearchParams(vi.mocked(service.listAudit).mock.calls.at(-1)?.[0]).get('outcome')
		).toBe('failure')
	);
	expect(await screen.findByText('No matching events')).toBeDefined();
});

// The real top bar renders page filters and actions exactly once and keeps
// every control operable next to the locale, theme, account, and navigation
// entries.
it('renders UsersPage filters and actions once in the real top bar and keeps them operable', async () => {
	let navigationActivations = 0;
	const router = createMemoryRouter([
		{
			path: '*',
			element: (
				<ConfigProvider theme={{ token: { motion: false } }}>
					<AntApp>
						<PageHeaderProvider scope="admin">
							<TopBar
								navigationSlot={
									<button
										type="button"
										aria-label="Open navigation"
										onClick={() => {
											navigationActivations += 1;
										}}
									>
										Open navigation
									</button>
								}
							/>
							<UsersPage />
						</PageHeaderProvider>
					</AntApp>
				</ConfigProvider>
			),
			errorElement: <div>route-crash</div>
		}
	]);
	render(<RouterProvider router={router} />);

	expect(screen.getAllByLabelText('Search users')).toHaveLength(1);
	expect(screen.getAllByLabelText('Platform role')).toHaveLength(1);
	expect(screen.getAllByLabelText('Status')).toHaveLength(1);
	expect(screen.getAllByRole('button', { name: 'Refresh' })).toHaveLength(1);
	expect(screen.getAllByRole('button', { name: 'Create user' })).toHaveLength(1);

	// Typing keeps its value and the refresh still drives the query.
	await screen.findByText('member');
	const search = screen.getByLabelText('Search users');
	expect(
		screen.getByRole('button', { name: 'Create user' }).compareDocumentPosition(search) &
			Node.DOCUMENT_POSITION_FOLLOWING
	).toBeTruthy();
	fireEvent.change(search, { target: { value: 'member' } });
	expect((search as HTMLInputElement).value).toBe('member');
	fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
	await waitFor(() => expect(vi.mocked(service.listUsers)).toHaveBeenCalledTimes(2));

	// Locale, account, and navigation entries stay reachable beside the filters.
	// aria-label carries the target locale code, mirroring the top bar.
	expect(screen.getByRole('button', { name: 'zh-CN' })).toBeDefined();
	// The account button keeps the username as its accessible name.
	expect(screen.getByRole('button', { name: 'admin' })).toBeDefined();

	// The navigation slot hosts a live button whose click behavior works.
	const navigationButton = screen.getByRole('button', { name: 'Open navigation' });
	expect(navigationButton).toBeDefined();
	fireEvent.click(navigationButton);
	expect(navigationActivations).toBe(1);
});

it('opens a user detail from the URL and clears the query when it closes', async () => {
	const router = mount(UsersPage, `?user=${member.id}`);
	const drawer = await screen.findByRole('dialog', { name: 'member' });
	await within(drawer).findByRole('textbox', { name: 'Display name' });
	fireEvent.click(within(drawer).getByRole('button', { name: 'Close' }));
	await waitFor(() => expect(router.state.location.search).toBe(''));
	expect(screen.queryByRole('dialog', { name: 'member' })).toBeNull();
});
