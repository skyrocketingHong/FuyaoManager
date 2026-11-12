import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Modal, Select, Space, Table, Typography, message } from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import StatusTag from '../../components/StatusTag';
import { Panel, TablePanel } from '../../components/Panel';
import {
	canEditSite,
	createRelease,
	getCurrentRelease,
	getSite,
	getSiteEdge,
	listBuilds,
	listReleases,
	rollbackRelease,
	type BuildTask,
	type Release,
	type ReleaseCreate,
	type Site,
	type SiteEdgeState
} from '../../lib/api/management';
import { ApiError } from '../../lib/api/client';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

interface CurrentView {
	release: Release;
	originUrl: string;
}

/** Edge facts are explicit per-site state: loading and error never carry
 * facts, so the UI cannot borrow the previous site's deployable answer. */
type EdgeFacts =
	| { kind: 'loading' }
	| { kind: 'error' }
	| { kind: 'ready'; state: SiteEdgeState };

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
	const [edge, setEdge] = useState<EdgeFacts>({ kind: 'loading' });
	// The latest route param, readable from async handlers: a closure captured
	// before a site switch still holds the old siteId.
	const siteIdRef = useRef(siteId);
	// Mount/site generation: bumped on every site switch and on unmount.
	const generation = useRef(0);
	// Request epoch: late responses for a previous site (or after unmount)
	// can never overwrite the current state.
	const epoch = useRef(0);

	const refreshEdge = useCallback(async () => {
		if (!siteId) return;
		const myGen = generation.current;
		setEdge({ kind: 'loading' });
		try {
			const next = await getSiteEdge(siteId);
			if (siteIdRef.current !== siteId || myGen !== generation.current) return;
			setEdge({ kind: 'ready', state: next });
		} catch {
			if (siteIdRef.current !== siteId || myGen !== generation.current) return;
			setEdge({ kind: 'error' });
		}
	}, [siteId]);

	const refresh = useCallback(async () => {
		if (!siteId) return;
		// A refresh captures the generation at its start: a success path
		// invoked by an old handler must not bump the epoch with the previous
		// site's request and overwrite the current site's list.
		const myGen = generation.current;
		const my = ++epoch.current;
		try {
			const [releaseList, buildList] = await Promise.all([
				listReleases(siteId),
				listBuilds(siteId)
			]);
			if (my !== epoch.current || myGen !== generation.current) return;
			setReleases(releaseList);
			setSucceeded(buildList.filter((b) => b.status === 'succeeded'));
			try {
				const view = await getCurrentRelease(siteId);
				if (my !== epoch.current || myGen !== generation.current) return;
				setCurrent(view);
			} catch (err) {
				if (my !== epoch.current || myGen !== generation.current) return;
				if (err instanceof ApiError && err.code === 'no_live_release') {
					setCurrent(null);
				} else {
					throw err;
				}
			}
		} catch (err) {
			if (my !== epoch.current || myGen !== generation.current) return;
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
		generation.current++;
		setSite(null);
		setCurrent(null);
		setReleases(null);
		setSucceeded([]);
		setCandidate(null);
		setErrorKey(null);
		setRollbackTarget(null);
		setEdge({ kind: 'loading' });
		// In-flight action spinners belong to the previous site's rows.
		setPublishing(false);
		setRollingBack(false);
		siteIdRef.current = siteId;
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
		void refreshEdge();
		return () => {
			epoch.current++;
			generation.current++;
			cancelled = true;
		};
	}, [siteId, refresh, refreshEdge, navigate]);

	async function handlePublish() {
		// Act on the effective candidate: the user's explicit selection when
		// made, otherwise the newest unreleased succeeded build shown in the
		// selector. The action is always bound to a concrete build id.
		const target = effectiveCandidate;
		if (!siteId || target === null) return;
		const myGen = generation.current;
		setPublishing(true);
		try {
			const body: ReleaseCreate = { buildId: target, reasons: ['manual'] };
			await createRelease(siteId, body);
			if (generation.current !== myGen) return;
			await refresh();
		} catch (err) {
			if (generation.current !== myGen) return;
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
		const myGen = generation.current;
		setRollingBack(true);
		try {
			await rollbackRelease(siteId, rollbackTarget.id);
			setRollbackTarget(null);
			if (generation.current !== myGen) return;
			await refresh();
		} catch (err) {
			if (generation.current !== myGen) return;
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setRollingBack(false);
		}
	}

	const editable = site !== null && canEditSite(site);
	// Deployment facts: nothing known (loading, fetch failure) counts as
	// deployable — the isolation presentation is the conservative default.
	// Deployment gating: only an answer for the CURRENT site, explicitly
	// ready, with isolated===false opens the deployable surfaces — anything
	// else (loading, error, a mismatched site payload, an absent field) stays
	// conservative.
	const trusted =
		edge.kind === 'ready' && edge.state.site?.siteId === siteId && edge.state.isolated === false;
	const isolated = !trusted;
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

	const headerContent = editable && succeeded.length > 0 && (
		<Select
			id="release-candidate"
			style={{ minWidth: 220 }}
			value={effectiveCandidate}
			onChange={setCandidate}
			options={candidateOptions}
			aria-label={t('releases.candidate')}
		/>
	);
	const headerActions = (
		<Space size={8}>
			{!isolated && current && current.originUrl ? (
				<Button href={current.originUrl} target="_blank">
					<Trans text={t('releases.open')} />
				</Button>
			) : null}
			<Button
				type="primary"
				loading={publishing}
				disabled={!publishable || isolated}
				onClick={() => void handlePublish()}
			>
				<Trans text={t('releases.publish')} />
			</Button>
		</Space>
	);
	const headerActive = usePageHeader({ content: headerContent, actions: headerActions });

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>{headerActions}</div>
			) : null}
			<Panel title={<Trans text={t('releases.title')} />}>
				{errorKey !== null ? (
					<Alert
						type="error"
						showIcon
						title={t(errorKey ?? 'error.generic')}
						style={{ marginBottom: 16 }}
					/>
				) : null}
				{releases === null ? (
					<PageLoading spec="section" label={t('common.loading')} />
				) : (
					<>
						{current ? (
							<Alert
								type="success"
								showIcon
								title={`${t('releases.current')}: ${current.release.theme.themeId} @ ${current.release.theme.version}`}
								style={{ marginBottom: 16 }}
							/>
						) : (
							<Alert
								type="info"
								showIcon
								title={t('error.noLiveRelease')}
								style={{ marginBottom: 16 }}
							/>
						)}
						{edge.kind === 'loading' ? (
							<Typography.Text type="secondary" style={{ display: 'block', marginBottom: 16 }}>
								{t('common.loading')}
							</Typography.Text>
						) : edge.kind === 'error' ? (
							<Alert
								type="warning"
								showIcon
								title={t('edge.loadFailed')}
								action={
									<Button size="small" onClick={() => void refreshEdge()}>
										{t('common.retry')}
									</Button>
								}
								style={{ marginBottom: 16 }}
							/>
						) : edge.state.isolated ? (
							<Alert
								type="info"
								showIcon
								title={t('edge.isolatedNotice')}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{!editable ? (
							<Alert
								type="info"
								showIcon
								title={t('releases.roleNotice')}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{editable && succeeded.length === 0 ? (
							<Alert
								type="info"
								showIcon
								title={t('releases.noSucceededBuild')}
								style={{ marginBottom: 16 }}
							/>
						) : null}
						{!headerActive && editable && succeeded.length > 0 ? (
							<Space orientation="vertical" style={{ width: '100%', marginBottom: 16 }}>
								<label htmlFor="release-candidate">{t('releases.candidate')}</label>
								{headerContent}
							</Space>
						) : null}
						<TablePanel>
						<Table<Release>
							rowKey="id"
							dataSource={releases}
							pagination={false}
							columns={[
								{
									title: t('releases.buildColumn'),
									render: (_, record) => (
										<Typography.Text className="version-text">{record.buildId.slice(0, 8)}</Typography.Text>
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
										<StatusTag
											kind={status === 'live' ? 'success' : status === 'failed' ? 'danger' : 'neutral'}
										>
											{t(`releases.status.${status}` as never)}
										</StatusTag>
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
												<Button
													size="small"
													disabled={isolated}
													onClick={() => setRollbackTarget(record)}
												>
													{t('releases.rollback')}
												</Button>
											) : null}
										</Space>
									)
								}
							]}
						/>
						</TablePanel>
					</>
				)}
			</Panel>
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
		</div>
	);
}
