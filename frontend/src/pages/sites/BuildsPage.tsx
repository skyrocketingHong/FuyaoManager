import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Popconfirm, Space, Table, Typography, message } from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import StatusTag from '../../components/StatusTag';
import { TablePanel } from '../../components/Panel';
import {
	canEditSite,
	closePreviewLink,
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
	const [closing, setClosing] = useState<string | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	// Request epoch: every refresh bumps it; a response applies only while it
	// is still the newest. Route changes and unmount bump it too, so a late
	// answer for a previous site (or after leaving the page) can never
	// overwrite the current list.
	const epoch = useRef(0);

	const refresh = useCallback(async () => {
		if (!siteId) return;
		const my = ++epoch.current;
		try {
			const list = await listBuilds(siteId);
			if (my !== epoch.current) return;
			setBuilds(list);
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

	// Site change (same component instance, different route param): drop all
	// previous data first so nothing from the old site survives, then load.
	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setBuilds(null);
		setErrorKey(null);
		let cancelled = false;
		getSite(siteId)
			.then((s) => {
				if (!cancelled) setSite(s);
			})
			.catch((err) => {
				if (cancelled) return;
				if (apiErrorKey(err) === 'error.unauthenticated') {
					navigate('/login');
				}
			});
		void refresh();
		return () => {
			// Leaving the page or switching sites invalidates everything
			// in flight, including the site record.
			epoch.current++;
			cancelled = true;
		};
	}, [siteId, refresh, navigate]);

	// Poll while any task is queued or running; cleared on unmount and site
	// switch via the effect cleanup above (refresh identity changes with
	// siteId, re-running this effect).
	useEffect(() => {
		if (builds === null || !builds.some((b) => !terminalStatus.has(b.status))) return;
		const timer = window.setInterval(() => void refresh(), 3000);
		return () => window.clearInterval(timer);
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

	async function handlePreview(buildID: string) {
		if (!siteId) return;
		try {
			const link = await createPreviewLink(siteId, buildID);
			window.open(link.url, '_blank', 'noopener');
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	// Closing is a server-side action: tokens are revoked and the listener and
	// port are released. It is confirmed, idempotent, and scoped to this one
	// build's preview.
	async function handleClosePreview(buildID: string) {
		if (!siteId) return;
		setClosing(buildID);
		try {
			await closePreviewLink(siteId, buildID);
			void messageApi.success(t('builds.previewClosed'));
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setClosing(null);
		}
	}

	const editable = site !== null && canEditSite(site);

	const createButton = (
		<Popconfirm title={t('builds.create')} onConfirm={() => void handleCreate()} disabled={!editable}>
			<Button type="primary" loading={creating} disabled={!editable}>
				<Trans text={t('builds.create')} />
			</Button>
		</Popconfirm>
	);
	const headerActive = usePageHeader({ actions: createButton });

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{createButton}</div>
			) : null}
			{errorKey !== null ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey ?? 'error.generic')}
					style={{ marginBottom: 16 }}
				/>
			) : null}
			{builds === null ? (
				<PageLoading spec="page" label={t('common.loading')} />
			) : builds.length === 0 ? (
				<TablePanel>
					<p>{t('builds.empty')}</p>
				</TablePanel>
			) : (
				<TablePanel>
				<Table<BuildTask>
					rowKey="id"
					dataSource={builds}
					pagination={false}
					columns={[
						{
							title: 'ID',
							dataIndex: 'id',
							render: (id: string) => (
								<Typography.Text copyable code>
									{id.slice(0, 8)}
								</Typography.Text>
							)
						},
						{
							title: t('builds.status'),
							dataIndex: 'status',
							render: (status: string) => (
								<StatusTag
									kind={
										status === 'succeeded'
											? 'success'
											: status === 'failed'
												? 'danger'
												: status === 'running'
													? 'info'
													: 'neutral'
									}
								>
									{t(`builds.status.${status}` as never)}
								</StatusTag>
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
										{String(
											(record.inputDigests as Record<string, string>).contentDigest ?? ''
										).slice(7, 19)}
										…
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
											<Trans text={t('builds.preview')} />
										</Button>
									) : null}
									{record.status === 'succeeded' && editable ? (
										<Popconfirm
											title={t('builds.previewCloseConfirm')}
											onConfirm={() => void handleClosePreview(record.id)}
										>
											<Button size="small" loading={closing === record.id}>
												<Trans text={t('builds.previewClose')} />
											</Button>
										</Popconfirm>
									) : null}
								</Space>
							)
						}
					]}
				/>
			</TablePanel>
			)}
		</div>
	);
}
