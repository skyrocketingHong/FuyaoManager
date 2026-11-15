import GithubSlugger from 'github-slugger';
import { toString } from 'mdast-util-to-string';
import remarkGfm from 'remark-gfm';
import remarkParse from 'remark-parse';
import { unified } from 'unified';
import type { Heading, Root } from 'mdast';

type HeadingInfo = { node: Heading; text: string; slug: string; depth: number };

function headings(tree: Root): HeadingInfo[] {
	const slugger = new GithubSlugger();
	const result: HeadingInfo[] = [];
	function visit(node: { type: string; children?: unknown[] }) {
		if (node.type === 'heading') {
			const heading = node as Heading;
			const content = toString(heading, { includeHtml: false, includeImageAlt: false });
			result.push({
				node: heading,
				text: content.trim(),
				slug: slugger.slug(content),
				depth: heading.depth
			});
		}
		for (const child of node.children ?? []) {
			if (child && typeof child === 'object' && 'type' in child)
				visit(child as { type: string; children?: unknown[] });
		}
	}
	visit(tree);
	return result;
}

/** Attach local preview targets without putting article fragments into the app route. */
export function remarkPreviewHeadings(options: { prefix: string }) {
	return (tree: Root) => {
		for (const heading of headings(tree)) {
			heading.node.data ??= {};
			const properties = (heading.node.data.hProperties ?? {}) as Record<string, unknown>;
			heading.node.data.hProperties = {
				...properties,
				id: `${options.prefix}-${heading.slug}`,
				'data-post-heading': heading.slug,
				tabIndex: -1
			};
		}
	};
}

/** Retain source positions on rendered blocks and text for editor synchronization. */
export function remarkSourcePositions() {
	return (tree: Root) => {
		type Positioned = {
			type: string;
			children?: unknown[];
			position?: { start: { offset?: number }; end: { offset?: number } };
			data?: { hName?: string; hProperties?: Record<string, unknown> };
		};
		const blocks = new Set(['heading', 'paragraph', 'listItem', 'code', 'thematicBreak', 'tableRow', 'image']);
		const text = new Set(['text', 'inlineCode', 'code', 'image']);
		function visit(node: Positioned) {
			const start = node.position?.start.offset;
			const end = node.position?.end.offset;
			if (start !== undefined && end !== undefined && (blocks.has(node.type) || text.has(node.type))) {
				node.data ??= {};
				if (node.type === 'text') node.data.hName = 'span';
				node.data.hProperties = {
					...node.data.hProperties,
					'data-source-start': start,
					'data-source-end': end,
					...(blocks.has(node.type) ? { 'data-source-block': '' } : {}),
					...(text.has(node.type) ? { 'data-source-text': '' } : {})
				};
			}
			for (const child of node.children ?? []) {
				if (child && typeof child === 'object' && 'type' in child) visit(child as Positioned);
			}
		}
		visit(tree);
	};
}

/** Build links with the same github-slugger rules used by the published theme. */
export function buildTableOfContents(source: string, maxDepth: number): string {
	const tree = unified().use(remarkParse).use(remarkGfm).parse(source);
	const all = headings(tree);
	const stack: number[] = [];
	return all
		.filter((heading) => heading.depth <= maxDepth && heading.text && heading.slug)
		.map((heading) => {
			while (stack.length && stack[stack.length - 1] >= heading.depth) stack.pop();
			const label = heading.text.replace(/[\\\[\]]/g, '\\$&');
			const line = `${'  '.repeat(stack.length)}- [${label}](#${encodeURIComponent(heading.slug)})`;
			stack.push(heading.depth);
			return line;
		})
		.join('\n');
}
