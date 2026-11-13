/**
 * AccountSettingsPage: the rename flow keeps the draft on failure, applies
 * the fresh username to the session store on success, and only enables the
 * save action once the draft actually differs from the stored value.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, fireEvent, act } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import { ApiError } from '../src/lib/api/client';
import type { User } from '../src/lib/api/management';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const alice: User = { id: 'u1', username: 'alice', displayName: 'Alice', platformRole: 'member' };

let mockSession: { status: string; user: User | null };
const renameCurrentUser = vi.fn();
const sessionListeners = new Set<() => void>();

/** Swaps the session and notifies the subscribers, mirroring the real
 * store's emit. */
function setSession(next: { status: string; user: User | null }): void {
	mockSession = next;
	for (const listener of sessionListeners) listener();
}

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		updateAccountProfile: vi.fn()
	};
});
vi.mock('../src/lib/session', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/session')>();
	return {
		...actual,
		sessionSnapshot: () => mockSession,
		subscribeSession: (listener: () => void) => {
			sessionListeners.add(listener);
			return () => {
				sessionListeners.delete(listener);
			};
		},
		renameCurrentUser: (...args: unknown[]) => renameCurrentUser(...args)
	};
});

import { updateAccountProfile } from '../src/lib/api/management';
import AccountSettingsPage from '../src/pages/AccountSettingsPage';

beforeAll(() => {
	if (!window.matchMedia) {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			value: (query: string) => ({
				matches: false,
				media: query,
				onchange: null,
				addListener: () => undefined,
				removeListener: () => undefined,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				dispatchEvent: () => false
			})
		});
	}
});

beforeEach(() => {
	vi.mocked(updateAccountProfile).mockReset();
	renameCurrentUser.mockReset();
	sessionListeners.clear();
	mockSession = { status: 'authenticated', user: alice };
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

function mount() {
	// A data router is required: the draft guard's useBlocker rejects the
	// declarative <MemoryRouter> mode.
	const router = createMemoryRouter([{ path: '/account', element: <AccountSettingsPage /> }], {
		initialEntries: ['/account']
	});
	return render(
		<ConfigProvider>
			<RouterProvider router={router} />
		</ConfigProvider>
	);
}

function findButton(label: string): HTMLButtonElement {
	const button = screen
		.getAllByRole('button')
		.find((b) => (b.textContent ?? '').replace(/\s/g, '') === label);
	if (!(button instanceof HTMLButtonElement)) throw new Error(`button ${label} missing`);
	return button;
}

async function settled(rounds = 12) {
	for (let i = 0; i < rounds; i++) await Promise.resolve();
}

describe('AccountSettingsPage rename flow', () => {
	it('keeps the save action disabled until the draft differs from the stored username', async () => {
		mount();
		await waitFor(() =>
			expect((screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement).value).toBe(
				'alice'
			)
		);
		expect(findButton(zh['common.save'] as string).disabled).toBe(true);
		fireEvent.change(screen.getByLabelText(zh['account.usernameLabel']), {
			target: { value: 'alice.renamed' }
		});
		expect(findButton(zh['common.save'] as string).disabled).toBe(false);
		// A dirty draft is not a validation failure: no warning state.
		expect(document.querySelector('.ant-form-item-has-warning')).toBeNull();
		expect(document.querySelector('.ant-form-item-status-warning')).toBeNull();
	});

	it('applies the fresh username to the session store and clears the draft on success', async () => {
		vi.mocked(updateAccountProfile).mockResolvedValue({ ...alice, username: 'alice.renamed' });
		mount();
		const input = await waitFor(() => {
			const element = screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement;
			expect(element.value).toBe('alice');
			return element;
		});
		fireEvent.change(input, { target: { value: 'alice.renamed' } });
		await act(async () => {
			fireEvent.click(findButton(zh['common.save'] as string));
		});
		await waitFor(() => expect(screen.getByText(zh['account.saved'])).toBeDefined());

		expect(updateAccountProfile).toHaveBeenCalledWith('alice.renamed');
		expect(renameCurrentUser).toHaveBeenCalledWith('u1', 'alice.renamed');
	});

	it('keeps the draft and surfaces the failure when the rename is rejected', async () => {
		vi.mocked(updateAccountProfile).mockRejectedValue(
			new ApiError(409, 'username_taken', '', null)
		);
		mount();
		const input = await waitFor(() => {
			const element = screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement;
			expect(element.value).toBe('alice');
			return element;
		});
		fireEvent.change(input, { target: { value: 'alice.taken' } });
		await act(async () => {
			fireEvent.click(findButton(zh['common.save'] as string));
		});
		await waitFor(() =>
			expect(screen.getByText(zh['error.usernameTaken'] as string)).toBeDefined()
		);

		// The draft stays for another attempt; nothing was applied.
		expect((screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement).value).toBe(
			'alice.taken'
		);
		expect(renameCurrentUser).not.toHaveBeenCalled();
		await settled();
	});
});

describe('AccountSettingsPage cross-account guards', () => {
	it('a rename landing after switching to another account never applies', async () => {
		const bob: User = { id: 'u2', username: 'bob', displayName: 'Bob', platformRole: 'member' };
		let resolveRename!: (v: User) => void;
		vi.mocked(updateAccountProfile).mockImplementation(
			() =>
				new Promise<User>((resolve) => {
					resolveRename = resolve;
				})
		);
		mount();
		const input = await waitFor(() => {
			const element = screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement;
			expect(element.value).toBe('alice');
			return element;
		});
		fireEvent.change(input, { target: { value: 'alice.renamed' } });
		await act(async () => {
			fireEvent.click(findButton(zh['common.save'] as string));
		});
		expect(updateAccountProfile).toHaveBeenCalledWith('alice.renamed');

		// The logged-in account switches to Bob while the rename is in flight.
		await act(async () => {
			setSession({ status: 'authenticated', user: bob });
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		await waitFor(() =>
			expect((screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement).value).toBe(
				'bob'
			)
		);

		await act(async () => {
			resolveRename({ ...alice, username: 'alice.renamed' });
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		// A's late rename applied nothing: the store was never relabeled and
		// no success message appeared.
		expect(renameCurrentUser).not.toHaveBeenCalled();
		expect(screen.queryByText(zh['account.saved'])).toBeNull();
		expect((screen.getByLabelText(zh['account.usernameLabel']) as HTMLInputElement).value).toBe(
			'bob'
		);
	});
});
