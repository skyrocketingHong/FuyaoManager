import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { ApiError, currentAuthGeneration } from '../api/client';
import { loadSession, sessionSnapshot, subscribeSession } from '../session';
import { apiErrorKey } from '../i18n/errors';
import type { MessageKey } from '../i18n';

function refreshDeniedRole(error: unknown) {
	if (error instanceof ApiError && error.status === 403 && error.code === 'forbidden')
		void loadSession();
}

// Reference UsersPage's request-generation guard, with account generation
// and cancellation added for the manager's cookie-session lifecycle.
export function useAdminQuery<T>(load: (signal: AbortSignal) => Promise<T>) {
	useSyncExternalStore(subscribeSession, sessionSnapshot);
	const generation = currentAuthGeneration();
	const [revision, setRevision] = useState(0);
	const [result, setResult] = useState<{
		load: typeof load;
		generation: number;
		revision: number;
		data: T | null;
		error: MessageKey | null;
		loading: boolean;
	}>();
	useEffect(() => {
		const controller = new AbortController();
		let active = true;
		const current = () => active && generation === currentAuthGeneration();
		setResult((previous) => ({
			load,
			generation,
			revision,
			data: previous?.load === load && previous.generation === generation ? previous.data : null,
			error: null,
			loading: true
		}));
		void load(controller.signal)
			.then((data) => {
				if (current()) setResult({ load, generation, revision, data, error: null, loading: false });
			})
			.catch((error: unknown) => {
				if (!current()) return;
				setResult({
					load,
					generation,
					revision,
					data: null,
					error: apiErrorKey(error) ?? 'error.generic',
					loading: false
				});
				refreshDeniedRole(error);
			});
		return () => {
			active = false;
			controller.abort();
		};
	}, [load, generation, revision]);
	const current = result?.load === load && result.generation === generation;
	const reload = useCallback(() => {
		setResult((previous) => (previous ? { ...previous, loading: true } : previous));
		setRevision((value) => value + 1);
	}, []);
	return {
		data: current ? result.data : null,
		error: current ? result.error : null,
		loading: !current || result.loading,
		reload
	};
}

export function useAdminBatches<T extends { total: number; pageSize: number }, Row>(
	identity: string,
	loadPage: (page: number, signal: AbortSignal) => Promise<T>,
	rowsOf: (data: T) => Row[],
	keyOf: (row: Row) => string
) {
	useSyncExternalStore(subscribeSession, sessionSnapshot);
	const scope = `${currentAuthGeneration()}:${identity}`;
	const [cursor, setCursor] = useState({ scope: '', page: 1 });
	const page = cursor.scope === scope ? cursor.page : 1;
	const load = useCallback((signal: AbortSignal) => loadPage(page, signal), [loadPage, page]);
	const query = useAdminQuery(load);
	const [cache, setCache] = useState<{ scope: string; page: number; data: T; rows: Row[] }>();
	useEffect(() => {
		if (!query.data) return;
		const data = query.data;
		setCache((previous) => {
			const incoming = rowsOf(data);
			if (page === 1 || previous?.scope !== scope) return { scope, page, data, rows: incoming };
			const seen = new Set(previous.rows.map(keyOf));
			return { scope, page, data, rows: [...previous.rows, ...incoming.filter((row) => !seen.has(keyOf(row)))] };
		});
	}, [query.data, scope, page, rowsOf, keyOf]);
	const current = cache?.scope === scope ? cache : undefined;
	const hasMore = Boolean(current && current.page * current.data.pageSize < current.data.total);
	const loadMore = () => {
		if (!current || !hasMore || query.loading || query.error) return;
		setCursor({ scope, page: current.page + 1 });
	};
	const reload = () => {
		if (page === 1) query.reload();
		else setCursor({ scope, page: 1 });
	};
	return { data: current?.data ?? null, rows: current?.rows ?? [], loading: query.loading, error: query.error, hasMore, loadMore, reload, retry: query.reload };
}

export type RunAdminMutation = <T>(
	task: () => Promise<T>,
	after?: (value: T) => void | Promise<void>
) => Promise<boolean>;
export function useAdminMutation() {
	useSyncExternalStore(subscribeSession, sessionSnapshot);
	const generation = currentAuthGeneration();
	const epoch = useRef(0);
	const pending = useRef(false);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState<MessageKey | null>(null);
	useEffect(() => {
		epoch.current++;
		pending.current = false;
		setBusy(false);
		setError(null);
		return () => {
			epoch.current++;
		};
	}, [generation]);
	const run: RunAdminMutation = async (task, after) => {
		if (pending.current) return false;
		pending.current = true;
		setBusy(true);
		setError(null);
		const mine = epoch.current;
		const current = () => mine === epoch.current && generation === currentAuthGeneration();
		try {
			const value = await task();
			if (!current()) return false;
			await after?.(value);
			return true;
		} catch (cause) {
			if (current()) {
				setError(apiErrorKey(cause) ?? 'error.generic');
				refreshDeniedRole(cause);
			}
			return false;
		} finally {
			if (current()) {
				pending.current = false;
				setBusy(false);
			}
		}
	};
	return { run, busy, error, clearError: () => setError(null) };
}
