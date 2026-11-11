import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Card, Popconfirm, Space, Spin, Table, Tag, Typography, message } from 'antd';
import SiteNav from '../../components/sites/SiteNav';
import {
	canEditSite,
	createBuild,
	createPreviewLink,
	getSite,
	listBuilds,
	type BuildTask,
	type Site
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

const terminalStatus = new Set(['succeeded', 'failed']);

export default function BuildsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [builds, setBuilds] = useState<BuildTask[] | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [creating, setCreating] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();
	const pollRef = useRef<number | null>(null);

	const refresh = useCallback(async () => {
		if (!siteId) return;
		try {
			const list = await listBuilds(siteId);
			setBuilds(list);
			return list;
		} catch (err) {
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
		let cancelled = false;
		getSite(siteId)
			.then((s) => {
				if (!cancelled) setSite(s);
			})
			.catch(() => undefined);
		return () => {
			cancelled = true;
		};
	}, [siteId]);

	useEffect(() => {
		void refresh();
	}, [refresh]);

	// Poll while any task is queued or running.
	useEffect(() => {
		if (builds === null || !builds.some((b) => !terminalStatus.has(b.status))) return;
		pollRef.current = window.setInterval(() => void refresh(), 3000);
		return () => {
			if (pollRef.current !== null) window.clearInterval(pollRef.current);
		};
	}, [builds, refresh]);

	async function handleCreate() {
		if (!siteId) return;
		setCreating(true);
		try {
			await createBuild(siteId);
			await refresh();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		} finally {
			setCreating(false);
		}
	}

	async function handlePreview(buildId: string) {
		if (!siteId) return;
		try {
			const link = await createPreviewLink(siteId, buildId);
			window.open(link.url, '_blank', 'noopener');
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable = site !== null && canEditSite(site);

	return (
		<>
			{contextHolder}
			<SiteNav active="builds" />
			<Card
				title={t('builds.title')}
				extra={
					<Popconfirm
						title={t('builds.create')}
						onConfirm={() => void handleCreate()}
						disabled={!editable}
					>
						<Button type="primary" loading={creating} disabled={!editable}>
							{t('builds.create')}
						</Button>
					</Popconfirm>
				}
				style={{ marginTop: 16 }}
			>
				{errorKey !== null ? (
					<Alert type="error" showIcon message={t(errorKey ?? 'error.generic')} style={{ marginBottom: 16 }} />
				) : null}
				{builds === null ? (
					<p role="status">
						<Spin /> {t('common.loading')}
					</p>
				) : builds.length === 0 ? (
					<p>{t('builds.empty')}</p>
				) : (
					<Table<BuildTask>
						rowKey="id"
						size="small"
						dataSource={builds}
						pagination={false}
						columns={[
							{
								title: 'ID',
							 dataIndex: 'id',
								render: (id: string) => <Typography.Text copyable code>{id.slice(0, 8)}</Typography.Text>
							},
							{
								title: t('builds.status'),
								dataIndex: 'status',
								render: (status: string) => (
									<Tag
										color={
											status === 'succeeded'
												? 'green'
												: status === 'failed'
													? 'red'
													: status === 'running'
														? 'blue'
														: 'default'
										}
									>
										{t(`builds.status.${status}` as never)}
									</Tag>
								)
							},
							{
								title: t('releases.themeColumn'),
								render: (_, record) => `${record.theme.themeId} @ ${record.theme.version}`
							},
							{
								title: t('builds.siteSettingsRevision'),
								dataIndex: 'siteSettingsRevision'
							},
							{
								title: t('builds.themeRevision'),
								dataIndex: 'themeSelectionRevision'
							},
							{
								title: t('builds.inputDigests'),
								render: (_, record) =>
									record.inputDigests ? (
										<Typography.Text type="secondary" style={{ fontSize: 12 }}>
											{String((record.inputDigests as Record<string, string>).contentDigest ?? '').slice(7, 19)}…
										</Typography.Text>
									) : null
							},
							{
								render: (_, record) => (
									<Space>
										{record.failure ? (
											<Typography.Text type="danger">
												{t('builds.failureStage')}: {record.failure.stage}
											</Typography.Text>
										) : null}
										{record.status === 'succeeded' ? (
											<Button size="small" onClick={() => void handlePreview(record.id)}>
												{t('builds.preview')}
											</Button>
										) : null}
									</Space>
								)
							}
						]}
					/>
				)}
			</Card>
		</>
	);
}
