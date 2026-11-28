// @vitest-environment jsdom
import { expect, it } from 'vitest';
import { observeScrollEdges } from '../src/lib/ui/scrollFade';

it('keeps the transition enabled at either end of an overflowing scroll region', () => {
	const node = document.createElement('div');
	Object.defineProperties(node, {
		clientHeight: { value: 100 },
		scrollHeight: { value: 240, configurable: true }
	});
	const stop = observeScrollEdges(node);
	try {
		expect(node.hasAttribute('data-scroll-fade')).toBe(true);
		expect(node.hasAttribute('data-scroll-fade-top')).toBe(false);
		expect(node.hasAttribute('data-scroll-fade-bottom')).toBe(true);

		node.scrollTop = 140;
		node.dispatchEvent(new Event('scroll'));
		expect(node.hasAttribute('data-scroll-fade')).toBe(true);
		expect(node.hasAttribute('data-scroll-fade-top')).toBe(true);
		expect(node.hasAttribute('data-scroll-fade-bottom')).toBe(false);

		Object.defineProperty(node, 'scrollHeight', { value: 100 });
		node.dispatchEvent(new Event('scroll'));
		expect(node.hasAttribute('data-scroll-fade')).toBe(false);
	} finally {
		stop();
	}
	expect(node.getAttributeNames()).toEqual([]);
});
