import { useId, useMemo, useRef } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import type { PluggableList } from 'unified';
import { remarkPreviewHeadings, remarkSourcePositions } from './markdown';

export default function MarkdownPreview({
	source,
	imageURL
}: {
	source: string;
	imageURL?: (source: string) => string;
}) {
	const root = useRef<HTMLDivElement>(null);
	const id = useId().replace(/[^a-zA-Z0-9_-]/g, '');
	const plugins = useMemo<PluggableList>(
		() => [remarkGfm, [remarkPreviewHeadings, { prefix: `post-preview-${id}` }], remarkSourcePositions],
		[id]
	);
	return (
		<div className="markdown-preview" ref={root}>
			<ReactMarkdown
				remarkPlugins={plugins}
				skipHtml
				components={{
					img: ({ node: _node, src, ...props }) => (
						<img {...props} src={src && imageURL ? imageURL(src) : src} />
					),
					a: ({ node: _node, href, ...props }) => {
						if (!href?.startsWith('#'))
							return <a {...props} href={href} target="_blank" rel="noopener noreferrer" />;
						return (
							<a
								{...props}
								href={href}
								onClick={(event) => {
									if (
										event.defaultPrevented ||
										event.button !== 0 ||
										event.metaKey ||
										event.ctrlKey ||
										event.shiftKey ||
										event.altKey
									)
										return;
									event.preventDefault();
									let fragment = href.slice(1);
									try {
										fragment = decodeURIComponent(fragment);
									} catch {
										/* Preserve malformed legacy fragments. */
									}
									const nodes = Array.from(
										root.current?.querySelectorAll<HTMLElement>('[data-post-heading]') ?? []
									);
									const target =
										nodes.find((element) => element.dataset.postHeading === fragment) ??
										nodes.find(
											(element) => `user-content-${element.dataset.postHeading}` === fragment
										);
									target?.scrollIntoView?.({ block: 'start' });
									target?.focus({ preventScroll: true });
								}}
							/>
						);
					}
				}}
			>
				{source}
			</ReactMarkdown>
		</div>
	);
}
