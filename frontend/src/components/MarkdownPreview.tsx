import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

export default function MarkdownPreview({ source }: { source: string }) {
	return (
		<div className="markdown-preview">
			<ReactMarkdown
				remarkPlugins={[remarkGfm]}
				skipHtml
				components={{
					a: ({ node: _node, ...props }) => (
						<a {...props} target="_blank" rel="noopener noreferrer" />
					)
				}}
			>
				{source}
			</ReactMarkdown>
		</div>
	);
}
