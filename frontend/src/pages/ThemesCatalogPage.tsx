import { useCallback } from 'react';
import ThemeSourcesTable from '../features/themes/ThemeSourcesTable';
import { PageTabs, usePageHeader } from '../components/PageHeader';
import { queryChoice, usePageQuery } from '../lib/navigation/query';
import { useAdminQuery } from '../lib/admin/requests';
import { Alert, Empty, Tooltip, Typography } from 'antd';
import DataTable from '../components/DataTable';
import type { ColumnsType } from 'antd/es/table';
import { listThemes, type Theme } from '../lib/api/management';
import { t } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import { useSyncExternalStore } from 'react';
import StatusTag from '../components/StatusTag';
import { TablePanel } from '../components/Panel';
import { RetryButton } from '../components/ActionButton';

export default function ThemesCatalogPage() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [params, setParams] = usePageQuery();
	const authorized = session.user?.platformRole === 'admin';
	const tab = authorized ? queryChoice(params, 'tab', ['catalog', 'github'], 'catalog') : 'catalog';
	const catalog = useAdminQuery(useCallback(() => listThemes(), []));
	const tabs = authorized && (
		<PageTabs
			value={tab}
			ariaLabel={t('nav.themesCatalog')}
			options={[
				{ value: 'catalog', label: t('themeSources.catalog') },
				{ value: 'github', label: t('themeSources.tab') }
			]}
			onChange={(value) => setParams({ tab: value === 'catalog' ? null : value })}
		/>
	);
	const inHeader = usePageHeader({ content: tabs });
	if (tab === 'github')
		return (
			<div className="page-stack page-stack--fill">
				{!inHeader && tabs}
				<ThemeSourcesTable onInstalled={catalog.reload} />
			</div>
		);
	const columns: ColumnsType<Theme> = [
		{
			title: t('themes.themeColumn'),
			sorter: (a, b) => (a.name ?? a.themeId).localeCompare(b.name ?? b.themeId),
			render: (_, theme) => (
				<>
					<div>{theme.name ?? theme.themeId}</div>
					<Typography.Text className="version-text text-caption" type="secondary">
						{theme.themeId} @ {theme.version}
					</Typography.Text>
				</>
			)
		},
		{
			title: t('themes.adapterColumn'),
			dataIndex: 'adapter',
			width: 200,
			render: (adapter: string) => <span className="version-text">{adapter}</span>
		},
		{
			title: t('themes.capabilitiesColumn'),
			dataIndex: 'capabilities',
			render: (caps: string[]) => (
				<span style={{ display: 'inline-flex', flexWrap: 'wrap', gap: 4 }}>
					{(caps ?? []).map((cap) => (
						<StatusTag key={cap} kind="neutral">
							{cap}
						</StatusTag>
					))}
				</span>
			)
		},
		{
			title: t('themes.requirementsColumn'),
			render: (_, theme) => (
				<Typography.Text type="secondary" className="text-caption">
					{(theme.outputs?.urlConventions ?? []).join(', ') || '—'}
					{(theme.outputs?.requiredArtifacts ?? []).length > 0
						? ` · ${t('themes.artifacts')}: ${(theme.outputs?.requiredArtifacts ?? []).join(', ')}`
						: ''}
				</Typography.Text>
			)
		},
		{
			title: t('themes.configColumn'),
			sorter: (a, b) => Number(Boolean(a.configSchema)) - Number(Boolean(b.configSchema)),
			width: 120,
			render: (_, theme) =>
				theme.configSchema ? (
					<StatusTag kind="success">{t('themes.schemaDeclared')}</StatusTag>
				) : (
					<StatusTag kind="neutral">{t('themes.emptyConfigOnly')}</StatusTag>
				)
		}
	];

	return (
		<div className="page-stack page-stack--fill">
			{!inHeader && tabs}
			{catalog.error !== null ? (
				<Alert
					type="error"
					showIcon
					title={t(catalog.error)}
					action={<RetryButton size="small" onClick={catalog.reload} />}
				/>
			) : null}
			{catalog.error === null ? (
				<TablePanel
					className="fill-height"
					tools={
						<Tooltip title={t('themes.registerHint')}>
							<Typography.Text type="secondary" className="version-text text-caption">
								{t('themes.registerHintShort')}
							</Typography.Text>
						</Tooltip>
					}
				>
					<DataTable<Theme>
						className="themes-table repository-table"
						scrollLabel={t('nav.themesCatalog')}
						rowKey="id"
						columns={columns}
						dataSource={catalog.data ?? []}
						loading={catalog.loading}
						pagination={false}
						locale={{ emptyText: <Empty description={t('themes.emptyCatalog')} /> }}
					/>
				</TablePanel>
			) : null}
		</div>
	);
}
