// @vitest-environment jsdom
import { afterAll, afterEach, beforeEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import UpdatesPage from '../src/pages/UpdatesPage';
import { setLocale } from '../src/lib/i18n';
import * as service from '../src/lib/api/updates';

declare const process: { env: { NODE_ENV: string | undefined } };

vi.mock('../src/lib/api/updates', () => ({ readUpdates: vi.fn(), manageUpdates: vi.fn() }));
const originalMode = vi.hoisted(() => {
	const old = process.env.NODE_ENV;
	process.env.NODE_ENV = 'development';
	return old;
});
afterAll(() => {
	process.env.NODE_ENV = originalMode;
});

const initial = (): service.UpdateStatus => ({
	enabled: true,
	repository: 'owner/repo',
	channel: 'main',
	auto: false,
	pinned: false,
	checking: false,
	updating: false,
	tokenSet: false,
	managerCommit: 'a'.repeat(40),
	current: {
		version: '0.7.0',
		commit: 'a'.repeat(40),
		runId: 1,
		attempt: 1,
		branch: 'main',
		createdAt: '',
		url: ''
	},
	previous: null,
	versions: [
		{
			version: '0.8.0',
			commit: 'b'.repeat(40),
			runId: 2,
			attempt: 1,
			branch: 'main',
			createdAt: '2026-11-15T00:00:00Z',
			url: ''
		}
	],
	task: null,
	lastCheck: '',
	lastError: '',
	logs: []
});

function Header() {
	const header = usePageHeaderContent();
	return <header>{header?.actions}</header>;
}
function mount() {
	const router = createMemoryRouter(
		[
			{
				path: '/updates',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope="updates">
								<Header />
								<UpdatesPage />
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				)
			}
		],
		{ initialEntries: ['/updates'] }
	);
	return render(<RouterProvider router={router} />);
}
function button(label: string) {
	return screen.getByRole('button', {
		name: (name) => name.replace(/\s/g, '') === label.replace(/\s/g, '')
	});
}
beforeEach(() => {
	setLocale('en-US');
	vi.mocked(service.readUpdates).mockResolvedValue(initial());
	vi.mocked(service.manageUpdates).mockResolvedValue(initial());
});
afterEach(() => {
	cleanup();
	vi.clearAllMocks();
});

it('keeps header hooks stable across load, failure, retry and success', async () => {
	vi.mocked(service.readUpdates).mockRejectedValueOnce(new Error('offline'));
	mount();
	await screen.findByText('Updater status is unavailable. Refresh to verify the original task.');
	fireEvent.click(button('Retry'));
	await screen.findByText('Application and supervisor');
	expect(button('Check updates')).toBeTruthy();
	expect(button('Install latest').hasAttribute('disabled')).toBe(false);
});

it('shows the completed update check as an antd message without moving the page', async () => {
	mount();
	await screen.findByText('Application and supervisor');
	fireEvent.click(button('Check updates'));
	const notice = await screen.findByText('Update check completed.');
	expect(notice.closest('.ant-message')).not.toBeNull();
	expect(document.querySelector('.updates-page')?.textContent).not.toContain(
		'Update check completed.'
	);
});

it('requires confirmation and sends a checked commit and run, then retains the task receipt', async () => {
	vi.mocked(service.manageUpdates).mockImplementation(async (action) => ({
		...initial(),
		updating: action.action === 'deploy',
		task: {
			id: 'task-2',
			status: 'running',
			phase: 'downloading',
			commit: 'b'.repeat(40),
			startedAt: ''
		}
	}));
	mount();
	await screen.findByText('Application and supervisor');
	fireEvent.click(button('Install latest'));
	await screen.findByRole('dialog', { name: 'Deploy this application build?' });
	expect(service.manageUpdates).not.toHaveBeenCalled();
	fireEvent.click(button('Confirm'));
	await waitFor(() =>
		expect(service.manageUpdates).toHaveBeenCalledWith({
			action: 'deploy',
			runId: 2,
			commit: 'b'.repeat(40)
		})
	);
});

it('disables deployments while pinned and distinguishes an absent supervisor', async () => {
	vi.mocked(service.readUpdates).mockResolvedValue({ ...initial(), pinned: true });
	const view = mount();
	await screen.findByText('Application and supervisor');
	expect(button('Install latest').hasAttribute('disabled')).toBe(true);
	view.unmount();
	vi.mocked(service.readUpdates).mockResolvedValue({ ...initial(), enabled: false });
	mount();
	await screen.findByText('The platform update supervisor has not been configured.');
});

it('rejects a confirmation after another client changes the update channel', async () => {
	mount();
	await screen.findByText('Application and supervisor');
	fireEvent.click(button('Install latest'));
	await screen.findByRole('dialog', { name: 'Deploy this application build?' });
	vi.mocked(service.readUpdates).mockResolvedValue({ ...initial(), channel: 'dev', versions: [] });
	await act(async () => {
		fireEvent.click(button('Confirm'));
	});
	await waitFor(() => expect(service.readUpdates).toHaveBeenCalledTimes(2));
	expect(service.manageUpdates).not.toHaveBeenCalled();
});

it('polls the original receipt after a lost response without submitting twice', async () => {
	mount();
	await screen.findByText('Application and supervisor');
	fireEvent.click(button('Install latest'));
	await screen.findByRole('dialog', { name: 'Deploy this application build?' });
	vi.mocked(service.manageUpdates).mockImplementationOnce(async () => {
		vi.mocked(service.readUpdates).mockResolvedValue({
			...initial(),
			updating: true,
			task: {
				id: 'original-receipt',
				status: 'running',
				phase: 'switching',
				commit: 'b'.repeat(40),
				startedAt: ''
			}
		});
		throw new Error('connection interrupted');
	});
	fireEvent.click(button('Confirm'));
	await screen.findByText(
		'The result is awaiting confirmation. Refresh the original task; this action will not be submitted again automatically.'
	);
	await screen.findByText('original-receipt');
	expect(service.manageUpdates).toHaveBeenCalledTimes(1);
	expect(button('Install latest').hasAttribute('disabled')).toBe(true);
});
