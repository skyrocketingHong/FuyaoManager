// @vitest-environment jsdom
import { afterEach, expect, it, vi } from 'vitest';
import {
	mountAlbumHorizontalScroll,
	staggerAlbumCards
} from '../src/features/albums/galleryMotion';

function motionPreference(initial: boolean) {
	const preference = Object.assign(new EventTarget(), { matches: initial });
	vi.stubGlobal('matchMedia', () => preference);
	return preference;
}

const OriginalResizeObserver = globalThis.ResizeObserver;

afterEach(() => {
	vi.unstubAllGlobals();
	globalThis.ResizeObserver = OriginalResizeObserver;
});

it('uses a 1× native scroll runway and resets when motion is reduced', () => {
	const preference = motionPreference(false);
	const viewport = document.createElement('section');
	const track = document.createElement('ul');
	viewport.append(track);
	let width = 400;
	Object.defineProperties(viewport, {
		clientWidth: { get: () => width },
		clientHeight: { value: 260 }
	});
	Object.defineProperty(track, 'scrollWidth', { value: 1000 });
	const resize: Array<() => void> = [];
	globalThis.ResizeObserver = class {
		constructor(callback: ResizeObserverCallback) {
			resize.push(() => callback([], this));
		}
		observe() {}
		unobserve() {}
		disconnect() {}
	};
	const frames: FrameRequestCallback[] = [];
	vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => frames.push(callback));
	vi.stubGlobal('cancelAnimationFrame', () => {});
	const stop = mountAlbumHorizontalScroll(viewport, track);

	expect(viewport.style.getPropertyValue('--album-runway-height')).toBe('860px');
	expect(viewport.tabIndex).toBe(0);
	viewport.scrollTop = 150;
	viewport.dispatchEvent(new Event('scroll'));
	expect(track.style.transform).toBe('translate3d(-150px, 0, 0)');
	const end = new KeyboardEvent('keydown', { key: 'End', cancelable: true });
	viewport.dispatchEvent(end);
	expect(end.defaultPrevented).toBe(true);
	expect(track.style.transform).toBe('translate3d(-600px, 0, 0)');

	width = 600;
	resize[0]();
	frames.shift()?.(0);
	expect(viewport.style.getPropertyValue('--album-runway-height')).toBe('660px');
	expect(track.style.transform).toBe('translate3d(-400px, 0, 0)');

	preference.matches = true;
	preference.dispatchEvent(new Event('change'));
	frames.shift()?.(0);
	expect(viewport.hasAttribute('data-horizontal-ready')).toBe(false);
	expect(viewport.hasAttribute('tabindex')).toBe(false);
	expect(track.style.transform).toBe('');
	stop?.();
});

it('leaves a static gallery if measurements fail', () => {
	motionPreference(false);
	globalThis.ResizeObserver = class {
		observe() {}
		unobserve() {}
		disconnect() {}
	};
	const viewport = document.createElement('section');
	const track = document.createElement('ul');
	Object.defineProperty(track, 'scrollWidth', {
		get: () => {
			throw new Error('layout unavailable');
		}
	});

	const stop = mountAlbumHorizontalScroll(viewport, track);
	expect(viewport.hasAttribute('data-horizontal-ready')).toBe(false);
	expect(viewport.style.getPropertyValue('--album-runway-height')).toBe('');
	expect(track.style.transform).toBe('');
	stop?.();
});

it('staggers an intersecting group at 75 ms and stops on reduced motion', () => {
	const preference = motionPreference(false);
	const intersect: IntersectionObserverCallback[] = [];
	vi.stubGlobal(
		'IntersectionObserver',
		class {
			constructor(callback: IntersectionObserverCallback) {
				intersect.push(callback);
			}
			observe() {}
			unobserve() {}
			disconnect() {}
		}
	);
	const track = document.createElement('ul');
	const first = document.createElement('li');
	const second = document.createElement('li');
	track.append(first, second);
	const cancel = vi.fn();
	const animate = vi.fn(() => ({ cancel }));
	for (const element of [track, first, second]) {
		Object.defineProperty(element, 'animate', { value: animate });
	}
	const stop = staggerAlbumCards(track);

	expect(animate).not.toHaveBeenCalled();
	const entries: IntersectionObserverEntry[] = [first, second].map((target) => {
		const bounds = target.getBoundingClientRect();
		return {
			target,
			isIntersecting: true,
			intersectionRatio: 1,
			time: 0,
			boundingClientRect: bounds,
			intersectionRect: bounds,
			rootBounds: bounds
		};
	});
	intersect[0](entries, {} as IntersectionObserver);
	const keyframes = [
		{ opacity: 0, transform: 'translateY(16px)' },
		{ opacity: 1, transform: 'translateY(0)' }
	];
	expect(animate).toHaveBeenNthCalledWith(1, keyframes, {
		duration: 500,
		delay: 0,
		easing: 'cubic-bezier(0.16, 1, 0.3, 1)',
		fill: 'backwards'
	});
	expect(animate).toHaveBeenNthCalledWith(2, keyframes, {
		duration: 500,
		delay: 75,
		easing: 'cubic-bezier(0.16, 1, 0.3, 1)',
		fill: 'backwards'
	});
	preference.matches = true;
	preference.dispatchEvent(new Event('change'));
	expect(cancel).toHaveBeenCalledTimes(2);
	stop?.();
});
