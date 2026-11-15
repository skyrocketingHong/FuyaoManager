import { getPost, listPosts, putPost, type Post } from '../../lib/api/management';
import type { ContentTarget } from '../../lib/api/content';

function withTags(post: Post, tags: string[]): Partial<Post> {
	return {
		category: post.category,
		slug: post.slug,
		title: post.title,
		date: post.date,
		lastmod: new Date().toISOString(),
		draft: post.draft,
		categories: post.categories,
		tags,
		description: post.description,
		cover: post.cover,
		coverAlt: post.coverAlt,
		coverSource: post.coverSource,
		coverSourceLink: post.coverSourceLink,
		body: post.body
	};
}

// Enumerate before changing any files so reordering cannot skip later pages.
export async function updatePostTags(
	target: ContentTarget,
	from: string,
	to: string | null,
	isCurrent: () => boolean
): Promise<{ changed: number; failed: number }> {
	const paths = new Set<string>();
	const size = 100;
	for (let page = 1; isCurrent(); page++) {
		const result = await listPosts(target, '', page, size);
		if (!isCurrent()) return { changed: 0, failed: 0 };
		for (const post of result.posts) {
			if (post.tags?.includes(from)) paths.add(post.path);
		}
		if (!result.posts.length || page * size >= result.total) break;
	}
	let changed = 0;
	let failed = 0;
	for (const path of paths) {
		if (!isCurrent()) break;
		try {
			const { post, etag } = await getPost(target, path);
			if (!isCurrent()) break;
			if (!post.tags?.includes(from)) continue;
			const tags = Array.from(new Set(post.tags.flatMap((tag) => tag === from ? (to ? [to] : []) : [tag])));
			await putPost(target, path, withTags(post, tags), etag);
			changed++;
		} catch {
			failed++;
		}
	}
	return { changed, failed };
}
