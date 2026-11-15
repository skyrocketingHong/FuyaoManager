import { memo, useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import { Button, Select } from 'antd';
import {
	Bold, Code, Code2, Heading1, Heading2, Heading3, Heading4, Heading5, Heading6,
	Image, IndentDecrease, IndentIncrease, Italic,
	Link2, List, ListChecks, ListOrdered, ListTree, Minus, Quote, Redo2,
	Strikethrough, Table2, Undo2, type LucideIcon
} from 'lucide-react';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';
import { markdownCommand, type MarkdownCommand } from './markdownCommands';

type HistoryCommand = 'undo' | 'redo';
type ToolbarCommand = {
	key: string;
	command: MarkdownCommand | HistoryCommand;
	label: string;
	icon: LucideIcon;
	heading?: number;
};

function MarkdownToolbar({ value, disabled, textarea, onChange, onEmptyToc, controls }: {
	value: string;
	disabled: boolean;
	textarea: () => HTMLTextAreaElement | null;
	onChange: (value: string) => void;
	onEmptyToc: () => void;
	controls: string;
}) {
	useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const [depth, setDepth] = useState(3);
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
	const commands: ToolbarCommand[] = [
		{ key: 'bold', command: 'bold', label: t('posts.toolbarBold'), icon: Bold },
		{ key: 'italic', command: 'italic', label: t('posts.toolbarItalic'), icon: Italic },
		{ key: 'strike', command: 'strike', label: t('posts.toolbarStrike'), icon: Strikethrough },
		{ key: 'link', command: 'link', label: t('posts.toolbarLink'), icon: Link2 },
		{ key: 'image', command: 'image', label: t('posts.toolbarImage'), icon: Image },
		...[Heading1, Heading2, Heading3, Heading4, Heading5, Heading6].map((icon, index) => ({
			key: `heading-${index + 1}`, command: 'heading' as const,
			label: t('posts.headingLevelValue', { count: index + 1 }), icon, heading: index + 1
		})),
		{ key: 'bullet', command: 'bullet', label: t('posts.toolbarBullet'), icon: List },
		{ key: 'numbered', command: 'numbered', label: t('posts.toolbarNumbered'), icon: ListOrdered },
		{ key: 'task', command: 'task', label: t('posts.toolbarTask'), icon: ListChecks },
		{ key: 'quote', command: 'quote', label: t('posts.toolbarQuote'), icon: Quote },
		{ key: 'code', command: 'code', label: t('posts.toolbarCode'), icon: Code },
		{ key: 'codeBlock', command: 'codeBlock', label: t('posts.toolbarCodeBlock'), icon: Code2 },
		{ key: 'table', command: 'table', label: t('posts.toolbarTable'), icon: Table2 },
		{ key: 'rule', command: 'rule', label: t('posts.toolbarRule'), icon: Minus },
		{ key: 'indent', command: 'indent', label: t('posts.toolbarIndent'), icon: IndentIncrease },
		{ key: 'outdent', command: 'outdent', label: t('posts.toolbarOutdent'), icon: IndentDecrease },
		{ key: 'undo', command: 'undo', label: t('posts.toolbarUndo'), icon: Undo2 },
		{ key: 'redo', command: 'redo', label: t('posts.toolbarRedo'), icon: Redo2 }
	];

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

	function apply(command: MarkdownCommand | HistoryCommand, heading = 2) {
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
					{commands.map(({ key, command, label, icon: Icon, heading }, index) => (
						<Button key={key} type="text" title={label} icon={<Icon size={16} aria-hidden="true" />}
							aria-label={label} tabIndex={nativeFocusGroup ? undefined : index === focused ? 0 : -1}
							disabled={disabled} onFocus={nativeFocusGroup ? undefined : () => setFocused(index)}
							onMouseDown={(event) => event.preventDefault()} onClick={() => apply(command, heading)} />
					))}
				</div>
			</div>
			<div className="post-toc-controls">
				<Button type="text" title={t('posts.toolbarToc')} aria-label={t('posts.toolbarToc')}
					aria-controls={controls} icon={<ListTree size={16} aria-hidden="true" />} disabled={disabled}
					onMouseDown={(event) => event.preventDefault()} onClick={() => apply('toc')} />
				<Select aria-label={t('posts.tocDepth')} value={depth} onChange={setDepth} disabled={disabled}
					options={[1, 2, 3, 4, 5, 6].map((value) => ({ value, label: t('posts.tocDepthValue', { count: value }) }))} />
			</div>
		</div>
	);
}

export default memo(MarkdownToolbar);
