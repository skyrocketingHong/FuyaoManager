/**
 * FriendsPage: edits the site's friends document (online/offline groups)
 * as one validated whole. The document-level digest drives If-Match saves;
 * a 412 keeps the local rows and offers a resync.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Card, Input, Popconfirm, Space, Spin, message } from 'antd';
import Trans from '../../components/i18n/Trans';
import { getFriends, getSite, putFriends, type Friend, type Site } from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

type Group = 'online' | 'offline';

export default function FriendsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [online, setOnline] = useState<Friend[] | null>(null);
	const [offline, setOffline] = useState<Friend[] | null>(null);
	const [digest, setDigest] = useState('');
	const [dirty, setDirty] = useState(false);
	const [saving, setSaving] = useState(false);
	const [conflict, setConflict] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	const epoch = useRef(0);

	const load = useCallback(async () => {
		if (!siteId) return;
		const my = ++epoch.current;
		try {
			const { friends, digest } = await getFriends(siteId);
			if (my !== epoch.current) return;
			setOnline(friends.online ?? []);
			setOffline(friends.offline ?? []);
			setDigest(digest);
			setDirty(false);
			setConflict(false);
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
	}, [siteId, navigate]);

	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setOnline(null);
		setOffline(null);
		let cancelled = false;
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
	}, [siteId]);

	function mutate(group: Group, updater: (rows: Friend[]) => Friend[]) {
		setDirty(true);
		if (group === 'online') setOnline((rows) => updater(rows ?? []));
		else setOffline((rows) => updater(rows ?? []));
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
		if (!siteId || online === null || offline === null) return;
		setSaving(true);
		try {
			const saved = await putFriends(siteId, { online, offline }, digest);
			setDigest(saved.digest);
			setDirty(false);
			setConflict(false);
			void messageApi.success(t('friends.saved'));
		} catch (err) {
			if (apiErrorKey(err) === 'error.conflict') {
				setConflict(true);
			} else {
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
			}
		} finally {
			setSaving(false);
		}
	}

	async function syncAfterConflict() {
		await load();
		void messageApi.info(t('friends.conflictSynced'));
	}

	const editable = site !== null && (site.role === 'owner' || site.role === 'editor');

	function groupEditor(group: Group, rows: Friend[] | null) {
		return (
			<Card
				size="small"
				title={group === 'online' ? t('friends.online') : t('friends.offline')}
				extra={
					editable ? (
						<Button size="small" onClick={() => addRow(group)}>
							{t('friends.add')}
						</Button>
					) : null
				}
				style={{ marginBottom: 12 }}
			>
				{rows === null ? (
					<Spin size="small" />
				) : rows.length === 0 ? (
					t('friends.emptyGroup')
				) : (
					rows.map((row, index) => (
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
			</Card>
		);
	}

	return (
		<>
			{contextHolder}
			<div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
				<Space>
					{conflict ? (
						<Button onClick={() => void syncAfterConflict()}>{t('posts.conflictAction')}</Button>
					) : null}
					<Button
						type="primary"
						loading={saving}
						disabled={!editable || !dirty}
						onClick={() => void handleSave()}
					>
						<Trans text={t('common.save')} />
					</Button>
				</Space>
			</div>
			{conflict ? (
				<Alert
					type="warning"
					showIcon
					message={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{groupEditor('online', online)}
			{groupEditor('offline', offline)}
		</>
	);
}
