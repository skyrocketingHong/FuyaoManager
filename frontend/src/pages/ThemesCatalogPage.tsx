/**
 * ThemesCatalogPage: the platform-scope registered-theme catalog — versions,
 * adapters, capabilities, URL conventions, required artifacts, and whether a
 * config schema is declared. Registration itself stays operator-driven
 * (CLI), so this page is read-only by design.
 */
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Alert, Space, Table, Tag, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { listThemes, type Theme } from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import { useSyncExternalStore } from 'react';

export default function ThemesCatalogPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const [themes, setThemes] = useState<Theme[] | null>(null);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);

	useEffect(() => {
		if (session.status === 'anonymous') {
			navigate('/login');
			return;
		}
		if (session.status !== 'authenticated') return;
		let cancelled = false;
		listThemes()
			.then((list) => {
				if (!cancelled) setThemes(list);
			})
			.catch((err) => {
				if (cancelled) return;
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') {
					navigate('/login');
					return;
				}
				setErrorKey(key);
			});
		return () => {
			cancelled = true;
		};
	}, [session.status, navigate]);

	const columns: ColumnsType<Theme> = [
		{
			title: t('themes.themeColumn'),
			render: (_, theme) => (
				<>
					<div>{theme.name ?? theme.themeId}</div>
					<Typography.Text type="secondary" style={{ fontSize: 12 }}>
						{theme.themeId} @ {theme.version}
					</Typography.Text>
				</>
			)
		},
		{ title: t('themes.adapterColumn'), dataIndex: 'adapter', width: 200 },
		{
			title: t('themes.capabilitiesColumn'),
			dataIndex: 'capabilities',
			render: (caps: string[]) => (
				<span>
					{(caps ?? []).map((cap) => (
						<Tag key={cap} style={{ marginInlineEnd: 4 }}>
							{cap}
						</Tag>
					))}
				</span>
			)
		},
		{
			title: t('themes.requirementsColumn'),
			render: (_, theme) => (
				<Typography.Text type="secondary" style={{ fontSize: 12 }}>
					{(theme.outputs?.urlConventions ?? []).join(', ') || '—'}
					{(theme.outputs?.requiredArtifacts ?? []).length > 0
						? ` · ${t('themes.artifacts')}: ${(theme.outputs?.requiredArtifacts ?? []).join(', ')}`
						: ''}
				</Typography.Text>
			)
		},
		{
			title: t('themes.configColumn'),
			width: 120,
			render: (_, theme) =>
				theme.configSchema ? (
					<Tag color="green">{t('themes.schemaDeclared')}</Tag>
				) : (
					<Tag>{t('themes.emptyConfigOnly')}</Tag>
				)
		}
	];

	return (
		<>
			{errorKey !== null ? (
				<Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			<Table<Theme>
				rowKey="id"
				size="small"
				columns={columns}
				dataSource={themes ?? []}
				loading={themes === null}
				pagination={false}
				locale={{ emptyText: t('themes.emptyCatalog') }}
			/>
			<Space style={{ marginTop: 12 }}>
				<Typography.Text type="secondary" style={{ fontSize: 12 }}>
					{t('themes.registerHint')}
				</Typography.Text>
			</Space>
		</>
	);
}
