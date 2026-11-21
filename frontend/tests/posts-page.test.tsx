// @vitest-environment jsdom
/**
 * PostsPage mounted-view coverage: the list renders with search + paging
 * bound to the typed API, the draft gate is visible, and viewers get no
 * create action.
 */
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import {
	MemoryRouter,
	Route,
	Routes,
	createMemoryRouter,
	RouterProvider,
	Outlet
} from 'react-router-dom';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import * as management from '../src/lib/api/management';
import { ApiError } from '../src/lib/api/client';
import type { Site } from '../src/lib/api/management';
import { initLocale } from '../src/lib/i18n';

// rc-util otherwise gives every modal the same test-id, breaking dialog names.
declare const process: { env: { NODE_ENV: string | undefined } };
const originalNodeEnv = process.env.NODE_ENV;
process.env.NODE_ENV = 'development';
const [
	{ App: AntApp, ConfigProvider },
	{ PageHeaderProvider, usePageHeaderContent },
	{ default: PostsPage },
	{ default: PostEditorPage },
	{ default: PostVocabularyPage },
	{ default: PostMediaPage }
] = await Promise.all([
	import('antd'),
	import('../src/components/PageHeader'),
	import('../src/pages/content/posts/PostsPage'),
	import('../src/pages/content/posts/PostEditorPage'),
	import('../src/pages/content/posts/PostVocabularyPage'),
	import('../src/pages/content/posts/PostMediaPage')
]);
afterAll(() => { process.env.NODE_ENV = originalNodeEnv; });

vi.mock('../src/lib/api/management', async (importOriginal) => ({
	...(await importOriginal<object>()),
	getSite: vi.fn(),
	listPosts: vi.fn(),
	createPost: vi.fn(),
	getPost: vi.fn(),
	putPost: vi.fn(),
	getPostVocabulary: vi.fn(),
	putPostVocabulary: vi.fn(),
	renamePostTag: vi.fn(),
	getPostDirectory: vi.fn(),
	deletePostCategory: vi.fn(),
	putPostDirectory: vi.fn(),
	listPostMedia: vi.fn(),
	importPostMedia: vi.fn(),
	getPostTaxonomy: vi.fn()
}));

const emptyTaxonomy = {
	directories: [] as { slug: string; title: string; postCount: number }[],
	categories: [] as { name: string; postCount: number }[],
	tags: [] as { name: string; postCount: number }[],
	postCount: 0,
	canEdit: true
};

const ownerSite: Site = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Blog',
	description: '',
	defaultLocale: 'zh-CN',
	visibility: 'private',
	provisionState: 'ready',
	role: 'owner',
	settingsRevision: 1,
	createdAt: '2026-01-01T00:00:00Z',
	updatedAt: '2026-01-01T00:00:00Z'
};

const samplePost = {
	path: 'tech/hello.md',
	category: 'tech',
	title: 'Hello',
	slug: 'hello',
	date: '2026-11-11T10:00:00.000Z',
	draft: true,
	categories: [],
	tags: [],
	body: 'x',
	digest: 'sha256:' + 'a'.repeat(64)
};

// jsdom lacks matchMedia, which antd's responsive observer requires.
beforeAll(() => {
	Object.defineProperty(window, 'matchMedia', {
		writable: true,
		value: (query: string) => ({
			matches: false,
			media: query,
			onchange: null,
			addListener: () => {},
			removeListener: () => {},
			addEventListener: () => {},
			removeEventListener: () => {},
			dispatchEvent: () => false
		})
	});
});

function Header() {
	const header = usePageHeaderContent();
	return (
		<div>
			{header?.leading}
			{header?.content}
			{header?.actions}
		</div>
	);
}

function mountPosts() {
	const router = createMemoryRouter(
		[
			{
				path: '/sites/:siteId/posts',
				element: (
					<ConfigProvider theme={{ token: { motion: false } }}>
						<AntApp>
							<PageHeaderProvider scope="posts">
								<Header />
								<Outlet />
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <div>route-crash</div>,
				children: [
					{ index: true, element: <PostsPage /> },
					{ path: 'new', element: <PostEditorPage mode="new" /> },
					{ path: 'categories', element: <PostVocabularyPage kind="categories" /> },
					{ path: 'tags', element: <PostVocabularyPage kind="tags" /> },
					{ path: 'media', element: <PostMediaPage /> },
					{ path: ':postPath/edit', element: <PostEditorPage mode="edit" /> }
				]
			}
		],
		{ initialEntries: ['/sites/11111111-1111-1111-1111-111111111111/posts'] }
	);
	return { ...render(<RouterProvider router={router} />), router };
}

describe('PostsPage', () => {
	afterEach(() => {
		cleanup();
		vi.unstubAllGlobals();
	});

	beforeEach(() => {
		vi.clearAllMocks();
		const NativeRequest = globalThis.Request;
		vi.stubGlobal(
			'Request',
			class extends NativeRequest {
				constructor(input: RequestInfo | URL, init?: RequestInit) {
					super(input, init ? { ...init, signal: undefined } : init);
				}
			}
		);
		vi.mocked(management.getSite).mockImplementation(() => Promise.resolve(ownerSite));
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 1 });
		vi.mocked(management.createPost).mockImplementation(async (_target, post) => ({
			post: { ...samplePost, ...post, digest: 'sha256:' + 'c'.repeat(64) },
			etag: '"sha256:' + 'c'.repeat(64) + '"'
		}));
		vi.mocked(management.getPostTaxonomy).mockResolvedValue(emptyTaxonomy);
		vi.mocked(management.listPostMedia).mockResolvedValue({ items: [], canEdit: true });
		vi.mocked(management.getPost).mockResolvedValue({ post: samplePost, etag: samplePost.digest });
		vi.mocked(management.getPostVocabulary).mockResolvedValue({
			vocabulary: {
				version: 1,
				categories: [],
				tags: [],
				digest: 'sha256:' + 'd'.repeat(64),
				canEdit: true
			},
			etag: '"sha256:' + 'd'.repeat(64) + '"'
		});
		vi.mocked(management.putPostVocabulary).mockImplementation(async (_target, value) => ({
			vocabulary: { ...value, digest: 'sha256:' + 'e'.repeat(64), canEdit: true },
			etag: '"sha256:' + 'e'.repeat(64) + '"'
		}));
		initLocale();
	});

	it('renders the post list with the draft badge and the create action', async () => {
		render(
			<MemoryRouter initialEntries={['/sites/11111111-1111-1111-1111-111111111111/posts']}>
				<Routes>
					<Route path="/sites/:siteId/posts" element={<PostsPage />} />
				</Routes>
			</MemoryRouter>
		);
		await waitFor(() => {
			expect(screen.getByText('Hello')).toBeTruthy();
		});
		expect(screen.getByText('hello')).toBeTruthy();
		expect(screen.getByText('Draft')).toBeTruthy();
		expect(screen.getByText('New post')).toBeTruthy();
		expect(vi.mocked(management.listPosts).mock.calls[0][1]).toBe('');
	});

	it('hides the create action for viewers', async () => {
		vi.mocked(management.getSite).mockResolvedValue({ ...ownerSite, role: 'viewer' });
		render(
			<MemoryRouter initialEntries={['/sites/11111111-1111-1111-1111-111111111111/posts']}>
				<Routes>
					<Route path="/sites/:siteId/posts" element={<PostsPage />} />
				</Routes>
			</MemoryRouter>
		);
		await waitFor(() => {
			expect(screen.getByText('Hello')).toBeTruthy();
		});
		const create = screen.getByRole('button', { name: /New post/ });
		expect(create.hasAttribute('disabled')).toBe(true);
	});

	it('shows unified category badges and the monospace identifier below the title', async () => {
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [{ ...samplePost, categories: ['技术'] }], total: 1 });
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			...emptyTaxonomy,
			categoryGroups: [{ name: 'tech', title: '技术', directory: 'tech', aliases: ['tech', '技术'], postCount: 1 }]
		});
		mountPosts();
		const row = (await screen.findByText('Hello')).closest('tr')!;
		await waitFor(() => expect(within(row).getAllByText('技术')).toHaveLength(1));
		expect(within(row).getByText('hello').classList.contains('post-identifier')).toBe(true);
		expect(within(row).queryByText('tech/hello.md')).toBeNull();
	});
	it('renders the search and create action once through the top slot', async () => {
		mountPosts();
		await screen.findByText('Hello');
		expect(screen.getAllByPlaceholderText('Search title / slug / tag')).toHaveLength(1);
		expect(screen.getAllByRole('button', { name: /New post/ })).toHaveLength(1);
	});

	it('loads further batches without pagination and resets after search and size changes', async () => {
		vi.stubGlobal('IntersectionObserver', undefined);
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 25 });
		mountPosts();
		await screen.findByText('Hello');
		expect(vi.mocked(management.listPosts).mock.calls[0]).toEqual([expect.anything(), '', 1, 20]);
		const scrollRegion = screen.getByRole('region', { name: 'Posts' });
		expect(scrollRegion.tabIndex).toBe(0);
		scrollRegion.focus();
		expect(document.activeElement).toBe(scrollRegion);
		scrollRegion.scrollTop = 120;

		// Searching returns to page one with the typed query.
		const search = screen.getByPlaceholderText('Search title / slug / tag');
		fireEvent.change(search, { target: { value: 'hello' } });
		fireEvent.keyDown(search, { key: 'Enter' });
		expect(scrollRegion.scrollTop).toBe(0);
		await waitFor(() =>
			expect(vi.mocked(management.listPosts).mock.calls.at(-1)).toEqual([
				expect.anything(),
				'hello',
				1,
				20
			])
		);

		// Loading more keeps the query and the selected batch size.
		expect(screen.queryByLabelText('Page 2')).toBeNull();
		fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
		await waitFor(() =>
			expect(vi.mocked(management.listPosts).mock.calls.at(-1)).toEqual([
				expect.anything(),
				'hello',
				2,
				20
			])
		);

		// Changing the page size returns to page one and drives the same size.
		fireEvent.mouseDown(screen.getByRole('combobox', { name: 'Posts per load' }));
		fireEvent.click(
			await screen.findByText('50 posts', { selector: '.ant-select-item-option-content' })
		);
		await waitFor(() =>
			expect(vi.mocked(management.listPosts).mock.calls.at(-1)).toEqual([
				expect.anything(),
				'hello',
				1,
				50
			])
		);
	});

	it('offers a working retry after a failed list load', async () => {
		vi.mocked(management.listPosts).mockRejectedValueOnce(new TypeError('offline'));
		mountPosts();
		const retry = await screen.findByRole('button', { name: 'Retry' });
		expect(screen.queryByText('Hello')).toBeNull();
		fireEvent.click(retry);
		await screen.findByText('Hello');
		expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull();
	});

	it('sorts the complete article list and preserves the order while loading more', async () => {
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 25 });
		mountPosts();
		await screen.findByText('Hello');
		const title = screen.getByRole('columnheader', { name: /Title/ });
		fireEvent.click(title);
		await waitFor(() => expect(management.listPosts).toHaveBeenLastCalledWith(expect.anything(), '', 1, 20, { field: 'title', direction: 'asc' }));
		fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
		await waitFor(() => expect(management.listPosts).toHaveBeenLastCalledWith(expect.anything(), '', 2, 20, { field: 'title', direction: 'asc' }));
		fireEvent.click(title);
		await waitFor(() => expect(management.listPosts).toHaveBeenLastCalledWith(expect.anything(), '', 1, 20, { field: 'title', direction: 'desc' }));
		fireEvent.click(title);
		await waitFor(() => expect(management.listPosts).toHaveBeenLastCalledWith(expect.anything(), '', 1, 20));
	});

	it('loads the next batch when the last article enters the viewport', async () => {
		let trigger: IntersectionObserverCallback | undefined;
		const observe = vi.fn();
		vi.stubGlobal(
			'IntersectionObserver',
			class {
				constructor(callback: IntersectionObserverCallback) {
					trigger = callback;
				}
				observe = observe;
				disconnect = vi.fn();
			}
		);
		const first = Array.from({ length: 20 }, (_, index) => ({
			...samplePost,
			path: `tech/${index}.md`,
			title: `Post ${index}`
		}));
		vi.mocked(management.listPosts)
			.mockResolvedValueOnce({ posts: first, total: 21 })
			.mockResolvedValueOnce({
				posts: [{ ...samplePost, path: 'tech/20.md', title: 'Post 20' }],
				total: 21
			});
		render(
			<div className="posts-table">
				<table>
					<tbody><tr data-row-key="unrelated"><td>Unrelated row</td></tr></tbody>
				</table>
			</div>
		);
		mountPosts();
		const lastRow = (await screen.findByText('Post 19')).closest('tr');
		await waitFor(() => expect(observe).toHaveBeenCalledWith(lastRow));
		const notify = trigger!;
		const entries = [{ isIntersecting: true } as IntersectionObserverEntry];
		act(() => notify(entries, {} as IntersectionObserver));
		expect(await screen.findByText('Post 20')).toBeDefined();
		expect(management.listPosts).toHaveBeenLastCalledWith(ownerSite.siteId, '', 2, 20);
		expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
		act(() => notify(entries, {} as IntersectionObserver));
		expect(management.listPosts).toHaveBeenCalledTimes(2);
	});

	it('picks taxonomy candidates in the editor and saves arrays, refreshing after the save', async () => {
		let finishCreate!: () => void;
		vi.mocked(management.createPost).mockImplementationOnce(async (_target, post) => {
			await new Promise<void>((resolve) => { finishCreate = resolve; });
			return {
				post: { ...samplePost, ...post, digest: 'sha256:' + 'c'.repeat(64) },
				etag: '"sha256:' + 'c'.repeat(64) + '"'
			};
		});
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			directories: [{ slug: 'tech', title: 'Tech', postCount: 2 }],
			categories: [
				{ name: 'go', postCount: 3 },
				{ name: 'travel', postCount: 1 }
			],
			tags: [{ name: 'perf', postCount: 4 }],
			postCount: 5,
			canEdit: true
		});
		const { router } = mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: /New post/ }));
		await waitFor(() => expect(document.querySelector('.post-editor-form')).not.toBeNull());

		// One classification control supplies both the primary and additional categories.
		fireEvent.mouseDown(screen.getByLabelText('Categories'));
		fireEvent.click(await screen.findByText('Tech', { selector: '.ant-select-item-option-content' }));
		fireEvent.mouseDown(screen.getByLabelText('Categories'));
		fireEvent.click(await screen.findByText('go', { selector: '.ant-select-item-option-content' }));
		const tags = screen.getByLabelText('Tags');
		fireEvent.mouseDown(tags);
		fireEvent.change(tags, { target: { value: 'newtag' } });
		fireEvent.keyDown(tags, { key: 'Enter', keyCode: 13 });

		// Required title and slug must pass before the form submits.
		fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'kept' } });
		fireEvent.change(screen.getByLabelText('Unique identifier'), { target: { value: 'kept' } });
		const taxonomyCallsBefore = vi.mocked(management.getPostTaxonomy).mock.calls.length;
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.createPost).toHaveBeenCalled());
		const body = vi.mocked(management.createPost).mock.calls.at(-1)?.[1] as {
			category: string;
			categories: string[];
			tags: string[];
		};
		expect(body.category).toBe('tech');
		expect(body.categories).toEqual(['tech', 'go']);
		expect(body.tags).toEqual(['newtag']);
		expect(management.putPostVocabulary).not.toHaveBeenCalled();
		expect(router.state.location.pathname).toBe(`/sites/${ownerSite.siteId}/posts/new`);
		expect(management.getPost).not.toHaveBeenCalled();
		await act(async () => { finishCreate(); });

		// The saved article refreshes the taxonomy for the next editor.
		await waitFor(() =>
			expect(vi.mocked(management.getPostTaxonomy).mock.calls.length).toBeGreaterThan(
				taxonomyCallsBefore
			)
		);
		await waitFor(() =>
			expect(router.state.location.pathname).toBe(`/sites/${ownerSite.siteId}/posts/tech%2Fhello.md/edit`)
		);
		await waitFor(() =>
			expect(management.getPost).toHaveBeenCalledWith(ownerSite.siteId, 'tech/hello.md')
		);
		await waitFor(() => expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Hello'));
		expect(screen.queryByRole('button', { name: 'Keep editing' })).toBeNull();
	}, 15000);

	it('shows a retryable taxonomy error and keeps the editor values', async () => {
		mountPosts();
		await screen.findByText('Hello');
		const taxonomyCallsBefore = vi.mocked(management.getPostTaxonomy).mock.calls.length;
		vi.mocked(management.getPostTaxonomy).mockRejectedValueOnce(new TypeError('offline'));
		fireEvent.click(screen.getByRole('button', { name: /New post/ }));
		const title = await screen.findByLabelText('Title');
		fireEvent.change(title, { target: { value: 'kept' } });
		const retry = await screen.findByRole('button', { name: 'Retry' });
		fireEvent.click(retry);
		await waitFor(() => expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull());
		expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('kept');
		// The retry re-read the taxonomy instead of faking an empty one.
		await waitFor(() => expect(management.getPostTaxonomy).toHaveBeenCalledTimes(taxonomyCallsBefore + 2));
	}, 15000);

	it('opens an encoded article route and renders Markdown without executing raw HTML', async () => {
		vi.mocked(management.getPost).mockResolvedValue({
			post: {
				...samplePost,
				body: '# Preview heading\n\n| A | B |\n| - | - |\n| 1 | 2 |\n\n<script>window.attack = true</script>\n\n[unsafe](javascript:alert(1))'
			},
			etag: samplePost.digest
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		expect(await screen.findByRole('heading', { name: 'Preview heading' })).toBeDefined();
		expect(management.getPost).toHaveBeenCalledWith(ownerSite.siteId, 'tech/hello.md');
		expect(document.querySelector('.markdown-preview table')).not.toBeNull();
		expect(document.querySelector('.markdown-preview script')).toBeNull();
		expect(screen.getByText('unsafe').closest('a')?.getAttribute('href') ?? '').not.toMatch(
			/^javascript:/i
		);
	});

	it('edits the title in the top slot and inserts Markdown around the selected text', async () => {
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		const title = (await screen.findByLabelText('Title')) as HTMLInputElement;
		await waitFor(() => expect(title.value).toBe('Hello'));
		expect(title.closest('.post-editor-form')).toBeNull();
		const bold = screen.getByRole('button', { name: 'Bold' });
		bold.focus();
		fireEvent.keyDown(bold, { key: 'ArrowRight' });
		expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Italic' }));
		const body = screen.getByLabelText('Body (Markdown)') as HTMLTextAreaElement;
		expect(screen.queryByText('Body (Markdown)')).toBeNull();
		fireEvent.change(body, { target: { value: 'Hello world' } });
		body.setSelectionRange(0, 5);
		fireEvent.mouseDown(screen.getByRole('button', { name: 'Bold' }));
		fireEvent.click(screen.getByRole('button', { name: 'Bold' }));
		expect(body.value).toBe('**Hello** world');
		fireEvent.change(title, { target: { value: 'Updated title' } });
		expect(title.value).toBe('Updated title');
	});

	it('uses the save time for an unchanged last-modified field', async () => {
		vi.mocked(management.putPost).mockImplementationOnce(async (_target, _path, submitted) => ({
			post: { ...samplePost, ...submitted },
			etag: '"new-etag"'
		}));
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		const title = (await screen.findByLabelText('Title')) as HTMLInputElement;
		await waitFor(() => expect(title.value).toBe('Hello'));
		fireEvent.change(title, { target: { value: 'Changed' } });
		const before = Date.now();
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.putPost).toHaveBeenCalledTimes(1));
		const lastmod = vi.mocked(management.putPost).mock.calls[0][2].lastmod!;
		expect(lastmod.endsWith('Z')).toBe(true);
		expect(Date.parse(lastmod)).toBeGreaterThanOrEqual(before);
		expect(Date.parse(lastmod)).toBeLessThanOrEqual(Date.now());
	});

	it('allows editing an existing article identifier while retaining its record key', async () => {
		vi.mocked(management.putPost).mockImplementationOnce(async (_target, _key, submitted) => ({
			post: { ...samplePost, ...submitted }, etag: '"new-etag"'
		}));
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		const identifier = (await screen.findByLabelText('Unique identifier')) as HTMLInputElement;
		await waitFor(() => expect(identifier.value).toBe('hello'));
		fireEvent.change(identifier, { target: { value: 'renamed-identifier' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.putPost).toHaveBeenCalledWith(
			ownerSite.siteId, samplePost.path, expect.objectContaining({ slug: 'renamed-identifier' }), expect.any(String)
		));
	});

	it('keeps edits made during saving and uses the returned digest on the next save', async () => {
		let finish!: (result: { post: typeof samplePost; etag: string }) => void;
		vi.mocked(management.putPost).mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finish = resolve;
				})
		);
		vi.mocked(management.putPost).mockResolvedValueOnce({
			post: { ...samplePost, title: 'Later', digest: 'sha256:' + 'c'.repeat(64) },
			etag: '"sha256:' + 'c'.repeat(64) + '"'
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		const title = await screen.findByLabelText('Title');
		fireEvent.change(title, { target: { value: 'First' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.putPost).toHaveBeenCalledTimes(1));
		expect(vi.mocked(management.putPost).mock.calls[0][2].date).toBe(samplePost.date);
		fireEvent.change(title, { target: { value: 'Later' } });
		finish({
			post: { ...samplePost, title: 'First', digest: 'sha256:' + 'b'.repeat(64) },
			etag: '"sha256:' + 'b'.repeat(64) + '"'
		});
		await waitFor(() =>
			expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Later')
		);
		await waitFor(() =>
			expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(
				false
			)
		);
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.putPost).toHaveBeenCalledTimes(2));
		expect(vi.mocked(management.putPost).mock.calls[1][3]).toBe('"sha256:' + 'b'.repeat(64) + '"');
	});

	it('keeps the draft when leaving is cancelled', async () => {
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
		const title = await screen.findByLabelText('Title');
		fireEvent.change(title, { target: { value: 'Unsaved' } });
		fireEvent.click(screen.getByRole('button', { name: 'Back to posts' }));
		const cancel = await screen.findByRole('button', { name: 'Keep editing' });
		fireEvent.click(cancel);
		await waitFor(() =>
			expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Unsaved')
		);
	});

	it('shows existing categories, directories and tags without an import step', async () => {
		const categories = [{ name: '技术', postCount: 1 }];
		const directories = [{ slug: 'tech', title: '技术', postCount: 1 }];
		const categoryGroups = [
			{ name: 'tech', title: '技术', directory: 'tech', aliases: ['tech', '技术'], postCount: 1 }
		];
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			...emptyTaxonomy,
			categories,
			directories,
			categoryGroups,
			tags: [{ name: 'Go', postCount: 1 }]
		});
		const { router } = mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Categories' }));
		await waitFor(() => expect(router.state.location.pathname).toBe(`/sites/${ownerSite.siteId}/posts/categories`));
		expect(await screen.findByText('技术')).toBeDefined();
		expect(screen.queryByRole('button', { name: 'Import existing terms' })).toBeNull();
		expect(screen.queryByRole('button', { name: 'Add to vocabulary' })).toBeNull();
		fireEvent.click(screen.getByRole('radio', { name: 'Tags' }));
		await waitFor(() => expect(router.state.location.pathname).toBe(`/sites/${ownerSite.siteId}/posts/tags`));
		expect(await screen.findByText('Go')).toBeDefined();
		fireEvent.change(screen.getByLabelText('Enter a tag'), { target: { value: 'Go' } });
		fireEvent.click(screen.getByRole('button', { name: 'Add' }));
		expect(await screen.findByText('This term already exists.')).toBeDefined();
		expect(management.putPostVocabulary).not.toHaveBeenCalled();
	});

	it('creates a directory through the scoped index ETag', async () => {
		const initial = {
			slug: 'notes',
			title: 'notes',
			digest: 'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
			canEdit: true
		};
		vi.mocked(management.getPostDirectory).mockResolvedValue({
			directory: initial,
			etag: '"' + initial.digest + '"'
		});
		vi.mocked(management.putPostDirectory).mockImplementationOnce(async () => {
			vi.mocked(management.getPostTaxonomy).mockResolvedValue({
				...emptyTaxonomy,
				directories: [{ slug: 'notes', title: '笔记', postCount: 0 }]
			});
			return { directory: { ...initial, title: '笔记' }, etag: '"updated"' };
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Categories' }));
		const addCategory = await screen.findByRole('button', { name: 'Add category' });
		await waitFor(() => expect((addCategory as HTMLButtonElement).disabled).toBe(false));
		fireEvent.click(addCategory);
		fireEvent.change(screen.getByLabelText('Category slug'), { target: { value: 'notes' } });
		fireEvent.change(screen.getByLabelText('Display name'), { target: { value: '笔记' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() =>
			expect(management.putPostDirectory).toHaveBeenCalledWith(
				ownerSite.siteId,
				'notes',
				'笔记',
				'"' + initial.digest + '"'
			)
		);
		expect(await screen.findByText('笔记')).toBeDefined();
	});

	it('deletes an unused category with its current revision', async () => {
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			...emptyTaxonomy,
			categoryGroups: [{ name: 'notes', title: 'Notes', directory: 'notes', aliases: ['notes'], postCount: 0 }]
		});
		vi.mocked(management.getPostDirectory).mockResolvedValue({
			directory: { slug: 'notes', title: 'Notes', digest: 'digest', canEdit: true }, etag: '"category-etag"'
		});
		vi.mocked(management.deletePostCategory).mockResolvedValue();
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Categories' }));
		const row = (await screen.findByText('Notes')).closest('tr')!;
		fireEvent.click(within(row).getByRole('button', { name: 'Delete' }));
		await screen.findByText('Delete this unused term?');
		fireEvent.click(screen.getByRole('button', { name: 'OK' }));
		await waitFor(() => expect(management.deletePostCategory).toHaveBeenCalledWith(ownerSite.siteId, 'notes', '"category-etag"'));
	});

	it('renames a Unicode tag with one vocabulary ETag', async () => {
		const tagged = { ...samplePost, tags: ['Go', 'Other'] };
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [tagged], total: 1 });
		vi.mocked(management.getPost).mockResolvedValue({ post: tagged, etag: '"post-etag"' });
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			...emptyTaxonomy, tags: [{ name: 'Go', postCount: 1 }, { name: 'Other', postCount: 1 }]
		});
		vi.mocked(management.renamePostTag).mockImplementation(async () => {
			vi.mocked(management.getPostTaxonomy).mockResolvedValue({
				...emptyTaxonomy, tags: [{ name: 'Beta 战士 👨‍🔬', postCount: 1 }, { name: 'Other', postCount: 1 }]
			});
			return { result: { changed: 1, vocabulary: { version: 1, categories: [], tags: ['Beta 战士 👨‍🔬'] }, digest: 'next' }, etag: '"next"' };
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Tags' }));
		const row = (await screen.findByText('Go')).closest('tr')!;
		fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
		const editor = await screen.findByRole('dialog', { name: 'Edit tag' });
		fireEvent.change(within(editor).getByLabelText('Enter a tag'), { target: { value: 'Beta 战士 👨‍🔬' } });
		fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
		const confirmation = await screen.findByRole('dialog', { name: 'Update the articles using this tag?' });
		fireEvent.click(within(confirmation).getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.renamePostTag).toHaveBeenCalledWith(
			ownerSite.siteId, 'Go', 'Beta 战士 👨‍🔬', '"sha256:' + 'd'.repeat(64) + '"'
		));
		expect(management.putPost).not.toHaveBeenCalled();
	});

	it('keeps the old tag when an article changed concurrently', async () => {
		const tagged = { ...samplePost, tags: ['Go'] };
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [tagged], total: 1 });
		vi.mocked(management.getPost).mockResolvedValue({ post: tagged, etag: '"stale"' });
		vi.mocked(management.getPostTaxonomy).mockResolvedValue({
			...emptyTaxonomy, tags: [{ name: 'Go', postCount: 1 }]
		});
		vi.mocked(management.renamePostTag).mockRejectedValue(new ApiError(412, 'precondition_failed', '', null));
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Tags' }));
		const row = (await screen.findByText('Go')).closest('tr')!;
		fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
		const editor = await screen.findByRole('dialog', { name: 'Edit tag' });
		fireEvent.change(within(editor).getByLabelText('Enter a tag'), { target: { value: 'Rust' } });
		fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
		const confirmation = await screen.findByRole('dialog', { name: 'Update the articles using this tag?' });
		fireEvent.click(within(confirmation).getByRole('button', { name: 'Save' }));
		expect(await screen.findByText('The tag list changed. Review it and retry.')).toBeDefined();
		expect(management.putPostVocabulary).not.toHaveBeenCalled();
		expect(screen.getByText('Go')).toBeDefined();
	});

	it.each(['Beta 战士 👨‍🔬', 'Windows 10 Insider Preview 🐱‍👤', ' WWDC'])(
		'edits the Unicode tag %s without changing its characters', async (tag) => {
			vi.mocked(management.getPostTaxonomy).mockResolvedValue({
				...emptyTaxonomy, tags: [{ name: tag, postCount: 1 }]
			});
			vi.mocked(management.renamePostTag).mockResolvedValue({
				result: { changed: 1, vocabulary: { version: 1, categories: [], tags: [tag + ' revised'] }, digest: 'next' },
				etag: '"next"'
			});
			mountPosts();
			await screen.findByText('Hello');
			fireEvent.click(screen.getByRole('radio', { name: 'Tags' }));
			const row = (await screen.findByText(tag)).closest('tr')!;
			fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
			const editor = await screen.findByRole('dialog', { name: 'Edit tag' });
			fireEvent.change(within(editor).getByLabelText('Enter a tag'), { target: { value: tag + ' revised' } });
			fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
			const confirmation = await screen.findByRole('dialog', { name: 'Update the articles using this tag?' });
			fireEvent.click(within(confirmation).getByRole('button', { name: 'Save' }));
			await waitFor(() => expect(management.renamePostTag).toHaveBeenCalledWith(
				ownerSite.siteId, tag, tag + ' revised', '"sha256:' + 'd'.repeat(64) + '"'
			));
		}
	);

	it.each(['Beta 战士 👨‍🔬', 'Windows 10 Insider Preview 🐱‍👤', ' WWDC'])(
		'edits the migrated Unicode category %s through its identifier', async (title) => {
			const slug = 'category-0123456789abcdef';
			vi.mocked(management.getPostTaxonomy).mockResolvedValue({
				...emptyTaxonomy,
				categoryGroups: [{ name: slug, title, directory: slug, aliases: [slug, title], postCount: 0 }]
			});
			vi.mocked(management.getPostDirectory).mockResolvedValue({
				directory: { slug, title, digest: 'digest', canEdit: true }, etag: '"category-etag"'
			});
			vi.mocked(management.putPostDirectory).mockResolvedValue({
				directory: { slug, title: title + ' updated', digest: 'updated', canEdit: true }, etag: '"updated"'
			});
			mountPosts();
			await screen.findByText('Hello');
			fireEvent.click(screen.getByRole('radio', { name: 'Categories' }));
			const row = (await screen.findByText(title)).closest('tr')!;
			fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
			const editor = await screen.findByRole('dialog', { name: 'Edit category' });
			const name = within(editor).getByLabelText('Display name') as HTMLInputElement;
			expect(name.value).toBe(title);
			fireEvent.change(name, { target: { value: title + ' updated' } });
			fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
			await waitFor(() => expect(management.putPostDirectory).toHaveBeenCalledWith(
				ownerSite.siteId, slug, title + ' updated', '"category-etag"'
			));
		}
	);

	it('does not overwrite an index created after the directory list was loaded', async () => {
		const existing = {
			slug: 'notes',
			title: 'Other author',
			digest: 'sha256:' + 'a'.repeat(64),
			canEdit: true
		};
		vi.mocked(management.getPostDirectory).mockResolvedValue({
			directory: existing,
			etag: '"' + existing.digest + '"'
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Categories' }));
		const addCategory = await screen.findByRole('button', { name: 'Add category' });
		await waitFor(() => expect((addCategory as HTMLButtonElement).disabled).toBe(false));
		fireEvent.click(addCategory);
		fireEvent.change(screen.getByLabelText('Category slug'), { target: { value: 'notes' } });
		fireEvent.change(screen.getByLabelText('Display name'), { target: { value: 'My title' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		expect(await screen.findByText('This category already exists.')).toBeDefined();
		expect(management.putPostDirectory).not.toHaveBeenCalled();
		expect((screen.getByLabelText('Display name') as HTMLInputElement).value).toBe('My title');
	});

	it('bulk imports an image folder while retaining the legacy public path', async () => {
		const path = '/wp-content/uploads/2020/06/image-1.png';
		vi.mocked(management.importPostMedia).mockImplementationOnce(async () => {
			vi.mocked(management.listPostMedia).mockResolvedValue({
				items: [{ path, size: 20 }],
				canEdit: true
			});
			return { path, size: 20 };
		});
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('radio', { name: 'Media' }));
		const folderButton = await screen.findByRole('button', { name: 'Upload folder' });
		await waitFor(() => expect((folderButton as HTMLButtonElement).disabled).toBe(false));
		const image = new File(['image'], 'image-1.png', { type: 'image/png' });
		Object.defineProperty(image, 'webkitRelativePath', {
			value: 'wp-content/uploads/2020/06/image-1.png'
		});
		const picker = document.querySelector<HTMLInputElement>('input[webkitdirectory]')!;
		fireEvent.change(picker, { target: { files: [image] } });
		await waitFor(() =>
			expect(management.importPostMedia).toHaveBeenCalledWith(ownerSite.siteId, image, path)
		);
		expect(await screen.findByText(path)).toBeDefined();
	});
});
