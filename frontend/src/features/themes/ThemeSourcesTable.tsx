import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Empty, Space } from 'antd';
import DataTable from '../../components/DataTable';
import { TablePanel } from '../../components/Panel';
import StatusTag from '../../components/StatusTag';
import VersionBadge from '../../components/VersionBadge';
import VersionText from '../../components/VersionText';
import { RetryButton } from '../../components/ActionButton';
import { useAdminQuery } from '../../lib/admin/requests';
import { ApiError, currentAuthGeneration } from '../../lib/api/client';
import {
	checkThemeSource,
	installThemeSource,
	readThemeSources,
	type ThemeCandidate,
	type ThemeSource
} from '../../lib/api/themeSources';
import { t, type MessageKey } from '../../lib/i18n';
import { apiErrorKey } from '../../lib/i18n/errors';

export default function ThemeSourcesTable({ onInstalled }: { onInstalled: () => void }) {
	const { modal } = App.useApp();
	const query = useAdminQuery(readThemeSources);
	const generation = currentAuthGeneration();
	const [inspection, setInspection] = useState<Record<string, ThemeCandidate>>({});
	const [pending, setPending] = useState<{ id: string; kind: 'check' | 'install' }>();
	const [error, setError] = useState<MessageKey>();
	const mounted = useRef(false);
	const locked = useRef(false);
	const installed = (query.data ?? [])
		.map(({ task }) => (task?.state === 'installed' ? task.commit : ''))
		.join('/');
	const installing = query.data?.some(({ task }) => task?.state === 'installing') ?? false;

	useEffect(() => {
		mounted.current = true;
		return () => {
			mounted.current = false;
		};
	}, []);
	useEffect(() => {
		if (installed.replaceAll('/', '')) onInstalled();
	}, [installed, onInstalled]);
	useEffect(() => {
		if (!installing || query.loading || query.error) return;
		const timer = window.setTimeout(query.reload, 1500);
		return () => window.clearTimeout(timer);
	}, [installing, query.loading, query.error, query.reload, query.data]);

	async function run(id: string, candidate?: ThemeCandidate) {
		if (!mounted.current || locked.current || generation !== currentAuthGeneration()) return;
		locked.current = true;
		setError(undefined);
		setPending({ id, kind: candidate ? 'install' : 'check' });
		const current = () => mounted.current && generation === currentAuthGeneration();
		try {
			if (candidate) {
				await installThemeSource(id, candidate.commit);
				if (current()) query.reload();
			} else {
				const latest = await checkThemeSource(id);
				if (current()) setInspection((previous) => ({ ...previous, [id]: latest }));
			}
		} catch (cause) {
			if (!current()) return;
			setError(apiErrorKey(cause) ?? 'error.generic');
			if (cause instanceof ApiError && cause.code === 'theme_source_conflict') {
				setInspection((previous) =>
					Object.fromEntries(Object.entries(previous).filter(([key]) => key !== id))
				);
			}
		} finally {
			locked.current = false;
			if (current()) setPending(undefined);
		}
	}

	function confirm(source: ThemeSource, candidate: ThemeCandidate) {
		modal.confirm({
			title: t('themeSources.confirm', { version: candidate.version }),
			content: t('themeSources.installEffect', { repo: source.url }),
			okText: t('themeSources.install'),
			cancelText: t('common.cancel'),
			focusable: { autoFocusButton: 'cancel' },
			onOk: () => run(source.id, candidate)
		});
	}

	return (
		<>
			{(query.error || error) && (
				<Alert
					type="error"
					showIcon
					title={t(query.error ?? error ?? 'error.generic')}
					action={query.error && <RetryButton onClick={query.reload} />}
				/>
			)}
			<TablePanel className="fill-height">
				<DataTable<ThemeSource>
					className="repository-table"
					rowKey="id"
					dataSource={query.data ?? []}
					pagination={false}
					loading={query.loading && query.data === null}
					scrollLabel={t('themeSources.tab')}
					locale={{
						emptyText: (
							<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('themeSources.empty')} />
						)
					}}
					columns={[
						{
							title: t('themeSources.repository'),
							dataIndex: 'id',
							render: (_, source) => (
								<div className="maintenance-identity">
									<a href={source.url} target="_blank" rel="noreferrer">
										{source.id}
									</a>
									<VersionText className="subtle">
										{source.branch}/{source.path}
									</VersionText>
								</div>
							)
						},
						{
							title: t('themeSources.version'),
							render: (_, source) => {
								const version = inspection[source.id] ?? source.task;
								return version ? (
									<VersionBadge
										version={version.version}
										details={<VersionText>{version.commit}</VersionText>}
									/>
								) : (
									'—'
								);
							}
						},
						{
							title: t('admin.status'),
							render: (_, source) => {
								const checked = inspection[source.id];
								const task = source.task;
								if (
									task &&
									(!checked || task.commit === checked.commit || task.state === 'installing')
								) {
									return (
										<Space orientation="vertical" size={2}>
											<StatusTag
												kind={
													task.state === 'failed'
														? 'danger'
														: task.state === 'installing'
															? 'info'
															: 'success'
												}
											>
												{t(`themeSources.${task.state}`)}
											</StatusTag>
											{task.error && (
												<span className="subtle">
													{t(
														apiErrorKey(new ApiError(0, task.error, '', null)) ??
															'themeSources.installFailed'
													)}
												</span>
											)}
										</Space>
									);
								}
								return checked ? (
									<StatusTag kind={checked.state === 'conflict' ? 'warning' : 'neutral'}>
										{t(`themeSources.${checked.state}`)}
									</StatusTag>
								) : (
									'—'
								);
							}
						},
						{
							title: t('admin.actions'),
							render: (_, source) => {
								const checked = inspection[source.id];
								const ready =
									checked?.state === 'available' &&
									!(source.task?.state === 'installed' && source.task.commit === checked.commit);
								return (
									<Space wrap size={8}>
										<Button
											size="small"
											disabled={Boolean(pending)}
											loading={pending?.id === source.id && pending.kind === 'check'}
											onClick={() => void run(source.id)}
										>
											{t('themeSources.check')}
										</Button>
										{ready && (
											<Button
												size="small"
												type="primary"
												disabled={installing || Boolean(pending)}
												loading={pending?.id === source.id && pending.kind === 'install'}
												onClick={() => confirm(source, checked)}
											>
												{t('themeSources.install')}
											</Button>
										)}
									</Space>
								);
							}
						}
					]}
				/>
			</TablePanel>
		</>
	);
}
