import { useCallback } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { matchRoute } from '.';

// Ported from robot-platform's workspaceNavigation: each stable workspace
// keeps its path and validates its own query-driven subviews.
export function queryValue(params: URLSearchParams, name: string): string | undefined {
	const values = params.getAll(name);
	return values.length === 1 && values[0] ? values[0] : undefined;
}

export function queryChoice<T extends string>(
	params: URLSearchParams,
	name: string,
	choices: readonly T[],
	fallback: T
): T {
	const value = queryValue(params, name);
	return choices.find((choice) => choice === value) ?? fallback;
}

export function usePageQuery() {
	const { pathname, search, hash } = useLocation();
	const navigate = useNavigate();
	const setQuery = useCallback(
		(values: Record<string, string | null | undefined>, replace = false) => {
			const params = new URLSearchParams(search);
			for (const [name, value] of Object.entries(values)) {
				if (value) params.set(name, value);
				else params.delete(name);
			}
			const next = params.size ? `?${params}` : '';
			if (next !== search)
				navigate({ pathname, search: next, hash }, { replace, preventScrollReset: true });
		},
		[pathname, search, hash, navigate]
	);
	return [new URLSearchParams(search), setQuery] as const;
}

export function tabDestination(pathname: string, search: string, tab: string) {
	const params = new URLSearchParams(search);
	params.delete('view');
	params.set('tab', tab);
	return { pathname, search: `?${params}` };
}

const internalOrigin = 'https://workspace.invalid';

export function requestedWorkspace(search: string): string | null {
	const next = queryValue(new URLSearchParams(search), 'next');
	if (!next?.startsWith('/') || next.startsWith('//') || next.includes('\\') || next.length > 4096)
		return null;
	const target = new URL(next, internalOrigin);
	return target.origin === internalOrigin && matchRoute(target.pathname)
		? `${target.pathname}${target.search}${target.hash}`
		: null;
}

export function authDestination(path: string, next: string | null): string {
	return next ? `${path}?${new URLSearchParams({ next })}` : path;
}
