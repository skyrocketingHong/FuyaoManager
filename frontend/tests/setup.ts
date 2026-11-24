/**
 * Shared vitest setup: jsdom lacks ResizeObserver (required by antd v6
 * rc-component observers) and matchMedia (required by antd theming).
 * Polyfill both as no-op observers for every mounted-view suite. jsdom also
 * has no canvas; keep its "not implemented" report silent for pages that
 * carry one.
 */
if (typeof window !== 'undefined') {
	if (typeof window.ResizeObserver === 'undefined') {
		class ResizeObserverStub {
			observe(): void {}
			unobserve(): void {}
			disconnect(): void {}
		}
		Object.defineProperty(window, 'ResizeObserver', {
			writable: true,
			value: ResizeObserverStub
		});
		Object.defineProperty(globalThis, 'ResizeObserver', {
			writable: true,
			value: ResizeObserverStub
		});
	}
	if (!window.matchMedia) {
		Object.defineProperty(window, 'matchMedia', {
			writable: true,
			value: (query: string) => ({
				matches: false,
				media: query,
				onchange: null,
				addListener: () => undefined,
				removeListener: () => undefined,
			addEventListener: () => undefined,
			removeEventListener: () => undefined,
			dispatchEvent: () => false
		})
	});
	}
	// jsdom's getContext only reports "not implemented" and returns null. The
	// sign-in aurora then shows its sky gradient alone; a suite that needs a 2D
	// context installs its own stand-in.
	if (typeof window.HTMLCanvasElement !== 'undefined') {
		window.HTMLCanvasElement.prototype.getContext = () => null;
	}
}
