/**
 * SiteDetailPage edge-fact lifecycle: the facts are loading/error/ready state
 * bound to the site and the mount generation — a failed lookup shows a short
 * failure with retry instead of borrowing the previous site's deployable
 * answer, only an explicit ready isolated===false opens the deployable
 * surfaces, stale answers never land, and unmount drops in-flight work.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup, act, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import type { Site, SiteEdgeState } from '../src/lib/api/management';
import zh from '../src/lib/i18n/locales/zh-CN.json';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		getSiteEdge: vi.fn(),
		getCurrentRelease: vi.fn(),
		putSiteVisibility: vi.fn(),
		reapplySiteEdge: vi.fn(),
		provisionSite: vi.fn()
	};
});

import { getCurrentRelease, getSite, getSiteEdge } from '../src/lib/api/management';
import { ApiError } from '../src/lib/api/client';
import SiteDetailPage from '../src/pages/sites/SiteDetailPage';

const SITE_A = '11111111-1111-1111-1111-111111111111';
const SITE_B = '22222222-2222-2222-2222-222222222222';

const site = (id: string, name: string): Site => ({
	siteId: id,
	slug: name.toLowerCase(),
	visibility: 'private',
	provisionState: 'ready',
	name,
	description: '',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
});

function edgeState(siteId: string, port: number, isolated: boolean, enabled = true): SiteEdgeState {
	return {
		site: {
			siteId,
			slug: 'blog',
			visibility: 'private',
			port,
			originUrl: `http://127.0.0.1:${port}`,
			currentOk: !isolated
		},
		enabled,
		isolated,
		inSync: !isolated
	};
}

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

let navigateRef: ((to: string) => void) | null = null;
function NavBridge() {
	navigateRef = useNavigate();
	return null;
}

function mountAt(initialSite: string) {
	// Rendered without the SiteBoundary wrapper: the boundary's loading→ready
	// phases remount the page (its own getSite calls would multiply the edge
	// lookups this suite reasons about). The page falls back to its own
	// detail store, which uses the same getSite mock.
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[`/sites/${initialSite}`]}>
				<NavBridge />
				<Routes>
					<Route path="/sites/:siteId" element={<SiteDetailPage />} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(getSiteEdge).mockReset();
	vi.mocked(getCurrentRelease).mockReset();
	navigateRef = null;
	vi.mocked(getSite).mockImplementation(async (id: string) => site(id, id === SITE_A ? 'SiteA' : 'SiteB'));
	vi.mocked(getCurrentRelease).mockRejectedValue(new ApiError(404, 'no_live_release', '', null));
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

function findSwitch(): HTMLButtonElement {
	const button = document.querySelector('.ant-switch');
	if (!(button instanceof HTMLButtonElement)) throw new Error('visibility switch missing');
	return button;
}

function findRetry(): HTMLButtonElement {
	const button = screen
		.getAllByRole('button')
		.find((b) => (b.textContent ?? '').replace(/\s/g, '') === zh['common.retry']);
	if (!(button instanceof HTMLButtonElement)) throw new Error('retry button missing');
	return button;
}

describe('SiteDetailPage edge-fact lifecycle', () => {
	it('keeps the entry links and an unlocked switch while deployment is enabled', async () => {
		vi.mocked(getSiteEdge).mockResolvedValue(edgeState(SITE_A, 41000, false));
		mountAt(SITE_A);
		await waitFor(() => expect(findSwitch().disabled).toBe(false));
		expect(screen.queryByText(zh['edge.isolatedNotice'])).toBeNull();
		expect(document.querySelector('a[href="http://127.0.0.1:41000"]')).not.toBeNull();
	});

	it('renders the isolation notice, plain-text reservation, and a locked switch when isolated', async () => {
		vi.mocked(getSiteEdge).mockResolvedValue(edgeState(SITE_A, 41000, true));
		mountAt(SITE_A);
		await waitFor(() => expect(screen.getByText(zh['edge.isolatedNotice'])).toBeDefined());
		expect(findSwitch().disabled).toBe(true);
		expect(document.querySelector('a[href="http://127.0.0.1:41000"]')).toBeNull();
		expect(document.body.textContent).toContain('http://127.0.0.1:41000');
		expect(screen.getByText(zh['edge.pointerMismatch'])).toBeDefined();
	});

	it('shows a short failure with retry on the next site and never borrows site A facts', async () => {
		let bCalls = 0;
		vi.mocked(getSiteEdge).mockImplementation((id: string) => {
			if (id === SITE_A) return Promise.resolve(edgeState(SITE_A, 41000, false));
			bCalls++;
			if (bCalls === 1) return Promise.reject(new Error('network down'));
			return Promise.resolve(edgeState(SITE_B, 41001, false));
		});
		mountAt(SITE_A);
		await waitFor(() => expect(findSwitch().disabled).toBe(false));

		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}`);
		});
		// Site B's lookup failed: a short failure with retry, the switch stays
		// locked, and site A's links are gone.
		await waitFor(() => expect(screen.getByText(zh['edge.loadFailed'])).toBeDefined());
		expect(findSwitch().disabled).toBe(true);
		expect(document.querySelector('a[href="http://127.0.0.1:41000"]')).toBeNull();

		// Retry recovers into the fresh, site-bound facts.
		await act(async () => {
			fireEvent.click(findRetry());
		});
		await waitFor(() => expect(screen.queryByText(zh['edge.loadFailed'])).toBeNull());
		await waitFor(() => expect(findSwitch().disabled).toBe(false));
		expect(document.querySelector('a[href="http://127.0.0.1:41001"]')).not.toBeNull();
	});

	it('drops a stale site A answer during A→B→A and recovers from the fresh lookup', async () => {
		let aCalls = 0;
		let resolveStaleA!: (v: SiteEdgeState) => void;
		vi.mocked(getSiteEdge).mockImplementation((id: string) => {
			if (id !== SITE_A) return Promise.reject(new Error('network down'));
			aCalls++;
			if (aCalls === 1) {
				return new Promise<SiteEdgeState>((resolve) => {
					resolveStaleA = resolve;
				});
			}
			return Promise.resolve(edgeState(SITE_A, 41000, false));
		});
		mountAt(SITE_A);
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}`);
		});
		await waitFor(() => expect(screen.getByText(zh['edge.loadFailed'])).toBeDefined());
		await act(async () => {
			navigateRef?.(`/sites/${SITE_A}`);
		});
		// The fresh A lookup answers; the stale first answer must not land.
		await waitFor(() => expect(findSwitch().disabled).toBe(false));
		expect(document.querySelector('a[href="http://127.0.0.1:41000"]')).not.toBeNull();

		await act(async () => {
			resolveStaleA(edgeState(SITE_A, 41000, true));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(findSwitch().disabled).toBe(false);
		expect(screen.queryByText(zh['edge.isolatedNotice'])).toBeNull();
	});

	it('ignores a response landing after unmount', async () => {
		let resolveA!: (v: SiteEdgeState) => void;
		vi.mocked(getSiteEdge).mockImplementation(
			() =>
				new Promise<SiteEdgeState>((resolve) => {
					resolveA = resolve;
				})
		);
		const view = mountAt(SITE_A);
		// The in-flight request must exist before unmounting.
		await waitFor(() => expect(vi.mocked(getSiteEdge)).toHaveBeenCalledWith(SITE_A));
		view.unmount();
		await act(async () => {
			resolveA(edgeState(SITE_A, 41000, false));
			await Promise.resolve();
		});
		// Nothing to assert on a detached tree — the absence of a thrown
		// update-on-unmounted error is the observable outcome.
		expect(document.body.textContent).not.toContain(zh['edge.isolatedNotice']);
	});
});
