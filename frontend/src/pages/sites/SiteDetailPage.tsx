import { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Card, Descriptions, Spin, Tag } from 'antd';
import { SiteDetail } from '../../lib/sites/site-detail';
import { i18nSnapshot, subscribeI18n, t } from '../../lib/i18n';

export default function SiteDetailPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const { locale } = useSyncExternalStore(subscribeI18n, i18nSnapshot);
	// A fresh instance per siteId keeps data and error state from surviving a
	// switch between sites.
	const detail = useMemo(() => new SiteDetail(), [siteId]);

	useEffect(() => {
		void detail.load(siteId ?? '', async () => {
			navigate('/login');
		});
	}, [detail, siteId, navigate]);

	const state = useSyncExternalStore(detail.subscribe, detail.getSnapshot);

	return (
		<>
			{state.site ? (
				<Card title={state.site.name} style={{ marginTop: 16 }}>
					<Descriptions column={1} bordered size="small">
						<Descriptions.Item label={t('site.slug')}>{state.site.slug}</Descriptions.Item>
						<Descriptions.Item label={t('site.role')}>
							<Tag>{t(`role.${state.site.role}` as never)}</Tag>
						</Descriptions.Item>
						<Descriptions.Item label={t('site.locale')}>
							{state.site.defaultLocale}
						</Descriptions.Item>
						<Descriptions.Item label={t('site.revision')}>
							{state.site.settingsRevision}
						</Descriptions.Item>
						<Descriptions.Item label={t('site.updatedAt')}>
							{new Date(state.site.updatedAt).toLocaleString(locale)}
						</Descriptions.Item>
						<Descriptions.Item label={t('site.createdAt')}>
							{new Date(state.site.createdAt).toLocaleString(locale)}
						</Descriptions.Item>
					</Descriptions>
				</Card>
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
