// Platform maintenance controls ported from robot-platform's
// PlatformComponentsPage: header actions, facts, channel switches, confirmed
// version selection and durable task polling, adapted to this console's API.
import { useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Descriptions, Empty, Input, Select, Space, Switch } from 'antd';
import { RefreshButton, RetryButton } from '../components/ActionButton';
import { Panel } from '../components/Panel';
import PageLoading from '../components/PageLoading';
import StatusTag from '../components/StatusTag';
import { usePageHeader } from '../components/PageHeader';
import { t, type MessageKey } from '../lib/i18n';
import { useUpdateManager } from '../lib/updates/useUpdateManager';
import { timeText } from '../lib/admin/presentation';
import type { UpdateAction, UpdateVersion } from '../lib/api/updates';
import '../styles/updates.css';

const short = (value?: string) => (value ? value.slice(0, 12) : '—');
const versionKey = (version: UpdateVersion) => `${version.runId}:${version.commit}`;

export default function UpdatesPage() {
	const { modal, message } = App.useApp();
	const manager = useUpdateManager();
	const { data, busy, fresh } = manager;
	const [selected, setSelected] = useState<string>();
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
	useEffect(() => {
		setSelected(undefined);
		setToken('');
	}, [data?.repository, data?.channel]);
	const disabled =
		busy ||
		!fresh ||
		!data?.enabled ||
		!!data?.updating ||
		!!data?.checking ||
		data?.task?.status === 'recovery_required';
	const deployDisabled = disabled || !!data?.pinned;
	const candidate = data?.versions.find((v) => versionKey(v) === selected);
	const latest = data?.versions[0];
	const confirm = (action: UpdateAction, version?: UpdateVersion) => {
		setConfirmationError('');
		const source = data?.channel;
		const commit = data?.current.commit;
		modal.confirm({
			title: t(action.action === 'rollback' ? 'updates.confirmRollback' : 'updates.confirmDeploy'),
			content: (
				<>
					{t('updates.restartNotice')} {version && <code>{short(version.commit)}</code>}
				</>
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
					current.pinned
				) {
					setConfirmationError(t('updates.changed'));
					return;
				}
				await manager.run(action);
			}
		});
	};
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
				disabled={deployDisabled || !latest || latest.commit === data?.current.commit}
				onClick={() =>
					latest &&
					confirm({ action: 'deploy', runId: latest.runId, commit: latest.commit }, latest)
				}
			>
				{t('updates.update')}
			</Button>
		</Space>
	);
	const inHeader = usePageHeader({ actions });
	if (manager.loading && !data) return <PageLoading />;
	return (
		<div className="page-stack updates-page">
			{!inHeader && <div className="table-panel-header">{actions}</div>}
			{manager.error && (
				<Alert
					type="warning"
					showIcon
					title={manager.error}
					action={<RetryButton onClick={() => void manager.refresh()} />}
				/>
			)}
			{confirmationError && <Alert type="warning" showIcon title={confirmationError} />}
			{data && !data.enabled && <Empty description={t('updates.unavailable')} />}
			{data?.enabled && (
				<>
					<Panel
						title={t('updates.application')}
						tags={
							<StatusTag kind={data.updating ? 'info' : data.lastError ? 'warning' : 'success'}>
								{t(
									data.updating
										? 'updates.running'
										: data.lastError
											? 'updates.needsAttention'
											: 'updates.ready'
								)}
							</StatusTag>
						}
					>
						<Descriptions
							column={{ xs: 1, md: 2 }}
							items={[
								{
									key: 'version',
									label: t('updates.current'),
									children: (
										<code>
											{data.current.version} · {short(data.current.commit)}
										</code>
									)
								},
								{
									key: 'manager',
									label: t('updates.manager'),
									children: <code>{short(data.managerCommit)}</code>
								},
								{
									key: 'source',
									label: t('updates.source'),
									children: <code>{data.repository}</code>
								},
								{
									key: 'latest',
									label: t('updates.latest'),
									children: latest ? (
										<code>{short(latest.commit)}</code>
									) : (
										t(data.lastCheck ? 'updates.noBuilds' : 'updates.notChecked')
									)
								},
								{
									key: 'check',
									label: t('updates.lastCheck'),
									children: data.lastCheck ? timeText(data.lastCheck) : '—'
								}
							]}
						/>
						<div className="updates-controls">
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
						{data.lastError && <Alert type="warning" showIcon title={data.lastError} />}
					</Panel>
					<Panel title={t('updates.versions')}>
						<div className="updates-version-picker">
							<Select
								aria-label={t('updates.versions')}
								placeholder={t('updates.choose')}
								value={candidate ? selected : undefined}
								disabled={disabled}
								options={data.versions.map((v) => ({
									value: versionKey(v),
									label: `${short(v.commit)} · #${v.runId} · ${timeText(v.createdAt)}`
								}))}
								onChange={setSelected}
							/>
							<Button
								disabled={deployDisabled || !candidate || candidate.commit === data.current.commit}
								onClick={() =>
									candidate &&
									confirm(
										{ action: 'deploy', runId: candidate.runId, commit: candidate.commit },
										candidate
									)
								}
							>
								{t('updates.deploy')}
							</Button>
							<Button
								danger
								disabled={deployDisabled || !data.previous}
								onClick={() => confirm({ action: 'rollback' }, data.previous ?? undefined)}
							>
								{t('updates.rollback')}
							</Button>
						</div>
					</Panel>
					<Panel title={t('updates.task')}>
						{data.task ? (
							<Descriptions
								column={{ xs: 1, md: 2 }}
								items={[
									{ key: 'id', label: t('updates.taskId'), children: <code>{data.task.id}</code> },
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
										key: 'commit',
										label: t('updates.target'),
										children: <code>{short(data.task.commit)}</code>
									},
									{
										key: 'phase',
										label: t('updates.phase'),
										children: t(`updates.phase.${data.task.phase}` as MessageKey)
									},
									{ key: 'error', label: t('updates.result'), children: data.task.error || '—' }
								]}
							/>
						) : (
							<Empty description={t('updates.noTask')} />
						)}
					</Panel>
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
							<Button htmlType="submit" disabled={disabled || !token}>
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
					<Panel title={t('updates.logs')}>
						<ol className="updates-logs">
							{data.logs.map((line, index) => (
								<li key={`${index}-${line}`}>{line}</li>
							))}
						</ol>
					</Panel>
				</>
			)}
		</div>
	);
}
