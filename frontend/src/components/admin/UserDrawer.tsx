// The reference UsersPage's editor, lifecycle confirmations and session
// controls, adapted to passkeys and per-site memberships.
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Alert, App, Button, Drawer, Input, Select, Space, Tooltip } from 'antd';
import DataTable from '../DataTable';
import PageLoading from '../PageLoading';
import StatusTag from '../StatusTag';
import { Panel, TablePanel } from '../Panel';
import { sessionSnapshot, subscribeSession, loadSession } from '../../lib/session';
import { t, type MessageKey } from '../../lib/i18n';
import { timeText } from '../../lib/admin/presentation';
import { useAdminQuery, type useAdminMutation } from '../../lib/admin/requests';
import * as service from '../../lib/api/admin';

type Props = {
	id: string;
	mutation: ReturnType<typeof useAdminMutation>;
	onClose: (discard?: boolean) => void;
	onChanged: () => void;
	onCode: (code: service.AccountCode) => void;
	onDirtyChange: (dirty: boolean) => void;
};
export default function UserDrawer({
	id,
	mutation,
	onClose,
	onChanged,
	onCode,
	onDirtyChange
}: Props) {
	const { modal, message } = App.useApp();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const load = useCallback((signal: AbortSignal) => service.getUser(id, signal), [id]);
	const query = useAdminQuery(load);
	const [profile, setProfile] = useState<service.AdminProfile | null>(null);
	const baseline = useRef<service.AdminProfile | null>(null);
	useEffect(() => {
		if (query.data && profile === null) {
			const value = {
				username: query.data.user.username,
				displayName: query.data.user.displayName
			};
			baseline.current = value;
			setProfile(value);
		}
	}, [query.data, profile]);
	const dirty =
		profile !== null &&
		(profile.username !== baseline.current?.username ||
			profile.displayName !== baseline.current?.displayName);
	useEffect(() => {
		onDirtyChange(dirty);
	}, [dirty, onDirtyChange]);
	const data = query.data;
	const user = data?.user;
	const self = session.user?.id === id;
	const unavailable = mutation.busy || query.loading;
	const active = user?.status === 'active';
	const canDelete = !self && (active || user?.status === 'disabled');
	const changed = async (refreshSession = false) => {
		query.reload();
		onChanged();
		void message.success(t('common.saved'));
		if (refreshSession) await loadSession();
	};
	function confirm(
		title: MessageKey,
		content: MessageKey,
		task: () => Promise<unknown>,
		refreshSession = false,
		close = false
	) {
		modal.confirm({
			title: `${t(title)} · ${user?.username ?? ''}`,
			content: t(content),
			okText: t('admin.confirm'),
			cancelText: t('admin.cancel'),
			okButtonProps: { danger: true },
			onOk: async () => {
				await mutation.run(task, async () => {
					if (close) {
						onClose(true);
						onChanged();
					} else await changed(refreshSession);
				});
			}
		});
	}
	return (
		<Drawer
			open
			title={user?.username ?? t('admin.manage')}
			size={820}
			styles={{ wrapper: { maxWidth: '100vw' } }}
			destroyOnHidden
			onClose={() => onClose()}
			closable={!mutation.busy}
			keyboard={!mutation.busy}
			mask={{ closable: !mutation.busy }}
		>
			<div className="page-stack admin-user-detail">
				{mutation.error && <Alert type="error" showIcon title={t(mutation.error)} />}
				{query.error && (
					<Alert
						type="error"
						showIcon
						title={t(query.error)}
						action={<Button onClick={query.reload}>{t('common.retry')}</Button>}
					/>
				)}
				{query.loading && !data && <PageLoading label={t('common.loading')} />}
				{data && user && (
					<>
						<Panel
							title={t('admin.profile')}
							tags={
								<StatusTag kind={active ? 'success' : 'neutral'}>
									{t(`admin.status.${user.status}` as MessageKey)}
								</StatusTag>
							}
						>
							<form
								className="admin-profile-form"
								onSubmit={(event) => {
									event.preventDefault();
									if (!profile || unavailable || !dirty) return;
									void mutation.run(
										() => service.updateUser(id, profile),
										async (saved) => {
											const value = { username: saved.username, displayName: saved.displayName };
											baseline.current = value;
											setProfile(value);
											await changed(self);
										}
									);
								}}
							>
								<div className="form-grid">
									<label className="field">
										<span className="field-label">{t('admin.username')}</span>
										<Input
											required
											pattern="[a-z0-9][a-z0-9._-]{2,63}"
											minLength={3}
											maxLength={64}
											value={profile?.username ?? ''}
											disabled={unavailable || user.status === 'deleted'}
											onChange={(event) =>
												setProfile((value) =>
													value ? { ...value, username: event.target.value } : value
												)
											}
										/>
									</label>
									<label className="field">
										<span className="field-label">{t('admin.displayName')}</span>
										<Input
											maxLength={200}
											value={profile?.displayName ?? ''}
											disabled={unavailable || user.status === 'deleted'}
											onChange={(event) =>
												setProfile((value) =>
													value ? { ...value, displayName: event.target.value } : value
												)
											}
										/>
									</label>
								</div>
								<Button
									type="primary"
									htmlType="submit"
									disabled={!dirty || unavailable || user.status === 'deleted'}
								>
									{t('admin.saveProfile')}
								</Button>
							</form>
						</Panel>
						<Panel title={t('admin.platformRole')}>
							<Space wrap>
								<Select
									aria-label={t('admin.platformRole')}
									value={user.platformRole}
									disabled={!active || unavailable}
									className="admin-role-select"
									options={[
										{ value: 'member', label: t('admin.role.member') },
										{ value: 'admin', label: t('admin.role.admin') }
									]}
									onChange={(role: service.AdminUser['platformRole']) =>
										confirm(
											'admin.changeRole',
											'admin.roleConsequence',
											() => service.setUserRole(id, role),
											self
										)
									}
								/>
								<Button
									disabled={unavailable || !active}
									onClick={() => void mutation.run(() => service.issueUserCode(id), onCode)}
								>
									{t('admin.issueCode')}
								</Button>
								{user.status === 'disabled' ? (
									<Button
										disabled={unavailable}
										onClick={() =>
											void mutation.run(
												() => service.setUserStatus(id, 'active'),
												() => changed()
											)
										}
									>
										{t('admin.enable')}
									</Button>
								) : (
									<Button
										disabled={unavailable || self || !active}
										onClick={() =>
											confirm('admin.disable', 'admin.disableConsequence', () =>
												service.setUserStatus(id, 'disabled')
											)
										}
									>
										{t('admin.disable')}
									</Button>
								)}
								<Button
									danger
									disabled={unavailable || !canDelete}
									onClick={() =>
										confirm(
											'common.delete',
											'admin.deleteConsequence',
											() => service.deleteUser(id),
											false,
											true
										)
									}
								>
									{t('common.delete')}
								</Button>
							</Space>
						</Panel>
						<TablePanel title={t('admin.sites')}>
							<DataTable
								scroll={{ x: 'max-content' }}
								rowKey="siteId"
								dataSource={data.access.sites}
								pagination={false}
								columns={[
									{ title: t('site.name'), dataIndex: 'name' },
									{
										title: t('sidebar.role'),
										render: (_, site) => (
											<Select
												aria-label={`${t('admin.sites')} ${site.name}`}
												className="admin-role-select"
												value={site.role}
												disabled={unavailable || !active}
												options={(['', 'viewer', 'editor', 'owner'] as const).map((role) => ({
													value: role,
													label: role ? t(`role.${role}`) : t('admin.noAccess')
												}))}
												onChange={(role: service.SiteAccess['role']) =>
													confirm('admin.changeAccess', 'admin.accessConsequence', () =>
														service.setSiteAccess(id, site.siteId, role)
													)
												}
											/>
										)
									}
								]}
							/>
						</TablePanel>
						<TablePanel title={t('admin.passkeys')}>
							<DataTable
								scroll={{ x: 'max-content' }}
								rowKey="id"
								dataSource={data.access.passkeys}
								pagination={false}
								columns={[
									{
										title: t('admin.passkeyId'),
										dataIndex: 'id',
										render: (value: string) => <code>{value}</code>
									},
									{
										title: 'RP',
										render: (_, key) => (
											<Space orientation="vertical" size={4}>
												<code>{key.rpId}</code>
												{key.currentRp && <StatusTag kind="info">{t('admin.currentRp')}</StatusTag>}
											</Space>
										)
									},
									{ title: t('admin.lastUsed'), dataIndex: 'lastUsedAt', render: timeText },
									{
										title: t('admin.actions'),
										render: (_, key) => (
											<Tooltip
												title={
													self &&
													key.currentRp &&
													data.access.passkeys.filter((item) => item.currentRp).length <= 1
														? t('error.lastPasskey')
														: undefined
												}
											>
												<Button
													danger
													disabled={
														unavailable ||
														user.status === 'deleted' ||
														(self &&
															key.currentRp &&
															data.access.passkeys.filter((item) => item.currentRp).length <= 1)
													}
													onClick={() =>
														confirm(
															'admin.revokePasskey',
															'admin.passkeyConsequence',
															() => service.revokePasskey(id, key.id),
															self
														)
													}
												>
													{t('admin.revoke')}
												</Button>
											</Tooltip>
										)
									}
								]}
							/>
						</TablePanel>
						<TablePanel
							title={t('admin.sessions')}
							tools={
								<Button
									danger
									disabled={unavailable || data.access.sessions.length === 0}
									onClick={() =>
										confirm(
											'admin.revokeAllSessions',
											'admin.sessionConsequence',
											() => service.revokeSessions(id),
											self
										)
									}
								>
									{t('admin.revokeAllSessions')}
								</Button>
							}
						>
							<DataTable
								scroll={{ x: 'max-content' }}
								rowKey="id"
								dataSource={data.access.sessions}
								pagination={false}
								columns={[
									{ title: t('admin.createdAt'), dataIndex: 'createdAt', render: timeText },
									{ title: t('admin.lastSeen'), dataIndex: 'lastSeenAt', render: timeText },
									{ title: t('admin.expiresAt'), dataIndex: 'expiresAt', render: timeText },
									{
										title: t('admin.sessionKind'),
										render: (_, value) => (
											<Space orientation="vertical" size={4}>
												<span>
													{t(
														value.kind === 'full' ? 'admin.fullSession' : 'admin.restrictedSession'
													)}
												</span>
												{value.current && (
													<StatusTag kind="info">{t('admin.currentSession')}</StatusTag>
												)}
											</Space>
										)
									},
									{
										title: t('admin.actions'),
										render: (_, value) => (
											<Button
												danger
												disabled={unavailable}
												onClick={() =>
													confirm(
														'admin.revoke',
														'admin.sessionConsequence',
														() => service.revokeSessions(id, value.id),
														self && value.current
													)
												}
											>
												{t('admin.revoke')}
											</Button>
										)
									}
								]}
							/>
						</TablePanel>
					</>
				)}
			</div>
		</Drawer>
	);
}
