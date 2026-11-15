// Read generations and unknown-result handling adapted from robot-platform's
// platformMaintenance.ts and PlatformComponentsPage. Poll only original tasks.
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { ApiError, currentAuthGeneration } from '../api/client';
import { loadSession, sessionSnapshot, subscribeSession } from '../session';
import { t } from '../i18n';
import { manageUpdates, readUpdates, type UpdateAction, type UpdateStatus } from '../api/updates';

export function useUpdateManager() {
	useSyncExternalStore(subscribeSession, sessionSnapshot);
	const generation = currentAuthGeneration();
	const sequence = useRef(0);
	const mutation = useRef(false);
	const mounted = useRef(false);
	const [state, setState] = useState<{
		generation: number;
		data: UpdateStatus | null;
		error: string;
		loading: boolean;
	}>({ generation, data: null, error: '', loading: true });
	const [busy, setBusy] = useState(false);
	const [note, setNote] = useState<{ type: 'success' | 'warning'; text: string } | null>(null);
	const current = useCallback(
		() => mounted.current && generation === currentAuthGeneration(),
		[generation]
	);
	const refresh = useCallback(
		async (signal?: AbortSignal) => {
			if (mutation.current) return null;
			const id = ++sequence.current;
			try {
				const data = await readUpdates(signal);
				if (!current() || id !== sequence.current) return null;
				setState({ generation, data, error: '', loading: false });
				return data;
			} catch (cause) {
				if (!current() || signal?.aborted || id !== sequence.current) return null;
				setState((previous) => ({
					...previous,
					generation,
					error: t('updates.readFailed'),
					loading: false
				}));
				if (cause instanceof ApiError && cause.status === 403) void loadSession();
				return null;
			}
		},
		[current, generation]
	);
	useEffect(() => {
		mounted.current = true;
		mutation.current = false;
		setBusy(false);
		setNote(null);
		setState({ generation, data: null, error: '', loading: true });
		const controller = new AbortController();
		void refresh(controller.signal);
		const timer = window.setInterval(() => void refresh(controller.signal), 3000);
		return () => {
			mounted.current = false;
			sequence.current++;
			controller.abort();
			window.clearInterval(timer);
		};
	}, [generation, refresh]);
	const run = async (action: UpdateAction) => {
		if (mutation.current) return false;
		mutation.current = true;
		sequence.current++;
		setBusy(true);
		setNote(null);
		try {
			const data = await manageUpdates(action);
			if (!current()) return false;
			setState({ generation, data, error: '', loading: false });
			setNote({
				type: 'success',
				text: t(action.action === 'check' ? 'updates.checked' : 'updates.accepted')
			});
			return true;
		} catch (cause) {
			if (current()) {
				const unknown =
					action.action !== 'check' && (!(cause instanceof ApiError) || cause.status >= 500);
				setNote({
					type: 'warning',
					text: unknown
						? t('updates.unknown')
						: cause instanceof ApiError
							? cause.detail || t('updates.actionFailed')
							: t('updates.actionFailed')
				});
				if (cause instanceof ApiError && cause.status === 403) void loadSession();
			}
			return false;
		} finally {
			if (current()) {
				mutation.current = false;
				setBusy(false);
				void refresh();
			}
		}
	};
	return {
		data: state.generation === generation ? state.data : null,
		error: state.error,
		loading: state.loading,
		fresh: state.generation === generation && !!state.data && !state.error,
		busy,
		note,
		refresh,
		run
	};
}
