import { beforeEach, expect, it, vi } from 'vitest';
import { api } from '../src/lib/api/client';
import { contentResourcePath, platformContent, putContentSources } from '../src/lib/api/content';
import {
	getFriends,
	getPost,
	getPostDirectory,
	listPosts,
	processAlbums,
	putAlbumYear,
	putFriends,
	putPost,
	putPostDirectory,
	postPreviewImageURL
} from '../src/lib/api/management';

vi.mock('../src/lib/api/client', () => ({ api: vi.fn(), setCsrfToken: vi.fn() }));
beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(api).mockResolvedValue({ data: {}, etag: '"content"' });
});

it('keeps the returned HTTP ETag for consecutive article writes and directory updates', async () => {
	vi.mocked(api).mockResolvedValue({
		data: { digest: 'unquoted-json-digest' },
		etag: '"exact-response-etag"'
	});
	const saved = await putPost('website', 'tech/hello.md', { title: 'Updated' }, '"previous"');
	expect(saved.etag).toBe('"exact-response-etag"');
	expect(api).toHaveBeenLastCalledWith('PUT', '/api/v1/sites/website/posts/tech%2Fhello.md', {
		body: { title: 'Updated' },
		etag: '"previous"'
	});
	await getPostDirectory(platformContent, 'notes');
	expect(api).toHaveBeenLastCalledWith('GET', '/api/v1/content/posts/directories/notes');
	await putPostDirectory(platformContent, 'notes', '笔记', '"index"');
	expect(api).toHaveBeenLastCalledWith('PUT', '/api/v1/content/posts/directories/notes', {
		body: { title: '笔记' },
		etag: '"index"'
	});
});

it('routes platform resources without a site identity and retains existing site resource paths', () => {
	for (const resource of ['posts', 'albums', 'friends', 'footprints'] as const) {
		expect(contentResourcePath(platformContent, resource)).toBe(`/api/v1/content/${resource}`);
		expect(contentResourcePath('website', resource)).toBe(
			`/api/v1/sites/website/${resource === 'friends' || resource === 'footprints' ? 'content/' : ''}${resource}`
		);
	}
});

it('resolves legacy image paths through the authenticated source-scoped endpoint', () => {
	expect(postPreviewImageURL(platformContent, '/wp-content/uploads/2020/06/image-1.png')).toBe(
		'/api/v1/content/posts/media/file?path=%2Fwp-content%2Fuploads%2F2020%2F06%2Fimage-1.png'
	);
	expect(postPreviewImageURL('site', 'https://other.example/a.png')).toBe(
		'https://other.example/a.png'
	);
});
it('uses the platform prefix for list, encoded post details, media tasks and conditional writes', async () => {
	await listPosts(platformContent, 'search me', 2, 10);
	expect(api).toHaveBeenLastCalledWith(
		'GET',
		'/api/v1/content/posts?query=search%20me&page=2&pageSize=10'
	);
	await getPost(platformContent, 'tech/hello.md');
	expect(api).toHaveBeenLastCalledWith('GET', '/api/v1/content/posts/tech%2Fhello.md');
	await getFriends(platformContent);
	expect(api).toHaveBeenLastCalledWith('GET', '/api/v1/content/friends');
	await putFriends(platformContent, { online: [], offline: [] }, '"friends"');
	expect(api).toHaveBeenLastCalledWith('PUT', '/api/v1/content/friends', {
		body: { friends: { online: [], offline: [] } },
		etag: '"friends"'
	});
	await processAlbums(platformContent);
	expect(api).toHaveBeenLastCalledWith('POST', '/api/v1/content/albums/process', { body: {} });
	await putAlbumYear(platformContent, '2026', [], '"album"');
	expect(api).toHaveBeenLastCalledWith('PUT', '/api/v1/content/albums/2026', {
		body: { year: { title: '', cover: '', photos: [] } },
		etag: '"album"'
	});
});
it('keeps a source binding revision separate from content digests', async () => {
	const body = {
		sources: {
			posts: 'platform',
			albums: 'independent',
			footprints: 'platform',
			friends: 'independent'
		} as const,
		revision: 3
	};
	await putContentSources('website', body, '"sources-3"');
	expect(api).toHaveBeenCalledExactlyOnceWith('PUT', '/api/v1/sites/website/content-sources', {
		body,
		etag: '"sources-3"'
	});
});
