/**
 * useToolbarScroll: the operation bar's sideways-scrolling page area
 * (robot-platform's frontend/components/platform/operationbar/
 * useToolbarScroll.ts). Reports which edges still hide content, for the
 * fades, and turns a vertical wheel or trackpad scroll into horizontal
 * scrolling while content remains in that direction. A wheel that already
 * scrolls sideways, a browser zoom gesture (Ctrl or Cmd), and a scroll toward
 * an edge with nothing left behind it are left to the page.
 */
import { useEffect, useRef, useState } from 'react';
import { scrollFadeEdges } from '../../lib/ui/scrollFade';

/** Pixels for one wheel event: a line step is 16px, a page step is the visible width. */
export function toolbarWheelDistance(vertical: number, horizontal: number, mode: number, width: number) {
	if (Math.abs(horizontal) >= Math.abs(vertical)) return 0;
	if (mode === 1) return vertical * 16;
	return vertical * (mode === 2 ? width : 1);
}

export function useToolbarScroll() {
	const scroll = useRef<HTMLDivElement>(null);
	const content = useRef<HTMLDivElement>(null);
	const [edges, setEdges] = useState({ left: false, right: false });
	useEffect(() => {
		const element = scroll.current;
		if (!element) return;
		const measure = () => {
			const next = scrollFadeEdges(element.scrollLeft, element.clientWidth, element.scrollWidth);
			setEdges((current) =>
				current.left === next.top && current.right === next.bottom
					? current
					: { left: next.top, right: next.bottom }
			);
		};
		const wheel = (event: WheelEvent) => {
			if (event.defaultPrevented || !event.cancelable || event.ctrlKey || event.metaKey) return;
			const maximum = element.scrollWidth - element.clientWidth;
			const delta = toolbarWheelDistance(
				event.deltaY,
				event.deltaX,
				event.deltaMode,
				element.clientWidth
			);
			if (maximum <= 1 || !delta) return;
			const next = Math.max(0, Math.min(maximum, element.scrollLeft + delta));
			if (Math.abs(next - element.scrollLeft) < 1) return;
			event.preventDefault();
			element.scrollLeft = next;
		};
		// The page area's controls change with the page, so both the viewport
		// and the content stay observed.
		const observer = new ResizeObserver(measure);
		observer.observe(element);
		if (content.current) observer.observe(content.current);
		element.addEventListener('wheel', wheel, { passive: false });
		element.addEventListener('scroll', measure, { passive: true });
		measure();
		return () => {
			observer.disconnect();
			element.removeEventListener('wheel', wheel);
			element.removeEventListener('scroll', measure);
		};
	}, []);
	return { scroll, content, left: edges.left, right: edges.right };
}
