/**
 * FriendsPage: edits the site's friends document (online/offline groups)
 * as one validated whole through the shared EditableDoc state machine.
 * A save confirms only its submitted snapshot (edits made while the request
 * is in flight stay dirty), conflict sync refreshes the baseline and ETag
 * without discarding local edits, and discarding them is an explicit action.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Input, Popconfirm, Space, message } from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import { Panel } from '../../components/Panel';
import { useContentScope } from '../../components/ContentScopeProvider';
import { getFriends, getSite, putFriends, type Friend, type Site } from '../../lib/api/management';
import { EditableDoc } from '../../lib/editing/EditableDoc';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

type Group = 'online' | 'offline';

interface FriendsRows {
	online: Friend[];
	offline: Friend[];
}

export default function FriendsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	// A fresh store per site keeps data and error state from surviving a
	// switch between sites; the epoch guards late responses against that
	// same switch.
	const editor = useMemo(() => new EditableDoc<FriendsRows>(), [scope.key]);
	const state = useSyncExternalStore(editor.subscribe, editor.getSnapshot);
	const dirty = editor.isDirty();
	const epoch = useRef(0);

	const load = useCallback(async () => {
		if (!target) return;
		const my = ++epoch.current;
		editor.markLoading();
		try {
			const { friends, digest } = await getFriends(target);
			if (my !== epoch.current) return;
			editor.load({ online: friends.online ?? [], offline: friends.offline ?? [] }, digest);
			setErrorKey(null);
		} catch (err) {
			if (my !== epoch.current) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		}
	}, [target, navigate, editor]);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		setSite(null);
		let cancelled = false;
		if (siteId && scope.canEdit === undefined)
			getSite(siteId)
				.then((s) => {
					if (!cancelled) setSite(s);
				})
				.catch(() => {});
		void load();
		return () => {
			epoch.current++;
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [scope.key]);

	function mutate(group: Group, updater: (rows: Friend[]) => Friend[]) {
		editor.mutate((current) => ({
			...current,
			[group]: updater(current[group] ?? [])
		}));
	}

	function updateRow(group: Group, index: number, patch: Partial<Friend>) {
		mutate(group, (rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
	}

	function addRow(group: Group) {
		mutate(group, (rows) => [...rows, { name: '', url: '' }]);
	}

	function removeRow(group: Group, index: number) {
		mutate(group, (rows) => rows.filter((_, i) => i !== index));
	}

	function moveRow(group: Group, index: number, delta: -1 | 1) {
		mutate(group, (rows) => {
			const next = [...rows];
			const target = index + delta;
			if (target < 0 || target >= next.length) return next;
			[next[index], next[target]] = [next[target], next[index]];
			return next;
		});
	}

	async function handleSave() {
		if (!target) return;
		const snapshot = editor.beginSave();
		if (snapshot === null) return;
		try {
			const saved = await putFriends(target, snapshot, state.etag);
			editor.resolveSave(
				{ online: saved.friends.online ?? [], offline: saved.friends.offline ?? [] },
				saved.digest
			);
			void messageApi.success(t('friends.saved'));
		} catch (err) {
			if (apiErrorKey(err) === 'error.conflict') {
				editor.rejectSave(true);
			} else {
				editor.rejectSave(false);
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
			}
		}
	}

	// Conflict sync adopts the server version as the new baseline and ETag
	// while keeping the local draft: unsaved edits are never dropped here.
	async function syncAfterConflict() {
		if (!target) return;
		const my = epoch.current;
		try {
			const { friends, digest } = await getFriends(target);
			if (my !== epoch.current) return;
			editor.syncAfterConflict(
				{ online: friends.online ?? [], offline: friends.offline ?? [] },
				digest
			);
			void messageApi.info(t('friends.conflictSynced'));
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	useUnsavedGuard(dirty);
	const rows: FriendsRows | null = state.draft ?? state.baseline;

	const headerActions = (
		<Space size={8}>
			{state.conflict ? (
				<>
					<Button onClick={() => void syncAfterConflict()}>{t('posts.conflictAction')}</Button>
					<Button danger onClick={() => editor.discardLocal()}>
						{t('posts.discardLocal')}
					</Button>
				</>
			) : null}
			<Button
				type="primary"
				loading={state.saving}
				disabled={!editable || !dirty}
				onClick={() => void handleSave()}
			>
				<Trans text={t('common.save')} />
			</Button>
		</Space>
	);
	const headerActive = usePageHeader({ actions: headerActions });

	function groupEditor(group: Group) {
		const groupRows = rows ? rows[group] : null;
		return (
			<Panel
				title={group === 'online' ? t('friends.online') : t('friends.offline')}
				tags={
					editable ? (
						<Button size="small" onClick={() => addRow(group)}>
							{t('friends.add')}
						</Button>
					) : null
				}
			>
				{groupRows === null || state.loading ? (
					<PageLoading spec="section" />
				) : groupRows.length === 0 ? (
					t('friends.emptyGroup')
				) : (
					groupRows.map((row, index) => (
						<Space key={index} style={{ display: 'flex', marginBottom: 4 }} align="center">
							<Input
								size="small"
								style={{ width: 140 }}
								placeholder={t('friends.fieldName')}
								value={row.name}
								disabled={!editable}
								onChange={(e) => updateRow(group, index, { name: e.target.value })}
							/>
							<Input
								size="small"
								style={{ width: 200 }}
								placeholder="https://…"
								value={row.url}
								disabled={!editable}
								onChange={(e) => updateRow(group, index, { url: e.target.value })}
							/>
							<Input
								size="small"
								style={{ width: 180 }}
								placeholder={t('friends.fieldDescription')}
								value={row.description ?? ''}
								disabled={!editable}
								onChange={(e) => updateRow(group, index, { description: e.target.value })}
							/>
							<Input
								size="small"
								style={{ width: 180 }}
								placeholder={t('friends.fieldAvatar')}
								value={row.avatar ?? ''}
								disabled={!editable}
								onChange={(e) => updateRow(group, index, { avatar: e.target.value })}
							/>
							{editable ? (
								<Space size={0}>
									<Button size="small" type="text" onClick={() => moveRow(group, index, -1)}>
										↑
									</Button>
									<Button size="small" type="text" onClick={() => moveRow(group, index, 1)}>
										↓
									</Button>
									<Popconfirm
										title={t('friends.removeConfirm')}
										onConfirm={() => removeRow(group, index)}
									>
										<Button size="small" type="text" danger>
											×
										</Button>
									</Popconfirm>
								</Space>
							) : null}
						</Space>
					))
				)}
			</Panel>
		);
	}

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{headerActions}</div>
			) : null}
			{state.conflict ? (
				<Alert
					type="warning"
					showIcon
					title={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{groupEditor('online')}
			{groupEditor('offline')}
		</div>
	);
}
