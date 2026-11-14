/**
 * ThemesCatalogPage tests: a genuinely empty catalog (with the CLI
 * registration hint) and a load failure are distinct states — a failure is
 * never disguised as an empty directory.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const listThemes = vi.fn();
vi.mock('../src/lib/api/management', () => ({
	listThemes: (...args: unknown[]) => listThemes(...args)
}));

const mockSession = {
	status: 'authenticated',
	user: { id: 'u1', username: 'alice', displayName: 'Alice' },
	platformInitialized: true,
	passkeyBound: true,
	passkeyAvailable: true
};
vi.mock('../src/lib/session', () => ({
	subscribeSession: () => () => undefined,
	sessionSnapshot: () => mockSession
}));

import ThemesCatalogPage from '../src/pages/ThemesCatalogPage';
import { setLocale } from '../src/lib/i18n';
import { ApiError } from '../src/lib/api/client';

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

afterEach(cleanup);

function mountPage() {
	const router = createMemoryRouter([{ path: '/themes', element: <ThemesCatalogPage /> }], {
		initialEntries: ['/themes']
	});
	return render(
		<ConfigProvider>
			<RouterProvider router={router} />
		</ConfigProvider>
	);
}

describe('ThemesCatalogPage', () => {
	it('shows the real empty state with the CLI registration hint', async () => {
		listThemes.mockResolvedValue([]);
		mountPage();
		await waitFor(() => {
			expect(screen.getByText(zh['themes.emptyCatalog'])).toBeDefined();
		});
		expect(screen.getByText(zh['themes.registerHintShort'])).toBeDefined();
	});

	it('shows the failure state, not the empty directory', async () => {
		listThemes.mockRejectedValue(new ApiError(500, 'internal_error', 'boom', null));
		mountPage();
		await waitFor(() => {
			expect(screen.getByText(zh['error.generic'])).toBeDefined();
		});
		expect(screen.queryByText(zh['themes.emptyCatalog'])).toBeNull();
	});

	it('renders the full theme fields when the catalog has entries', async () => {
		listThemes.mockResolvedValue([
			{
				id: '5f0e858f-1d2a-4c1b-9c9d-000000000001',
				themeId: 'fuyao-homepage',
				name: 'Fuyao Homepage',
				version: '27.2.0',
				adapter: 'node22-sveltekit-static',
				inputProtocol: 'fuyao.theme.build-input/v1',
				capabilities: ['blog', 'albums'],
				outputs: {
					spaFallback: null,
					urlConventions: ['release.json', 'posts', 'data'],
					requiredArtifacts: ['client']
				},
				configSchema: { type: 'object' },
				manifestDigest: 'a'.repeat(64),
				registeredAt: '2026-11-01T00:00:00Z',
				updatedAt: '2026-11-01T00:00:00Z'
			}
		]);
		mountPage();
		await screen.findByText('Fuyao Homepage');
		expect(screen.getByText('fuyao-homepage @ 27.2.0')).toBeDefined();
		expect(screen.getByText('node22-sveltekit-static')).toBeDefined();
		expect(screen.getByText('blog')).toBeDefined();
		expect(screen.getByText('albums')).toBeDefined();
		// URL conventions and the artifact requirement render in one cell.
		expect(screen.getByText(/release\.json, posts, data/)).toBeDefined();
		expect(screen.getByText(/client/)).toBeDefined();
		expect(screen.getByText(zh['themes.schemaDeclared'])).toBeDefined();
		// The operator registration hint stays beside the populated catalog.
		expect(screen.getByText(zh['themes.registerHintShort'])).toBeDefined();
	});

	it('recovers through the retry action', async () => {
		listThemes.mockRejectedValueOnce(new ApiError(500, 'internal_error', 'boom', null));
		mountPage();
		await waitFor(() => {
			expect(screen.getByText(zh['error.generic'])).toBeDefined();
		});
		listThemes.mockResolvedValue([]);
		// antd may insert a space between the two CJK characters of 重试.
		const retry = screen.getAllByRole('button').find(
			(b) => (b.textContent ?? '').replace(/\s/g, '') === zh['common.retry']
		);
		(retry as HTMLButtonElement).click();
		await waitFor(() => {
			expect(screen.getByText(zh['themes.emptyCatalog'])).toBeDefined();
		});
	});
});
