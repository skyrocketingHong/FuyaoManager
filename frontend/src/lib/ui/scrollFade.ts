// Edge calculation from robot-platform/libraries/ui-sdk/src/scrollFade.ts.
export function scrollFadeEdges(scrollTop: number, clientHeight: number, scrollHeight: number) {
	const remaining = scrollHeight - clientHeight;
	if (
		![scrollTop, clientHeight, scrollHeight].every(Number.isFinite) ||
		clientHeight <= 0 ||
		remaining <= 1
	)
		return { top: false, bottom: false };
	const position = Math.min(Math.max(scrollTop, 0), remaining);
	return { top: position > 1, bottom: remaining - position > 1 };
}

export function observeScrollEdges(node: HTMLElement) {
	const update = () => {
		const { top, bottom } = scrollFadeEdges(node.scrollTop, node.clientHeight, node.scrollHeight);
		node.toggleAttribute('data-scroll-fade-top', top);
		node.toggleAttribute('data-scroll-fade-bottom', bottom);
	};
	const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(update);
	resize?.observe(node);
	for (const child of node.children) resize?.observe(child);
	const content = new MutationObserver(() => {
		resize?.disconnect();
		resize?.observe(node);
		for (const child of node.children) resize?.observe(child);
		update();
	});
	content.observe(node, { childList: true });
	node.addEventListener('scroll', update, { passive: true });
	update();
	return () => {
		node.removeEventListener('scroll', update);
		resize?.disconnect();
		content.disconnect();
		node.removeAttribute('data-scroll-fade-top');
		node.removeAttribute('data-scroll-fade-bottom');
	};
}
