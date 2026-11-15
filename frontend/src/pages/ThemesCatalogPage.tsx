/**
 * ThemesCatalogPage: the platform-scope registered-theme catalog — versions,
 * adapters, capabilities, URL conventions, required artifacts, and whether a
 * config schema is declared. Registration itself stays operator-driven
 * (`fuyao-admin register-theme`), so this page is read-only by design. An
 * empty catalog is a real state with the registration hint; a load failure
 * is never disguised as an empty catalog.
 */
import { useCallback, useEffect, useState } from 'react';
import { Alert, Empty, Tooltip, Typography } from 'antd';
import DataTable from '../components/DataTable';
import type { ColumnsType } from 'antd/es/table';
import { listThemes, type Theme } from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import { useSyncExternalStore } from 'react';
import StatusTag from '../components/StatusTag';
import { TablePanel } from '../components/Panel';
import { RetryButton } from '../components/ActionButton';

export default function ThemesCatalogPage() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [themes, setThemes] = useState<Theme[] | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	const load = useCallback(() => {
		if (session.status !== 'authenticated') return;
		let cancelled = false;
		setErrorKey(null);
		listThemes()
			.then((list) => {
				if (!cancelled) setThemes(list);
			})
			.catch((err) => {
				if (cancelled) return;
				setErrorKey(apiErrorKey(err) ?? 'error.generic');
			});
		return () => {
			cancelled = true;
		};
	}, [session.status]);

	useEffect(() => load(), [load]);

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
			{errorKey !== null ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={
						<RetryButton
							size="small"
							onClick={() => {
								setThemes(null);
								load();
							}}
						 />
					}
				/>
			) : null}
			{errorKey === null ? (
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
						className="themes-table"
						rowKey="id"
						columns={columns}
						dataSource={themes ?? []}
						loading={themes === null}
						pagination={false}
						locale={{ emptyText: <Empty description={t('themes.emptyCatalog')} /> }}
					/>
				</TablePanel>
			) : null}
		</div>
	);
}
