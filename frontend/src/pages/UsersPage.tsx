// Table, editor lifecycle and request guards adapted from the reference UsersPage.
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Alert, App, Button, Empty, Input, Modal, Select, Space, Typography } from 'antd';
import { RefreshCw, UserPlus } from 'lucide-react';
import DataTable from '../components/DataTable';
import UserDrawer from '../components/admin/UserDrawer';
import PageLoading from '../components/PageLoading';
import StatusTag from '../components/StatusTag';
import { TablePanel } from '../components/Panel';
import { usePageHeader } from '../components/PageHeader';
import { useUnsavedGuard } from '../lib/editing/useUnsavedGuard';
import { currentAuthGeneration } from '../lib/api/client';
import { loadSiteList } from '../lib/sites/site-list';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import { t, type MessageKey } from '../lib/i18n';
import { useAdminMutation, useAdminQuery } from '../lib/admin/requests';
import { timeText } from '../lib/admin/presentation';
import * as service from '../lib/api/admin';
import { selectedTableSort, sortColumn, type TableSort } from '../lib/table-sort';

export default function UsersPage() {
	const { modal } = App.useApp();
	useSyncExternalStore(subscribeSession, sessionSnapshot);
	const generation = currentAuthGeneration();
	const [search, setSearch] = useState('');
	const [q, setQuery] = useState('');
	const [role, setRole] = useState('');
	const [status, setStatus] = useState('');
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(50);
	const [ordering, setOrdering] = useState<TableSort>();
	const queryString = service.queryString({ q, role, status, page, pageSize, sort: ordering?.field, order: ordering?.direction });
	const load = useCallback(
		(signal: AbortSignal) => service.listUsers(queryString, signal),
		[queryString]
	);
	const query = useAdminQuery(load);
	useEffect(() => {
		if (!query.data) return;
		const lastPage = Math.max(1, Math.ceil(query.data.total / pageSize));
		if (page > lastPage) setPage(lastPage);
	}, [query.data?.total, page, pageSize]);
	const mutation = useAdminMutation();
	const [selected, setSelected] = useState<string | null>(null);
	const [drawerDirty, setDrawerDirty] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const [profile, setProfile] = useState<service.AdminProfile>({ username: '', displayName: '' });
	const [code, setCode] = useState<{ generation: number; value: service.AccountCode } | null>(null);
	const form = useRef<HTMLFormElement>(null);
	const createDirty = createOpen && (profile.username !== '' || profile.displayName !== '');
	const validProfile =
		/^[a-z0-9][a-z0-9._-]{2,63}$/.test(profile.username) && profile.displayName.length <= 200;
	useUnsavedGuard(createDirty || drawerDirty || mutation.busy);
	useEffect(() => {
		setSelected(null);
		setDrawerDirty(false);
		setCreateOpen(false);
		setCode(null);
		setProfile({ username: '', displayName: '' });
	}, [generation]);
	const changed = useCallback(() => {
		query.reload();
		void loadSiteList(true);
	}, [query.reload]);
	const showCode = (value: service.AccountCode) =>
		setCode({ generation: currentAuthGeneration(), value });
	const closeDrawer = (discard = false) => {
		if (mutation.busy && !discard) return;
		const close = () => {
			setSelected(null);
			setDrawerDirty(false);
			mutation.clearError();
		};
		if (drawerDirty && !discard)
			modal.confirm({
				title: t('draft.leaveTitle'),
				content: t('draft.leaveBody'),
				okText: t('draft.leaveConfirm'),
				cancelText: t('draft.leaveCancel'),
				onOk: close
			});
		else close();
	};
	const closeCreate = () => {
		if (mutation.busy) return;
		const close = () => {
			setCreateOpen(false);
			setProfile({ username: '', displayName: '' });
			mutation.clearError();
		};
		if (createDirty)
			modal.confirm({
				title: t('draft.leaveTitle'),
				content: t('draft.leaveBody'),
				okText: t('draft.leaveConfirm'),
				cancelText: t('draft.leaveCancel'),
				onOk: close
			});
		else close();
	};
	const content = (
		<Space wrap size={8}>
			<Input.Search
				aria-label={t('admin.search')}
				placeholder={t('admin.search')}
				maxLength={128}
				allowClear
				value={search}
				onChange={(event) => setSearch(event.target.value)}
				onSearch={(value) => {
					setQuery(value);
					setPage(1);
				}}
				className="admin-search"
			/>
			<Select
				className="admin-filter"
				aria-label={t('admin.platformRole')}
				placeholder={t('admin.allRoles')}
				allowClear
				value={role || undefined}
				onChange={(value) => {
					setRole(value ?? '');
					setPage(1);
				}}
				options={[
					{ value: 'admin', label: t('admin.role.admin') },
					{ value: 'member', label: t('admin.role.member') }
				]}
			/>
			<Select
				className="admin-filter"
				aria-label={t('admin.status')}
				placeholder={t('admin.statusPlaceholder')}
				allowClear
				value={status || undefined}
				onChange={(value) => {
					setStatus(value ?? '');
					setPage(1);
				}}
				options={(['active', 'disabled', 'pending', 'deleted'] as const).map((value) => ({
					value,
					label: t(`admin.status.${value}`)
				}))}
			/>
		</Space>
	);
	const actions = (
		<Space wrap size={8}>
			<Button
				icon={<RefreshCw size={16} />}
				onClick={query.reload}
				disabled={query.loading || mutation.busy}
			>
				{t('admin.refresh')}
			</Button>
			<Button
				type="primary"
				icon={<UserPlus size={16} />}
				disabled={mutation.busy}
				onClick={() => {
					mutation.clearError();
					setCreateOpen(true);
				}}
			>
				{t('admin.create')}
			</Button>
		</Space>
	);
	const inHeader = usePageHeader({ content, actions });
	const visibleCode = code?.generation === generation ? code.value : null;
	return (
		<div className="page-stack page-stack--fill">
			{!inHeader && (
				<div className="table-panel-header">
					{content}
					{actions}
				</div>
			)}
			{query.error && (
				<Alert
					type="error"
					showIcon
					title={t(query.error)}
					action={<Button onClick={query.reload}>{t('common.retry')}</Button>}
				/>
			)}
			{mutation.error && !selected && !createOpen && (
				<Alert type="error" showIcon title={t(mutation.error)} />
			)}
			{query.loading && !query.data ? (
				<PageLoading label={t('common.loading')} />
			) : (
				query.data && (
					<TablePanel className="fill-height">
						<DataTable<service.AdminUser>
							manualSorting
							scroll={{ x: 'max-content' }}
							styles={{ content: { overflowY: 'auto' } }}
							rowKey="id"
							dataSource={query.data.users}
							loading={query.loading}
							onChange={(_, __, sorter, extra) => {
								if (extra.action === 'sort') {
									setOrdering(selectedTableSort(sorter));
									setPage(1);
								}
							}}
							locale={{
								emptyText: (
									<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('admin.noUsers')} />
								)
							}}
							pagination={{
								current: page,
								pageSize,
								total: query.data.total,
								showSizeChanger: true,
								onChange: (next, size) => {
									setPage(next);
									setPageSize(size);
								}
							}}
							columns={[
								{ title: t('admin.username'), dataIndex: 'username', ...sortColumn('username', ordering) },
								{ title: t('admin.displayName'), dataIndex: 'displayName', ...sortColumn('displayName', ordering) },
								{
									title: t('admin.platformRole'),
									...sortColumn('platformRole', ordering),
									dataIndex: 'platformRole',
									render: (value: service.AdminUser['platformRole']) => (
										<StatusTag kind={value === 'admin' ? 'info' : 'neutral'}>
											{t(`admin.role.${value}`)}
										</StatusTag>
									)
								},
								{
									title: t('admin.status'),
									...sortColumn('status', ordering),
									dataIndex: 'status',
									render: (value: service.AdminUser['status']) => (
										<StatusTag kind={value === 'active' ? 'success' : 'neutral'}>
											{t(`admin.status.${value}` as MessageKey)}
										</StatusTag>
									)
								},
								{ title: t('admin.createdAt'), dataIndex: 'createdAt', render: timeText, ...sortColumn('createdAt', ordering) },
								{
									title: t('admin.actions'),
									render: (_, user) => (
										<Button
											disabled={mutation.busy || query.loading}
											onClick={() => {
												mutation.clearError();
												setSelected(user.id);
												setDrawerDirty(false);
											}}
										>
											{t('admin.manage')}
										</Button>
									)
								}
							]}
						/>
					</TablePanel>
				)
			)}
			<Modal
				title={t('admin.create')}
				open={createOpen}
				destroyOnHidden
				confirmLoading={mutation.busy}
				okText={t('admin.create')}
				cancelText={t('admin.cancel')}
				okButtonProps={{ disabled: !validProfile }}
				onOk={() => form.current?.requestSubmit()}
				onCancel={closeCreate}
				closable={!mutation.busy}
				keyboard={!mutation.busy}
				mask={{ closable: !mutation.busy }}
			>
				{mutation.error && <Alert type="error" showIcon title={t(mutation.error)} />}
				<form
					ref={form}
					className="admin-create-form"
					onSubmit={(event) => {
						event.preventDefault();
						if (!validProfile || mutation.busy) return;
						void mutation.run(
							() => service.createUser(profile),
							(value) => {
								setCreateOpen(false);
								setProfile({ username: '', displayName: '' });
								showCode(value);
								changed();
							}
						);
					}}
				>
					<label className="field">
						<span className="field-label">{t('admin.username')}</span>
						<Input
							autoComplete="off"
							required
							pattern="[a-z0-9][a-z0-9._-]{2,63}"
							minLength={3}
							maxLength={64}
							value={profile.username}
							disabled={mutation.busy}
							onChange={(event) =>
								setProfile((value) => ({ ...value, username: event.target.value }))
							}
						/>
					</label>
					<label className="field">
						<span className="field-label">{t('admin.displayName')}</span>
						<Input
							maxLength={200}
							value={profile.displayName}
							disabled={mutation.busy}
							onChange={(event) =>
								setProfile((value) => ({ ...value, displayName: event.target.value }))
							}
						/>
					</label>
				</form>
			</Modal>
			{selected && (
				<UserDrawer
					key={selected}
					id={selected}
					mutation={mutation}
					onClose={closeDrawer}
					onChanged={changed}
					onCode={showCode}
					onDirtyChange={setDrawerDirty}
				/>
			)}
			{visibleCode && (
				<Modal
					title={t('admin.code')}
					open
					destroyOnHidden
					onCancel={() => setCode(null)}
					footer={
						<Button type="primary" onClick={() => setCode(null)}>
							{t('admin.close')}
						</Button>
					}
				>
					{visibleCode && (
						<div className="admin-code-content">
							<strong>{visibleCode.user.username}</strong>
							<Typography.Paragraph className="admin-code" copyable={{ text: visibleCode.code }}>
								{visibleCode.code}
							</Typography.Paragraph>
							<p>
								{t('admin.codeExpires')} {timeText(visibleCode.expiresAt)}
							</p>
							<Alert type="info" showIcon title={t('admin.codeHint')} />
						</div>
					)}
				</Modal>
			)}
		</div>
	);
}
