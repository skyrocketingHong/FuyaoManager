// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { App as AntApp, ConfigProvider } from 'antd';
import { ContentScopeProvider } from '../src/components/ContentScopeProvider';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import PostsPage from '../src/pages/content/posts/PostsPage';
import PostEditorPage from '../src/pages/content/posts/PostEditorPage';
import AlbumsPage from '../src/pages/content/AlbumsPage';
import FriendsPage from '../src/pages/content/FriendsPage';
import FootprintsPage from '../src/pages/content/FootprintsPage';
import ContentSourcesPage from '../src/pages/sites/ContentSourcesPage';
import SiteOverviewPage from '../src/pages/sites/SiteOverviewPage';
import { ApiError } from '../src/lib/api/client';
import * as service from '../src/lib/api/content';
import * as content from '../src/lib/api/management';
import { setLocale } from '../src/lib/i18n';
import { getVisibleButton } from './visible-button';

const NativeRequest = globalThis.Request;

const runtime = vi.hoisted(() => ({
	state: { status: 'authenticated', user: { id: 'admin', platformRole: 'admin' } },
	site: {
		siteId: 'site',
		name: 'Website',
		kind: 'static',
		managementMode: 'platform',
		role: 'owner',
		workspace: {
			themeEnabled: true,
			themeSelectable: true,
			buildable: true,
			deployable: false,
			hasCaddy: true
		}
	}
}));
vi.mock('../src/lib/session', () => ({
	sessionSnapshot: () => runtime.state,
	subscribeSession: () => () => undefined,
	loadSession: vi.fn()
}));
vi.mock('../src/components/layout/SiteBoundary', () => ({ useCurrentSite: () => runtime.site }));
vi.mock('../src/lib/api/content', async (original) => ({
	...(await original<typeof service>()),
	getPlatformContent: vi.fn(),
	getContentSources: vi.fn(),
	putContentSources: vi.fn(),
	getSiteOverview: vi.fn()
}));
vi.mock('../src/lib/api/management', async (original) => ({
	...(await original<typeof content>()),
	getSite: vi.fn(),
	getCurrentRelease: vi.fn(),
	listPosts: vi.fn(),
	getPostTaxonomy: vi.fn(),
	getFriends: vi.fn(),
	putFriends: vi.fn(),
	getAlbums: vi.fn(),
	getAlbumYear: vi.fn(),
	putAlbumYear: vi.fn(),
	uploadAlbumPhoto: vi.fn(),
	listMediaTasks: vi.fn(),
	getFootprints: vi.fn(),
	getSiteConfig: vi.fn()
}));

const independent: service.ContentSources = {
	posts: 'independent',
	albums: 'independent',
	footprints: 'independent',
	friends: 'independent'
};
function Header() {
	const value = usePageHeaderContent();
	return (
		<div>
			{value?.leading}
			{value?.content}
			{value?.actions}
		</div>
	);
}
function mount(element: React.ReactNode, path = '/content/posts') {
	const router = createMemoryRouter(
		[
			{
				path: path.startsWith('/sites/') ? '/sites/:siteId/*' : '*',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope={path}>
								<Header />
								{element}
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <div>route-crash</div>
			}
		],
		{ initialEntries: [path] }
	);
	return render(<RouterProvider router={router} />);
}
const site = () => runtime.site as unknown as content.Site;
beforeEach(() => {
	vi.stubEnv('NODE_ENV', 'development');
	vi.clearAllMocks();
	localStorage.clear();
	setLocale('en-US');
	runtime.site = { ...runtime.site, role: 'owner' };
	vi.mocked(content.getSite).mockResolvedValue(site());
	vi.mocked(content.getCurrentRelease).mockRejectedValue(
		new ApiError(404, 'no_live_release', '', null)
	);
	vi.mocked(service.getPlatformContent).mockResolvedValue({
		canEdit: true,
		counts: { posts: 0, albums: 0, photos: 0, places: 0, friends: 0 },
		amap: {}
	});
	vi.mocked(service.getContentSources).mockResolvedValue({
		data: { sources: { ...independent }, revision: 1 },
		etag: '"sources-1"'
	});
	vi.mocked(content.listPosts).mockResolvedValue({ posts: [], total: 0 });
	vi.mocked(content.getPostTaxonomy).mockResolvedValue({
		directories: [],
		categories: [],
		tags: [],
		postCount: 0,
		canEdit: true
	});
	vi.mocked(content.getFriends).mockResolvedValue({
		friends: { online: [], offline: [] },
		digest: '"friends"'
	});
	vi.mocked(content.getAlbums).mockResolvedValue({
		index: { schemaVersion: 1, years: [], totalPhotos: 0, devices: [] },
		digest: '"albums"'
	});
	vi.mocked(content.listMediaTasks).mockResolvedValue([]);
	vi.mocked(content.getFootprints).mockResolvedValue({
		footprints: { cities: [] },
		digest: '"places"'
	});
});
afterEach(() => {
	cleanup();
	vi.unstubAllEnvs();
	vi.unstubAllGlobals();
});

it.each([
	['posts', PostsPage, 'listPosts'],
	['albums', AlbumsPage, 'getAlbums'],
	['friends', FriendsPage, 'getFriends'],
	['footprints', FootprintsPage, 'getFootprints']
] as const)(
	'reuses the %s editor for platform content without a synthetic site identity',
	async (resource, Page, method) => {
		mount(
			<ContentScopeProvider resource={resource}>
				<Page />
			</ContentScopeProvider>,
			`/content/${resource}`
		);
		await waitFor(() => expect(content[method]).toHaveBeenCalled());
		expect(vi.mocked(content[method]).mock.calls[0][0]).toEqual({ type: 'platform' });
		expect(content.getSite).not.toHaveBeenCalled();
		expect(content.getSiteConfig).not.toHaveBeenCalled();
		expect(screen.queryByText('route-crash')).toBeNull();
	}
);

it('makes shared site content read-only and links to its platform editor', async () => {
	vi.mocked(service.getContentSources).mockResolvedValue({
		data: { sources: { ...independent, posts: 'platform' }, revision: 2 },
		etag: '"sources-2"'
	});
	mount(
		<ContentScopeProvider site={site()} resource="posts">
			<PostsPage />
		</ContentScopeProvider>,
		'/sites/site/posts'
	);
	expect(
		(await screen.findByRole('link', { name: 'Manage platform content' })).getAttribute('href')
	).toBe('/content/posts');
	await waitFor(() => expect(content.listPosts).toHaveBeenCalledWith('site', '', 1, 20));
	expect((screen.getByRole('button', { name: 'New post' }) as HTMLButtonElement).disabled).toBe(
		true
	);
});

it('moves the selected photo in the underlying album after sorting its table', async () => {
	vi.mocked(content.getAlbums).mockResolvedValue({
		index: { schemaVersion: 1, years: [2026], totalPhotos: 3, devices: [] }, digest: '"index"'
	});
	const year = { year: 2026, photos: [
		{ filename: 'c.jpg', path: '/c.jpg', title: 'Charlie' },
		{ filename: 'a.jpg', path: '/a.jpg', title: 'Alpha' },
		{ filename: 'b.jpg', path: '/b.jpg', title: 'Bravo' }
	] };
	vi.mocked(content.getAlbumYear).mockResolvedValue({ year, digest: '"year"' });
	vi.mocked(content.putAlbumYear).mockResolvedValue({ year, digest: '"saved"' });
	mount(<ContentScopeProvider resource="albums"><AlbumsPage /></ContentScopeProvider>, '/content/albums?view=list');
	await screen.findByText('Charlie');
	fireEvent.click(screen.getByRole('columnheader', { name: /Photo/ }));
	const row = screen.getByText('Bravo').closest('tr')!;
	fireEvent.click(getVisibleButton(row, 'Move up'));
	fireEvent.click(screen.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(content.putAlbumYear).toHaveBeenCalled());
	const saved = vi.mocked(content.putAlbumYear).mock.calls[0][2];
	expect(saved.map((photo) => photo.filename)).toEqual(['c.jpg', 'b.jpg', 'a.jpg']);
});

it('uses a year deep link, shows real thumbnails and keeps photo edits when changing the view', async () => {
	vi.stubGlobal('Request', class extends NativeRequest {
		constructor(input: RequestInfo | URL, init?: RequestInit) {
			super(input, { ...init, signal: undefined });
		}
	});
	const photo = { filename: 'morning.jpg', path: '2025/01/morning.jpg', title: 'Morning',
		variants: [{ path: '2025/01/morning 480.webp', width: 480, height: 360 }] };
	vi.mocked(content.getAlbums).mockResolvedValue({
		index: { schemaVersion: 1, years: [2025, 2026], totalPhotos: 1, devices: [] }, digest: '"index"'
	});
	vi.mocked(content.getAlbumYear).mockResolvedValue({ year: { year: 2025, photos: [photo] }, digest: '"year"' });
	vi.mocked(content.putAlbumYear).mockResolvedValue({ year: { year: 2025, photos: [{ ...photo, title: 'Sunrise' }] }, digest: '"saved"' });
	mount(<ContentScopeProvider resource="albums"><AlbumsPage /></ContentScopeProvider>, '/content/albums?year=2025');
	const open = await screen.findByRole('button', { name: 'Open photo Morning' });
	expect(content.getAlbumYear).toHaveBeenCalledWith({ type: 'platform' }, '2025', expect.any(AbortSignal));
	expect(open.querySelector('img')?.getAttribute('src')).toBe(
		'/api/v1/content/albums/thumbnail?path=2025%2F01%2Fmorning+480.webp'
	);
	fireEvent.click(open);
	const editor = await screen.findByRole('dialog', { name: 'Edit photo' });
	fireEvent.change(within(editor).getByRole('textbox', { name: 'Title' }), { target: { value: 'Sunrise' } });
	fireEvent.click(within(editor).getByRole('button', { name: 'Apply changes' }));
	await screen.findByRole('button', { name: 'Open photo Sunrise' });
	fireEvent.click(screen.getByRole('radio', { name: 'Horizontal gallery' }));
	await screen.findByRole('region', { name: 'Photo gallery, use arrow keys to browse' });
	expect(screen.getByText('Unsaved')).toBeTruthy();
	fireEvent.click(screen.getByRole('button', { name: 'Save' }));
	await waitFor(() => expect(content.putAlbumYear).toHaveBeenCalledWith(
		{ type: 'platform' }, '2025', [{ filename: 'morning.jpg', title: 'Sunrise', description: '', alt: '' }], '"year"', '', ''
	));
});

it('shows a first-use empty state and submits multiple selected originals in order', async () => {
	vi.mocked(content.uploadAlbumPhoto)
		.mockResolvedValueOnce({ path: '2026/first.jpg', taskId: 'first' })
		.mockResolvedValueOnce({ path: '2026/second.jpg', taskId: 'second' });
	mount(<ContentScopeProvider resource="albums"><AlbumsPage /></ContentScopeProvider>, '/content/albums');
	expect(await screen.findByText('No albums yet. Upload photos to create a year album.')).toBeTruthy();
	const input = screen.getByLabelText('Upload photos', { selector: 'input' });
	const first = new File(['first'], 'first.jpg', { type: 'image/jpeg' });
	const second = new File(['second'], 'second.jpg', { type: 'image/jpeg' });
	fireEvent.change(input, { target: { files: [first, second] } });
	await waitFor(() => expect(content.uploadAlbumPhoto).toHaveBeenCalledTimes(2));
	expect(content.uploadAlbumPhoto).toHaveBeenNthCalledWith(1, { type: 'platform' }, first);
	expect(content.uploadAlbumPhoto).toHaveBeenNthCalledWith(2, { type: 'platform' }, second);
});

it('uses the platform permission rather than a site role for platform writes', async () => {
	vi.mocked(service.getPlatformContent).mockResolvedValue({
		canEdit: false,
		counts: { posts: 0, albums: 0, photos: 0, places: 0, friends: 0 },
		amap: {}
	});
	mount(
		<ContentScopeProvider resource="posts">
			<PostsPage />
		</ContentScopeProvider>
	);
	await waitFor(() => expect(content.listPosts).toHaveBeenCalled());
	expect((screen.getByRole('button', { name: 'New post' }) as HTMLButtonElement).disabled).toBe(
		true
	);
});

it('does not start an editor until its scope metadata can be loaded', async () => {
	vi.mocked(service.getPlatformContent).mockRejectedValueOnce(new TypeError('offline'));
	mount(
		<ContentScopeProvider resource="posts">
			<PostsPage />
		</ContentScopeProvider>
	);
	fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
	await waitFor(() => expect(content.listPosts).toHaveBeenCalledTimes(1));
});

it('saves source choices with their own revision and ETag without deploying', async () => {
	vi.mocked(service.putContentSources).mockResolvedValue({
		data: { sources: { ...independent, posts: 'platform' }, revision: 2 },
		etag: '"sources-2"'
	});
	mount(<ContentSourcesPage />, '/sites/site/content-sources');
	fireEvent.mouseDown(await screen.findByRole('combobox', { name: 'Posts' }));
	fireEvent.click(
		await screen.findByText('Platform library', { selector: '.ant-select-item-option-content' })
	);
	fireEvent.click(screen.getByRole('button', { name: 'Save' }));
	await waitFor(() =>
		expect(service.putContentSources).toHaveBeenCalledExactlyOnceWith(
			'site',
			{ sources: { ...independent, posts: 'platform' }, revision: 1 },
			'"sources-1"'
		)
	);
	await waitFor(() =>
		expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
	);
	expect(
		screen.getByText(
			'Saving content sources does not publish the website. Existing independent content is retained.'
		)
	).toBeDefined();
});

it('preserves source choices through conflict sync and saves against the new baseline', async () => {
	vi.mocked(service.putContentSources).mockRejectedValueOnce(
		new ApiError(412, 'precondition_failed', '', null)
	);
	mount(<ContentSourcesPage />, '/sites/site/content-sources');
	fireEvent.mouseDown(await screen.findByRole('combobox', { name: 'Posts' }));
	fireEvent.click(
		await screen.findByText('Platform library', { selector: '.ant-select-item-option-content' })
	);
	fireEvent.click(screen.getByRole('button', { name: 'Save' }));
	await screen.findByRole('button', { name: 'Sync and keep editing' });
	vi.mocked(service.getContentSources).mockResolvedValue({
		data: { sources: { ...independent, friends: 'platform' }, revision: 3 },
		etag: '"sources-3"'
	});
	fireEvent.click(screen.getByRole('button', { name: 'Sync and keep editing' }));
	await waitFor(() =>
		expect(screen.queryByRole('button', { name: 'Sync and keep editing' })).toBeNull()
	);
	vi.mocked(service.putContentSources).mockResolvedValue({
		data: { sources: { ...independent, posts: 'platform' }, revision: 4 },
		etag: '"sources-4"'
	});
	fireEvent.click(screen.getByRole('button', { name: 'Save' }));
	await waitFor(() =>
		expect(service.putContentSources).toHaveBeenLastCalledWith(
			'site',
			{ sources: { ...independent, posts: 'platform', friends: 'platform' }, revision: 3 },
			'"sources-3"'
		)
	);
});

it('shows server content counts on an adopted static site without fabricated traffic data', async () => {
	vi.mocked(service.getSiteOverview).mockResolvedValue({
		counts: { posts: 12, albums: 3, photos: 42, places: 8, friends: 4 },
		sources: independent,
		hasCaddy: true,
		workspace: runtime.site.workspace,
		analytics: {
			configured: false,
			available: false,
			provider: 'cloudflare',
			cache: 'unknown',
			latest: null
		},
		theme: {
			siteId: 'site',
			selected: false,
			theme: null,
			themeConfig: null,
			revision: 0,
			updatedAt: '2026-01-01T00:00:00Z'
		}
	});
	mount(<SiteOverviewPage />, '/sites/site');
	expect(await screen.findByText('42')).toBeDefined();
	expect(screen.getByText('12')).toBeDefined();
	expect(screen.getByRole('link', { name: 'Caddy configuration' }).getAttribute('href')).toBe(
		'/sites/site/caddy'
	);
	expect(screen.queryByText(/Cloudflare|visitors|traffic/i)).toBeNull();
});

it('resets loaded batches, query, and search display when the content scope switches', async () => {
	const platformPost: content.Post = {
		path: 'tech/platform.md',
		category: 'tech',
		title: 'Platform post',
		slug: 'platform',
		date: '2026-11-11T10:00:00.000Z',
		draft: false,
		categories: [],
		tags: [],
		body: 'x',
		digest: 'sha256:' + 'a'.repeat(64)
	};
	const sitePost: content.Post = {
		...platformPost,
		path: 'tech/site.md',
		title: 'Site post',
		digest: 'sha256:' + 'b'.repeat(64)
	};
	vi.mocked(content.listPosts).mockResolvedValue({ posts: [platformPost], total: 30 });

	function ScopeHarness() {
		const [siteScope, setSiteScope] = useState(false);
		return (
			<>
				<button type="button" onClick={() => setSiteScope(true)}>
					switch-scope
				</button>
				{siteScope ? (
					<ContentScopeProvider site={site()} resource="posts">
						<PostsPage />
					</ContentScopeProvider>
				) : (
					<ContentScopeProvider resource="posts">
						<PostsPage />
					</ContentScopeProvider>
				)}
			</>
		);
	}
	mount(<ScopeHarness />, '/content/posts');
	await screen.findByText('Platform post');

	// The next platform batch stays pending while the scope switches.
	let resolveLate!: (value: { posts: content.Post[]; total: number }) => void;
	vi.mocked(content.listPosts).mockImplementationOnce(
		() =>
			new Promise((resolve) => {
				resolveLate = resolve;
			})
	);
	fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
	await waitFor(() =>
		expect(content.listPosts).toHaveBeenLastCalledWith({ type: 'platform' }, '', 2, 20)
	);
	const latePost: content.Post = {
		...platformPost,
		path: 'tech/late.md',
		title: 'Late platform post'
	};
	// The new scope's first load carries its own post.
	vi.mocked(content.listPosts).mockResolvedValueOnce({ posts: [sitePost], total: 1 });

	fireEvent.click(screen.getByText('switch-scope'));
	await waitFor(() => expect(content.listPosts).toHaveBeenLastCalledWith('site', '', 1, 20));

	// The late platform response must not cover the new scope's data.
	resolveLate({ posts: [latePost], total: 30 });
	await screen.findByText('Site post');
	expect(screen.queryByText('Late platform post')).toBeNull();
	expect(screen.queryByText('Platform post')).toBeNull();

	// The page and the search display were reset with the scope.
	const search = screen.getByPlaceholderText('Search title / slug / tag') as HTMLInputElement;
	expect(search.value).toBe('');
	expect(screen.queryByLabelText('Page 2')).toBeNull();
});

it('discards stale taxonomy responses when the scope switches', async () => {
	let resolvePlatformTaxonomy!: (value: content.PostTaxonomy) => void;
	vi.mocked(content.getPostTaxonomy).mockImplementationOnce(
		() =>
			new Promise<content.PostTaxonomy>((resolve) => {
				resolvePlatformTaxonomy = resolve;
			})
	);

	function ScopeHarness() {
		const [siteScope, setSiteScope] = useState(false);
		return (
			<>
				<button type="button" onClick={() => setSiteScope(true)}>
					switch-scope
				</button>
				{siteScope ? (
					<ContentScopeProvider site={site()} resource="posts">
						<PostEditorPage mode="new" />
					</ContentScopeProvider>
				) : (
					<ContentScopeProvider resource="posts">
						<PostEditorPage mode="new" />
					</ContentScopeProvider>
				)}
			</>
		);
	}
	mount(<ScopeHarness />, '/content/posts');
	// The platform taxonomy read stays pending while the scope switches.
	await waitFor(() => expect(content.getPostTaxonomy).toHaveBeenCalledTimes(1));

	fireEvent.click(screen.getByText('switch-scope'));
	await waitFor(() => expect(content.getPostTaxonomy).toHaveBeenCalledTimes(2));
	// The second call reads the site scope through the bound target.
	expect(content.getPostTaxonomy).toHaveBeenLastCalledWith('site', expect.anything());

	// The stale platform response resolves late and must be discarded.
	resolvePlatformTaxonomy({
		directories: [{ slug: 'platform', title: 'Platform', postCount: 1 }],
		categories: [{ name: 'platform-cat', postCount: 1 }],
		tags: [],
		postCount: 1,
		canEdit: true
	} satisfies content.PostTaxonomy);
	// The site scope loads its own (empty) taxonomy without any UI error, and
	// the stale response does not trigger another read.
	await waitFor(() => {
		expect(screen.queryByRole('alert')).toBeNull();
	});
	expect(content.getPostTaxonomy).toHaveBeenCalledTimes(2);
});
