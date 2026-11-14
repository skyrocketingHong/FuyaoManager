// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { App as AntApp, ConfigProvider } from 'antd';
import { ContentScopeProvider } from '../src/components/ContentScopeProvider';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import PostsPage from '../src/pages/sites/PostsPage';
import AlbumsPage from '../src/pages/sites/AlbumsPage';
import FriendsPage from '../src/pages/sites/FriendsPage';
import FootprintsPage from '../src/pages/sites/FootprintsPage';
import ContentSourcesPage from '../src/pages/sites/ContentSourcesPage';
import SiteOverviewPage from '../src/pages/sites/SiteOverviewPage';
import { ApiError } from '../src/lib/api/client';
import * as service from '../src/lib/api/content';
import * as content from '../src/lib/api/management';
import { setLocale } from '../src/lib/i18n';

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
	getFriends: vi.fn(),
	putFriends: vi.fn(),
	getAlbums: vi.fn(),
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

it('resets pagination, query, and search display when the content scope switches', async () => {
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

	// Page two of the platform scope stays pending while the scope switches.
	let resolveLate!: (value: { posts: content.Post[]; total: number }) => void;
	vi.mocked(content.listPosts).mockImplementationOnce(
		() =>
			new Promise((resolve) => {
				resolveLate = resolve;
			})
	);
	fireEvent.click(screen.getByLabelText('Page 2'));
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
	await waitFor(() =>
		expect(content.listPosts).toHaveBeenLastCalledWith('site', '', 1, 20)
	);

	// The late platform response must not cover the new scope's data.
	resolveLate({ posts: [latePost], total: 30 });
	await screen.findByText('Site post');
	expect(screen.queryByText('Late platform post')).toBeNull();
	expect(screen.queryByText('Platform post')).toBeNull();

	// The page and the search display were reset with the scope.
	const search = (screen.getByPlaceholderText('Search title / slug / tag') as HTMLInputElement);
	expect(search.value).toBe('');
	expect(screen.queryByLabelText('Page 2')).toBeNull();
});
