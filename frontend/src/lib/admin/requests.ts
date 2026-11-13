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
