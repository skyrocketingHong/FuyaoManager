/**
 * Cross-site and stale-response regression: mounting the real BuildsPage in
 * jsdom with a router and switching the route parameter between two sites —
 * a late listBuilds response for the previous site must never overwrite the
 * current site's list, and unmount must stop applying responses.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import type { BuildTask, Site } from '../src/lib/api/management';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		listBuilds: vi.fn()
	};
});

import { getSite, listBuilds } from '../src/lib/api/management';
import BuildsPage from '../src/pages/sites/BuildsPage';

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

const build = (siteId: string, id: string): BuildTask => ({
	id,
	siteId,
	status: 'succeeded',
	requestedBy: siteId,
	theme: { themeId: 'fuyao-homepage', version: '27.1.1', manifestDigest: 'a'.repeat(64) },
	themeConfig: {},
	siteSettings: { site: { name: 'n', description: '', defaultLocale: 'zh-CN' } },
	siteSettingsRevision: 1,
	themeSelectionRevision: 1,
	failure: null,
	inputDigests: null,
	createdAt: '2026-01-01T00:00:00Z',
	startedAt: null,
	finishedAt: null,
	sourceDigest: null,
	sourceFileCount: null
});

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

// Navigation bridge: MemoryRouter's initialEntries only apply at mount, so
// route switches go through a real navigate() call.
let navigateRef: ((to: string) => void) | null = null;
function NavBridge() {
	navigateRef = useNavigate();
	return null;
}

function mountAt(initialSite: string) {
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[`/sites/${initialSite}/builds`]}>
				<NavBridge />
				<Routes>
					<Route path="/sites/:siteId/builds" element={<BuildsPage />} />
					<Route path="/login" element={<div>login</div>} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(listBuilds).mockReset();
	navigateRef = null;
});

describe('BuildsPage stale responses', () => {
	it('a late response for site A never overwrites site B after a route switch', async () => {
		const pending = new Map<string, (v: BuildTask[]) => void>();
		vi.mocked(listBuilds).mockImplementation(
			(siteId: string) =>
				new Promise((resolve) => {
					if (siteId === SITE_A) pending.set(siteId, resolve);
					else resolve([build(siteId, 'bbbbbbbb-build-b')]);
				})
		);
		vi.mocked(getSite).mockImplementation(async (siteId: string) =>
			site(siteId, siteId === SITE_A ? 'SiteA' : 'SiteB')
		);

		mountAt(SITE_A);
		// Switch to site B before A's listBuilds resolves.
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/builds`);
		});
		await waitFor(() => {
			expect(screen.getByText('bbbbbbbb')).toBeDefined();
		});

		// Site A's response finally arrives — very late.
		await act(async () => {
			pending.get(SITE_A)?.([build(SITE_A, 'aaaaaaaa-build-a')]);
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		// Site B's list is untouched.
		expect(screen.getByText('bbbbbbbb')).toBeDefined();
		expect(screen.queryByText('aaaaaaaa')).toBeNull();
	});

	it('stops applying responses after unmount', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		let resolveList!: (v: BuildTask[]) => void;
		vi.mocked(listBuilds).mockImplementationOnce(
			() => new Promise((resolve) => (resolveList = resolve))
		);
		const view = mountAt(SITE_A);
		view.unmount();
		// The response lands after unmount; nothing may throw into React.
		await act(async () => {
			resolveList([build(SITE_A, 'ghost-build')]);
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(document.body.textContent ?? '').not.toContain('ghost');
	});
});
