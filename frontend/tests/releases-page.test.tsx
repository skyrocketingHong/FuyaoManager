/**
 * ReleasesPage mounted regressions: the publish action is bound to the
 * explicit candidate build, a persisted publish failure (422 application/json
 * release record) surfaces structured feedback and refreshes the history,
 * and a 409 conflict answers with the mapped warning.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, act, waitFor, cleanup, isInaccessible } from '@testing-library/react';
import { fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import { ApiError } from '../src/lib/api/client';
import type { BuildTask, Release, Site, SiteEdgeState } from '../src/lib/api/management';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		listBuilds: vi.fn(),
		listReleases: vi.fn(),
		getCurrentRelease: vi.fn(),
		createRelease: vi.fn(),
		getSiteEdge: vi.fn()
	};
});

import {
	getSite,
	getSiteEdge,
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
		<ConfigProvider theme={{ token: { motion: false } }}>
			<MemoryRouter initialEntries={[`/sites/${SITE}/releases`]}>
				<Routes>
					<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
					<Route path="/login" element={<div>login</div>} />
				</Routes>
			</MemoryRouter>
		</ConfigProvider>
	);
}

function edgeState(isolated: boolean): SiteEdgeState {
	return {
		site: {
			siteId: SITE,
			slug: 'blog',
			visibility: 'private',
			port: 41000,
			originUrl: 'http://127.0.0.1:41000',
			currentOk: !isolated
		},
		enabled: true,
		isolated,
		inSync: !isolated
	};
}

function primeState(builds: BuildTask[], releases: Release[]) {
	vi.mocked(getSite).mockResolvedValue(site());
	vi.mocked(listBuilds).mockResolvedValue(builds);
	vi.mocked(listReleases).mockResolvedValue(releases);
	vi.mocked(getSiteEdge).mockResolvedValue(edgeState(false));
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
	vi.mocked(getSiteEdge).mockReset();
});

afterEach(() => {
	cleanup();
});

/** antd inserts a space between two-CJK-character button labels. */
function findButton(label: string): HTMLButtonElement {
	const button = Array.from(
		document.querySelectorAll<HTMLButtonElement>('button:not([role]), button[role="button"]')
	)
		.filter((candidate) => (candidate.textContent ?? '').replace(/\s/g, '') === label)
		.find((candidate) => !isInaccessible(candidate));
	if (!(button instanceof HTMLButtonElement)) throw new Error(`button ${label} missing`);
	return button;
}

describe('ReleasesPage declared workspace capabilities', () => {
	it.each([true, false])(
		'uses deployable=%s instead of the legacy edge switch for an adopted static site',
		async (deployable) => {
			const current = succeededBuild('existing');
			const next = succeededBuild('candidate');
			primeState(
				[next, current],
				[releaseRow('live', current.id, 'live'), releaseRow('previous', current.id, 'superseded')]
			);
			vi.mocked(getSite).mockResolvedValue({
				...site(),
				kind: 'static',
				workspace: {
					themeEnabled: true,
					themeSelectable: true,
					buildable: true,
					deployable,
					hasCaddy: true
				}
			} as Site);
			vi.mocked(getSiteEdge).mockResolvedValue(edgeState(deployable));
			mount();
			await waitFor(() => expect(screen.getByText(/当前生效/)).toBeDefined());
			await waitFor(() => expect(findButton(zh['releases.publish']).disabled).toBe(!deployable));
			expect(findButton(zh['releases.rollback']).disabled).toBe(!deployable);
			expect(screen.queryByText(zh['edge.isolatedNotice'])).toBeNull();
		},
		15000
	);
});

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

		// Structured failure text is visible: the stage uses the mapped label
		// inside the composed failure toast...
		expect(
			screen.getByText(new RegExp(zh['releases.failureStage.artifacts'] as string))
		).toBeDefined();
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

describe('ReleasesPage isolation facts', () => {
	it('disables publish and rollback and hides the site link while isolated', async () => {
		const b1 = succeededBuild('b1-isolated');
		primeState(
			[b1],
			[releaseRow('rel-1', b1.id, 'live'), releaseRow('rel-2', b1.id, 'superseded')]
		);
		vi.mocked(getSiteEdge).mockResolvedValue(edgeState(true));
		mount();
		// Flush the edge promise: the isolated facts gate the write buttons.
		await act(async () => {
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(screen.getByText(zh['edge.isolatedNotice'])).toBeDefined();

		expect(findButton(zh['releases.publish'] as string).disabled).toBe(true);
		expect(findButton(zh['releases.rollback'] as string).disabled).toBe(true);
		expect(screen.queryByText(zh['releases.open'] as string)).toBeNull();
		// The history stays readable.
		expect(screen.getByText(/当前生效/)).toBeDefined();
	}, 15000);

	it('keeps publish, rollback, and the site link available while enabled', async () => {
		const b1 = succeededBuild('b1-enabled');
		const b2 = succeededBuild('b2-enabled');
		primeState(
			[b2, b1],
			[releaseRow('rel-1', b1.id, 'live'), releaseRow('rel-2', b1.id, 'superseded')]
		);
		mount();
		await waitFor(() => expect(screen.getByText(/当前生效/)).toBeDefined());
		// The edge facts land through their own promise: wait for the notice
		// to disappear instead of assuming the flush timing.
		await waitFor(() => expect(screen.queryByText(zh['edge.isolatedNotice'])).toBeNull());

		expect(findButton(zh['releases.publish'] as string).disabled).toBe(false);
		expect(findButton(zh['releases.rollback'] as string).disabled).toBe(false);
		expect(screen.getByText(zh['releases.open'] as string)).toBeDefined();
	}, 15000);

	it('shows a short failure instead of the isolation notice when the edge facts fail', async () => {
		const b1 = succeededBuild('b1-edgefail');
		primeState([b1], [releaseRow('rel-1', b1.id, 'live')]);
		vi.mocked(getSiteEdge).mockRejectedValue(new Error('network down'));
		mount();
		await waitFor(() => expect(screen.getByText(zh['edge.loadFailed'])).toBeDefined());
		expect(screen.queryByText(zh['edge.isolatedNotice'])).toBeNull();
		expect(findButton(zh['releases.publish'] as string).disabled).toBe(true);
	});

	it('drops a late isolated answer for the previous site after a switch', async () => {
		const SITE_B = '22222222-2222-2222-2222-222222222222';
		let navigateRef: ((to: string) => void) | null = null;
		function NavBridge() {
			navigateRef = useNavigate();
			return null;
		}
		const siteB = { ...site(), siteId: SITE_B, slug: 'other' };
		vi.mocked(getSite).mockImplementation(async (id: string) => (id === SITE ? site() : siteB));
		vi.mocked(listBuilds).mockImplementation(async (id: string) =>
			id === SITE ? [succeededBuild('b1-late-a')] : [succeededBuild('b2-late-b')]
		);
		vi.mocked(listReleases).mockImplementation(async (id: string) =>
			id === SITE ? [releaseRow('rel-1', 'b1-late-a', 'live')] : []
		);
		vi.mocked(getCurrentRelease).mockImplementation(async (id: string) => {
			if (id === SITE) {
				return {
					release: releaseRow('rel-1', 'b1-late-a', 'live'),
					originUrl: 'http://127.0.0.1:41000'
				};
			}
			throw new ApiError(404, 'no_live_release', '', null);
		});
		let resolveEdgeA!: (v: SiteEdgeState) => void;
		vi.mocked(getSiteEdge).mockImplementation((id: string) => {
			if (id === SITE) {
				return new Promise<SiteEdgeState>((resolve) => {
					resolveEdgeA = resolve;
				});
			}
			return Promise.resolve({
				...edgeState(true),
				site: { ...edgeState(true).site, siteId: SITE_B }
			});
		});

		render(
			<ConfigProvider theme={{ token: { motion: false } }}>
				<MemoryRouter initialEntries={[`/sites/${SITE}/releases`]}>
					<NavBridge />
					<Routes>
						<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
						<Route path="/login" element={<div>login</div>} />
					</Routes>
				</MemoryRouter>
			</ConfigProvider>
		);
		await waitFor(() => expect(vi.mocked(listReleases)).toHaveBeenCalledWith(SITE));

		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/releases`);
		});
		await waitFor(() => expect(vi.mocked(listReleases)).toHaveBeenCalledWith(SITE_B));
		await waitFor(() => expect(screen.getByText(zh['edge.isolatedNotice'])).toBeDefined());

		// Site A's late deployable answer must never reach site B.
		await act(async () => {
			resolveEdgeA(edgeState(false));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		await waitFor(() => expect(screen.getByText(zh['edge.isolatedNotice'])).toBeDefined());
		expect(findButton(zh['releases.publish'] as string).disabled).toBe(true);
	});
});

describe('ReleasesPage cross-site action guards', () => {
	it('a publish landing after a site switch never overwrites the new site', async () => {
		const SITE_B = '22222222-2222-2222-2222-222222222222';
		let navigateRef: ((to: string) => void) | null = null;
		function NavBridge() {
			navigateRef = useNavigate();
			return null;
		}
		vi.mocked(getSite).mockImplementation(async (id: string) => ({
			...site(),
			siteId: id,
			slug: id === SITE ? 'blog' : 'other'
		}));
		vi.mocked(listBuilds).mockImplementation(async (id: string) =>
			id === SITE ? [succeededBuild('b1-publish')] : []
		);
		vi.mocked(listReleases).mockImplementation(async (id: string) => (id === SITE ? [] : []));
		vi.mocked(getCurrentRelease).mockImplementation(async (id: string) => {
			if (id === SITE) {
				return {
					release: releaseRow('rel-a', 'b1-publish', 'live'),
					originUrl: 'http://127.0.0.1:41000'
				};
			}
			throw new ApiError(404, 'no_live_release', '', null);
		});
		vi.mocked(getSiteEdge).mockImplementation(async (id: string) => ({
			...edgeState(false),
			site: { ...edgeState(false).site, siteId: id }
		}));
		let resolvePublish!: (v: Release) => void;
		vi.mocked(createRelease).mockImplementation(
			() =>
				new Promise<Release>((resolve) => {
					resolvePublish = resolve;
				})
		);

		render(
			<ConfigProvider theme={{ token: { motion: false } }}>
				<MemoryRouter initialEntries={[`/sites/${SITE}/releases`]}>
					<NavBridge />
					<Routes>
						<Route path="/sites/:siteId/releases" element={<ReleasesPage />} />
						<Route path="/login" element={<div>login</div>} />
					</Routes>
				</MemoryRouter>
			</ConfigProvider>
		);
		await waitFor(() => expect(findButton(zh['releases.publish'] as string).disabled).toBe(false));

		await act(async () => {
			fireEvent.click(findButton(zh['releases.publish'] as string));
		});
		// Switch to site B while the publish is in flight.
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/releases`);
		});
		await waitFor(() =>
			expect(screen.getByText(zh['error.noLiveRelease'] as string)).toBeDefined()
		);

		await act(async () => {
			resolvePublish(releaseRow('rel-b', 'b1-publish', 'live'));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		// Site B's empty history is untouched; no error surfaced from the
		// previous site's action.
		expect(vi.mocked(createRelease)).toHaveBeenCalledWith(SITE, expect.anything());
		expect(screen.getByText(zh['error.noLiveRelease'] as string)).toBeDefined();
		expect(screen.queryByText('b1-publi')).toBeNull();
	});
});

describe('ReleasesPage payload identity', () => {
	it('a ready payload for another site keeps publish and rollback disabled', async () => {
		const b1 = succeededBuild('b1-foreign');
		primeState(
			[b1],
			[releaseRow('rel-1', b1.id, 'live'), releaseRow('rel-2', b1.id, 'superseded')]
		);
		vi.mocked(getSiteEdge).mockResolvedValue({
			...edgeState(false),
			site: { ...edgeState(false).site, siteId: '99999999-9999-9999-9999-999999999999' }
		});
		mount();
		await act(async () => {
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(findButton(zh['releases.publish'] as string).disabled).toBe(true);
		expect(findButton(zh['releases.rollback'] as string).disabled).toBe(true);
		expect(screen.queryByText(zh['releases.open'] as string)).toBeNull();
	}, 15000);
});
