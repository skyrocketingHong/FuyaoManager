import { useEffect, type RefObject } from 'react';

type Anchor = { source: number; preview: number };

/** Interpolate between actual source and rendered block positions. */
export function interpolateScroll(anchors: Anchor[], value: number, reverse = false): number {
	if (!anchors.length) return 0;
	const key = reverse ? 'preview' : 'source';
	const result = reverse ? 'source' : 'preview';
	if (value <= anchors[0][key]) return anchors[0][result];
	for (let i = 1; i < anchors.length; i++) {
		const previous = anchors[i - 1];
		const next = anchors[i];
		if (value <= next[key]) {
			const distance = next[key] - previous[key];
			return distance > 0
				? previous[result] + (next[result] - previous[result]) * (value - previous[key]) / distance
				: next[result];
		}
	}
	return anchors[anchors.length - 1][result];
}

function sourceOffsetToText(raw: string, rendered: string, offset: number): number {
	let source = 0;
	let text = 0;
	const limit = Math.max(0, Math.min(offset, raw.length));
	while (source < limit && text < rendered.length) {
		if (raw[source] === '\\' && source + 1 < limit && raw[source + 1] === rendered[text]) {
			source++;
		}
		if (raw[source] === '&') {
			const entity = raw.slice(source).match(/^&(?:#[0-9]+|#x[0-9a-f]+|[a-z][a-z0-9]+);/i)?.[0];
			if (entity && source + entity.length <= limit) {
				const decoder = document.createElement('textarea');
				decoder.innerHTML = entity;
				if (decoder.value !== entity && rendered.startsWith(decoder.value, text)) {
					text += decoder.value.length;
					source += entity.length;
					continue;
				}
			}
		}
		if (raw[source] === rendered[text]) text++;
		source++;
	}
	return text;
}

function textPosition(element: HTMLElement, offset: number): { node: Text; offset: number } | null {
	const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
	let remaining = offset;
	let last: Text | null = null;
	while (walker.nextNode()) {
		const node = walker.currentNode as Text;
		last = node;
		if (remaining <= node.length) return { node, offset: remaining };
		remaining -= node.length;
	}
	return last ? { node: last, offset: last.length } : null;
}

function measureSource(textarea: HTMLTextAreaElement, source: string, offsets: number[]): number[] {
	const style = getComputedStyle(textarea);
	const mirror = document.createElement('div');
	const properties = [
		'fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'fontVariant', 'lineHeight',
		'letterSpacing', 'wordSpacing', 'textIndent', 'textTransform', 'tabSize', 'direction',
		'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft', 'wordBreak', 'overflowWrap'
	] as const;
	for (const property of properties) mirror.style[property] = style[property];
	Object.assign(mirror.style, {
		position: 'fixed', left: '-100000px', top: '0', visibility: 'hidden',
		boxSizing: 'border-box', width: `${textarea.clientWidth}px`, whiteSpace: 'pre-wrap',
		overflow: 'hidden', border: '0', height: 'auto'
	});
	mirror.setAttribute('aria-hidden', 'true');
	const markers: HTMLSpanElement[] = [];
	let last = 0;
	for (const offset of offsets) {
		mirror.append(document.createTextNode(source.slice(last, offset)));
		const marker = document.createElement('span');
		marker.textContent = '\u200b';
		mirror.append(marker);
		markers.push(marker);
		last = offset;
	}
	mirror.append(document.createTextNode(source.slice(last) + '\u200b'));
	document.body.append(mirror);
	const top = mirror.getBoundingClientRect().top;
	const positions = markers.map((marker) => marker.getBoundingClientRect().top - top);
	mirror.remove();
	return positions;
}

export function useMarkdownSync({ textarea, preview, cursor, source, rendered, enabled }: {
	textarea: () => HTMLTextAreaElement | null;
	preview: RefObject<HTMLElement>;
	cursor: RefObject<HTMLElement>;
	source: string;
	rendered: string;
	enabled: boolean;
}) {
	useEffect(() => {
		const input = textarea();
		const pane = preview.current;
		const marker = cursor.current;
		if (!input || !pane || !marker || !enabled || source !== rendered) {
			if (marker) marker.hidden = true;
			return;
		}
		const previewPane = pane;
		const caret = marker;
		let anchors: Anchor[] = [];
		let frame = 0;
		let expectedSource: number | null = null;
		let expectedPreview: number | null = null;

		function rebuild() {
			const blocks = Array.from(previewPane.querySelectorAll<HTMLElement>('[data-source-block]'))
				.map((element) => ({ element, offset: Number(element.dataset.sourceStart) }))
				.filter(({ offset }) => Number.isFinite(offset) && offset >= 0 && offset <= source.length)
				.sort((a, b) => a.offset - b.offset);
			const positions = measureSource(input!, source, blocks.map(({ offset }) => offset));
			const paneTop = previewPane.getBoundingClientRect().top;
			const next: Anchor[] = [{ source: 0, preview: 0 }];
			for (let i = 0; i < blocks.length; i++) {
				const point = {
					source: positions[i],
					preview: blocks[i].element.getBoundingClientRect().top - paneTop + previewPane.scrollTop
				};
				const previous = next[next.length - 1];
				if (point.source > previous.source && point.preview > previous.preview) next.push(point);
			}
			const end = { source: Math.max(0, input!.scrollHeight - input!.clientHeight),
				preview: Math.max(0, previewPane.scrollHeight - previewPane.clientHeight) };
			anchors = next.filter((point) => point.source < end.source && point.preview < end.preview);
			anchors.push(end);
			updateCaret(false);
		}

		function scheduleRebuild() {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(rebuild);
		}
		function sourceScroll() {
			if (expectedSource !== null && Math.abs(input!.scrollTop - expectedSource) < 1) {
				expectedSource = null;
				return;
			}
			expectedSource = null;
			const next = interpolateScroll(anchors, input!.scrollTop);
			if (Math.abs(previewPane.scrollTop - next) > 1) {
				expectedPreview = next;
				previewPane.scrollTop = next;
			}
		}
		function previewScroll() {
			if (expectedPreview !== null && Math.abs(previewPane.scrollTop - expectedPreview) < 1) {
				expectedPreview = null;
				return;
			}
			expectedPreview = null;
			const next = interpolateScroll(anchors, previewPane.scrollTop, true);
			if (Math.abs(input!.scrollTop - next) > 1) {
				expectedSource = next;
				input!.scrollTop = next;
			}
		}
		function updateCaret(reveal = true) {
			if (document.activeElement !== input) {
				caret.hidden = true;
				return;
			}
			const offset = input!.selectionStart;
			const candidates = Array.from(previewPane.querySelectorAll<HTMLElement>('[data-source-text]'));
			let nearest: HTMLElement | undefined;
			let best = Number.POSITIVE_INFINITY;
			for (const element of candidates) {
				const start = Number(element.dataset.sourceStart);
				const end = Number(element.dataset.sourceEnd);
				const distance = offset < start ? start - offset : offset > end ? offset - end : 0;
				if (distance < best) { nearest = element; best = distance; }
			}
			if (!nearest) { caret.hidden = true; return; }
			const start = Number(nearest.dataset.sourceStart);
			const end = Number(nearest.dataset.sourceEnd);
			const text = nearest.textContent ?? '';
			let textStart = start;
			if (nearest.parentElement?.tagName === 'PRE') {
				const openingFence = source.slice(start, end).match(/^ {0,3}(?:`{3,}|~{3,})[^\n]*\n/);
				if (openingFence) textStart += openingFence[0].length;
			}
			const index = sourceOffsetToText(source.slice(textStart, end), text, offset - textStart);
			const position = textPosition(nearest, index);
			let rect = nearest.getBoundingClientRect();
			if (position) {
				const range = document.createRange();
				range.setStart(position.node, position.offset);
				range.collapse(true);
				if (typeof range.getBoundingClientRect === 'function') {
					const measured = range.getBoundingClientRect();
					if (measured.height) rect = measured;
				}
			}
			const base = previewPane.getBoundingClientRect();
			const top = rect.top - base.top + previewPane.scrollTop;
			caret.style.top = `${top}px`;
			caret.style.left = `${rect.left - base.left + previewPane.scrollLeft}px`;
			caret.style.height = `${Math.max(16, rect.height)}px`;
			caret.hidden = false;
			if (reveal && (top < previewPane.scrollTop || top + rect.height > previewPane.scrollTop + previewPane.clientHeight)) {
				const next = Math.min(
					Math.max(0, previewPane.scrollHeight - previewPane.clientHeight),
					Math.max(0, top - previewPane.clientHeight / 3)
				);
				expectedPreview = next;
				previewPane.scrollTop = next;
			}
		}
		const onSelect = () => updateCaret();
		const onBlur = () => { caret.hidden = true; };
		input.addEventListener('scroll', sourceScroll);
		input.addEventListener('select', onSelect);
		input.addEventListener('click', onSelect);
		input.addEventListener('keyup', onSelect);
		input.addEventListener('focus', onSelect);
		input.addEventListener('blur', onBlur);
		previewPane.addEventListener('scroll', previewScroll);
		previewPane.addEventListener('load', scheduleRebuild, true);
		window.addEventListener('resize', scheduleRebuild);
		const mutations = new MutationObserver(scheduleRebuild);
		mutations.observe(previewPane, { childList: true, subtree: true, characterData: true });
		const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(scheduleRebuild);
		observer?.observe(input);
		observer?.observe(previewPane);
		scheduleRebuild();
		return () => {
			cancelAnimationFrame(frame);
			mutations.disconnect();
			observer?.disconnect();
			input.removeEventListener('scroll', sourceScroll);
			input.removeEventListener('select', onSelect);
			input.removeEventListener('click', onSelect);
			input.removeEventListener('keyup', onSelect);
			input.removeEventListener('focus', onSelect);
			input.removeEventListener('blur', onBlur);
			previewPane.removeEventListener('scroll', previewScroll);
			previewPane.removeEventListener('load', scheduleRebuild, true);
			window.removeEventListener('resize', scheduleRebuild);
			caret.hidden = true;
		};
	}, [textarea, preview, cursor, source, rendered, enabled]);
}
