// @vitest-environment jsdom
/**
 * PostsPage mounted-view coverage: the list renders with search + paging
 * bound to the typed API, the draft gate is visible, and viewers get no
 * create action.
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes, createMemoryRouter, RouterProvider, useRouteError } from 'react-router-dom';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { App as AntApp, ConfigProvider } from 'antd';
import { PageHeaderProvider, usePageHeaderContent } from '../src/components/PageHeader';
import PostsPage from '../src/pages/sites/PostsPage';
import * as management from '../src/lib/api/management';
import type { Site } from '../src/lib/api/management';
import { initLocale } from '../src/lib/i18n';

vi.mock('../src/lib/api/management', async (importOriginal) => ({
	...(await importOriginal<object>()),
	getSite: vi.fn(),
	listPosts: vi.fn(),
	createPost: vi.fn(),
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
			{header?.content}
			{header?.actions}
		</div>
	);
}

function RouteErrorDump() {
	const error = useRouteError();
	console.log('ROUTE-ERROR:', error, '| STACK:', JSON.stringify((error as Error)?.stack, null, 0).slice(0, 700));
	return <div>route-crash</div>;
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
								<PostsPage />
							</PageHeaderProvider>
						</AntApp>
					</ConfigProvider>
				),
				errorElement: <RouteErrorDump />
			}
		],
		{ initialEntries: ['/sites/11111111-1111-1111-1111-111111111111/posts'] }
	);
	return render(<RouterProvider router={router} />);
}

describe('PostsPage', () => {
	afterEach(cleanup);

	beforeEach(() => {
		vi.clearAllMocks();
		vi.mocked(management.getSite).mockImplementation(() => Promise.resolve(ownerSite));
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 1 });
		vi.mocked(management.createPost).mockResolvedValue({
			...samplePost,
			category: 'tech',
			digest: 'sha256:' + 'c'.repeat(64)
		});
		vi.mocked(management.getPostTaxonomy).mockResolvedValue(emptyTaxonomy);
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
		expect(screen.getByText('tech/hello.md')).toBeTruthy();
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
	it('renders the search and create action once through the top slot', async () => {
		mountPosts();
		await screen.findByText('Hello');
		expect(screen.getAllByPlaceholderText('Search title / slug / tag')).toHaveLength(1);
		expect(screen.getAllByRole('button', { name: /New post/ })).toHaveLength(1);
	});

	it('paginates with a shared page size and resets the page on search and size changes', async () => {
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 25 });
		mountPosts();
		await screen.findByText('Hello');
		expect(vi.mocked(management.listPosts).mock.calls[0]).toEqual([expect.anything(), '', 1, 20]);

		// Searching returns to page one with the typed query.
		const search = screen.getByPlaceholderText('Search title / slug / tag');
		fireEvent.change(search, { target: { value: 'hello' } });
		fireEvent.keyDown(search, { key: 'Enter' });
		await waitFor(() =>
			expect(vi.mocked(management.listPosts).mock.calls.at(-1)).toEqual([
				expect.anything(),
				'hello',
				1,
				20
			])
		);

		// Page navigation keeps the query and the shared page size.
		fireEvent.click(screen.getByLabelText('Page 2'));
		await waitFor(() =>
			expect(vi.mocked(management.listPosts).mock.calls.at(-1)).toEqual([
				expect.anything(),
				'hello',
				2,
				20
			])
		);

		// Changing the page size returns to page one and drives the same size.
		fireEvent.mouseDown(screen.getByRole('combobox'));
		fireEvent.click(
			await screen.findByText('50 / page', { selector: '.ant-select-item-option-content' })
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

	it('picks taxonomy candidates in the editor and saves arrays, refreshing after the save', async () => {
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
		mountPosts();
		await screen.findByText('Hello');
		const taxonomyCallsBefore = vi.mocked(management.getPostTaxonomy).mock.calls.length;

		fireEvent.click(screen.getByRole('button', { name: /New post/ }));
		await waitFor(() =>
			expect(
				document.querySelector('.ant-modal .ant-form')
			).not.toBeNull()
		);

		// The directory AutoComplete offers the taxonomy directories.
		const category = screen.getByLabelText('Category');
		fireEvent.mouseDown(category);
		fireEvent.change(category, { target: { value: 'tech' } });
		await waitFor(() => {
			const options = Array.from(
				document.querySelectorAll('.ant-select-item-option-content')
			).map((node) => node.textContent);
			if (!options.some((text) => text === 'Tech (tech)')) {
				throw new Error('directory candidates missing');
			}
		});
		fireEvent.keyDown(category, { key: 'Enter' });

		// Existing categories and tags are searchable selections.
		fireEvent.mouseDown(screen.getByLabelText('Categories'));
		fireEvent.click(
			await screen.findByText('go', { selector: '.ant-select-item-option-content' })
		);
		const tags = screen.getByLabelText('Tags');
		fireEvent.mouseDown(tags);
		fireEvent.change(tags, { target: { value: 'newtag' } });
		fireEvent.keyDown(tags, { key: 'Enter', keyCode: 13 });

		// Required title and slug must pass before the form submits.
		fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'kept' } });
		fireEvent.change(screen.getByLabelText('Slug'), { target: { value: 'kept' } });
		fireEvent.click(screen.getByRole('button', { name: 'Save' }));
		await waitFor(() => expect(management.createPost).toHaveBeenCalled());
		const body = vi.mocked(management.createPost).mock.calls.at(-1)?.[1] as {
			category: string;
			categories: string[];
			tags: string[];
		};
		expect(body.category).toBe('tech');
		expect(body.categories).toEqual(['go']);
		expect(body.tags).toEqual(['newtag']);

		// The saved article refreshes the taxonomy for the next editor.
		await waitFor(() =>
			expect(vi.mocked(management.getPostTaxonomy).mock.calls.length).toBeGreaterThan(
				taxonomyCallsBefore
			)
		);
	}, 15000);

	it('shows a retryable taxonomy error and keeps the editor values', async () => {
		vi.mocked(management.getPostTaxonomy).mockRejectedValueOnce(new TypeError('offline'));
		mountPosts();
		await screen.findByText('Hello');
		fireEvent.click(screen.getByRole('button', { name: /New post/ }));
		const title = await screen.findByLabelText('Title');
		console.log(
			'GETSITE-RESULTS:',
			JSON.stringify(vi.mocked(management.getSite).mock.results.map((r) => [r.type, typeof r.value, r.value && typeof (r.value as Promise<unknown>).then])),
			'TAX-RESULTS:',
			JSON.stringify(vi.mocked(management.getPostTaxonomy).mock.results.map((r) => [r.type, typeof r.value, r.value && typeof (r.value as Promise<unknown>).then])),
		);
		fireEvent.change(title, { target: { value: 'kept' } });
		const retry = await screen.findByRole('button', { name: 'Retry' });
		fireEvent.click(retry);
		await waitFor(() =>
			expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
		);
		expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('kept');
		// The retry re-read the taxonomy instead of faking an empty one.
		await waitFor(() => expect(management.getPostTaxonomy).toHaveBeenCalledTimes(2));
	}, 15000);
});
