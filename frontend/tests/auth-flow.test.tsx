/**
 * Auth page tests: LoginPage and BindPasskeyPage rendering and routing
 * behavior across the anonymous / restricted / authenticated states.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const alice = { id: '11111111-1111-1111-1111-111111111111', username: 'alice', displayName: 'Alice' };

type MockSession = {
	status: string;
	user: typeof alice | null;
	platformInitialized: boolean;
	passkeyBound: boolean;
	passkeyAvailable: boolean;
};

let mockSession: MockSession;
const mockSignInWithCode = vi.fn();
const mockSignInWithPasskey = vi.fn();
const mockBindPasskey = vi.fn();
const mockSignOut = vi.fn();

vi.mock('../src/lib/session', () => ({
	subscribeSession: () => () => undefined,
	sessionSnapshot: () => mockSession,
	signInWithCode: (...args: unknown[]) => mockSignInWithCode(...args),
	signInWithPasskey: (...args: unknown[]) => mockSignInWithPasskey(...args),
	bindPasskey: (...args: unknown[]) => mockBindPasskey(...args),
	signOut: (...args: unknown[]) => mockSignOut(...args),
	loadSession: vi.fn()
}));

import LoginPage from '../src/pages/LoginPage';
import BindPasskeyPage from '../src/pages/BindPasskeyPage';
import { setLocale } from '../src/lib/i18n';

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
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('zh-CN');
});

afterEach(() => {
	cleanup();
});

function mountPage(element: React.ReactElement, initial: string) {
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[initial]}>
				<Routes>
					<Route path="/login" element={element} />
					<Route path="/register-passkey" element={element} />
					<Route path="/sites" element={<div>sites-page</div>} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

describe('LoginPage', () => {
	it('shows the CLI guidance AND the redeem form before initialization', () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: false,
			passkeyBound: false,
			passkeyAvailable: true
		};
		mountPage(<LoginPage />, '/login');
		expect(screen.getByText(zh['login.initTitle'])).toBeDefined();
		expect(screen.getByPlaceholderText('XXXX-XXXX-XXXX-XXXX')).toBeDefined();
	});

	it('leads with the passkey button once initialized', () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		mountPage(<LoginPage />, '/login');
		expect(screen.getByText(zh['login.passkeyAction'])).toBeDefined();
		expect(screen.queryByText(zh['login.initTitle'])).toBeNull();
	});

	it('disables the passkey entry when ceremonies are unavailable', () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: false
		};
		mountPage(<LoginPage />, '/login');
		const button = screen
			.getAllByRole('button')
			.find((b) => (b.textContent ?? '').includes(zh['login.passkeyAction']));
		expect((button as HTMLButtonElement | undefined)?.disabled).toBe(true);
		expect(screen.getByText(zh['login.passkeyUnavailable'])).toBeDefined();
	});

	it('redeems a code through the store', async () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		mockSignInWithCode.mockResolvedValue('restricted');
		mountPage(<LoginPage />, '/login');
		fireEvent.change(screen.getByPlaceholderText('XXXX-XXXX-XXXX-XXXX'), {
			target: { value: 'AB2C-D4EF-GH2J-KL3M' }
		});
		const submit = screen
			.getAllByRole('button')
			.find((b) => (b.textContent ?? '').replace(/\s/g, '') === zh['login.action']);
		fireEvent.click(submit as HTMLButtonElement);
		await waitFor(() => {
			expect(mockSignInWithCode).toHaveBeenCalledWith('AB2C-D4EF-GH2J-KL3M');
		});
	});
});

describe('BindPasskeyPage', () => {
	it('renders the real account and the bind action for a restricted session', () => {
		mockSession = {
			status: 'restricted',
			user: alice,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		mountPage(<BindPasskeyPage />, '/register-passkey');
		expect(screen.getAllByText(zh['bind.title']).length).toBeGreaterThan(0);
		expect(
			screen.getByText((zh['bind.description'] as string).replace('{username}', 'alice'))
		).toBeDefined();
	});

	it('redirects an authenticated session to the console', () => {
		mockSession = {
			status: 'authenticated',
			user: alice,
			platformInitialized: true,
			passkeyBound: true,
			passkeyAvailable: true
		};
		mountPage(<BindPasskeyPage />, '/register-passkey');
		expect(screen.getByText('sites-page')).toBeDefined();
	});

	it('redirects an anonymous browser to login', () => {
		mockSession = {
			status: 'anonymous',
			user: null,
			platformInitialized: true,
			passkeyBound: false,
			passkeyAvailable: true
		};
		mountPage(<BindPasskeyPage />, '/register-passkey');
		expect(screen.queryByText(zh['bind.title'])).toBeNull();
	});
});
