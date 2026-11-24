/**
 * Page change transitions (robot-platform's useWorkspaceNavigation `commit`
 * and the view-transition rules in libraries/ui/styles/stage.css): a
 * navigation that changes the page cross-fades, with the outgoing page fading
 * out quickly and the incoming one easing in.
 *
 * react-router runs the transition for a navigation that passes
 * `viewTransition`, and repeats it for the matching back/forward trips. This
 * wrapper sets that option once for the whole console instead of at every
 * navigate call and link. It stays out of the way when the navigation replaces
 * the history entry, changes only the query string or hash, explicitly sets
 * the option, or when the browser cannot animate (no View Transition API, or
 * the system asks to reduce motion) — those switch instantly.
 */
import { createBrowserRouter, resolvePath, type To } from 'react-router-dom';

type Router = ReturnType<typeof createBrowserRouter>;
type NavigateOptions = NonNullable<Parameters<Router['navigate']>[1]>;
/** `router.navigate` without its overloads: a number steps through history, null means the current location. */
type Navigate = (to: To | number | null, options?: NavigateOptions) => Promise<void>;

/** Whether `to` leaves the current page: another pathname, not another tab or filter. */
export function isPageChange(to: To, pathname: string): boolean {
	return resolvePath(to, pathname).pathname !== pathname;
}

/** Whether the browser can animate page changes right now. */
export function pageTransitionSupported(): boolean {
	return (
		typeof document !== 'undefined' &&
		typeof document.startViewTransition === 'function' &&
		typeof window !== 'undefined' &&
		typeof window.matchMedia === 'function' &&
		!window.matchMedia('(prefers-reduced-motion: reduce)').matches
	);
}

export function withPageTransitions(router: Router): Router {
	const navigate = router.navigate as Navigate;
	router.navigate = ((to: To | number | null, options?: NavigateOptions) => {
		const choice = options && 'viewTransition' in options ? options.viewTransition : undefined;
		if (to === null || typeof to === 'number' || options?.replace || choice !== undefined)
			return navigate(to, options);
		if (pageTransitionSupported() && isPageChange(to, router.state.location.pathname))
			return navigate(to, { ...options, viewTransition: true } as NavigateOptions);
		return navigate(to, options);
	}) as Router['navigate'];
	return router;
}
