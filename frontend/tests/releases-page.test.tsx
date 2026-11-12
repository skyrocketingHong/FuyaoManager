/**
 * ReleasesPage mounted regressions: the publish action is bound to the
 * explicit candidate build, a persisted publish failure (422 application/json
 * release record) surfaces structured feedback and refreshes the history,
 * and a 409 conflict answers with the mapped warning.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach } from 'vitest';
import { render, screen, act, waitFor } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import { ApiError } from '../src/lib/api/client';
import type { BuildTask, Release, Site } from '../src/lib/api/management';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		listBuilds: vi.fn(),
		listReleases: vi.fn(),
		getCurrentRelease: vi.fn(),
		createRelease: vi.fn()
	};
});

import {
	getSite,
	listBuilds,
	listReleases,
	getCurrentRelease,
	createRelease
} from '../src/lib/api/management';
import ReleasesPage from '../src/pages/sites/ReleasesPage';
import zh from '../src/lib/i18n/locales/zh-CN.json';

const SITE = '11111111-1111-1111-1111-111111111111';

const site = (): Site => ({
	siteId: SITE,
	slug: 'blog',
	visibility: 'private',
	provisionState: 'ready',
	name: 'Site',
	description: '',
	defaultLocale: 'zh-CN',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
});

const succeededBuild = (id: string): BuildTask => ({
	id,
	siteId: SITE,
	status: 'succeeded',
	requestedBy: SITE,
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
	sourceFileCount: null,
	previewActive: false
});

const releaseRow = (id: string, buildId: string, status: Release['status']): Release => ({
	id,
	siteId: SITE,
	buildId,
	theme: { themeId: 'fuyao-homepage', version: '27.1.1', manifestDigest: 'a'.repeat(64) },
	inputDigests: null,
	routes: { spaFallback: null, urlConventions: [] },
	reasons: ['manual'],
	status,
	failure: null,
	createdBy: SITE,
	createdAt: '2026-01-01T00:00:00Z',
	activatedAt: null,
	supersededAt: null
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

async function settled(rounds = 12): Promise<void> {
	for (let i = 0; i < rounds; i++) await Promise.resolve();
}

function mount() {
	return render(
		<ConfigProvider>
			<MemoryRouter initialEntries={[`/sites/${SITE}/releases`]}>
				<Routes>
					<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
					<Route path="/login" element={<div>login</div>} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

function primeState(builds: BuildTask[], releases: Release[]) {
	vi.mocked(getSite).mockResolvedValue(site());
	vi.mocked(listBuilds).mockResolvedValue(builds);
	vi.mocked(listReleases).mockResolvedValue(releases);
	vi.mocked(getCurrentRelease).mockImplementation(async () => ({
		release: releaseRow('rel-live', releases[0]?.buildId ?? 'b1', 'live'),
		originUrl: 'http://127.0.0.1:41000'
	}));
}

beforeEach(() => {
	vi.mocked(getSite).mockReset();
	vi.mocked(listBuilds).mockReset();
	vi.mocked(listReleases).mockReset();
	vi.mocked(getCurrentRelease).mockReset();
	vi.mocked(createRelease).mockReset();
});

/** antd inserts a space between two-CJK-character button labels. */
function findButton(label: string): HTMLButtonElement {
	const button = screen
		.getAllByRole('button')
		.find((b) => (b.textContent ?? '').replace(/\s/g, '') === label);
	if (!(button instanceof HTMLButtonElement)) throw new Error(`button ${label} missing`);
	return button;
}

describe('ReleasesPage publish feedback and candidate binding', () => {
	it('publishes the explicit unreleased candidate, not the latest succeeded build', async () => {
		const b1 = succeededBuild('b1-release-1');
		const b2 = succeededBuild('b2-pending-2');
		// b1 is newest and already released; b2 is the only publishable one.
		primeState([b2, b1], [releaseRow('rel-1', b1.id, 'live')]);
		vi.mocked(createRelease).mockResolvedValue(releaseRow('rel-2', b2.id, 'live'));
		mount();
		await waitFor(() => {
			expect(vi.mocked(listReleases)).toHaveBeenCalled();
		});
		await settled();

		await act(async () => {
			fireEvent.click(findButton(zh['releases.publish'] as string));
		});
		await settled();

		expect(createRelease).toHaveBeenCalledWith(SITE, {
			buildId: b2.id,
			reasons: ['manual']
		});
	});

	it('surfaces a persisted publish failure and refreshes the history', async () => {
		const b1 = succeededBuild('b1-failing-1');
		primeState([b1], []);
		vi.mocked(getCurrentRelease).mockRejectedValue(new ApiError(404, 'no_live_release', '', null));
		const failedRecord = {
			...releaseRow('rel-f', b1.id, 'failed'),
			failure: {
				stage: 'artifacts',
				message: 'missing',
				missingArtifacts: ['sitemap.xml']
			}
		};
		vi.mocked(createRelease).mockRejectedValue(
			new ApiError(422, 'internal_error', '', null, failedRecord)
		);
		mount();
		await waitFor(() => {
			expect(vi.mocked(listBuilds)).toHaveBeenCalled();
		});
		await settled();
		const before = vi.mocked(listReleases).mock.calls.length;

		await act(async () => {
			fireEvent.click(findButton(zh['releases.publish'] as string));
		});
		await settled();

		// Structured failure text is visible...
		expect(screen.getByText(/artifacts/)).toBeDefined();
		expect(screen.getByText(/sitemap\.xml/)).toBeDefined();
		// ...and the history was refreshed so the attempt is listed.
		expect(vi.mocked(listReleases).mock.calls.length).toBeGreaterThan(before);
	});

	it('answers a 409 build conflict with the mapped warning', async () => {
		const b1 = succeededBuild('b1-conflict-1');
		primeState([b1], []);
		vi.mocked(getCurrentRelease).mockRejectedValue(new ApiError(404, 'no_live_release', '', null));
		vi.mocked(createRelease).mockRejectedValue(
			new ApiError(409, 'release_build_conflict', '', null)
		);
		mount();
		await waitFor(() => {
			expect(vi.mocked(listBuilds)).toHaveBeenCalled();
		});
		await settled();

		await act(async () => {
			fireEvent.click(findButton(zh['releases.publish'] as string));
		});
		await settled();

		expect(screen.getByText(zh['error.releaseBuildConflict'] as string)).toBeDefined();
	});
});
