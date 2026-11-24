/**
 * Operation bar tests (the robot platform's single-row page area): the page
 * slots sit inside one scrolling region, navigation and the session controls
 * stay outside it, and the fades follow the edges that still hide content.
 * jsdom has no layout, so the scroll region reports its geometry through
 * stand-in properties and the test plays the browser by setting scrollLeft
 * and firing scroll. Covered: a vertical wheel scrolls the region sideways
 * and claims the event, line and page wheel modes, and the wheel is left to
 * the page when it already scrolls sideways, is a browser zoom gesture, was
 * handled by a control, or points at an edge with nothing left behind it.
 */
// @vitest-environment jsdom
import { describe, expect, it, vi, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ConfigProvider } from 'antd';
import TopBar from '../src/components/layout/TopBar';
import { PageHeaderProvider, usePageHeader } from '../src/components/PageHeader';
import { toolbarWheelDistance } from '../src/components/layout/useToolbarScroll';

const alice = {
	id: '11111111-1111-1111-1111-111111111111',
	username: 'alice',
	displayName: 'Alice',
	platformRole: 'admin'
};
// External-store snapshots keep one reference across calls.
const sessionState = { status: 'authenticated', user: alice, platformInitialized: true };
vi.mock('../src/lib/session', () => ({
	subscribeSession: () => () => undefined,
	sessionSnapshot: () => sessionState,
	signInWithCode: vi.fn(),
	signInWithPasskey: vi.fn(),
	bindPasskey: vi.fn(),
	signOut: vi.fn(),
	loadSession: vi.fn()
}));
vi.mock('../src/lib/api/client', () => ({
	api: vi.fn(),
	currentAuthGeneration: () => 0
}));

function Page() {
	usePageHeader({
		leading: <button type="button">leading-control</button>,
		actions: <button type="button">primary-action</button>,
		content: (
			<div className="filter-group">
				<button type="button">filter-control</button>
			</div>
		)
	});
	return null;
}

function mount() {
	return render(
		<ConfigProvider theme={{ token: { motion: false } }}>
			<MemoryRouter>
				<PageHeaderProvider scope="test">
					<TopBar navigationSlot={<button type="button">navigation-control</button>} />
					<Page />
				</PageHeaderProvider>
			</MemoryRouter>
		</ConfigProvider>
	);
}

const region = () => document.querySelector<HTMLElement>('.topbar-scroll');
const scroller = () => document.querySelector<HTMLElement>('.topbar-main');

// The scroll region as a browser would report it: a 200px window on 500px
// of content, so it can scroll 300px.
function layout(width = 200, content = 500) {
	const geometry = { left: 0, client: width, scroll: content };
	const element = scroller()!;
	Object.defineProperty(element, 'clientWidth', { configurable: true, get: () => geometry.client });
	Object.defineProperty(element, 'scrollWidth', { configurable: true, get: () => geometry.scroll });
	Object.defineProperty(element, 'scrollLeft', {
		configurable: true,
		get: () => geometry.left,
		set: (value: number) => {
			geometry.left = value;
		}
	});
	return geometry;
}

// A wheel event over the page area; returns whether the region claimed it.
function wheel(init: WheelEventInit = {}, target: Element = scroller()!) {
	const event = new WheelEvent('wheel', { bubbles: true, cancelable: true, ...init });
	target.dispatchEvent(event);
	return event.defaultPrevented;
}

afterEach(() => {
	cleanup();
	localStorage.clear();
});

describe('wheel distance', () => {
	it('maps pixel, line and page wheel modes', () => {
		expect(toolbarWheelDistance(120, 0, 0, 200)).toBe(120);
		expect(toolbarWheelDistance(3, 0, 1, 200)).toBe(48);
		expect(toolbarWheelDistance(1, 0, 2, 200)).toBe(200);
		// A wheel that already scrolls sideways is not converted.
		expect(toolbarWheelDistance(10, 40, 0, 200)).toBe(0);
		expect(toolbarWheelDistance(40, 40, 0, 200)).toBe(0);
	});
});

describe('operation bar', () => {
	it('keeps the page slots in the scrolling region and the session controls outside it', () => {
		mount();
		const items = document.querySelector('.topbar-main .topbar-items');
		expect(items).not.toBeNull();
		for (const name of ['leading-control', 'primary-action', 'filter-control'])
			expect(items!.contains(screen.getByRole('button', { name }))).toBe(true);
		// Navigation and the session stay in place on either side.
		expect(region()!.contains(screen.getByRole('button', { name: 'navigation-control' }))).toBe(false);
		const session = document.querySelector('.topbar-session');
		expect(session!.contains(screen.getByRole('button', { name: 'alice' }))).toBe(true);
		expect(region()!.contains(session)).toBe(false);
		// Nothing is hidden before the region can scroll.
		expect(region()!.hasAttribute('data-fade-left')).toBe(false);
		expect(region()!.hasAttribute('data-fade-right')).toBe(false);
	});

	it('marks each edge that still hides content', () => {
		mount();
		const geometry = layout();
		fireEvent.scroll(scroller()!);
		expect(region()!.hasAttribute('data-fade-left')).toBe(false);
		expect(region()!.hasAttribute('data-fade-right')).toBe(true);

		geometry.left = 120;
		fireEvent.scroll(scroller()!);
		expect(region()!.hasAttribute('data-fade-left')).toBe(true);
		expect(region()!.hasAttribute('data-fade-right')).toBe(true);

		geometry.left = 300;
		fireEvent.scroll(scroller()!);
		expect(region()!.hasAttribute('data-fade-left')).toBe(true);
		expect(region()!.hasAttribute('data-fade-right')).toBe(false);

		// A page area that fits shows no fade at all.
		const fitting = layout(500, 500);
		fitting.left = 0;
		fireEvent.scroll(scroller()!);
		expect(region()!.hasAttribute('data-fade-left')).toBe(false);
		expect(region()!.hasAttribute('data-fade-right')).toBe(false);
	});

	it('turns a vertical wheel into sideways scrolling', () => {
		mount();
		const geometry = layout();
		expect(wheel({ deltaY: 120 })).toBe(true);
		expect(geometry.left).toBe(120);

		// Lines and pages use their own step.
		expect(wheel({ deltaY: 2, deltaMode: 1 })).toBe(true);
		expect(geometry.left).toBe(152);
		expect(wheel({ deltaY: 1, deltaMode: 2 })).toBe(true);
		expect(geometry.left).toBe(300);

		// At the end nothing is left to scroll towards, so the page gets it.
		expect(wheel({ deltaY: 100 })).toBe(false);
		expect(geometry.left).toBe(300);
		expect(wheel({ deltaY: -400 })).toBe(true);
		expect(geometry.left).toBe(0);
		expect(wheel({ deltaY: -100 })).toBe(false);
		expect(geometry.left).toBe(0);
	});

	it('leaves other wheels to the page', () => {
		mount();
		const geometry = layout();
		// Already scrolling sideways.
		expect(wheel({ deltaX: 40, deltaY: 0 })).toBe(false);
		// Browser zoom gestures.
		expect(wheel({ deltaY: 100, ctrlKey: true })).toBe(false);
		expect(wheel({ deltaY: 100, metaKey: true })).toBe(false);
		// No distance at all.
		expect(wheel({ deltaX: 0, deltaY: 0 })).toBe(false);
		expect(geometry.left).toBe(0);

		// A control that handled the wheel itself, such as a nested list.
		const filter = screen.getByRole('button', { name: 'filter-control' });
		const handled = (event: Event) => event.preventDefault();
		filter.addEventListener('wheel', handled);
		expect(wheel({ deltaY: 100 }, filter)).toBe(true);
		filter.removeEventListener('wheel', handled);
		expect(geometry.left).toBe(0);

		// Nothing to scroll: the page area fits.
		layout(500, 500);
		expect(wheel({ deltaY: 100 })).toBe(false);
	});

	it('stops listening once the bar unmounts', () => {
		const view = mount();
		const geometry = layout();
		const element = scroller()!;
		view.unmount();
		expect(wheel({ deltaY: 100 }, element)).toBe(false);
		expect(geometry.left).toBe(0);
	});
});
