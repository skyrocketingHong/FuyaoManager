/**
 * Page change view transitions (the robot platform cross-fades page changes):
 * the router wrapper turns the transition on for a navigation to another
 * page and for the matching back trip, and leaves everything else instant —
 * a change that only touches the query string, a replace navigation, an
 * explicit choice at the call site, reduced motion and a browser without the
 * View Transition API. A small harness runs on the real RouterProvider with a
 * stand-in for document.startViewTransition, which jsdom does not implement.
 */
// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import {
	Link,
	RouterProvider,
	createMemoryRouter,
	useLocation,
	useNavigate
} from 'react-router-dom';
import { isPageChange, withPageTransitions } from '../src/lib/navigation/pageTransition';

function Controls() {
	const { pathname, search } = useLocation();
	const navigate = useNavigate();
	return (
		<>
			<output data-testid="location">{pathname + search}</output>
			<Link to="/users">to-users</Link>
			<button onClick={() => navigate({ pathname, search: '?tab=other' })}>query-only</button>
			<button onClick={() => navigate('/users', { replace: true })}>replace-users</button>
			<button onClick={() => navigate('/users', { viewTransition: false })}>opt-out</button>
			<button onClick={() => navigate({ pathname, search: '?tab=other' }, { viewTransition: true })}>
				opt-in
			</button>
			<button onClick={() => navigate(-1)}>back</button>
		</>
	);
}

type Update = () => void | Promise<void>;

// The stand-in runs the router's update callback and settles like a finished
// transition, so the provider's cleanup runs as in a browser.
function installViewTransition() {
	const startViewTransition = vi.fn((update: Update) => {
		const finished = Promise.resolve(update()).then(
			() => undefined,
			() => undefined
		);
		return {
			finished,
			ready: finished,
			skipTransition: () => undefined,
			types: new Set<string>(),
			updateTransitionDone: () => undefined
		};
	});
	Object.defineProperty(document, 'startViewTransition', {
		configurable: true,
		writable: true,
		value: startViewTransition
	});
	return startViewTransition;
}

function mount(path = '/sites') {
	const router = withPageTransitions(
		createMemoryRouter([{ path: '*', element: <Controls /> }], { initialEntries: [path] })
	);
	render(<RouterProvider router={router} />);
	return router;
}

const shownLocation = () => screen.getByTestId('location').textContent;

async function click(name: string) {
	await act(async () => {
		fireEvent.click(screen.getByRole('button', { name }));
	});
}

afterEach(() => {
	cleanup();
	vi.unstubAllGlobals();
	vi.restoreAllMocks();
	Reflect.deleteProperty(document, 'startViewTransition');
});

beforeEach(() => {
	// jsdom's Request rejects AbortSignal values; the router constructs one
	// for every navigation.
	const NativeRequest = globalThis.Request;
	vi.stubGlobal(
		'Request',
		class extends NativeRequest {
			constructor(input: RequestInfo | URL, init?: RequestInit) {
				super(input, init ? { ...init, signal: undefined } : init);
			}
		}
	);
});

describe('page change detection', () => {
	it('treats another pathname as a page change and anything else as the same page', () => {
		expect(isPageChange('/users', '/sites')).toBe(true);
		expect(isPageChange('../users', '/sites/abc')).toBe(true);
		expect(isPageChange({ pathname: '/sites', search: '?tab=x' }, '/sites')).toBe(false);
		expect(isPageChange('?tab=x', '/sites/abc')).toBe(false);
		expect(isPageChange('#details', '/sites')).toBe(false);
		// Trailing-slash forms of the same page are one page.
		expect(isPageChange('/sites/', '/sites')).toBe(true);
	});
});

describe('page change transitions', () => {
	it('cross-fades a navigation to another page and shows the new page', async () => {
		const startViewTransition = installViewTransition();
		mount('/sites');
		expect(shownLocation()).toBe('/sites');

		// The link is a plain push navigation without an explicit choice.
		await act(async () => {
			fireEvent.click(screen.getByRole('link', { name: 'to-users' }));
		});
		expect(startViewTransition).toHaveBeenCalledTimes(1);
		await waitFor(() => expect(shownLocation()).toBe('/users'));
	});

	it('keeps a query-only change and a replace navigation instant', async () => {
		const startViewTransition = installViewTransition();
		mount('/sites');

		await click('query-only');
		expect(startViewTransition).not.toHaveBeenCalled();
		await waitFor(() => expect(shownLocation()).toBe('/sites?tab=other'));

		await click('replace-users');
		expect(startViewTransition).not.toHaveBeenCalled();
		await waitFor(() => expect(shownLocation()).toBe('/users'));
	});

	it('follows an explicit choice at the call site', async () => {
		const startViewTransition = installViewTransition();
		mount('/sites');

		await click('opt-out');
		expect(startViewTransition).not.toHaveBeenCalled();
		await waitFor(() => expect(shownLocation()).toBe('/users'));

		// The same page with another query string still transitions when asked.
		await click('opt-in');
		expect(startViewTransition).toHaveBeenCalledTimes(1);
		await waitFor(() => expect(shownLocation()).toBe('/users?tab=other'));
	});

	it('repeats the transition for the back trip to the previous page', async () => {
		const startViewTransition = installViewTransition();
		mount('/sites');

		await act(async () => {
			fireEvent.click(screen.getByRole('link', { name: 'to-users' }));
		});
		await waitFor(() => expect(shownLocation()).toBe('/users'));

		await click('back');
		expect(startViewTransition).toHaveBeenCalledTimes(2);
		await waitFor(() => expect(shownLocation()).toBe('/sites'));
	});

	it('switches instantly when the system asks to reduce motion', async () => {
		const startViewTransition = installViewTransition();
		vi.spyOn(window, 'matchMedia').mockImplementation(
			(query: string) =>
				({
					matches: query.includes('reduce'),
					media: query,
					onchange: null,
					addEventListener: () => undefined,
					removeEventListener: () => undefined,
					addListener: () => undefined,
					removeListener: () => undefined,
					dispatchEvent: () => false
				}) as MediaQueryList
		);
		mount('/sites');

		await act(async () => {
			fireEvent.click(screen.getByRole('link', { name: 'to-users' }));
		});
		expect(startViewTransition).not.toHaveBeenCalled();
		await waitFor(() => expect(shownLocation()).toBe('/users'));
	});

	it('switches instantly without the View Transition API', async () => {
		mount('/sites');

		await act(async () => {
			fireEvent.click(screen.getByRole('link', { name: 'to-users' }));
		});
		await waitFor(() => expect(shownLocation()).toBe('/users'));
	});
});
