import { beforeEach, expect, it, vi } from 'vitest';
import { updatePostTags } from '../src/features/posts/manageTags';
import * as api from '../src/lib/api/management';

vi.mock('../src/lib/api/management', () => ({ listPosts: vi.fn(), getPost: vi.fn(), putPost: vi.fn() }));

const post: api.Post = {
	path: 'tech/a.md', category: 'tech', slug: 'a', title: 'A', date: '2026-01-01T00:00:00Z',
	draft: false, categories: ['Tech'], tags: ['Go', 'Other'], body: 'Unchanged body', digest: 'digest'
};

beforeEach(() => vi.resetAllMocks());

it('updates all collected pages with ETags and reports conflicts without overwriting', async () => {
	vi.mocked(api.listPosts)
		.mockResolvedValueOnce({ posts: [post], total: 101 })
		.mockResolvedValueOnce({ posts: [{ ...post, path: 'tech/b.md', slug: 'b' }], total: 101 });
	vi.mocked(api.getPost).mockImplementation(async (_, path) => ({
		post: { ...post, path, slug: path.endsWith('b.md') ? 'b' : 'a' }, etag: `"${path}"`
	}));
	vi.mocked(api.putPost)
		.mockResolvedValueOnce({ post, etag: '"saved"' })
		.mockRejectedValueOnce(new Error('concurrent edit'));
	const result = await updatePostTags('site', 'Go', 'Rust', () => true);
	expect(result).toEqual({ changed: 1, failed: 1 });
	expect(api.listPosts).toHaveBeenCalledTimes(2);
	expect(api.putPost).toHaveBeenCalledTimes(2);
	expect(api.putPost).toHaveBeenNthCalledWith(1, 'site', 'tech/a.md', expect.objectContaining({
		tags: ['Rust', 'Other'], body: 'Unchanged body', categories: ['Tech'], lastmod: expect.stringMatching(/Z$/)
	}), '"tech/a.md"');
	expect(api.putPost).toHaveBeenNthCalledWith(2, 'site', 'tech/b.md', expect.anything(), '"tech/b.md"');
});

it('removes only the chosen tag and preserves the article content', async () => {
	vi.mocked(api.listPosts).mockResolvedValue({ posts: [post], total: 1 });
	vi.mocked(api.getPost).mockResolvedValue({ post, etag: '"current"' });
	vi.mocked(api.putPost).mockResolvedValue({ post, etag: '"saved"' });
	expect(await updatePostTags('site', 'Go', null, () => true)).toEqual({ changed: 1, failed: 0 });
	expect(api.putPost).toHaveBeenCalledWith('site', post.path, expect.objectContaining({
		tags: ['Other'], body: 'Unchanged body', categories: ['Tech']
	}), '"current"');
});
