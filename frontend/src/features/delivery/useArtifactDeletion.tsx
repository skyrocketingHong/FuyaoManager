import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Alert, App, Button, Space, type TableProps } from 'antd';
import { Trash2 } from 'lucide-react';
import StatusTag from '../../components/StatusTag';
import { deleteArtifacts, type ArtifactKind, type ArtifactState } from '../../lib/api/artifacts';
import { t, type MessageKey } from '../../lib/i18n';
import { currentAuthGeneration } from '../../lib/api/client';
import { apiErrorKey } from '../../lib/i18n/errors';
import { sessionSnapshot, subscribeSession } from '../../lib/session';

type Artifact = {
	id: string;
	status: string;
	artifactState?: ArtifactState;
	previewActive?: boolean;
};

export function ArtifactStatus({ state }: { state: ArtifactState | undefined }) {
	if (!state || state === 'available') return null;
	return (
		<StatusTag kind={state === 'deleting' ? 'warning' : 'neutral'}>
			{t(`artifacts.${state}`)}
		</StatusTag>
	);
}

export function useArtifactDeletion<Row extends Artifact>(
	siteId: string | undefined,
	isOwner: boolean,
	kind: ArtifactKind,
	rows: readonly Row[],
	reload: () => Promise<void>
) {
	const { modal, message } = App.useApp();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const scope = `${currentAuthGeneration()}:${siteId ?? ''}`;
	const activeScope = useRef(scope);
	activeScope.current = scope;
	const busyRef = useRef<string>();
	const mounted = useRef(false);
	useEffect(() => {
		mounted.current = true;
		return () => {
			mounted.current = false;
		};
	}, []);
	const [selection, setSelection] = useState<{ scope: string; ids: string[] }>();
	const [deleting, setDeleting] = useState<string>();
	const [error, setError] = useState<{ scope: string; key: MessageKey }>();
	const allowed = Boolean(siteId && isOwner && session.user?.platformRole === 'admin');
	const canDelete = (row: Row) =>
		allowed &&
		row.artifactState !== 'deleted' &&
		(kind === 'builds'
			? row.status === 'succeeded' && !row.previewActive
			: row.status === 'superseded');
	const eligible = new Set(rows.filter(canDelete).map((row) => row.id));
	const ids = selection?.scope === scope ? selection.ids.filter((id) => eligible.has(id)) : [];
	const busy = deleting === scope;

	function confirm(idsToDelete: string[]) {
		if (
			!siteId ||
			busyRef.current === scope ||
			!idsToDelete.length ||
			!idsToDelete.every((id) => eligible.has(id))
		)
			return;
		busyRef.current = scope;
		const current = () =>
			mounted.current &&
			activeScope.current === scope &&
			scope.startsWith(`${currentAuthGeneration()}:`);
		modal.confirm({
			title: t('artifacts.confirm', { count: idsToDelete.length }),
			content: t(
				kind === 'builds' ? 'artifacts.buildsConsequences' : 'artifacts.releasesConsequences'
			),
			okText: t('artifacts.delete'),
			okButtonProps: { danger: true },
			cancelText: t('common.cancel'),
			focusable: { autoFocusButton: 'cancel' },
			afterClose: () => {
				if (busyRef.current === scope) busyRef.current = undefined;
			},
			onOk: async () => {
				if (!current()) return;
				setDeleting(scope);
				setError(undefined);
				try {
					await deleteArtifacts(siteId, kind, idsToDelete);
					if (!current()) return;
					setSelection(undefined);
					void message.success(t('artifacts.deletedCount', { count: idsToDelete.length }));
				} catch (cause) {
					if (current()) setError({ scope, key: apiErrorKey(cause) ?? 'error.generic' });
				} finally {
					if (current()) {
						await reload();
						if (current()) setDeleting((state) => (state === scope ? undefined : state));
					}
				}
			}
		});
	}

	const rowSelection: TableProps<Row>['rowSelection'] = allowed
		? {
				selectedRowKeys: ids,
				getCheckboxProps: (row) => ({
					disabled: busy || !canDelete(row),
					'aria-label': t('artifacts.select', { id: row.id })
				}),
				onChange: (keys) => {
					if (keys.length > 100) void message.warning(t('artifacts.limit'));
					setSelection({ scope, ids: keys.slice(0, 100).map(String) });
				}
			}
		: undefined;

	return {
		rowSelection,
		actions: ids.length > 0 && (
			<Space size={8}>
				<Button onClick={() => setSelection(undefined)} disabled={busy}>
					{t('artifacts.clear')}
				</Button>
				<Button danger icon={<Trash2 size={15} />} loading={busy} onClick={() => confirm(ids)}>
					{t('artifacts.deleteSelected', { count: ids.length })}
				</Button>
			</Space>
		),
		error: error?.scope === scope && (
			<Alert
				type="error"
				showIcon
				title={t(error.key)}
				closable
				onClose={() => setError(undefined)}
			/>
		),
		rowAction: (row: Row) =>
			canDelete(row) && (
				<Button danger size="small" disabled={busy} onClick={() => confirm([row.id])}>
					{t(row.artifactState === 'deleting' ? 'artifacts.retry' : 'artifacts.delete')}
				</Button>
			)
	};
}
