import { useRef, useState, type KeyboardEvent } from 'react';
import { Button, Select, Tooltip } from 'antd';
import {
	Bold,
	Code,
	Heading2,
	Italic,
	Link2,
	List,
	ListOrdered,
	ListTree,
	Quote
} from 'lucide-react';
import { t } from '../../lib/i18n';
import { buildTableOfContents } from './markdown';

type Command =
	| 'bold'
	| 'italic'
	| 'link'
	| 'heading'
	| 'bullet'
	| 'numbered'
	| 'quote'
	| 'code'
	| 'toc';

export default function MarkdownToolbar({
	value,
	disabled,
	textarea,
	onChange,
	onEmptyToc,
	controls
}: {
	value: string;
	disabled: boolean;
	textarea: () => HTMLTextAreaElement | null;
	onChange: (value: string) => void;
	onEmptyToc: () => void;
	controls: string;
}) {
	const [depth, setDepth] = useState(3);
	const [focused, setFocused] = useState(0);
	const root = useRef<HTMLDivElement>(null);
	const commands = [
		{ key: 'bold', label: t('posts.toolbarBold'), icon: Bold },
		{ key: 'italic', label: t('posts.toolbarItalic'), icon: Italic },
		{ key: 'link', label: t('posts.toolbarLink'), icon: Link2 },
		{ key: 'heading', label: t('posts.toolbarHeading'), icon: Heading2 },
		{ key: 'bullet', label: t('posts.toolbarBullet'), icon: List },
		{ key: 'numbered', label: t('posts.toolbarNumbered'), icon: ListOrdered },
		{ key: 'quote', label: t('posts.toolbarQuote'), icon: Quote },
		{ key: 'code', label: t('posts.toolbarCode'), icon: Code },
		{ key: 'toc', label: t('posts.toolbarToc'), icon: ListTree }
	] as const;

	function keyDown(event: KeyboardEvent<HTMLDivElement>) {
		if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return;
		const buttons = Array.from(
			root.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []
		);
		const current = buttons.indexOf(event.target as HTMLButtonElement);
		if (current < 0 || !buttons.length) return;
		event.preventDefault();
		const next =
			event.key === 'Home'
				? 0
				: event.key === 'End'
					? buttons.length - 1
					: (current + (event.key === 'ArrowRight' ? 1 : -1) + buttons.length) % buttons.length;
		setFocused(next);
		buttons[next].focus();
	}

	function apply(command: Command) {
		if (disabled) return;
		const input = textarea();
		const start = input?.selectionStart ?? value.length;
		const end = input?.selectionEnd ?? start;
		const selected = value.slice(start, end);
		let inserted = '';
		let from = 0;
		let to = 0;
		const text = selected || t('posts.toolbarText');
		switch (command) {
			case 'bold':
				inserted = `**${text}**`;
				from = 2;
				to = from + text.length;
				break;
			case 'italic':
				inserted = `*${text}*`;
				from = 1;
				to = from + text.length;
				break;
			case 'link':
				inserted = `[${selected || t('posts.toolbarLinkText')}](https://)`;
				from = inserted.indexOf('https://');
				to = from + 8;
				break;
			case 'heading':
				inserted = `## ${text}`;
				from = 3;
				to = from + text.length;
				break;
			case 'bullet':
				inserted = text
					.split('\n')
					.map((line) => `- ${line}`)
					.join('\n');
				from = 2;
				to = inserted.length;
				break;
			case 'numbered':
				inserted = text
					.split('\n')
					.map((line, index) => `${index + 1}. ${line}`)
					.join('\n');
				from = 3;
				to = inserted.length;
				break;
			case 'quote':
				inserted = text
					.split('\n')
					.map((line) => `> ${line}`)
					.join('\n');
				from = 2;
				to = inserted.length;
				break;
			case 'code':
				inserted = `\`${text}\``;
				from = 1;
				to = from + text.length;
				break;
			case 'toc': {
				const toc = buildTableOfContents(value, depth);
				if (!toc) {
					onEmptyToc();
					return;
				}
				inserted = `${start && value[start - 1] !== '\n' ? '\n' : ''}${toc}\n`;
				from = to = inserted.length;
				break;
			}
		}
		onChange(value.slice(0, start) + inserted + value.slice(end));
		requestAnimationFrame(() => {
			if (!input?.isConnected) return;
			input.focus();
			input.setSelectionRange(start + from, start + to);
		});
	}

	return (
		<div className="post-markdown-controls">
			<div
				ref={root}
				className="post-markdown-toolbar"
				role="toolbar"
				aria-label={t('posts.toolbar')}
				aria-controls={controls}
				onKeyDown={keyDown}
			>
				{commands.map(({ key, label, icon: Icon }, index) => (
					<Tooltip title={label} key={key}>
						<Button
							type="text"
							icon={<Icon size={16} aria-hidden="true" />}
							aria-label={label}
							tabIndex={index === focused ? 0 : -1}
							disabled={disabled}
							onFocus={() => setFocused(index)}
							onMouseDown={(event) => event.preventDefault()}
							onClick={() => apply(key)}
						/>
					</Tooltip>
				))}
			</div>
			<Select
				aria-label={t('posts.tocDepth')}
				value={depth}
				onChange={setDepth}
				disabled={disabled}
				options={[1, 2, 3, 4, 5, 6].map((value) => ({
					value,
					label: t('posts.tocDepthValue', { count: value })
				}))}
			/>
		</div>
	);
}
