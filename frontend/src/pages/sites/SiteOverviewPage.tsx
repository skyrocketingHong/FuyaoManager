import { useCallback } from 'react';
import { Alert, Space, Typography } from 'antd';
import { Link, useParams } from 'react-router-dom';
import { useCurrentSite } from '../../components/layout/SiteBoundary';
import { Panel } from '../../components/Panel';
import PageLoading from '../../components/PageLoading';
import { useAdminQuery } from '../../lib/admin/requests';
import { getSiteOverview, type SiteOverview } from '../../lib/api/content';
import { allowSitePage, siteWorkspace } from '../../lib/navigation/workspace';
import { t } from '../../lib/i18n';
import SiteDetailPage from './SiteDetailPage';
import ContentStatistics from '../../components/ContentStatistics';
import CaddySitePage from './CaddySitePage';
import StatusTag from '../../components/StatusTag';
import { getCurrentRelease } from '../../lib/api/management';
import { ApiError } from '../../lib/api/client';
import { RetryButton } from '../../components/ActionButton';

type Analytics = SiteOverview['analytics'];
function TrafficStatistics({
	analytics,
	loading,
	reload
}: {
	analytics: Analytics;
	loading: boolean;
	reload: () => void;
}) {
	if (analytics.configured !== true) return null;
	const latest = analytics.available === true ? analytics.latest : null;
	return (
		<Panel
			title={t('overview.traffic')}
			tags={
				latest && (
					<Space size={8}>
						<span>{latest.date}</span>
						{analytics.cache === 'stale' && (
							<StatusTag kind="neutral">{t('overview.cachedSnapshot')}</StatusTag>
						)}
					</Space>
				)
			}
		>
			{latest ? (
				<dl className="fact-grid">
					<div>
						<dt>{t('overview.pageViews')}</dt>
						<dd>{latest.pageViews}</dd>
					</div>
					<div>
						<dt>{t('overview.requests')}</dt>
						<dd>{latest.requests}</dd>
					</div>
					<div>
						<dt>{t('overview.uniques')}</dt>
						<dd>{latest.uniques}</dd>
					</div>
				</dl>
			) : (
				<Alert
					type="info"
					showIcon
					title={t('overview.trafficUnavailable')}
					action={
						<RetryButton loading={loading} onClick={reload} />
					}
				/>
			)}
		</Panel>
	);
}

function CaddyCurrentRelease({ siteId }: { siteId: string }) {
	const load = useCallback(async () => {
		try {
			const current = await getCurrentRelease(siteId);
			if (
				!current.release ||
				current.release.siteId !== siteId ||
				current.release.status !== 'live'
			)
				throw new Error('Unexpected current release');
			return current;
		} catch (error) {
			if (error instanceof ApiError && error.status === 404 && error.code === 'no_live_release')
				return null;
			throw error;
		}
	}, [siteId]);
	const query = useAdminQuery(load);
	const current = query.data;
	let safeOrigin = false;
	if (current?.originUrl) {
		try {
			safeOrigin = ['http:', 'https:'].includes(new URL(current.originUrl).protocol);
		} catch {
			/* Render an invalid origin as text. */
		}
	}
	return (
		<Panel title={t('overview.currentRelease')}>
			<Space orientation="vertical">
				{query.loading ? (
					<PageLoading spec="section" />
				) : query.error ? (
					<Alert
						type="error"
						showIcon
						title={t(query.error)}
						action={<RetryButton onClick={query.reload} />}
					/>
				) : current ? (
					<Space wrap>
						<StatusTag kind="success">{t('overview.platformPublished')}</StatusTag>
						<code>
							{current.release.theme.themeId} @ {current.release.theme.version}
						</code>
						{current.originUrl &&
							(safeOrigin ? (
								<a href={current.originUrl} target="_blank" rel="noreferrer">
									{current.originUrl}
								</a>
							) : (
								<Typography.Text>{current.originUrl}</Typography.Text>
							))}
					</Space>
				) : (
					<Typography.Text>{t('overview.caddyNotPublished')}</Typography.Text>
				)}
				<Link to={`/sites/${siteId}/caddy`}>{t('nav.caddy')}</Link>
			</Space>
		</Panel>
	);
}

export default function SiteOverviewPage() {
	const { siteId = '' } = useParams();
	const site = useCurrentSite();
	const workspace = site ? siteWorkspace(site) : null;
	const hasCaddy = site?.workspace?.hasCaddy === true;
	const hasContent = !!workspace && (workspace.themeEnabled || workspace.themeSelectable);
	const load = useCallback(
		(signal: AbortSignal) => (hasContent ? getSiteOverview(siteId, signal) : Promise.resolve(null)),
		[siteId, hasContent]
	);
	const query = useAdminQuery(load);
	const overview = query.data;
	if (!hasContent) return <CaddySitePage />;
	return (
		<div className="page-stack">
			{query.error && (
				<Alert
					type="error"
					title={t(query.error)}
					showIcon
					action={<RetryButton onClick={query.reload} />}
				/>
			)}
			<Panel title={t('overview.content')}>
				{!overview ? (
					query.loading ? (
						<PageLoading />
					) : null
				) : (
					<ContentStatistics counts={overview.counts} />
				)}
			</Panel>
			{overview && (
				<TrafficStatistics
					analytics={overview.analytics}
					loading={query.loading}
					reload={query.reload}
				/>
			)}
			{overview && (
				<Panel title={t('overview.themePanel')}>
					<Space wrap>
						{overview.theme.selected && overview.theme.theme ? (
							<code>
								{overview.theme.theme.themeId} @ {overview.theme.theme.version}
							</code>
						) : (
							t('themes.unselected')
						)}
						{allowSitePage(site, 'site.theme') && (
							<Link to={`/sites/${siteId}/theme`}>{t('nav.themeSelection')}</Link>
						)}
						{allowSitePage(site, 'site.sources') && (
							<Link to={`/sites/${siteId}/content-sources`}>{t('nav.contentSources')}</Link>
						)}
						{allowSitePage(site, 'site.builds') && (
							<Link to={`/sites/${siteId}/builds`}>{t('nav.builds')}</Link>
						)}
						{allowSitePage(site, 'site.releases') && (
							<Link to={`/sites/${siteId}/releases`}>{t('nav.releases')}</Link>
						)}
					</Space>
				</Panel>
			)}
			{hasCaddy ? <CaddyCurrentRelease key={siteId} siteId={siteId} /> : <SiteDetailPage />}
		</div>
	);
}
