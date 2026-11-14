// @vitest-environment jsdom
/**
 * PostsPage mounted-view coverage: the list renders with search + paging
 * bound to the typed API, the draft gate is visible, and viewers get no
 * create action.
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes, createMemoryRouter, RouterProvider } from 'react-router-dom';
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
	listPosts: vi.fn()
}));

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
				errorElement: <div>route-crash</div>
			}
		],
		{ initialEntries: ['/sites/11111111-1111-1111-1111-111111111111/posts'] }
	);
	return render(<RouterProvider router={router} />);
}

describe('PostsPage', () => {
	afterEach(cleanup);

	beforeEach(() => {
		vi.mocked(management.getSite).mockResolvedValue(ownerSite);
		vi.mocked(management.listPosts).mockResolvedValue({ posts: [samplePost], total: 1 });
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
});
