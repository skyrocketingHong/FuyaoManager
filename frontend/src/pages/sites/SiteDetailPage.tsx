/**
 * SiteDetailPage: the site overview — the real provisioning/visibility/edge
 * state, the current live release, and the entry points into content,
 * configuration, and builds. No invented metrics.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Space, Switch, Typography, message } from 'antd';
import { SiteDetail } from '../../lib/sites/site-detail';
import {
	canEditSite,
	getCurrentRelease,
	getSiteEdge,
	provisionSite,
	putSiteVisibility,
	reapplySiteEdge,
	type Release,
	type SiteEdgeState
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';
import { upsertSite } from '../../lib/sites/site-list';
import PageLoading from '../../components/PageLoading';
import StatusTag from '../../components/StatusTag';
import { Panel } from '../../components/Panel';
import { useCurrentSite } from '../../components/layout/SiteBoundary';

type ReleaseState =
	| { kind: 'loading' }
	| { kind: 'none' }
	| { kind: 'live'; release: Release; originUrl: string }
	| { kind: 'error' };

/** Edge facts are explicit per-site state: loading and error never carry
 * facts, so the UI cannot borrow the previous site's deployable answer. */
type EdgeFacts =
	| { kind: 'loading' }
	| { kind: 'error' }
	| { kind: 'ready'; state: SiteEdgeState };

export default function SiteDetailPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const boundarySite = useCurrentSite();
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const [messageApi, contextHolder] = message.useMessage();
	const [switching, setSwitching] = useState(false);
	const [edge, setEdge] = useState<EdgeFacts>({ kind: 'loading' });
	const [reapplying, setReapplying] = useState(false);
	const [release, setRelease] = useState<ReleaseState>({ kind: 'loading' });
	// A fresh instance per siteId keeps data and error state from surviving a
	// switch between sites.
	const detail = useMemo(() => new SiteDetail(), [siteId]);
	// The latest route param, readable from async handlers: a closure captured
	// before a site switch still holds the old siteId.
	const siteIdRef = useRef(siteId);
	siteIdRef.current = siteId;
	// Mount/site generation: bumped on every site switch and on unmount. A
	// response applies only while the generation still matches, so a stale
	// answer can never overwrite the current site's facts.
	const generation = useRef(0);

	useEffect(() => {
		generation.current++;
		void detail.load(siteId ?? '', async () => {});
		return () => {
			generation.current++;
		};
	}, [detail, siteId]);

	const state = useSyncExternalStore(detail.subscribe, detail.getSnapshot);
	const site = state.site ?? boundarySite;

	// Deployment gating: only an explicit ready answer with isolated===false
	// opens the deployable surfaces; loading and error stay conservative.
	const edgeReady = edge.kind === 'ready';
	const isolated = !edgeReady || edge.state.isolated === true;
	const edgeEnabled = edgeReady && edge.state.enabled === true;

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

	const refreshRelease = useCallback(async () => {
		if (!siteId) return;
		const myGen = generation.current;
		setRelease({ kind: 'loading' });
		try {
			const current = await getCurrentRelease(siteId);
			if (siteIdRef.current !== siteId || myGen !== generation.current) return;
			setRelease(
				current.release
					? { kind: 'live', release: current.release, originUrl: current.originUrl }
					: { kind: 'none' }
			);
		} catch (err) {
			if (siteIdRef.current !== siteId || myGen !== generation.current) return;
			if (apiErrorKey(err) === 'error.noLiveRelease') {
				setRelease({ kind: 'none' });
			} else {
				setRelease({ kind: 'error' });
			}
		}
	}, [siteId]);

	useEffect(() => {
		// A site switch starts from a clean slate: no facts from the previous
		// site survive into the loading state. The site record itself is not
		// a trigger — the edge and release requests depend on the siteId only.
		setEdge({ kind: 'loading' });
		setRelease({ kind: 'loading' });
		void refreshEdge();
		void refreshRelease();
	}, [refreshEdge, refreshRelease]);

	async function handleReapply() {
		if (!siteId) return;
		setReapplying(true);
		try {
			await reapplySiteEdge(siteId);
			void messageApi.success(t('edge.applied'));
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			await refreshEdge();
			setReapplying(false);
		}
	}

	async function handleVisibility(publicOn: boolean) {
		if (!siteId || !site) return;
		setSwitching(true);
		try {
			const updated = await putSiteVisibility(siteId, publicOn ? 'public' : 'private');
			upsertSite(updated);
			await detail.load(siteId, async () => {});
			await refreshEdge();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setSwitching(false);
		}
	}

	async function handleRetryProvision() {
		if (!siteId) return;
		try {
			const refreshed = await provisionSite(siteId);
			upsertSite(refreshed);
			await detail.load(siteId, async () => {});
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	if (!site) {
		return state.errorKey ? (
			<Alert type="error" showIcon title={t(state.errorKey as never)} />
		) : (
			<PageLoading spec="page" label={t('common.loading')} />
		);
	}

	return (
		<div className="page-stack">
			{contextHolder}
			<Panel title={t('overview.stateTitle')}>
				<dl className="fact-grid">
					<div>
						<dt>{t('site.slug')}</dt>
						<dd className="version-text">{site.slug}</dd>
					</div>
					<div>
						<dt>{t('site.provisionState')}</dt>
						<dd>
							<span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
								<StatusTag
									kind={
										site.provisionState === 'ready'
											? 'success'
											: site.provisionState === 'failed'
												? 'danger'
												: 'info'
									}
								>
									{t(`site.provision.${site.provisionState}` as never)}
								</StatusTag>
								{site.provisionState === 'failed' ? (
									<Button size="small" onClick={() => void handleRetryProvision()}>
										{t('sites.retryProvision')}
									</Button>
								) : null}
							</span>
						</dd>
					</div>
					<div>
						<dt>{t('site.visibility')}</dt>
						<dd>
							<Switch
								checked={site.visibility === 'public'}
								onChange={(checked) => void handleVisibility(checked)}
								loading={switching}
								disabled={!canEditSite(site) || isolated}
								checkedChildren={t('site.visibility.public')}
								unCheckedChildren={t('site.visibility.private')}
							/>
						</dd>
					</div>
					<div>
						<dt>{t('site.role')}</dt>
						<dd>
							<StatusTag kind="neutral">{t(`role.${site.role}` as never)}</StatusTag>
						</dd>
					</div>
				</dl>
			</Panel>

			<Panel title={t('edge.title')}>
				{edge.kind === 'loading' ? (
					<Typography.Text type="secondary">{t('common.loading')}</Typography.Text>
				) : edge.kind === 'error' ? (
					<Space wrap>
						<Typography.Text type="danger">{t('edge.loadFailed')}</Typography.Text>
						<Button size="small" onClick={() => void refreshEdge()}>
							{t('common.retry')}
						</Button>
					</Space>
				) : edge.state.isolated ? (
					<Space orientation="vertical" style={{ width: '100%' }}>
						<Typography.Text type="secondary">{t('edge.isolatedNotice')}</Typography.Text>
						{edge.state.site?.port ? (
							<Space wrap>
								{/* Reservation, not a running origin: rendered as plain
								    text and gated by currentOk, which only says the
								    on-disk pointer matches the live release. */}
								<Typography.Text className="version-text">
									{edge.state.site.originUrl}
								</Typography.Text>
								<StatusTag kind={edge.state.site.currentOk ? 'success' : 'neutral'}>
									{edge.state.site.currentOk ? t('edge.pointerMatch') : t('edge.pointerMismatch')}
								</StatusTag>
							</Space>
						) : (
							<Typography.Text type="secondary">{t('edge.pendingEntry')}</Typography.Text>
						)}
					</Space>
				) : edge.state.site?.port ? (
					<Space wrap>
						<a href={edge.state.site.originUrl} target="_blank" rel="noreferrer" className="version-text">
							{edge.state.site.originUrl}
						</a>
						<StatusTag kind={edge.state.inSync ? 'success' : 'warning'}>
							{edge.state.inSync ? t('edge.inSync') : t('edge.drifted')}
						</StatusTag>
						<StatusTag kind={edge.state.site.currentOk ? 'success' : 'neutral'}>
							{edge.state.site.currentOk ? t('edge.pointerMatch') : t('edge.pointerMismatch')}
						</StatusTag>
						{!edgeEnabled ? (
							<Typography.Text type="secondary">{t('edge.unconfigured')}</Typography.Text>
						) : null}
						{canEditSite(site) ? (
							<Button
								size="small"
								loading={reapplying}
								disabled={!edgeEnabled}
								onClick={() => void handleReapply()}
							>
								{t('edge.reapply')}
							</Button>
						) : null}
						{edge.state.lastError ? (
							<Typography.Text type="danger" style={{ fontSize: 12 }}>
								{edge.state.lastError}
							</Typography.Text>
						) : null}
					</Space>
				) : (
					<Typography.Text type="secondary">{t('edge.pendingEntry')}</Typography.Text>
				)}
			</Panel>

			<Panel title={t('overview.currentRelease')}>
				{release.kind === 'loading' ? (
					<PageLoading spec="section" />
				) : release.kind === 'none' ? (
					<Typography.Text type="secondary">{t('overview.noRelease')}</Typography.Text>
				) : release.kind === 'error' ? (
					<Alert type="error" showIcon title={t('error.generic')} />
				) : (
					<Space wrap>
						<StatusTag kind="success">{t('overview.live')}</StatusTag>
						<span className="version-text">
							{release.release.theme.themeId} @ {release.release.theme.version}
						</span>
						{isolated ? (
							<Typography.Text className="version-text">{release.originUrl}</Typography.Text>
						) : (
							<a href={release.originUrl} target="_blank" rel="noreferrer" className="version-text">
								{release.originUrl}
							</a>
						)}
					</Space>
				)}
			</Panel>

			<Panel title={t('overview.actionsTitle')}>
				<Space wrap>
					<Link to={`/sites/${site.siteId}/posts`}>
						<Button>{t('nav.posts')}</Button>
					</Link>
					<Link to={`/sites/${site.siteId}/config`}>
						<Button>{t('nav.config')}</Button>
					</Link>
					<Link to={`/sites/${site.siteId}/theme`}>
						<Button>{t('nav.theme')}</Button>
					</Link>
					<Link to={`/sites/${site.siteId}/builds`}>
						<Button>{t('nav.builds')}</Button>
					</Link>
					<Link to={`/sites/${site.siteId}/releases`}>
						<Button>{t('nav.releases')}</Button>
					</Link>
				</Space>
			</Panel>

			<dl className="fact-grid" style={{ padding: '0 4px' }}>
				<div>
					<dt>{t('site.updatedAt')}</dt>
					<dd>{new Date(site.updatedAt).toLocaleString(locale)}</dd>
				</div>
				<div>
					<dt>{t('site.createdAt')}</dt>
					<dd>{new Date(site.createdAt).toLocaleString(locale)}</dd>
				</div>
			</dl>
		</div>
	);
}
