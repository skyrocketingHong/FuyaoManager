import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Button, Select, Tooltip } from 'antd';
import {
	Bold, Code, Code2, Heading, Image, IndentDecrease, IndentIncrease, Italic,
	Link2, List, ListChecks, ListOrdered, ListTree, Minus, Quote, Redo2,
	Strikethrough, Table2, Undo2
} from 'lucide-react';
import { t } from '../../lib/i18n';
import { markdownCommand, type MarkdownCommand } from './markdownCommands';

type HistoryCommand = 'undo' | 'redo';

export default function MarkdownToolbar({ value, disabled, textarea, onChange, onEmptyToc, controls }: {
	value: string;
	disabled: boolean;
	textarea: () => HTMLTextAreaElement | null;
	onChange: (value: string) => void;
	onEmptyToc: () => void;
	controls: string;
}) {
	const [depth, setDepth] = useState(3);
	const [heading, setHeading] = useState(2);
	const [focused, setFocused] = useState(0);
	const root = useRef<HTMLDivElement>(null);
	const history = useRef({ entries: [value], index: 0 });
	const wasDisabled = useRef(disabled);
	const nativeFocusGroup = typeof HTMLElement !== 'undefined' && 'focusGroup' in HTMLElement.prototype;
	useEffect(() => {
		if (disabled || wasDisabled.current) {
			history.current = { entries: [value], index: 0 };
			wasDisabled.current = disabled;
			return;
		}
		const state = history.current;
		if (state.entries[state.index] === value) return;
		state.entries = [...state.entries.slice(0, state.index + 1), value].slice(-100);
		let size = state.entries.reduce((total, entry) => total + entry.length, 0);
		while (size > 4 * 1024 * 1024 && state.entries.length > 2) {
			size -= state.entries.shift()!.length;
		}
		state.index = state.entries.length - 1;
	}, [value, disabled]);
	const commands = [
		{ key: 'bold', label: t('posts.toolbarBold'), icon: Bold },
		{ key: 'italic', label: t('posts.toolbarItalic'), icon: Italic },
		{ key: 'strike', label: t('posts.toolbarStrike'), icon: Strikethrough },
		{ key: 'link', label: t('posts.toolbarLink'), icon: Link2 },
		{ key: 'image', label: t('posts.toolbarImage'), icon: Image },
		{ key: 'heading', label: t('posts.toolbarHeading'), icon: Heading },
		{ key: 'bullet', label: t('posts.toolbarBullet'), icon: List },
		{ key: 'numbered', label: t('posts.toolbarNumbered'), icon: ListOrdered },
		{ key: 'task', label: t('posts.toolbarTask'), icon: ListChecks },
		{ key: 'quote', label: t('posts.toolbarQuote'), icon: Quote },
		{ key: 'code', label: t('posts.toolbarCode'), icon: Code },
		{ key: 'codeBlock', label: t('posts.toolbarCodeBlock'), icon: Code2 },
		{ key: 'table', label: t('posts.toolbarTable'), icon: Table2 },
		{ key: 'rule', label: t('posts.toolbarRule'), icon: Minus },
		{ key: 'indent', label: t('posts.toolbarIndent'), icon: IndentIncrease },
		{ key: 'outdent', label: t('posts.toolbarOutdent'), icon: IndentDecrease },
		{ key: 'undo', label: t('posts.toolbarUndo'), icon: Undo2 },
		{ key: 'redo', label: t('posts.toolbarRedo'), icon: Redo2 },
		{ key: 'toc', label: t('posts.toolbarToc'), icon: ListTree }
	] as const;

	function keyDown(event: KeyboardEvent<HTMLDivElement>) {
		if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return;
		const buttons = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? []);
		const current = buttons.indexOf(event.target as HTMLButtonElement);
		if (current < 0 || !buttons.length) return;
		event.preventDefault();
		const rtl = root.current ? getComputedStyle(root.current).direction === 'rtl' : false;
		const forward = (event.key === 'ArrowRight') !== rtl;
		const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 :
			(current + (forward ? 1 : -1) + buttons.length) % buttons.length;
		setFocused(next);
		buttons[next].focus();
	}

	function apply(command: MarkdownCommand | HistoryCommand) {
		if (disabled) return;
		const input = textarea();
		if (!input) return;
		if (command === 'undo' || command === 'redo') {
			input.focus();
			const applied = typeof document.execCommand === 'function' && document.execCommand(command);
			if (applied) onChange(input.value);
			else {
				const state = history.current;
				const next = Math.max(0, Math.min(state.entries.length - 1, state.index + (command === 'undo' ? -1 : 1)));
				if (next !== state.index) {
					state.index = next;
					onChange(state.entries[next]);
				}
			}
			return;
		}
		const edit = markdownCommand(command, value, input.selectionStart, input.selectionEnd, {
			text: t('posts.toolbarText'), linkText: t('posts.toolbarLinkText'),
			imageText: t('posts.toolbarImageText'), tableColumn: t('posts.tableColumn'),
			tableValue: t('posts.tableValue'), heading, depth
		});
		if (!edit) {
			onEmptyToc();
			return;
		}
		input.focus();
		input.setSelectionRange(edit.start, edit.end);
		// insertText keeps the browser's native undo stack when supported.
		const applied = typeof document.execCommand === 'function' && document.execCommand('insertText', false, edit.text);
		onChange(applied ? input.value : value.slice(0, edit.start) + edit.text + value.slice(edit.end));
		requestAnimationFrame(() => {
			if (!input.isConnected) return;
			input.focus();
			input.setSelectionRange(edit.selectionStart, edit.selectionEnd);
		});
	}

	return (
		<div className="post-markdown-controls">
			<div ref={root} className="post-markdown-toolbar" role="toolbar"
				{...({ focusgroup: 'toolbar wrap' } as Record<string, string>)}
				aria-label={t('posts.toolbar')} aria-controls={controls}
				onKeyDown={nativeFocusGroup ? undefined : keyDown}>
				<div role="group" aria-label={t('posts.toolbar')}>
					{commands.map(({ key, label, icon: Icon }, index) => (
						<Tooltip title={label} key={key}>
							<Button type="text" icon={<Icon size={16} aria-hidden="true" />} aria-label={label}
								tabIndex={nativeFocusGroup ? undefined : index === focused ? 0 : -1} disabled={disabled}
								onFocus={nativeFocusGroup ? undefined : () => setFocused(index)}
								onMouseDown={(event) => event.preventDefault()} onClick={() => apply(key)} />
						</Tooltip>
					))}
				</div>
			</div>
			<Select aria-label={t('posts.headingLevel')} value={heading} onChange={setHeading} disabled={disabled}
				options={[1, 2, 3, 4, 5, 6].map((value) => ({ value, label: t('posts.headingLevelValue', { count: value }) }))} />
			<Select aria-label={t('posts.tocDepth')} value={depth} onChange={setDepth} disabled={disabled}
				options={[1, 2, 3, 4, 5, 6].map((value) => ({ value, label: t('posts.tocDepthValue', { count: value }) }))} />
		</div>
	);
}
