/**
 * Cross-site and stale-response regression: mounting the real BuildsPage in
 * jsdom with a router and switching the route parameter between two sites —
 * a late listBuilds response for the previous site must never overwrite the
 * current site's list, and unmount must stop applying responses.
 *
 * Preview window regressions: the blank window opens synchronously inside
 * the click with its opener severed, tracked per site and build; a
 * server-side close closes exactly that build's window, a failed close
 * keeps it, and a link response that lands after a site switch discards
 * only its own blank window.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, act, waitFor, cleanup, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import { ApiError } from '../src/lib/api/client';
import type { BuildTask, Site } from '../src/lib/api/management';
import zh from '../src/lib/i18n/locales/zh-CN.json';

vi.mock('../src/lib/api/management', async (importOriginal) => {
	const actual = await importOriginal<typeof import('../src/lib/api/management')>();
	return {
		...actual,
		getSite: vi.fn(),
		listBuilds: vi.fn(),
		createBuild: vi.fn(),
		createPreviewLink: vi.fn(),
		closePreviewLink: vi.fn()
	};
});

import {
	createBuild,
	createPreviewLink,
	closePreviewLink,
	getSite,
	listBuilds
} from '../src/lib/api/management';
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

const build = (siteId: string, id: string, previewActive = false): BuildTask => ({
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
	sourceFileCount: null,
	previewActive
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
	vi.mocked(createBuild).mockReset();
	vi.mocked(createPreviewLink).mockReset();
	vi.mocked(closePreviewLink).mockReset();
	navigateRef = null;
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
});

/** antd inserts a space between two-CJK-character button labels. */
function findButton(label: string): HTMLButtonElement {
	const button = screen
		.getAllByRole('button')
		.find((b) => (b.textContent ?? '').replace(/\s/g, '') === label);
	if (!(button instanceof HTMLButtonElement)) throw new Error(`button ${label} missing`);
	return button;
}

/** The index-th button with this label, for row-scoped assertions. */
function findButtonNth(label: string, index: number): HTMLButtonElement {
	const list = screen
		.getAllByRole('button')
		.filter((b) => (b.textContent ?? '').replace(/\s/g, '') === label);
	const button = list[index];
	if (!(button instanceof HTMLButtonElement)) throw new Error(`button ${label} #${index} missing`);
	return button;
}

interface FakeWindow {
	opener: unknown;
	closed: boolean;
	close: ReturnType<typeof vi.fn>;
	focus: ReturnType<typeof vi.fn>;
	location: { href: string };
}

function fakeWindow(): FakeWindow {
	return { opener: window, closed: false, close: vi.fn(), focus: vi.fn(), location: { href: '' } };
}

function stubOpen(win: FakeWindow | null) {
	return vi.spyOn(window, 'open').mockReturnValue(win as unknown as Window);
}

const previewLink = (url: string) => ({ url, expiresAt: '2026-01-01T01:00:00Z' });

/** Confirms one antd Popconfirm opened from the index-th button with this label. */
async function confirmPopconfirm(label: string, index = 0) {
	await act(async () => {
		fireEvent.click(findButtonNth(label, index));
	});
	const ok = await waitFor(() => {
		// New popovers append at the end of body; pick the newest OK so a
		// still-mounted previous popover cannot swallow the click.
		const list = screen.getAllByRole('button').filter((b) => (b.textContent ?? '') === 'OK');
		const button = list[list.length - 1];
		if (!button) throw new Error('Popconfirm OK button missing');
		return button;
	});
	await act(async () => {
		fireEvent.click(ok);
	});
}

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

describe('BuildsPage preview windows', () => {
	it('opens the blank window synchronously, severs the opener, navigates, and refreshes', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-open-1')]);
		vi.mocked(createPreviewLink).mockResolvedValue(previewLink('http://127.0.0.1:49200/__preview__/tok1'));
		const win = fakeWindow();
		stubOpen(win);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() => expect(win.location.href).toBe('http://127.0.0.1:49200/__preview__/tok1'));

		expect(vi.mocked(createPreviewLink)).toHaveBeenCalledWith(SITE_A, 'b1-open-1');
		expect(win.opener).toBeNull();
		// The live state was refreshed after opening.
		expect(vi.mocked(listBuilds).mock.calls.length).toBeGreaterThan(1);
	});

	it('reports feedback and issues nothing when the popup is blocked', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-blocked')]);
		stubOpen(null);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() =>
			expect(screen.getByText(zh['builds.previewBlocked'] as string)).toBeDefined()
		);
		expect(vi.mocked(createPreviewLink)).not.toHaveBeenCalled();
	});

	it('issues once for repeated clicks while a link request is in flight', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-double')]);
		let resolveLink!: (v: ReturnType<typeof previewLink>) => void;
		vi.mocked(createPreviewLink).mockImplementation(
			() => new Promise((resolve) => (resolveLink = resolve))
		);
		const win = fakeWindow();
		stubOpen(win);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		expect(vi.mocked(createPreviewLink)).toHaveBeenCalledTimes(1);

		await act(async () => {
			resolveLink(previewLink('http://127.0.0.1:49201/__preview__/tok2'));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(win.location.href).toBe('http://127.0.0.1:49201/__preview__/tok2');
	});

	it('discards only its own blank window when the site switches before the link resolves', async () => {
		const pending = new Map<string, (v: ReturnType<typeof previewLink>) => void>();
		vi.mocked(getSite).mockImplementation(async (id: string) => site(id, id === SITE_A ? 'SiteA' : 'SiteB'));
		vi.mocked(listBuilds).mockImplementation(async (id: string) => {
			if (id === SITE_A) return [build(SITE_A, 'aaaaaaaa-build-a')];
			return [build(SITE_B, 'bbbbbbbb-build-b')];
		});
		vi.mocked(createPreviewLink).mockImplementation(
			(_siteId: string, buildId: string) =>
				new Promise((resolve) => {
					pending.set(buildId, resolve);
				})
		);
		const win = fakeWindow();
		stubOpen(win);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		// Switch to site B while site A's link request is still in flight.
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/builds`);
		});
		await waitFor(() => expect(screen.getByText('bbbbbbbb')).toBeDefined());

		await act(async () => {
			pending.get('aaaaaaaa-build-a')?.(previewLink('http://127.0.0.1:49202/__preview__/tok3'));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		// The stale response navigated nothing and its own blank window died;
		// no navigation towards the old site's preview happened.
		expect(win.close).toHaveBeenCalled();
		expect(win.location.href).toBe('');
	});

	it('reuses the tracked window while the preview is active instead of issuing again', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		// First issue: the task is not yet marked active; the post-open refresh
		// sees previewActive=true for the live origin.
		vi.mocked(listBuilds).mockResolvedValueOnce([build(SITE_A, 'b1-active')]);
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-active', true)]);
		vi.mocked(createPreviewLink).mockResolvedValue(previewLink('http://127.0.0.1:49203/__preview__/tok4'));
		const win = fakeWindow();
		stubOpen(win);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() => expect(win.location.href).not.toBe(''));
		await waitFor(() => expect(vi.mocked(listBuilds).mock.calls.length).toBeGreaterThan(1));

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});

		// The second click focused the tracked window: no new window, no new
		// token, no extra navigation.
		expect(vi.mocked(createPreviewLink)).toHaveBeenCalledTimes(1);
		expect(win.focus).toHaveBeenCalled();
		expect(win.location.href).toBe('http://127.0.0.1:49203/__preview__/tok4');
	});

	it('closes exactly its own window on success and keeps it on failure', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-close-1'), build(SITE_A, 'b2-close-2')]);
		vi.mocked(createPreviewLink)
			.mockResolvedValueOnce(previewLink('http://127.0.0.1:49204/__preview__/tokA'))
			.mockResolvedValueOnce(previewLink('http://127.0.0.1:49205/__preview__/tokB'));
		const winA = fakeWindow();
		const winB = fakeWindow();
		// The Once overrides queue before the fixed return value, so the two
		// clicks must each get an explicit target.
		const openSpy = stubOpen(null);
		openSpy.mockReturnValueOnce(winA as unknown as Window);
		openSpy.mockReturnValueOnce(winB as unknown as Window);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() => expect(winA.location.href).not.toBe(''));
		await act(async () => {
			fireEvent.click(findButtonNth(zh['builds.preview'] as string, 1));
		});
		await waitFor(() => expect(winB.location.href).not.toBe(''));

		// Closing b1 closes winA only.
		await confirmPopconfirm(zh['builds.previewClose'] as string, 0);
		await waitFor(() => expect(winA.close).toHaveBeenCalled());
		expect(winB.close).not.toHaveBeenCalled();

		// A failed close keeps b2's window untouched.
		vi.mocked(closePreviewLink).mockRejectedValue(new ApiError(500, 'internal_error', '', null));
		await confirmPopconfirm(zh['builds.previewClose'] as string, 1);
		await waitFor(() =>
			expect(screen.getByText(zh['error.generic'] as string)).toBeDefined()
		);
		expect(winB.close).not.toHaveBeenCalled();
	}, 15000);
});

describe('BuildsPage generation guards', () => {
	it('a close landing after a site switch still closes its window and never refreshes the old site', async () => {
		vi.mocked(getSite).mockImplementation(async (id: string) =>
			site(id, id === SITE_A ? 'SiteA' : 'SiteB')
		);
		vi.mocked(listBuilds).mockImplementation(async (id: string) =>
			id === SITE_A ? [build(SITE_A, 'aaaaaaaa-build-a')] : [build(SITE_B, 'bbbbbbbb-build-b')]
		);
		vi.mocked(createPreviewLink).mockResolvedValue(previewLink('http://127.0.0.1:49300/__preview__/tok1'));
		let resolveClose!: () => void;
		vi.mocked(closePreviewLink).mockImplementation(
			() => new Promise<void>((resolve) => (resolveClose = resolve))
		);
		const win = fakeWindow();
		stubOpen(null).mockReturnValueOnce(win as unknown as Window);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() => expect(win.location.href).not.toBe(''));

		await confirmPopconfirm(zh['builds.previewClose'] as string, 0);
		// Switch sites while the close request is in flight.
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/builds`);
		});
		await waitFor(() => expect(screen.getByText('bbbbbbbb')).toBeDefined());
		const callsAtSwitch = vi.mocked(listBuilds).mock.calls.length;

		await act(async () => {
			resolveClose();
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		// The original window closed, but the late response refreshed nothing:
		// site B's list is untouched and site A's builds never appeared.
		expect(win.close).toHaveBeenCalled();
		expect(vi.mocked(closePreviewLink)).toHaveBeenCalledWith(SITE_A, 'aaaaaaaa-build-a');
		expect(screen.getByText('bbbbbbbb')).toBeDefined();
		expect(screen.queryByText('aaaaaaaa')).toBeNull();
		expect(vi.mocked(listBuilds).mock.calls.length).toBe(callsAtSwitch);
	});

	it('a create landing after a site switch never applies the old site list', async () => {
		vi.mocked(getSite).mockImplementation(async (id: string) =>
			site(id, id === SITE_A ? 'SiteA' : 'SiteB')
		);
		vi.mocked(listBuilds).mockImplementation(async (id: string) =>
			id === SITE_A ? [build(SITE_A, 'aaaaaaaa-build-a')] : [build(SITE_B, 'bbbbbbbb-build-b')]
		);
		let resolveCreate!: (v: BuildTask) => void;
		vi.mocked(createBuild).mockImplementation(
			() => new Promise((resolve) => (resolveCreate = resolve))
		);
		mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.create'] as string)).toBeDefined());

		await confirmPopconfirm(zh['builds.create'] as string, 0);
		await act(async () => {
			navigateRef?.(`/sites/${SITE_B}/builds`);
		});
		await waitFor(() => expect(screen.getByText('bbbbbbbb')).toBeDefined());
		const callsAtSwitch = vi.mocked(listBuilds).mock.calls.length;

		await act(async () => {
			resolveCreate(build(SITE_A, 'created-late'));
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});

		expect(vi.mocked(createBuild)).toHaveBeenCalledWith(SITE_A);
		expect(screen.getByText('bbbbbbbb')).toBeDefined();
		expect(screen.queryByText('createdl')).toBeNull();
		expect(vi.mocked(listBuilds).mock.calls.length).toBe(callsAtSwitch);
	});

	it('a close response after unmount still closes the original window', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockResolvedValue([build(SITE_A, 'b1-unmount')]);
		vi.mocked(createPreviewLink).mockResolvedValue(previewLink('http://127.0.0.1:49301/__preview__/tok2'));
		let resolveClose!: () => void;
		vi.mocked(closePreviewLink).mockImplementation(
			() => new Promise<void>((resolve) => (resolveClose = resolve))
		);
		const win = fakeWindow();
		stubOpen(null).mockReturnValueOnce(win as unknown as Window);
		const view = mountAt(SITE_A);
		await waitFor(() => expect(findButton(zh['builds.preview'] as string)).toBeDefined());

		await act(async () => {
			fireEvent.click(findButton(zh['builds.preview'] as string));
		});
		await waitFor(() => expect(win.location.href).not.toBe(''));
		await confirmPopconfirm(zh['builds.previewClose'] as string, 0);
		view.unmount();

		await act(async () => {
			resolveClose();
			for (let i = 0; i < 12; i++) await Promise.resolve();
		});
		expect(win.close).toHaveBeenCalled();
		expect(vi.mocked(closePreviewLink)).toHaveBeenCalledWith(SITE_A, 'b1-unmount');
	});
});

describe('BuildsPage session expiry', () => {
	it('a real 401 routes the page to the login screen', async () => {
		vi.mocked(getSite).mockResolvedValue(site(SITE_A, 'SiteA'));
		vi.mocked(listBuilds).mockRejectedValue(new ApiError(401, 'unauthenticated', '', null));
		mountAt(SITE_A);
		await waitFor(() => expect(screen.getByText('login')).toBeDefined());
	});
});
