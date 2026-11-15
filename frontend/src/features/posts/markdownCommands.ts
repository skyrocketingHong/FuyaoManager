import { buildTableOfContents } from './markdown';

export type MarkdownCommand =
	| 'bold' | 'italic' | 'strike' | 'link' | 'image' | 'heading'
	| 'bullet' | 'numbered' | 'task' | 'quote' | 'code' | 'codeBlock'
	| 'table' | 'rule' | 'indent' | 'outdent' | 'toc';

type CommandOptions = {
	text: string;
	linkText: string;
	imageText: string;
	tableColumn: string;
	tableValue: string;
	heading: number;
	depth: number;
};
type Edit = { start: number; end: number; text: string; selectionStart: number; selectionEnd: number };

/** Describe a single textarea replacement, keeping selections in source coordinates. */
export function markdownCommand(
	command: MarkdownCommand,
	source: string,
	selectionStart: number,
	selectionEnd: number,
	options: CommandOptions
): Edit | null {
	let start = Math.max(0, Math.min(selectionStart, source.length));
	let end = Math.max(start, Math.min(selectionEnd, source.length));
	const selected = source.slice(start, end);
	const content = selected || options.text;
	let text = '';
	let from = 0;
	let to = 0;
	const wrap = (before: string, after = before) => {
		text = `${before}${content}${after}`;
		from = before.length;
		to = from + content.length;
	};
	switch (command) {
		case 'bold': wrap('**'); break;
		case 'italic': wrap('*'); break;
		case 'strike': wrap('~~'); break;
		case 'code': {
			const runs = content.match(/`+/g) ?? [];
			const delimiter = '`'.repeat(Math.max(0, ...runs.map((run) => run.length)) + 1);
			const pad = content.startsWith('`') || content.endsWith('`') ? ' ' : '';
			wrap(delimiter + pad, pad + delimiter);
			break;
		}
		case 'link':
		case 'image': {
			const label = (selected || (command === 'image' ? options.imageText : options.linkText))
				.replace(/[\\\[\]]/g, '\\$&');
			text = `${command === 'image' ? '!' : ''}[${label}](https://)`;
			from = text.indexOf('https://');
			to = from + 'https://'.length;
			break;
		}
		case 'heading':
		case 'bullet':
		case 'numbered':
		case 'task':
		case 'quote':
		case 'indent':
		case 'outdent': {
			start = start ? source.lastIndexOf('\n', start - 1) + 1 : 0;
			const lineEnd = source.indexOf('\n', end > start && source[end - 1] === '\n' ? end - 1 : end);
			end = lineEnd < 0 ? source.length : lineEnd;
			const lines = source.slice(start, end).split('\n');
			text = lines.map((line, index) => {
				switch (command) {
					case 'heading': return `${'#'.repeat(Math.min(6, Math.max(1, options.heading)))} ${line.replace(/^#{1,6}\s+/, '') || options.text}`;
					case 'bullet': return `- ${line || options.text}`;
					case 'numbered': return `${index + 1}. ${line || options.text}`;
					case 'task': return `- [ ] ${line || options.text}`;
					case 'quote': return `> ${line}`;
					case 'indent': return `  ${line}`;
					case 'outdent': return line.replace(/^(?: {1,2}|\t)/, '');
				}
				return line;
			}).join('\n');
			from = 0;
			to = text.length;
			break;
		}
		case 'codeBlock': {
			const runs = content.match(/`+/g) ?? [];
			const fence = '`'.repeat(Math.max(3, ...runs.map((run) => run.length + 1)));
			const prefix = start && source[start - 1] !== '\n' ? '\n' : '';
			text = `${prefix}${fence}\n${content}\n${fence}\n`;
			from = prefix.length + fence.length + 1;
			to = from + content.length;
			break;
		}
		case 'table': {
			text = `| ${options.tableColumn} 1 | ${options.tableColumn} 2 |\n| --- | --- |\n| ${options.tableValue} 1 | ${options.tableValue} 2 |`;
			from = 2;
			to = from + options.tableColumn.length + 2;
			break;
		}
		case 'rule': {
			const prefix = start && source[start - 1] !== '\n' ? '\n' : '';
			text = `${prefix}\n---\n\n`;
			from = to = text.length;
			break;
		}
		case 'toc': {
			const toc = buildTableOfContents(source, options.depth);
			if (!toc) return null;
			// A selection is a position for the inserted directory, not content to discard.
			end = start;
			text = `${start && source[start - 1] !== '\n' ? '\n' : ''}${toc}\n\n`;
			from = to = text.length;
			break;
		}
	}
	return { start, end, text, selectionStart: start + from, selectionEnd: start + to };
}
