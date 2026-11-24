/**
 * Auth aurora and layout tests (the robot platform's login aurora, ported
 * behind the sign-in pages). jsdom has no canvas, no layout and no frame
 * clock, so the canvas reports a recording surface, ResizeObserver hands
 * over its callback, and requestAnimationFrame keeps a queue the test runs by
 * hand. Covered: a still frame and no frames while motion is off, playback
 * that continues the same loop across pause and resume, stopping while the
 * tab is hidden, the canvas resolution cap, cleanup on unmount, and a page
 * without a 2D context. The layout part checks the pause control and that
 * reduced motion stills the aurora from the start and drops the control.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import zh from '../src/lib/i18n/locales/zh-CN.json';
import { auroraCycleMs } from '../src/lib/ui/aurora';

const mocks = vi.hoisted(() => ({ paint: vi.fn() }));
// The painter is replaced so the frames the component draws can be counted.
vi.mock('../src/lib/ui/aurora', async (importOriginal) => ({
	...(await importOriginal<typeof import('../src/lib/ui/aurora')>()),
	createAuroraPainter: vi.fn(() => mocks.paint)
}));

const [{ default: AuthAurora }, { default: AuthLayout }, { setLocale }] = await Promise.all([
	import('../src/components/auth/AuthAurora'),
	import('../src/components/layout/AuthLayout'),
	import('../src/lib/i18n')
]);

type Frame = (time: number) => void;
type Resize = (entries: { contentRect: { width: number; height: number } }[]) => void;
const frames = new Map<number, Frame>();
let nextFrame = 1;
const requestFrame = vi.fn((callback: Frame) => {
	frames.set(nextFrame, callback);
	return nextFrame++;
});
const cancelFrame = vi.fn((id: number) => {
	frames.delete(id);
});
function runFrames(time: number) {
	const pending = [...frames.values()];
	frames.clear();
	for (const callback of pending) callback(time);
}

const resizes: Resize[] = [];
const observe = vi.fn();
const disconnected = vi.fn();
// A browser reports the element once when observation starts, then on every
// size change; the test plays both through this call.
function reportSize(width: number, height: number) {
	for (const callback of [...resizes])
		callback([{ contentRect: { width, height } }]);
}

const getContext = vi.fn();
let hidden = false;

function reduceMotion(matches: boolean) {
	vi.spyOn(window, 'matchMedia').mockImplementation(
		(query: string) =>
			({
				matches: query.includes('reduce') && matches,
				media: query,
				onchange: null,
				addEventListener: () => undefined,
				removeEventListener: () => undefined,
				addListener: () => undefined,
				removeListener: () => undefined,
				dispatchEvent: () => false
			}) as MediaQueryList
	);
}

// tests/setup.ts defines the shared ResizeObserver stub as a writable but
// non-configurable property, so vi.stubGlobal cannot replace it; assign over
// it and put the shared stub back afterwards.
const NativeResizeObserver = globalThis.ResizeObserver;
class FakeResizeObserver {
	constructor(private readonly callback: Resize) {
		resizes.push(callback);
	}
	observe = observe;
	// A disconnected observer reports nothing more, as in a browser.
	disconnect() {
		disconnected();
		const index = resizes.indexOf(this.callback);
		if (index >= 0) resizes.splice(index, 1);
	}
}

beforeEach(() => {
	vi.clearAllMocks();
	frames.clear();
	nextFrame = 1;
	resizes.length = 0;
	hidden = false;
	localStorage.clear();
	setLocale('zh-CN');
	globalThis.ResizeObserver = FakeResizeObserver as unknown as typeof ResizeObserver;
	vi.stubGlobal('requestAnimationFrame', requestFrame);
	vi.stubGlobal('cancelAnimationFrame', cancelFrame);
	getContext.mockImplementation(function (this: HTMLCanvasElement) {
		return { canvas: this };
	});
	Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
		configurable: true,
		value: getContext
	});
	Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
});

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
	globalThis.ResizeObserver = NativeResizeObserver;
	Reflect.deleteProperty(HTMLCanvasElement.prototype, 'getContext');
	// Back to the shared silent stub of tests/setup.ts.
	HTMLCanvasElement.prototype.getContext = () => null;
	Reflect.deleteProperty(document, 'hidden');
});

describe('AuthAurora', () => {
	it('paints one still frame and requests no frames while motion is off', () => {
		render(<AuthAurora moving={false} />);
		const canvas = document.querySelector('canvas')!;
		expect(resizes).toHaveLength(1);
		expect(observe).toHaveBeenCalledWith(canvas);

		reportSize(800, 600);
		expect(canvas.width).toBe(600);
		expect(canvas.height).toBe(450);
		expect(mocks.paint).toHaveBeenCalledTimes(1);
		expect(mocks.paint).toHaveBeenCalledWith(0);
		expect(requestFrame).not.toHaveBeenCalled();
	});

	it('keeps the canvas at most 720px on its long side', () => {
		render(<AuthAurora moving={false} />);
		const canvas = document.querySelector('canvas')!;

		// A large screen: 0.375 of 1920 is 720.
		reportSize(1920, 1080);
		expect([canvas.width, canvas.height]).toEqual([720, 405]);
		// A small element keeps 0.75 of its size.
		reportSize(400, 300);
		expect([canvas.width, canvas.height]).toEqual([300, 225]);
		// A size already drawn is not set up again.
		vi.mocked(mocks.paint).mockClear();
		reportSize(400, 300);
		expect(mocks.paint).not.toHaveBeenCalled();
	});

	it('plays while moving and continues the same loop across pause and resume', () => {
		const view = render(<AuthAurora moving />);
		reportSize(800, 600);
		// The first frame is painted at once, then frames are requested.
		expect(mocks.paint).toHaveBeenCalledTimes(1);
		expect(frames.size).toBe(1);

		// The first frame only starts the clock.
		act(() => runFrames(1000));
		expect(mocks.paint).toHaveBeenLastCalledWith(0);
		expect(frames.size).toBe(1);
		act(() => runFrames(1100));
		expect(mocks.paint).toHaveBeenLastCalledWith(100 / auroraCycleMs);

		view.rerender(<AuthAurora moving={false} />);
		expect(frames.size).toBe(0);
		expect(cancelFrame).toHaveBeenCalled();
		expect(mocks.paint).toHaveBeenCalledTimes(3);

		// Observation starts again with the effect, so the browser reports the
		// size once more; the loop continues where it stopped.
		view.rerender(<AuthAurora moving />);
		reportSize(800, 600);
		expect(mocks.paint).toHaveBeenLastCalledWith(100 / auroraCycleMs);
		expect(frames.size).toBe(1);
		act(() => runFrames(9000));
		expect(mocks.paint).toHaveBeenLastCalledWith(100 / auroraCycleMs);
		act(() => runFrames(9060));
		expect(mocks.paint).toHaveBeenLastCalledWith(160 / auroraCycleMs);
	});

	it('stops while the tab is hidden and continues when it is shown', () => {
		render(<AuthAurora moving />);
		reportSize(800, 600);
		act(() => runFrames(1000));
		act(() => runFrames(1200));
		expect(mocks.paint).toHaveBeenLastCalledWith(200 / auroraCycleMs);

		hidden = true;
		fireEvent(document, new Event('visibilitychange'));
		expect(frames.size).toBe(0);
		act(() => runFrames(5000));
		expect(mocks.paint).toHaveBeenLastCalledWith(200 / auroraCycleMs);

		hidden = false;
		fireEvent(document, new Event('visibilitychange'));
		expect(frames.size).toBe(1);
		act(() => runFrames(6000));
		expect(mocks.paint).toHaveBeenLastCalledWith(200 / auroraCycleMs);
	});

	it('ignores an empty size and cleans up on unmount', () => {
		const view = render(<AuthAurora moving />);
		const canvas = document.querySelector('canvas')!;
		reportSize(0, 0);
		expect(mocks.paint).not.toHaveBeenCalled();
		expect(requestFrame).not.toHaveBeenCalled();
		expect(canvas.width).toBe(300);

		view.unmount();
		expect(disconnected).toHaveBeenCalledTimes(1);
		expect(frames.size).toBe(0);
		hidden = false;
		fireEvent(document, new Event('visibilitychange'));
		reportSize(800, 600);
		expect(requestFrame).not.toHaveBeenCalled();
		expect(mocks.paint).not.toHaveBeenCalled();
	});

	it('shows only the sky when there is no 2D context', () => {
		getContext.mockReturnValue(null);
		render(<AuthAurora moving />);
		reportSize(800, 600);
		expect(document.querySelector('.auth-aurora canvas')).not.toBeNull();
		expect(mocks.paint).not.toHaveBeenCalled();
		expect(requestFrame).not.toHaveBeenCalled();
	});
});

describe('AuthLayout', () => {
	it('lays the aurora behind the content and pauses it on request', () => {
		render(
			<AuthLayout>
				<div>content</div>
			</AuthLayout>
		);
		const page = document.querySelector('main')!;
		const atmosphere = document.querySelector('.auth-atmosphere');
		// The aurora is decorative: the layer is hidden from assistive
		// technology and the content sits above it.
		expect(atmosphere?.getAttribute('aria-hidden')).toBe('true');
		expect(atmosphere?.querySelector('.auth-aurora canvas')).not.toBeNull();
		expect(page.contains(atmosphere)).toBe(true);
		expect(page.querySelector('.auth-layout-inner')?.contains(screen.getByText('content'))).toBe(true);
		expect(page.hasAttribute('data-motion-paused')).toBe(false);

		// The aurora plays from the first drawn frame.
		reportSize(800, 600);
		expect(mocks.paint).toHaveBeenCalledTimes(1);
		expect(frames.size).toBe(1);

		fireEvent.click(screen.getByRole('button', { name: zh['auth.pauseMotion'] }));
		expect(page.getAttribute('data-motion-paused')).toBe('');
		expect(frames.size).toBe(0);
		expect(screen.queryByRole('button', { name: zh['auth.pauseMotion'] })).toBeNull();
		// While paused, reported sizes redraw the still frame and start nothing.
		reportSize(800, 600);
		expect(requestFrame).toHaveBeenCalledTimes(1);

		fireEvent.click(screen.getByRole('button', { name: zh['auth.resumeMotion'] }));
		expect(page.hasAttribute('data-motion-paused')).toBe(false);
		expect(screen.getByRole('button', { name: zh['auth.pauseMotion'] })).toBeDefined();
		// Observation starts again with the effect, so the browser reports the
		// size once more; playback picks up where it stopped.
		reportSize(800, 600);
		expect(frames.size).toBe(1);
		expect(requestFrame).toHaveBeenCalledTimes(2);
	});

	it('stills the aurora from the start and drops the control under reduced motion', () => {
		reduceMotion(true);
		render(
			<AuthLayout>
				<div>content</div>
			</AuthLayout>
		);
		reportSize(800, 600);
		expect(mocks.paint).toHaveBeenCalledTimes(1);
		expect(requestFrame).not.toHaveBeenCalled();
		expect(screen.queryByRole('button', { name: zh['auth.pauseMotion'] })).toBeNull();
		expect(screen.queryByRole('button', { name: zh['auth.resumeMotion'] })).toBeNull();
	});
});
