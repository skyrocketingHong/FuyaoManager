import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	Button,
	Card,
	Modal,
	Select,
	Space,
	Spin,
	Table,
	Tag,
	Typography,
	message
} from 'antd';
import Trans from '../../components/i18n/Trans';
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

/** A publish 422 answering application/json carries a failed release record. */
function failedReleaseOf(err: unknown): Release | null {
	if (err instanceof ApiError && err.body && typeof err.body === 'object') {
		const body = err.body as Partial<Release>;
		if (body.status === 'failed' && body.failure) return body as Release;
	}
	return null;
}

export default function ReleasesPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [current, setCurrent] = useState<CurrentView | null>(null);
	const [releases, setReleases] = useState<Release[] | null>(null);
	const [succeeded, setSucceeded] = useState<BuildTask[]>([]);
	const [candidate, setCandidate] = useState<string | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [publishing, setPublishing] = useState(false);
	const [rollbackTarget, setRollbackTarget] = useState<Release | null>(null);
	const [rollingBack, setRollingBack] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();
	// Request epoch: late responses for a previous site (or after unmount)
	// can never overwrite the current state.
	const epoch = useRef(0);

	const refresh = useCallback(async () => {
		if (!siteId) return;
		const my = ++epoch.current;
		try {
			const [releaseList, buildList] = await Promise.all([
				listReleases(siteId),
				listBuilds(siteId)
			]);
			if (my !== epoch.current) return;
			setReleases(releaseList);
			setSucceeded(buildList.filter((b) => b.status === 'succeeded'));
			try {
				const view = await getCurrentRelease(siteId);
				if (my !== epoch.current) return;
				setCurrent(view);
			} catch (err) {
				if (my !== epoch.current) return;
				if (err instanceof ApiError && err.code === 'no_live_release') {
					setCurrent(null);
				} else {
					throw err;
				}
			}
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

	// Site change (same component instance, different route param): reset all
	// state — including the candidate and the rollback dialog — before
	// loading, so nothing from the previous site survives.
	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setCurrent(null);
		setReleases(null);
		setSucceeded([]);
		setCandidate(null);
		setErrorKey(null);
		setRollbackTarget(null);
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
			epoch.current++;
			cancelled = true;
		};
	}, [siteId, refresh, navigate]);

	async function handlePublish() {
		// Act on the effective candidate: the user's explicit selection when
		// made, otherwise the newest unreleased succeeded build shown in the
		// selector. The action is always bound to a concrete build id.
		const target = effectiveCandidate;
		if (!siteId || target === null) return;
		setPublishing(true);
		try {
			const body: ReleaseCreate = { buildId: target, reasons: ['manual'] };
			await createRelease(siteId, body);
			await refresh();
		} catch (err) {
			// A persisted failure record answers application/json with the
			// failed release; surface its structured detail and refresh the
			// history so the attempt is visible.
			const failed = failedReleaseOf(err);
			if (failed && failed.failure) {
				const missing = [
					...(failed.failure.missingArtifacts ?? []),
					...(failed.failure.missingCapabilities ?? []),
					...(failed.failure.missingUrlConventions ?? [])
				].join(', ');
				void messageApi.error(
					`${t('releases.status.failed')}: ${failed.failure.stage}${missing ? ` (${missing})` : ''}`
				);
			} else {
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') {
					navigate('/login');
					setPublishing(false);
					return;
				}
				// Unknown errors still get an explicit fallback message.
				void messageApi.error(key !== null ? t(key) : t('error.generic'));
			}
			await refresh();
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
	// Only releases that actually back content block a build from being
	// published again; failed attempts do not.
	const releasedBuildIds = new Set(
		(releases ?? []).filter((r) => r.status !== 'failed').map((r) => r.buildId)
	);
	const candidateOptions = succeeded.map((build) => ({
		value: build.id,
		label: releasedBuildIds.has(build.id)
			? `${build.id.slice(0, 8)} · ${t('releases.candidatePublished')}`
			: build.id.slice(0, 8)
	}));
	// Default the candidate to the newest succeeded build that has not been
	// released yet, without silently publishing anything.
	let effectiveCandidate: string | null =
		candidate !== null && succeeded.some((b) => b.id === candidate)
			? candidate
			: (succeeded.find((b) => !releasedBuildIds.has(b.id))?.id ?? null);
	const publishable =
		editable && effectiveCandidate !== null && !releasedBuildIds.has(effectiveCandidate);

	return (
		<>
			{contextHolder}
			<Card
				title={<Trans text={t('releases.title')} />}
				extra={
					<Space>
						{current && current.originUrl ? (
							<Button href={current.originUrl} target="_blank">
								<Trans text={t('releases.open')} />
							</Button>
						) : null}
						<Button
							type="primary"
							loading={publishing}
							disabled={!publishable}
							onClick={() => void handlePublish()}
						>
							<Trans text={t('releases.publish')} />
						</Button>
					</Space>
				}
				style={{ marginTop: 16 }}
			>
				{errorKey !== null ? (
					<Alert
						type="error"
						showIcon
						message={t(errorKey ?? 'error.generic')}
						style={{ marginBottom: 16 }}
					/>
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
							<Alert
								type="info"
								showIcon
								message={t('error.noLiveRelease')}
								style={{ marginBottom: 16 }}
							/>
						)}
						{!editable ? (
							<Alert
								type="info"
								showIcon
								message={t('releases.roleNotice')}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{editable && succeeded.length === 0 ? (
							<Alert
								type="info"
								showIcon
								message={t('releases.noSucceededBuild')}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{editable && succeeded.length > 0 ? (
							<Space direction="vertical" style={{ width: '100%', marginBottom: 16 }}>
								<label htmlFor="release-candidate">{t('releases.candidate')}</label>
								<Select
									id="release-candidate"
									style={{ maxWidth: 360 }}
									value={effectiveCandidate}
									onChange={setCandidate}
									options={candidateOptions}
									aria-label={t('releases.candidate')}
								/>
							</Space>
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
										<Tag
											color={status === 'live' ? 'green' : status === 'failed' ? 'red' : 'default'}
										>
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
