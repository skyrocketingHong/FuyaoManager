/**
 * useUnsavedGuard: shared draft protection for editors — warns on browser
 * close/reload (beforeunload) and on in-app navigation away from the dirty
 * page (react-router blocker). Confirmation goes through the AntApp modal
 * so it inherits the current theme and locale.
 *
 * Signing out ends the page without a router navigation, so the guard also
 * joins the leave-guard registry (leaveGuard.ts): the account menu asks the
 * same question before the session is cleared, and the navigation that
 * follows a confirmed sign-out passes the blocker once.
 */
import { useCallback, useEffect, useRef } from 'react';
import { useBlocker, type Location } from 'react-router-dom';
import { App } from 'antd';
import { t } from '../i18n';
import { registerLeaveGuard } from './leaveGuard';

export function useUnsavedGuard(
	dirty: boolean,
	allowNavigation?: (current: Location, next: Location) => boolean
): void {
	const { modal } = App.useApp();
	const proceedOnce = useRef(false);

	const askLeave = useCallback(
		(onConfirm: () => void, onCancel: () => void) => {
			modal.confirm({
				title: t('draft.leaveTitle'),
				content: t('draft.leaveBody'),
				okText: t('draft.leaveConfirm'),
				cancelText: t('draft.leaveCancel'),
				onOk: onConfirm,
				onCancel
			});
		},
		[modal]
	);

	useEffect(() => {
		if (!dirty) return;
		const handler = (event: BeforeUnloadEvent) => {
			event.preventDefault();
		};
		window.addEventListener('beforeunload', handler);
		return () => window.removeEventListener('beforeunload', handler);
	}, [dirty]);

	useEffect(() => {
		if (!dirty) return;
		return registerLeaveGuard({
			confirm: () =>
				new Promise<boolean>((resolve) => {
					askLeave(
						() => {
							proceedOnce.current = true;
							resolve(true);
						},
						() => resolve(false)
					);
				}),
			revoke: () => {
				proceedOnce.current = false;
			}
		});
	}, [dirty, askLeave]);

	const blocker = useBlocker(({ currentLocation, nextLocation }) => {
		// The navigation that follows a confirmed sign-out passes once.
		if (proceedOnce.current) {
			proceedOnce.current = false;
			return false;
		}
		return dirty && !allowNavigation?.(currentLocation, nextLocation);
	});

	useEffect(() => {
		if (blocker.state !== 'blocked') return;
		askLeave(
			() => blocker.proceed(),
			() => blocker.reset()
		);
	}, [blocker, askLeave]);
}
