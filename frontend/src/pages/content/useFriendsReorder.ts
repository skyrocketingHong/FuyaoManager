import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { FocusEvent, KeyboardEvent, MouseEvent, PointerEvent } from 'react';
import { flushSync } from 'react-dom';
import { t } from '../../lib/i18n';

const duration = 200;
const easing = 'cubic-bezier(0.2, 0, 0, 1)';
const reducedMotion = '(prefers-reduced-motion: reduce)';

interface Drag {
	list: HTMLDivElement;
	items: HTMLElement[];
	row: HTMLElement;
	handle: HTMLElement;
	name: string;
	from: number;
	to: number;
	pitch: number;
	offset: number;
	pointer?: { id: number; origin: number; clientY: number; listTop: number };
	landing?: Animation;
}

function translate(offset: number) {
	return `0px ${offset}px`;
}

function position(drag: Drag, offset: number) {
	const { from, items, pitch, row } = drag;
	drag.offset = Math.max(-from * pitch, Math.min((items.length - 1 - from) * pitch, offset));
	row.style.translate = translate(drag.offset);
	const to = from + Math.round(drag.offset / pitch);
	if (drag.to === to) return false;
	drag.to = to;
	items.forEach((item, index) => {
		if (index === from) return;
		const shift = index > from && index <= to ? -pitch : index < from && index >= to ? pitch : 0;
		item.style.translate = shift ? translate(shift) : '';
	});
	return true;
}

export function useFriendsReorder(enabled: boolean, onMove: (from: number, to: number) => void) {
	const listRef = useRef<HTMLDivElement>(null);
	const active = useRef<Drag>();
	const instructionsId = useId();
	const [announcement, setAnnouncement] = useState('');
	const announcementTimer = useRef<number>();

	function announce(message: string, afterFocus = false) {
		window.clearTimeout(announcementTimer.current);
		if (afterFocus) {
			setAnnouncement('');
			announcementTimer.current = window.setTimeout(() => setAnnouncement(message), 500);
		} else {
			setAnnouncement((previous) => (previous === message ? `${message}\u200b` : message));
		}
	}

	function clear(drag: Drag) {
		active.current = undefined;
		drag.list.removeAttribute('data-reordering');
		drag.row.removeAttribute('data-lifted');
		drag.handle.setAttribute('aria-pressed', 'false');
		for (const item of drag.items) item.style.removeProperty('translate');
		drag.landing?.cancel();
		if (drag.pointer && drag.handle.hasPointerCapture(drag.pointer.id)) {
			drag.handle.releasePointerCapture(drag.pointer.id);
		}
	}

	function cancel() {
		const drag = active.current;
		if (!drag) return;
		clear(drag);
		announce(t('friends.reorderCancelled', { name: drag.name }));
	}

	function finish(drag: Drag) {
		if (active.current !== drag) return;
		const { handle, from, to, items, name } = drag;
		const focused = document.activeElement === handle;
		let restoredFocus = false;
		clear(drag);
		if (from !== to) {
			// React owns row order. Commit the new order with the transform cleanup
			// before paint, then restore focus if moving the keyed node blurred it.
			flushSync(() => onMove(from, to));
			if (focused && document.activeElement !== handle) {
				handle.focus({ preventScroll: true });
				restoredFocus = true;
			}
		}
		announce(
			t('friends.reorderDropped', { name, position: to + 1, total: items.length }),
			restoredFocus
		);
	}

	function drop(drag: Drag) {
		if (window.matchMedia(reducedMotion).matches || typeof drag.row.animate !== 'function') {
			finish(drag);
			return;
		}
		let landing: Animation;
		try {
			landing = drag.row.animate(
				[
					{ translate: translate(drag.offset), scale: 1.04 },
					{ translate: translate((drag.to - drag.from) * drag.pitch), scale: 1 }
				],
				{ duration, easing, fill: 'forwards' }
			);
		} catch {
			finish(drag);
			return;
		}
		drag.landing = landing;
		// Cancellation from clear() leaves this completion inert.
		void landing.finished.then(
			() => finish(drag),
			() => finish(drag)
		);
	}

	function begin(handle: HTMLElement, name: string, pointer?: PointerEvent<HTMLElement>) {
		if (active.current?.landing) finish(active.current);
		if (active.current) return;
		const list = listRef.current;
		const row = handle.closest<HTMLElement>('.friends-row');
		if (!list || !row) return;
		const items = Array.from(list.children) as HTMLElement[];
		const from = items.indexOf(row);
		if (from === -1 || items.length < 2) return;
		const bounds = items.map((item) => item.getBoundingClientRect());
		const first = bounds[0];
		const pitch = bounds[1].top - first.top;
		if (
			!(first.height > 0 && pitch > 0) ||
			bounds.some(
				(rect, index) =>
					Math.abs(rect.height - first.height) > 0.5 ||
					Math.abs(rect.top - first.top - index * pitch) > 0.5
			)
		)
			return;

		let tracking: Drag['pointer'];
		if (pointer) {
			try {
				handle.setPointerCapture(pointer.pointerId);
			} catch {
				return;
			}
			const listTop = list.getBoundingClientRect().top;
			tracking = {
				id: pointer.pointerId,
				origin: pointer.clientY - listTop,
				clientY: pointer.clientY,
				listTop
			};
		}
		const drag: Drag = {
			list,
			items,
			row,
			handle,
			name,
			from,
			to: from,
			pitch,
			offset: 0,
			pointer: tracking
		};
		active.current = drag;
		list.setAttribute('data-reordering', '');
		row.setAttribute('data-lifted', '');
		handle.setAttribute('aria-pressed', 'true');
		announce(t('friends.reorderPicked', { name, position: from + 1, total: items.length }));
	}

	function activate(handle: HTMLElement, name: string) {
		const drag = active.current;
		if (drag?.handle === handle && !drag.pointer && !drag.landing) drop(drag);
		else begin(handle, name);
	}

	function pointerDrag(event: PointerEvent<HTMLElement>) {
		const drag = active.current;
		if (
			drag?.handle === event.currentTarget &&
			drag.pointer?.id === event.pointerId &&
			!drag.landing
		) {
			return drag;
		}
	}

	function movePointer(drag: Drag, clientY: number) {
		const pointer = drag.pointer!;
		pointer.clientY = clientY;
		position(drag, clientY - pointer.listTop - pointer.origin);
	}

	function keyDown(event: KeyboardEvent<HTMLElement>, name: string) {
		const { key, currentTarget: handle } = event;
		const drag = active.current;
		if (key === 'Escape' && drag?.handle === handle && !drag.landing) {
			event.preventDefault();
			cancel();
		} else if (key === ' ' || key === 'Enter') {
			event.preventDefault();
			if (!event.repeat) activate(handle, name);
		} else if (
			drag?.handle === handle &&
			!drag.pointer &&
			!drag.landing &&
			(key === 'ArrowUp' || key === 'ArrowDown')
		) {
			event.preventDefault();
			if (position(drag, drag.offset + (key === 'ArrowDown' ? drag.pitch : -drag.pitch))) {
				drag.row.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
				announce(
					t('friends.reorderMoved', { name, position: drag.to + 1, total: drag.items.length })
				);
			}
		}
	}

	useLayoutEffect(() => {
		const drag = active.current;
		if (
			drag &&
			(!enabled ||
				drag.list.children.length !== drag.items.length ||
				drag.items.some((item, index) => item !== drag.list.children[index]))
		)
			cancel();
	});

	useEffect(() => {
		const motion = window.matchMedia(reducedMotion);
		const interrupt = () => {
			const drag = active.current;
			if (drag?.landing) finish(drag);
			else cancel();
		};
		const motionChanged = () => {
			const drag = active.current;
			if (motion.matches && drag?.landing) finish(drag);
		};
		const scrolled = () => {
			const drag = active.current;
			if (!drag?.pointer || drag.landing) return;
			drag.pointer.listTop = drag.list.getBoundingClientRect().top;
			movePointer(drag, drag.pointer.clientY);
		};
		window.addEventListener('resize', interrupt);
		window.addEventListener('blur', interrupt);
		window.addEventListener('scroll', scrolled, true);
		motion.addEventListener('change', motionChanged);
		return () => {
			window.removeEventListener('resize', interrupt);
			window.removeEventListener('blur', interrupt);
			window.removeEventListener('scroll', scrolled, true);
			motion.removeEventListener('change', motionChanged);
			window.clearTimeout(announcementTimer.current);
			if (active.current) clear(active.current);
		};
		// The list is remounted when its content scope changes.
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, []);

	function handleProps(name: string) {
		return {
			'aria-pressed': false,
			'aria-describedby': instructionsId,
			onPointerDown: (event: PointerEvent<HTMLElement>) => {
				if (event.button !== 0 || !event.isPrimary) return;
				const drag = active.current;
				if (drag && !drag.pointer && !drag.landing) cancel();
				begin(event.currentTarget, name, event);
			},
			onPointerMove: (event: PointerEvent<HTMLElement>) => {
				const drag = pointerDrag(event);
				if (drag) movePointer(drag, event.clientY);
			},
			onPointerUp: (event: PointerEvent<HTMLElement>) => {
				const drag = pointerDrag(event);
				if (!drag) return;
				movePointer(drag, event.clientY);
				drop(drag);
			},
			onPointerCancel: (event: PointerEvent<HTMLElement>) => {
				if (pointerDrag(event)) cancel();
			},
			onLostPointerCapture: (event: PointerEvent<HTMLElement>) => {
				if (pointerDrag(event)) cancel();
			},
			onKeyDown: (event: KeyboardEvent<HTMLElement>) => keyDown(event, name),
			onClick: (event: MouseEvent<HTMLElement>) => {
				if (event.detail !== 0) return;
				event.currentTarget.focus({ preventScroll: true });
				activate(event.currentTarget, name);
			},
			onBlur: (event: FocusEvent<HTMLElement>) => {
				const drag = active.current;
				if (drag?.handle === event.currentTarget && !drag.pointer && !drag.landing) cancel();
			}
		};
	}

	return { listRef, instructionsId, announcement, handleProps, cancel };
}
