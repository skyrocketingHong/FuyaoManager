/**
 * VersionPopover tests (the robot platform's capsule morph, ported for the
 * top bar's version details). jsdom has no layout and no Web Animations API,
 * so sizes come from a getBoundingClientRect stand-in and Element.animate is
 * replaced by a recorder whose `finished` the test settles by hand. Covered:
 * the surface morphs from the capsule's size to the details' size with the
 * platform's 420ms motion and returns over 300ms, the details fade in and take focus, Escape closes
 * and returns focus to the capsule while the surface morphs back, the details
 * stay mounted but inert during that morph, an outside click closes too,
 * reduced motion and a browser without Web Animations show the final size
 * directly, and PlatformVersion loads the running service version on open.
 * The antd chain is loaded after NODE_ENV is set to development (restored
 * afterwards), matching the other shell suites.
 */
// @vitest-environment jsdom
import { useState } from 'react';
import { describe, expect, it, vi, afterAll, afterEach, beforeEach } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import zh from '../src/lib/i18n/locales/zh-CN.json';
import { version } from '../package.json';

declare const process: { env: { NODE_ENV: string | undefined } };

const originalNodeEnv = process.env.NODE_ENV;
process.env.NODE_ENV = 'development';

const apiRequest = vi.fn();
vi.mock('../src/lib/api/client', () => ({
	api: (...args: unknown[]) => apiRequest(...args)
}));

const [
	{ ConfigProvider },
	{ default: PlatformVersion },
	{ default: VersionPopover },
	{ setLocale }
] = await Promise.all([
	import('antd'),
	import('../src/components/layout/PlatformVersion'),
	import('../src/components/layout/VersionPopover'),
	import('../src/lib/i18n')
]);

const capsuleSize = { width: 96, height: 32 };
const detailsSize = { width: 240, height: 96 };
const capsuleBounds = { ...capsuleSize, left: 544, top: 4 };
const detailsBounds = { ...detailsSize, left: 400, top: 44 };
let popupLayout = detailsBounds;
let visibleSurface = detailsBounds;
const label = zh['common.versionDetails'].replace('{version}', version);

function Harness() {
	const [open, setOpen] = useState(false);
	return (
		<ConfigProvider theme={{ token: { motion: false } }}>
			<VersionPopover
				open={open}
				onOpenChange={setOpen}
				label={label}
				content={
					<div className="version-facts">
						<span>
							{zh['updates.appLabel']} {version}
						</span>
					</div>
				}
			>
				<span className="version-badge-label">{zh['updates.versionTag']}</span>
				<span className="version-text">{version}</span>
			</VersionPopover>
		</ConfigProvider>
	);
}

// Sizes by class: the capsule, the details panel and (the same box) the
// surface around it. Anything else measures as jsdom's default zero box.
function rectFor(element: Element) {
	const classes = (element.getAttribute('class') ?? '').split(/\s+/);
	const { left, top, width, height } = classes.includes('topbar-version')
		? capsuleBounds
		: classes.includes('version-popover-surface')
			? visibleSurface
			: classes.includes('version-popover')
				? popupLayout
				: classes.includes('version-popover-content')
					? visibleSurface
				: { left: 0, top: 0, width: 0, height: 0 };
	return {
		width,
		height,
		top,
		left,
		right: left + width,
		bottom: top + height,
		x: left,
		y: top,
		toJSON: () => ({})
	} as DOMRect;
}

type Recorded = {
	target: Element;
	keyframes: Keyframe[];
	options: KeyframeAnimationOptions;
	cancel: ReturnType<typeof vi.fn>;
	finish: () => void;
};
const animations: Recorded[] = [];

function installLayout() {
	vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
		return rectFor(this);
	});
}

function installAnimate() {
	Object.defineProperty(Element.prototype, 'animate', {
		configurable: true,
		writable: true,
		value: function animate(this: Element, keyframes: Keyframe[], options: KeyframeAnimationOptions) {
			let finish!: () => void;
			const finished = new Promise<void>((resolve) => {
				finish = resolve;
			});
			const cancel = vi.fn(() => {
				if (this.classList.contains('version-popover-surface')) visibleSurface = popupLayout;
			});
			animations.push({ target: this, keyframes, options, cancel, finish });
			return { cancel, finished } as unknown as Animation;
		}
	});
}

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

const capsule = () => screen.getByRole('button', { name: label });
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]');
const capsuleFrame = {
	transform: 'translate(144px, -40px) scale(0.4, 0.3333333333333333)',
	transformOrigin: 'top left',
	overflow: 'clip'
};
const detailsFrame = {
	...capsuleFrame,
	transform: 'translate(0px, 0px) scale(1, 1)'
};

beforeEach(() => {
	vi.clearAllMocks();
	animations.length = 0;
	popupLayout = detailsBounds;
	visibleSurface = detailsBounds;
	localStorage.clear();
	setLocale('zh-CN');
	installLayout();
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	Reflect.deleteProperty(Element.prototype, 'animate');
});

afterAll(() => {
	process.env.NODE_ENV = originalNodeEnv;
});

describe('VersionPopover', () => {
	it('morphs the details surface from the capsule, then back on Escape with focus restored', async () => {
		installAnimate();
		render(<Harness />);
		expect(capsule().getAttribute('aria-expanded')).toBe('false');
		expect(capsule().getAttribute('aria-haspopup')).toBe('dialog');
		// Closed: the trigger only announces that a dialog opens.
		expect(capsule().getAttribute('aria-controls')).toBeNull();

		fireEvent.click(capsule());
		await waitFor(() => expect(dialog()).not.toBeNull());
		expect(capsule().getAttribute('aria-expanded')).toBe('true');
		expect(capsule().getAttribute('aria-controls')).toBe(dialog()!.id);
		expect(dialog()!.getAttribute('aria-label')).toBe(label);
		expect(document.activeElement).toBe(dialog());

		const surface = dialog()!.closest('.version-popover-surface');
		expect(surface).not.toBeNull();
		expect(animations).toHaveLength(2);
		expect(animations[0].target).toBe(surface);
		expect(animations[0].keyframes).toEqual([capsuleFrame, detailsFrame]);
		expect(animations[0].options).toMatchObject({
			duration: 420,
			easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
			fill: 'backwards'
		});
		expect(animations[1].target).toBe(dialog()!.firstElementChild);
		expect(animations[1].keyframes).toEqual([{ opacity: 0.35 }, { opacity: 1 }]);
		expect(animations[1].options).toMatchObject({ duration: 210, easing: 'ease-out', fill: 'backwards' });

		fireEvent.keyDown(dialog()!, { key: 'Escape' });
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('false'));
		expect(document.activeElement).toBe(capsule());
		// The surface morphs back to the capsule's size; until it lands, the
		// details stay mounted and take no focus.
		expect(animations).toHaveLength(3);
		expect(animations[2].target).toBe(surface);
		expect(animations[2].keyframes).toEqual([detailsFrame, capsuleFrame]);
		expect(animations[2].options).toMatchObject({ duration: 300, easing: 'ease-in-out', fill: 'both' });
		expect(animations[0].cancel).toHaveBeenCalled();
		expect(dialog()).not.toBeNull();
		expect(dialog()!.inert).toBe(true);

		await act(async () => {
			animations[2].finish();
		});
		// The capsule opens the details again after the morph back.
		fireEvent.click(capsule());
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('true'));
	});

	it('reopens from the surface currently on screen during a close', async () => {
		installAnimate();
		const nativeFocus = HTMLElement.prototype.focus;
		vi.spyOn(HTMLElement.prototype, 'focus').mockImplementation(function (this: HTMLElement, options) {
			// jsdom does not enforce inert when focus() is called.
			if (!this.inert) nativeFocus.call(this, options);
		});
		render(<Harness />);
		fireEvent.click(capsule());
		await waitFor(() => expect(document.activeElement).toBe(dialog()));
		fireEvent.keyDown(dialog()!, { key: 'Escape' });
		visibleSurface = { left: 448, top: 24, width: 180, height: 72 };
		fireEvent.click(capsule());
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('true'));

		expect(animations[2].cancel).toHaveBeenCalled();
		expect(animations[3].keyframes[0]).toMatchObject({
			transform: 'translate(48px, -20px) scale(0.75, 0.75)'
		});
		expect(dialog()!.inert).toBe(false);
		expect(document.activeElement).toBe(dialog());
		await act(async () => {
			animations[2].finish();
		});
		expect(capsule().getAttribute('aria-expanded')).toBe('true');
	});

	it('rebases the displayed surface only when Ant Design changes its layout', async () => {
		installAnimate();
		render(<Harness />);
		fireEvent.click(capsule());
		await waitFor(() => expect(document.activeElement).toBe(dialog()));
		const popup = dialog()!.closest<HTMLElement>('.version-popover')!;

		// The transform changes the drawn bounds, but not Ant Design's layout.
		visibleSurface = { left: 448, top: 24, width: 180, height: 72 };
		await act(async () => popup.style.setProperty('--test-alignment', '1'));
		expect(animations).toHaveLength(2);

		// Ant Design moves the layout 50px left and up. Until the animation is
		// retargeted, the same in-flight transform draws 50px left and up too.
		popupLayout = { ...detailsBounds, left: 350, top: -6 };
		visibleSurface = { ...visibleSurface, left: 398, top: -26 };
		await act(async () => popup.style.setProperty('--test-alignment', '2'));

		expect(animations).toHaveLength(3);
		expect(animations[2].keyframes).toEqual([
			{ ...detailsFrame, transform: 'translate(98px, 30px) scale(0.75, 0.75)' },
			detailsFrame
		]);
	});

	it('closes on an outside click', async () => {
		installAnimate();
		render(<Harness />);
		fireEvent.click(capsule());
		await waitFor(() => expect(dialog()).not.toBeNull());

		fireEvent.mouseDown(document.body);
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('false'));
	});

	it('shows the details at their final size under reduced motion', async () => {
		reduceMotion(true);
		installAnimate();
		render(<Harness />);

		fireEvent.click(capsule());
		await waitFor(() => expect(dialog()).not.toBeNull());
		expect(animations).toHaveLength(0);
		// The details still take focus; only the motion is left out.
		expect(document.activeElement).toBe(dialog());

		fireEvent.keyDown(dialog()!, { key: 'Escape' });
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('false'));
		expect(animations).toHaveLength(0);
		expect(document.activeElement).toBe(capsule());
	});

	it('works without the Web Animations API', async () => {
		render(<Harness />);

		fireEvent.click(capsule());
		await waitFor(() => expect(dialog()).not.toBeNull());
		expect(capsule().getAttribute('aria-expanded')).toBe('true');
		expect(document.activeElement).toBe(dialog());

		fireEvent.keyDown(dialog()!, { key: 'Escape' });
		await waitFor(() => expect(capsule().getAttribute('aria-expanded')).toBe('false'));
		expect(document.activeElement).toBe(capsule());
	});
});

describe('PlatformVersion', () => {
	it('loads the running service version when the details open', async () => {
		apiRequest.mockResolvedValue({ data: { version: '1.2.3', commit: 'abcdef0123' }, etag: null });
		render(
			<ConfigProvider theme={{ token: { motion: false } }}>
				<PlatformVersion />
			</ConfigProvider>
		);
		expect(apiRequest).not.toHaveBeenCalled();
		expect(capsule().textContent).toContain(version);

		fireEvent.click(capsule());
		await waitFor(() => expect(dialog()).not.toBeNull());
		await waitFor(() => expect(dialog()!.textContent).toContain('abcdef0123'));
		expect(dialog()!.textContent).toContain('1.2.3');
		expect(apiRequest).toHaveBeenCalledTimes(1);
		expect(apiRequest.mock.calls[0].slice(0, 2)).toEqual(['GET', '/api/v1/version']);
	});
});
