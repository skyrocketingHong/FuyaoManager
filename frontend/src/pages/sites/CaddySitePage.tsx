import { useCallback } from 'react';
import { Alert, Button, Descriptions, Space, Typography } from 'antd';
import { useParams } from 'react-router-dom';
import { Panel } from '../../components/Panel';
import { useCurrentSite } from '../../components/layout/SiteBoundary';
import { usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import { api } from '../../lib/api/client';
import { useAdminQuery } from '../../lib/admin/requests';
import { t } from '../../lib/i18n';

type Config = {
	siteId: string;
	kind: string;
	present: boolean;
	syncedAt: string;
	definition: {
		hosts?: string[];
		listen?: string[];
		root?: string;
		readOnly?: boolean;
		reason?: string;
		redirect?: { location?: string; statusCode?: number };
	};
};
export default function CaddySitePage() {
	const { siteId } = useParams();
	const site = useCurrentSite();
	const load = useCallback(
		async (signal: AbortSignal) =>
			(await api<Config>('GET', `/api/v1/sites/${siteId}/caddy`, { signal })).data,
		[siteId]
	);
	const query = useAdminQuery(load);
	const actions = (
		<Button onClick={query.reload} disabled={query.loading}>
			{t('admin.refresh')}
		</Button>
	);
	const inHeader = usePageHeader({ actions });
	const config = query.data;
	const definition = config?.definition;
	const target = definition?.redirect?.location;
	const targetBase = target?.split('{')[0];
	return (
		<div className="page-stack">
			{!inHeader && actions}
			{query.error && (
				<Alert
					type="error"
					showIcon
					title={t(query.error)}
					action={<Button onClick={query.reload}>{t('common.retry')}</Button>}
				/>
			)}
			{!config && query.loading && <PageLoading label={t('common.loading')} />}
			{config && (
				<Panel title={site?.name}>
					{!config.present && <Alert type="warning" title={t('caddy.missing')} />}
					<Descriptions
						column={1}
						items={[
							{
								key: 'kind',
								label: t('caddy.kind'),
								children: t(
									config.kind === 'redirect'
										? 'caddy.redirect'
										: config.kind === 'static'
											? 'caddy.static'
											: config.kind === 'reverse_proxy'
												? 'caddy.proxy'
												: 'caddy.complex'
								)
							},
							{
								key: 'hosts',
								label: t('caddy.hosts'),
								children: (
									<Space orientation="vertical">
										{definition?.hosts?.map((host) => (
											<Typography.Text key={host} copyable>
												{host}
											</Typography.Text>
										))}
									</Space>
								)
							},
							{
								key: 'listen',
								label: t('caddy.listen'),
								children: <code>{definition?.listen?.join(', ')}</code>
							},
							...(definition?.root
								? [
										{
											key: 'root',
											label: t('caddy.root'),
											children: <Typography.Text copyable>{definition.root}</Typography.Text>
										}
									]
								: []),
							...(target
								? [
										{
											key: 'redirect',
											label: t('caddy.target'),
											children: (
												<Space orientation="vertical">
													<code>{target}</code>
													{targetBase && /^https?:\/\//.test(targetBase) && (
														<a href={targetBase} target="_blank" rel="noreferrer">
															{t('caddy.openTarget')}
														</a>
													)}
												</Space>
											)
										},
										{ key: 'status', label: 'HTTP', children: definition?.redirect?.statusCode }
									]
								: []),
							{ key: 'source', label: t('caddy.source'), children: t('caddy.sourceValue') },
							{ key: 'mode', label: t('caddy.control'), children: t('caddy.protected') },
							{
								key: 'sync',
								label: t('caddy.synced'),
								children: new Date(config.syncedAt).toLocaleString()
							}
						]}
					/>
				</Panel>
			)}
		</div>
	);
}
