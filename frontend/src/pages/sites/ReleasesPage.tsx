import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	Button,
	Card,
	Modal,
	Space,
	Spin,
	Table,
	Tag,
	Typography,
	message
} from 'antd';
import SiteNav from '../../components/sites/SiteNav';
import {
	canEditSite,
	createRelease,
	getCurrentRelease,
	getSite,
	listBuilds,
	listReleases,
	rollbackRelease,
	type BuildTask,
	type Release,
	type ReleaseCreate,
	type Site
} from '../../lib/api/management';
import { ApiError } from '../../lib/api/client';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

interface CurrentView {
	release: Release;
	originUrl: string;
}

export default function ReleasesPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [current, setCurrent] = useState<CurrentView | null>(null);
	const [releases, setReleases] = useState<Release[] | null>(null);
	const [succeeded, setSucceeded] = useState<BuildTask[]>([]);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [publishing, setPublishing] = useState(false);
	const [rollbackTarget, setRollbackTarget] = useState<Release | null>(null);
	const [rollingBack, setRollingBack] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();

	const refresh = useCallback(async () => {
		if (!siteId) return;
		try {
			const [releaseList, buildList] = await Promise.all([
				listReleases(siteId),
				listBuilds(siteId)
			]);
			setReleases(releaseList);
			setSucceeded(buildList.filter((b) => b.status === 'succeeded'));
			try {
				const view = await getCurrentRelease(siteId);
				setCurrent(view);
			} catch (err) {
				if (err instanceof ApiError && err.code === 'no_live_release') {
					setCurrent(null);
				} else {
					throw err;
				}
			}
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

	async function handlePublish(buildId: string) {
		if (!siteId) return;
		setPublishing(true);
		try {
			const body: ReleaseCreate = { buildId, reasons: ['manual'] };
			const release = await createRelease(siteId, body);
			if (release.status === 'failed' && release.failure) {
				void messageApi.error(
					`${t('releases.status.failed')}: ${release.failure.stage}${
						release.failure.missingArtifacts?.length
							? ` (${release.failure.missingArtifacts.join(', ')})`
							: ''
					}`
				);
			}
			await refresh();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		} finally {
			setPublishing(false);
		}
	}

	async function handleRollback() {
		if (!siteId || rollbackTarget === null) return;
		setRollingBack(true);
		try {
			await rollbackRelease(siteId, rollbackTarget.id);
			setRollbackTarget(null);
			await refresh();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setRollingBack(false);
		}
	}

	const editable = site !== null && canEditSite(site);
	const latestSucceeded = succeeded[0] ?? null;
	const publishedBuildIds = new Set((releases ?? []).map((r) => r.buildId));
	const publishable = latestSucceeded !== null && !publishedBuildIds.has(latestSucceeded.id);

	return (
		<>
			{contextHolder}
			<SiteNav active="releases" />
			<Card
				title={t('releases.title')}
				extra={
					<Space>
						{current && current.originUrl ? (
							<Button href={current.originUrl} target="_blank">
								{t('releases.open')}
							</Button>
						) : null}
						<Button
							type="primary"
							loading={publishing}
							disabled={!editable || !publishable}
							onClick={() => latestSucceeded && handlePublish(latestSucceeded.id)}
						>
							{t('releases.publish')}
						</Button>
					</Space>
				}
				style={{ marginTop: 16 }}
			>
				{errorKey !== null ? (
					<Alert type="error" showIcon message={t(errorKey ?? 'error.generic')} style={{ marginBottom: 16 }} />
				) : null}
				{releases === null ? (
					<p role="status">
						<Spin /> {t('common.loading')}
					</p>
				) : (
					<>
						{current ? (
							<Alert
								type="success"
								showIcon
								message={`${t('releases.current')}: ${current.release.theme.themeId} @ ${current.release.theme.version}`}
								style={{ marginBottom: 16 }}
							/>
						) : (
							<Alert type="info" showIcon message={t('error.noLiveRelease')} style={{ marginBottom: 16 }} />
						)}
						{!editable ? (
							<Alert type="info" showIcon message={t('releases.roleNotice')} style={{ marginBottom: 16 }} />
						) : null}
						{editable && !publishable && latestSucceeded === null ? (
							<Alert type="info" showIcon message={t('releases.noSucceededBuild')} style={{ marginBottom: 16 }} />
						) : null}
						<Table<Release>
							rowKey="id"
							size="small"
							dataSource={releases}
							pagination={false}
							columns={[
								{
									title: t('releases.buildColumn'),
									render: (_, record) => (
										<Typography.Text code>{record.buildId.slice(0, 8)}</Typography.Text>
									)
								},
								{
									title: t('releases.themeColumn'),
									render: (_, record) => `${record.theme.themeId} @ ${record.theme.version}`
								},
								{
									title: t('builds.status'),
									dataIndex: 'status',
									render: (status: string) => (
										<Tag color={status === 'live' ? 'green' : status === 'failed' ? 'red' : 'default'}>
											{t(`releases.status.${status}` as never)}
										</Tag>
									)
								},
								{
									title: t('releases.reasons'),
									render: (_, record) => record.reasons.join(', ')
								},
								{
									render: (_, record) => (
										<Space>
											{record.failure ? (
												<Typography.Text type="danger">{record.failure.stage}</Typography.Text>
											) : null}
											{editable && record.status !== 'live' && record.status !== 'failed' ? (
												<Button size="small" onClick={() => setRollbackTarget(record)}>
													{t('releases.rollback')}
												</Button>
											) : null}
										</Space>
									)
								}
							]}
						/>
					</>
				)}
			</Card>
			<Modal
				open={rollbackTarget !== null}
				title={t('releases.rollback')}
				onOk={() => void handleRollback()}
				onCancel={() => setRollbackTarget(null)}
				confirmLoading={rollingBack}
				okText={t('releases.rollback')}
			>
				<p>{t('releases.rollbackConfirm')}</p>
			</Modal>
		</>
	);
}
