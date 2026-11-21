// Maintenance table and exact-build confirmation adapted from robot-platform's PlatformComponentsPage.
import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Descriptions, Empty, Input, Select, Space, Switch } from 'antd';
import { RefreshButton, RetryButton } from '../components/ActionButton';
import DataTable from '../components/DataTable';
import { Panel, TablePanel } from '../components/Panel';
import PageLoading from '../components/PageLoading';
import StatusTag from '../components/StatusTag';
import VersionBadge from '../components/VersionBadge';
import VersionText from '../components/VersionText';
import { PageTabs, usePageHeader } from '../components/PageHeader';
import { t, type MessageKey } from '../lib/i18n';
import { queryChoice, usePageQuery } from '../lib/navigation/query';
import { useUpdateManager } from '../lib/updates/useUpdateManager';
import { timeText } from '../lib/admin/presentation';
import type { UpdateAction, UpdateVersion } from '../lib/api/updates';
import '../styles/updates.css';

const versionKey = (version: UpdateVersion) => `${version.runId}:${version.commit}`;
const numericVersion = new Intl.Collator('en', { numeric: true });
const validVersion = /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)$/;

function BuildDetails({
	commit,
	runId,
	attempt
}: {
	commit: string;
	runId?: number;
	attempt?: number;
}) {
	return (
		<div className="version-facts">
			<VersionText>{commit || '—'}</VersionText>
			{runId ? (
				<span>
					{t('updates.ciRun')}{' '}
					<VersionText>
						#{runId}.{attempt ?? 1}
					</VersionText>
				</span>
			) : null}
		</div>
	);
}

export default function UpdatesPage() {
	const { modal, message } = App.useApp();
	const manager = useUpdateManager();
	const { data, busy, fresh } = manager;
	const [params, setParams] = usePageQuery();
	const tab = queryChoice(params, 'tab', ['versions', 'activity', 'credentials'], 'versions');
	const [token, setToken] = useState('');
	const [confirmationError, setConfirmationError] = useState('');
	const lastNotice = useRef<typeof manager.note>(null);
	useEffect(() => {
		if (!manager.note) {
			lastNotice.current = null;
			return;
		}
		if (lastNotice.current === manager.note) return;
		lastNotice.current = manager.note;
		void message.open({ type: manager.note.type, content: manager.note.text });
	}, [manager.note, message]);
	useEffect(() => setToken(''), [data?.repository, data?.channel, tab]);

	const disabled =
		busy ||
		!fresh ||
		!data?.enabled ||
		data.updating ||
		data.checking ||
		data.task?.status === 'recovery_required';
	const deployDisabled = disabled || !!data?.pinned;
	const latest = data?.versions[0];
	const currentVersion = data?.current.version ?? '';
	const canUpdate =
		latest &&
		validVersion.test(latest.version) &&
		validVersion.test(currentVersion) &&
		numericVersion.compare(latest.version, currentVersion) > 0;
	const confirm = (action: UpdateAction, version?: UpdateVersion) => {
		setConfirmationError('');
		const source = data?.channel;
		const commit = data?.current.commit;
		modal.confirm({
			title: t(action.action === 'rollback' ? 'updates.confirmRollback' : 'updates.confirmDeploy'),
			content: (
				<div className="version-facts">
					{version && <VersionText>{version.version || t('updates.versionUnknown')}</VersionText>}
					<span>{t('updates.restartNotice')}</span>
					{version && <BuildDetails {...version} />}
				</div>
			),
			okText: t('updates.confirm'),
			cancelText: t('common.cancel'),
			focusable: { autoFocusButton: 'cancel' },
			onOk: async () => {
				const current = await manager.refresh();
				if (
					!current ||
					current.channel !== source ||
					current.current.commit !== commit ||
					current.updating ||
					current.pinned ||
					(action.action === 'deploy' &&
						!current.versions.some(
							(v) => v.runId === action.runId && v.commit === action.commit
						)) ||
					(action.action === 'rollback' && current.previous?.commit !== version?.commit)
				) {
					setConfirmationError(t('updates.changed'));
					return;
				}
				await manager.run(action);
			}
		});
	};
	const deploy = (version: UpdateVersion) =>
		confirm({ action: 'deploy', runId: version.runId, commit: version.commit }, version);
	const tabs = data?.enabled && (
		<PageTabs
			value={tab}
			ariaLabel={t('nav.updates')}
			options={[
				{ value: 'versions', label: t('updates.buildsTab') },
				{ value: 'activity', label: t('updates.activity') },
				{ value: 'credentials', label: t('updates.credentials') }
			]}
			onChange={(value) => setParams({ tab: value === 'versions' ? null : value })}
		/>
	);
	const actions = (
		<Space wrap>
			<RefreshButton onClick={() => void manager.refresh()} disabled={busy} />
			<Button
				onClick={() => void manager.run({ action: 'check' })}
				disabled={disabled}
				loading={busy}
			>
				{t('updates.check')}
			</Button>
			<Button
				type="primary"
				disabled={deployDisabled || !canUpdate}
				onClick={() => latest && deploy(latest)}
			>
				{t('updates.update')}
			</Button>
		</Space>
	);
	const inHeader = usePageHeader({ actions, content: tabs });

	if (manager.loading && !data) return <PageLoading />;
	return (
		<div className="page-stack page-stack--fill updates-page">
			{!inHeader && (
				<div className="table-panel-header">
					{tabs}
					{actions}
				</div>
			)}
			{manager.error && (
				<Alert
					type="warning"
					showIcon
					title={manager.error}
					action={<RetryButton onClick={() => void manager.refresh()} />}
				/>
			)}
			{confirmationError && <Alert type="warning" showIcon title={confirmationError} />}
			{data?.lastError && <Alert type="warning" showIcon title={data.lastError} />}
			{data && !data.enabled && <Empty description={t('updates.unavailable')} />}
			{data?.enabled && tab === 'versions' && (
				<>
					<TablePanel title={t('updates.application')}>
						<DataTable
							rowKey="id"
							pagination={false}
							scrollLabel={t('updates.application')}
							dataSource={[
								{
									id: 'app',
									label: t('updates.appLabel'),
									kind: t('updates.versionTag'),
									version: currentVersion,
									commit: data.current.commit
								},
								{
									id: 'manager',
									label: t('updates.manager'),
									kind: t('updates.manager'),
									version: data.managerVersion,
									commit: data.managerCommit
								}
							]}
							columns={[
								{ title: t('updates.component'), dataIndex: 'label' },
								{
									title: t('updates.current'),
									render: (_, item) => (
										<VersionBadge
											label={item.kind}
											version={item.version || t('updates.versionUnknown')}
											details={<BuildDetails commit={item.commit} />}
										/>
									)
								},
								{
									title: t('updates.latest'),
									render: (_, item) =>
										item.id === 'app' ? (
											latest ? (
												<VersionText>{latest.version || t('updates.versionUnknown')}</VersionText>
											) : (
												t(data.lastCheck ? 'updates.noBuilds' : 'updates.notChecked')
											)
										) : (
											'—'
										)
								}
							]}
						/>
					</TablePanel>
					<div className="maintenance-controls">
						<label>
							<span>{t('updates.channel')}</span>
							<Select<'main' | 'dev'>
								aria-label={t('updates.channel')}
								value={data.channel as 'main' | 'dev'}
								disabled={disabled}
								options={[
									{ value: 'main', label: t('updates.main') },
									{ value: 'dev', label: t('updates.dev') }
								]}
								onChange={(channel) => void manager.run({ action: 'channel', channel })}
							/>
						</label>
						<label>
							<span>{t('updates.auto')}</span>
							<Switch
								aria-label={t('updates.auto')}
								checked={data.auto}
								disabled={disabled}
								onChange={(on) => {
									if (!on) void manager.run({ action: 'auto', on });
									else
										modal.confirm({
											title: t('updates.auto'),
											content: t('updates.autoNotice'),
											focusable: { autoFocusButton: 'cancel' },
											okText: t('updates.confirm'),
											cancelText: t('common.cancel'),
											onOk: () => manager.run({ action: 'auto', on }).then(() => undefined)
										});
								}}
							/>
						</label>
						<label>
							<span>{t('updates.pin')}</span>
							<Switch
								aria-label={t('updates.pin')}
								checked={data.pinned}
								disabled={disabled}
								onChange={(on) => void manager.run({ action: 'pin', on })}
							/>
						</label>
					</div>
					{data.task && (
						<div className="maintenance-receipt" role="status">
							<Button type="link" onClick={() => setParams({ tab: 'activity' })}>
								{t('updates.task')}
							</Button>
							<VersionText>{data.task.id}</VersionText>
							<span>{t(`updates.task.${data.task.status}` as MessageKey)}</span>
						</div>
					)}
					<TablePanel
						className="fill-height"
						title={t('updates.versions')}
						tools={
							<Button
								danger
								size="small"
								disabled={deployDisabled || !data.previous}
								onClick={() => confirm({ action: 'rollback' }, data.previous ?? undefined)}
							>
								{t('updates.rollback')}
							</Button>
						}
					>
						<DataTable<UpdateVersion>
							className="repository-table"
							rowKey={versionKey}
							scrollLabel={t('updates.versions')}
							dataSource={data.versions}
							pagination={false}
							locale={{
								emptyText: (
									<Empty
										image={Empty.PRESENTED_IMAGE_SIMPLE}
										description={t(data.lastCheck ? 'updates.noBuilds' : 'updates.notChecked')}
									/>
								)
							}}
							columns={[
								{
									title: t('updates.buildsTab'),
									dataIndex: 'version',
									render: (_, item) => (
										<VersionBadge
											version={item.version || t('updates.versionUnknown')}
											details={<BuildDetails {...item} />}
										/>
									)
								},
								{
									title: t('admin.status'),
									render: (_, item) =>
										item.commit === data.current.commit ? (
											<StatusTag kind="success">{t('updates.activeVersion')}</StatusTag>
										) : item.version === currentVersion ? (
											<StatusTag kind="neutral">{t('updates.sameVersion')}</StatusTag>
										) : (
											'—'
										)
								},
								{ title: t('audit.time'), dataIndex: 'createdAt', render: timeText },
								{
									title: t('admin.actions'),
									render: (_, item) => (
										<Button
											size="small"
											disabled={deployDisabled || item.commit === data.current.commit}
											onClick={() => deploy(item)}
										>
											{t('updates.deploy')}
										</Button>
									)
								}
							]}
						/>
					</TablePanel>
				</>
			)}
			{data?.enabled && tab === 'activity' && (
				<>
					<Panel title={t('updates.task')}>
						{data.task ? (
							<Descriptions
								column={{ xs: 1, md: 2 }}
								items={[
									{
										key: 'id',
										label: t('updates.taskId'),
										children: <VersionText>{data.task.id}</VersionText>
									},
									{
										key: 'status',
										label: t('updates.status'),
										children: (
											<span role="status">
												{t(`updates.task.${data.task.status}` as MessageKey)}
											</span>
										)
									},
									{
										key: 'target',
										label: t('updates.target'),
										children: (
											<VersionBadge
												version={
													data.task.version ||
													data.versions.find((v) => v.commit === data.task?.commit)?.version ||
													t('updates.versionUnknown')
												}
												details={<BuildDetails commit={data.task.commit} />}
											/>
										)
									},
									{
										key: 'phase',
										label: t('updates.phase'),
										children: t(`updates.phase.${data.task.phase}` as MessageKey)
									},
									{ key: 'result', label: t('updates.result'), children: data.task.error || '—' }
								]}
							/>
						) : (
							<Empty description={t('updates.noTask')} />
						)}
					</Panel>
					<Panel title={t('updates.logs')}>
						<ol className="updates-logs">
							{data.logs.map((line, index) => (
								<li key={`${index}-${line}`}>{line}</li>
							))}
						</ol>
					</Panel>
				</>
			)}
			{data?.enabled && tab === 'credentials' && (
				<Panel title={t('updates.credentials')}>
					<form
						className="updates-token"
						onSubmit={(event) => {
							event.preventDefault();
							void manager.run({ action: 'token', token }).then((ok) => {
								if (ok) setToken('');
							});
						}}
					>
						<label htmlFor="github-update-token">
							{t(data.tokenSet ? 'updates.tokenSet' : 'updates.publicAccess')}
						</label>
						<Input.Password
							id="github-update-token"
							aria-label={t('updates.token')}
							autoComplete="off"
							value={token}
							maxLength={512}
							onChange={(event) => setToken(event.target.value)}
							disabled={disabled}
						/>
						<Button type="primary" htmlType="submit" disabled={disabled || !token}>
							{t('common.save')}
						</Button>
						<Button
							disabled={disabled || !data.tokenSet}
							onClick={() => void manager.run({ action: 'token', token: '' })}
						>
							{t('updates.clearToken')}
						</Button>
					</form>
				</Panel>
			)}
		</div>
	);
}
