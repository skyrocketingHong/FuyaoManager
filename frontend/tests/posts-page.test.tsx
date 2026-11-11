// @vitest-environment jsdom
/**
 * PostsPage mounted-view coverage: the list renders with search + paging
 * bound to the typed API, the draft gate is visible, and viewers get no
 * create action.
 */
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import PostsPage from '../src/pages/sites/PostsPage';
import * as management from '../src/lib/api/management';
import { initLocale } from '../src/lib/i18n';

vi.mock('../src/lib/api/management', async (importOriginal) => ({
	...(await importOriginal<object>()),
	getSite: vi.fn(),
	listPosts: vi.fn()
}));

const ownerSite = {
	siteId: '11111111-1111-1111-1111-111111111111',
	slug: 'blog',
	name: 'Blog',
	description: '',
	defaultLocale: 'zh-CN',
	visibility: 'private' as const,
	provisionState: 'ready' as const,
	role: 'owner' as const,
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

describe('PostsPage', () => {
	afterEach(cleanup)

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
});
