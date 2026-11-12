import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Descriptions, Space, Spin, Switch, Tag, Typography, message } from 'antd';
import { SiteDetail } from '../../lib/sites/site-detail';
import {
	canEditSite,
	getSiteEdge,
	provisionSite,
	putSiteVisibility,
	reapplySiteEdge,
	type SiteEdgeState
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';
import { upsertSite } from '../../lib/sites/site-list';

export default function SiteDetailPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	const [messageApi, contextHolder] = message.useMessage();
	const [switching, setSwitching] = useState(false);
	const [edge, setEdge] = useState<SiteEdgeState | null>(null);
	const [reapplying, setReapplying] = useState(false);
	// A fresh instance per siteId keeps data and error state from surviving a
	// switch between sites.
	const detail = useMemo(() => new SiteDetail(), [siteId]);

	useEffect(() => {
		void detail.load(siteId ?? '', async () => {
			navigate('/login');
		});
	}, [detail, siteId, navigate]);

	const state = useSyncExternalStore(detail.subscribe, detail.getSnapshot);
	const site = state.site;

	// Visibility gates the site's entry binding: private answers only on the
	// private network, public on all interfaces. The switch re-applies the
	// managed edge configuration server-side.
	const refreshEdge = useCallback(async () => {
		if (!siteId) return;
		try {
			setEdge(await getSiteEdge(siteId));
		} catch {
			// surfaced on the next action
		}
	}, [siteId]);

	useEffect(() => {
		void refreshEdge();
	}, [refreshEdge, state.site]);

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

	return (
		<>
			{contextHolder}
			{site ? (
				<Descriptions column={1} bordered size="small">
					<Descriptions.Item label={t('site.slug')}>{site.slug}</Descriptions.Item>
					<Descriptions.Item label={t('edge.title')}>
						{edge?.site?.port ? (
							<Space wrap>
								<a href={edge.site.originUrl} target="_blank" rel="noreferrer">
									{edge.site.originUrl}
								</a>
								<Tag color={edge.inSync ? 'success' : 'warning'}>
									{edge.inSync ? t('edge.inSync') : t('edge.drifted')}
								</Tag>
								{edge.lastError ? (
									<Typography.Text type="danger" style={{ fontSize: 12 }}>
										{edge.lastError}
									</Typography.Text>
								) : null}
								{canEditSite(site) ? (
									<Button size="small" loading={reapplying} onClick={() => void handleReapply()}>
										{t('edge.reapply')}
									</Button>
								) : null}
							</Space>
						) : (
							<Typography.Text type="secondary">{t('edge.pendingEntry')}</Typography.Text>
						)}
					</Descriptions.Item>
					<Descriptions.Item label={t('site.role')}>
						<Tag>{t(`role.${site.role}` as never)}</Tag>
					</Descriptions.Item>
					<Descriptions.Item label={t('site.visibility')}>
						<Switch
							checked={site.visibility === 'public'}
							onChange={(checked) => void handleVisibility(checked)}
							loading={switching}
							disabled={!canEditSite(site)}
							checkedChildren={t('site.visibility.public')}
							unCheckedChildren={t('site.visibility.private')}
						/>
					</Descriptions.Item>
					<Descriptions.Item label={t('site.provisionState')}>
						{site.provisionState === 'failed' ? (
							<Space>
								<Tag color="error">{t('site.provision.failed')}</Tag>
								<Button size="small" onClick={() => void handleRetryProvision()}>
									{t('sites.retryProvision')}
								</Button>
							</Space>
						) : (
							<Tag color={site.provisionState === 'ready' ? 'success' : 'processing'}>
								{t(`site.provision.${site.provisionState}` as never)}
							</Tag>
						)}
					</Descriptions.Item>
					<Descriptions.Item label={t('site.locale')}>{site.defaultLocale}</Descriptions.Item>
					<Descriptions.Item label={t('site.revision')}>{site.settingsRevision}</Descriptions.Item>
					<Descriptions.Item label={t('site.updatedAt')}>
						{new Date(site.updatedAt).toLocaleString(locale)}
					</Descriptions.Item>
					<Descriptions.Item label={t('site.createdAt')}>
						{new Date(site.createdAt).toLocaleString(locale)}
					</Descriptions.Item>
				</Descriptions>
			) : state.errorKey ? (
				<Alert
					type="error"
					showIcon
					message={t(state.errorKey as never)}
					style={{ marginTop: 16 }}
				/>
			) : (
				<p role="status" style={{ marginTop: 16 }}>
					<Spin /> {t('common.loading')}
				</p>
			)}
		</>
	);
}
