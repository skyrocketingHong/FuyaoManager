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
import { ArrowDown, ArrowUp, GripVertical, X } from 'lucide-react';
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
import { useFriendsReorder } from './useFriendsReorder';

type Group = 'online' | 'offline';

interface FriendsRows {
	online: Friend[];
	offline: Friend[];
}

function FriendsList({
	rows,
	editable,
	label,
	rowKeys,
	onUpdate,
	onRemove,
	onMove
}: {
	rows: Friend[];
	editable: boolean;
	label: string;
	rowKeys: WeakMap<Friend, number>;
	onUpdate: (index: number, patch: Partial<Friend>) => void;
	onRemove: (index: number) => void;
	onMove: (from: number, to: number) => void;
}) {
	const keys = rows.map((row) => rowKeys.get(row)!);
	const { listRef, instructionsId, announcement, handleProps, cancel } = useFriendsReorder(
		editable,
		onMove
	);

	function move(index: number, delta: -1 | 1) {
		const to = index + delta;
		if (to < 0 || to >= rows.length) return;
		cancel();
		onMove(index, to);
	}

	return (
		<>
			<div ref={listRef} className="friends-list" role="list" aria-label={label}>
				{rows.map((row, index) => {
					const name = row.name.trim() || t('friends.reorderUnnamed', { position: index + 1 });
					return (
						<div key={keys[index]} className="friends-row" role="listitem">
							<Input
								size="small"
								placeholder={t('friends.fieldName')}
								aria-label={t('friends.fieldName')}
								value={row.name}
								disabled={!editable}
								onChange={(e) => onUpdate(index, { name: e.target.value })}
							/>
							<Input
								size="small"
								placeholder="https://…"
								aria-label={t('config.linkUrl')}
								value={row.url}
								disabled={!editable}
								onChange={(e) => onUpdate(index, { url: e.target.value })}
							/>
							<Input
								size="small"
								placeholder={t('friends.fieldDescription')}
								aria-label={t('friends.fieldDescription')}
								value={row.description ?? ''}
								disabled={!editable}
								onChange={(e) => onUpdate(index, { description: e.target.value })}
							/>
							<Input
								size="small"
								placeholder={t('friends.fieldAvatar')}
								aria-label={t('friends.fieldAvatar')}
								value={row.avatar ?? ''}
								disabled={!editable}
								onChange={(e) => onUpdate(index, { avatar: e.target.value })}
							/>
							{editable ? (
								<Space size={0}>
									{rows.length > 1 ? (
										<Button
											size="small"
											type="text"
											className="friends-drag-handle"
											aria-label={t('friends.reorderHandle', { name })}
											{...handleProps(name)}
										>
											<GripVertical size={14} aria-hidden="true" />
										</Button>
									) : null}
									<Button
										size="small"
										type="text"
										aria-label={t('common.moveUp')}
										onClick={() => move(index, -1)}
									>
										<ArrowUp size={14} aria-hidden="true" />
									</Button>
									<Button
										size="small"
										type="text"
										aria-label={t('common.moveDown')}
										onClick={() => move(index, 1)}
									>
										<ArrowDown size={14} aria-hidden="true" />
									</Button>
									<Popconfirm title={t('friends.removeConfirm')} onConfirm={() => onRemove(index)}>
										<Button size="small" type="text" danger aria-label={t('common.remove')}>
											<X size={14} aria-hidden="true" />
										</Button>
									</Popconfirm>
								</Space>
							) : null}
						</div>
					);
				})}
			</div>
			{editable && (
				<>
					<span id={instructionsId} className="sr-only">
						{t('friends.reorderInstructions')}
					</span>
					<span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
						{announcement}
					</span>
				</>
			)}
		</>
	);
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
	const rowKeys = useMemo(() => new WeakMap<Friend, number>(), [editor]);
	const nextRowKey = useRef(0);
	// Identity belongs to the editor, so editable fields and duplicate links can
	// retain their DOM nodes without adding identifiers to the saved document.
	const identifyRow = useCallback(
		(row: Friend, previous?: Friend) => {
			rowKeys.set(row, (previous && rowKeys.get(previous)) ?? ++nextRowKey.current);
			return row;
		},
		[rowKeys]
	);
	const identifyRows = useCallback(
		(friends: Partial<FriendsRows>, previous?: FriendsRows): FriendsRows => ({
			online: (friends.online ?? []).map((row, index) =>
				identifyRow({ ...row }, previous?.online[index])
			),
			offline: (friends.offline ?? []).map((row, index) =>
				identifyRow({ ...row }, previous?.offline[index])
			)
		}),
		[identifyRow]
	);

	const load = useCallback(async () => {
		if (!target) return;
		const my = ++epoch.current;
		editor.markLoading();
		try {
			const { friends, digest } = await getFriends(target);
			if (my !== epoch.current) return;
			editor.load(identifyRows(friends), digest);
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
	}, [target, navigate, editor, identifyRows]);

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
		mutate(group, (rows) =>
			rows.map((row, i) => (i === index ? identifyRow({ ...row, ...patch }, row) : row))
		);
	}

	function addRow(group: Group) {
		mutate(group, (rows) => [...rows, identifyRow({ name: '', url: '' })]);
	}

	function removeRow(group: Group, index: number) {
		mutate(group, (rows) => rows.filter((_, i) => i !== index));
	}

	function moveRow(group: Group, from: number, to: number) {
		mutate(group, (rows) => {
			if (from === to || !rows[from] || !rows[to]) return rows;
			const next = [...rows];
			next.splice(to, 0, ...next.splice(from, 1));
			return next;
		});
	}

	async function handleSave() {
		if (!target) return;
		const snapshot = editor.beginSave();
		if (snapshot === null) return;
		try {
			const saved = await putFriends(target, snapshot, state.etag);
			editor.resolveSave(identifyRows(saved.friends, snapshot), saved.digest);
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
			editor.syncAfterConflict(identifyRows(friends), digest);
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
		const label = group === 'online' ? t('friends.online') : t('friends.offline');
		return (
			<Panel
				title={label}
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
					<FriendsList
						key={`${scope.key}:${group}`}
						rows={groupRows}
						editable={editable}
						label={label}
						rowKeys={rowKeys}
						onUpdate={(index, patch) => updateRow(group, index, patch)}
						onRemove={(index) => removeRow(group, index)}
						onMove={(from, to) => moveRow(group, from, to)}
					/>
				)}
			</Panel>
		);
	}

	return (
		<div className="page-stack page-stack--fill">
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
			<div className="friends-groups fill-height">
				{groupEditor('online')}
				{groupEditor('offline')}
			</div>
		</div>
	);
}
