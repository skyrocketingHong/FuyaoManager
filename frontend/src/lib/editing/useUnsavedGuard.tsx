/**
 * useUnsavedGuard: shared draft protection for editors — warns on browser
 * close/reload (beforeunload) and on in-app navigation away from the dirty
 * page (react-router blocker). Confirmation goes through the AntApp modal
 * so it inherits the current theme and locale.
 */
import { useEffect } from 'react';
import { useBlocker, type Location } from 'react-router-dom';
import { App } from 'antd';
import { t } from '../i18n';

export function useUnsavedGuard(
	dirty: boolean,
	allowNavigation?: (current: Location, next: Location) => boolean
): void {
	const { modal } = App.useApp();

	useEffect(() => {
		if (!dirty) return;
		const handler = (event: BeforeUnloadEvent) => {
			event.preventDefault();
		};
		window.addEventListener('beforeunload', handler);
		return () => window.removeEventListener('beforeunload', handler);
	}, [dirty]);

	const blocker = useBlocker(({ currentLocation, nextLocation }) =>
		dirty && !allowNavigation?.(currentLocation, nextLocation)
	);

	useEffect(() => {
		if (blocker.state !== 'blocked') return;
		modal.confirm({
			title: t('draft.leaveTitle'),
			content: t('draft.leaveBody'),
			okText: t('draft.leaveConfirm'),
			cancelText: t('draft.leaveCancel'),
			onOk: () => blocker.proceed(),
			onCancel: () => blocker.reset()
		});
	}, [blocker, modal]);
}
