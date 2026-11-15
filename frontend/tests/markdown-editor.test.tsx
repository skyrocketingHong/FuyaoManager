// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import { useRef, useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import MarkdownPreview from '../src/features/posts/MarkdownPreview';
import MarkdownToolbar from '../src/features/posts/MarkdownToolbar';
import { markdownCommand } from '../src/features/posts/markdownCommands';
import { interpolateScroll } from '../src/features/posts/useMarkdownSync';
import { setLocale } from '../src/lib/i18n';

const options = { text: 'Text', linkText: 'Link text', imageText: 'Image description', tableColumn: 'Column', tableValue: 'Value', heading: 3, depth: 2 };
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it('formats whole selected lines and creates Markdown constructs', () => {
	expect(markdownCommand('heading', 'one\ntwo', 1, 7, options)?.text).toBe('### one\n### two');
	expect(markdownCommand('task', 'one\ntwo', 1, 7, options)?.text).toBe('- [ ] one\n- [ ] two');
	expect(markdownCommand('numbered', 'one\ntwo', 1, 7, options)?.text).toBe('1. one\n2. two');
	expect(markdownCommand('outdent', '  one\n\ttwo', 0, 10, options)?.text).toBe('one\ntwo');
	expect(markdownCommand('strike', 'word', 0, 4, options)?.text).toBe('~~word~~');
	expect(markdownCommand('code', 'a`b', 0, 3, options)?.text).toBe('``a`b``');
	expect(markdownCommand('codeBlock', '```', 0, 3, options)?.text).toBe('````\n```\n````\n');
	expect(markdownCommand('image', 'logo', 0, 4, options)?.text).toBe('![logo](https://)');
	expect(markdownCommand('table', '', 0, 0, options)?.text).toContain('| --- | --- |');
});

it('inserts a depth-limited directory without replacing selected content', () => {
	const source = '# One\n## Two\n### Three';
	const edit = markdownCommand('toc', source, 0, source.length, options)!;
	const result = source.slice(0, edit.start) + edit.text + source.slice(edit.end);
	expect(result).toContain('- [One](#one)\n  - [Two](#two)');
	expect(result).not.toContain('[Three](#three)');
	expect(result.endsWith(source)).toBe(true);
	expect(markdownCommand('toc', 'plain', 0, 0, options)).toBeNull();
});

it('maps scroll positions between unequal source and preview blocks', () => {
	const anchors = [{ source: 0, preview: 0 }, { source: 100, preview: 300 }, { source: 200, preview: 400 }];
	expect(interpolateScroll(anchors, 50)).toBe(150);
	expect(interpolateScroll(anchors, 350, true)).toBe(150);
	expect(interpolateScroll(anchors, 900)).toBe(400);
	expect(interpolateScroll([], 50)).toBe(0);
});

it('retains source ranges through Markdown rendering for caret and block mapping', () => {
	const source = '# Heading\n\nA **bold** [link](https://example.com).\n\n- item\n\n```js\ncode\n```';
	render(<MarkdownPreview source={source} />);
	const heading = screen.getByRole('heading', { name: 'Heading' });
	expect(heading.dataset.sourceBlock).toBe('');
	expect(heading.dataset.sourceStart).toBe('0');
	const bold = screen.getByText('bold');
	expect(bold.dataset.sourceText).toBe('');
	expect(source.slice(Number(bold.dataset.sourceStart), Number(bold.dataset.sourceEnd))).toBe('bold');
	expect(screen.getByText('item').closest('li')?.getAttribute('data-source-block')).toBe('');
	expect(screen.getByText('code').getAttribute('data-source-block')).toBe('');
});

it('applies a toolbar command and supports undo and redo when native commands are unavailable', async () => {
	setLocale('en-US');
	function Editor() {
		const [value, setValue] = useState('hello');
		const textarea = useRef<HTMLTextAreaElement>(null);
		return <>
			<MarkdownToolbar value={value} disabled={false} textarea={() => textarea.current}
				onChange={setValue} onEmptyToc={() => {}} controls="body" />
			<textarea id="body" aria-label="Body" ref={textarea} value={value}
				onChange={(event) => setValue(event.target.value)} />
		</>;
	}
	render(<Editor />);
	const body = screen.getByRole('textbox', { name: 'Body' }) as HTMLTextAreaElement;
	body.setSelectionRange(0, 5);
	fireEvent.click(screen.getByRole('button', { name: 'Strikethrough' }));
	expect(body.value).toBe('~~hello~~');
	fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
	await waitFor(() => expect(body.value).toBe('hello'));
	fireEvent.click(screen.getByRole('button', { name: 'Redo' }));
	await waitFor(() => expect(body.value).toBe('~~hello~~'));
});
