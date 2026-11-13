// Search/filter/header/table composition from robot-platform's LogsPage.
import { useCallback, useState, type ComponentProps } from 'react';
import { Alert, Button, DatePicker, Descriptions, Empty, Input, Select, Space } from 'antd';
import { RefreshCw } from 'lucide-react';
import DataTable from '../components/DataTable';
import PageLoading from '../components/PageLoading';
import StatusTag from '../components/StatusTag';
import { TablePanel } from '../components/Panel';
import { usePageHeader } from '../components/PageHeader';
import { useAdminQuery } from '../lib/admin/requests';
import { auditLabels, operationText, timeText } from '../lib/admin/presentation';
import { t } from '../lib/i18n';
import { listAudit, queryString, type AuditEvent } from '../lib/api/admin';

export default function LogsPage() {
	const [search, setSearch] = useState('');
	const [q, setQuery] = useState('');
	const [actor, setActor] = useState('');
	const [operation, setOperation] = useState('');
	const [outcome, setOutcome] = useState('');
	const [range, setRange] = useState<ComponentProps<typeof DatePicker.RangePicker>['value']>(null);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(50);
	const params = queryString({
		q,
		actor,
		operation,
		outcome,
		page,
		pageSize,
		from: range?.[0]?.toISOString(),
		until: range?.[1]?.toISOString()
	});
	const load = useCallback((signal: AbortSignal) => listAudit(params, signal), [params]);
	const query = useAdminQuery(load);
	const operations = [...new Set([...Object.keys(auditLabels), ...(query.data?.operations ?? [])])];
	const content = (
		<Space size={8}>
			<Input.Search
				aria-label={t('audit.search')}
				placeholder={t('audit.search')}
				className="admin-search"
				value={search}
				maxLength={128}
				allowClear
				onChange={(event) => setSearch(event.target.value)}
				onSearch={(value) => {
					setQuery(value);
					setPage(1);
				}}
			/>
			<Input
				aria-label={t('audit.actor')}
				placeholder={t('audit.actor')}
				className="admin-filter"
				value={actor}
				maxLength={128}
				allowClear
				onChange={(event) => {
					setActor(event.target.value);
					setPage(1);
				}}
			/>
			<Select
				aria-label={t('audit.operation')}
				placeholder={t('audit.operation')}
				className="admin-filter"
				value={operation || undefined}
				allowClear
				showSearch
				optionFilterProp="label"
				options={operations.map((value) => ({ value, label: operationText(value) }))}
				onChange={(value) => {
					setOperation(value ?? '');
					setPage(1);
				}}
			/>
			<Select
				aria-label={t('audit.outcome')}
				placeholder={t('audit.outcome')}
				className="admin-filter"
				value={outcome || undefined}
				allowClear
				options={[
					{ value: 'success', label: t('audit.success') },
					{ value: 'failure', label: t('audit.failure') }
				]}
				onChange={(value) => {
					setOutcome(value ?? '');
					setPage(1);
				}}
			/>
			<DatePicker.RangePicker
				aria-label={t('audit.timeRange')}
				showTime
				value={range}
				onChange={(value) => {
					setRange(value);
					setPage(1);
				}}
			/>
		</Space>
	);
	const actions = (
		<Button icon={<RefreshCw size={16} />} disabled={query.loading} onClick={query.reload}>
			{t('admin.refresh')}
		</Button>
	);
	const inHeader = usePageHeader({ content, actions });
	return (
		<div className="page-stack">
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
			{query.loading && !query.data ? (
				<PageLoading label={t('common.loading')} />
			) : (
				query.data && (
					<TablePanel>
						<DataTable<AuditEvent>
							rowKey="id"
							dataSource={query.data.entries}
							loading={query.loading}
							scroll={{ x: 'max-content' }}
							locale={{
								emptyText: (
									<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('audit.empty')} />
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
							expandable={{
								expandedRowRender: (event) => (
									<Descriptions
										size="small"
										column={1}
										items={[
											{ key: 'id', label: t('audit.eventId'), children: <code>{event.id}</code> },
											{
												key: 'operation',
												label: t('audit.operation'),
												children: <code>{event.operation}</code>
											},
											{
												key: 'actor',
												label: t('audit.actorId'),
												children: event.actorId ? (
													<code>{event.actorId}</code>
												) : (
													t('audit.anonymous')
												)
											},
											{
												key: 'route',
												label: t('audit.route'),
												children: (
													<code>
														{event.method} {event.route}
													</code>
												)
											},
											{
												key: 'site',
												label: t('admin.sites'),
												children: event.siteId ? <code>{event.siteId}</code> : t('admin.never')
											},
											{ key: 'peer', label: t('audit.peer'), children: <code>{event.peerIp}</code> }
										]}
									/>
								)
							}}
							columns={[
								{ title: t('audit.time'), dataIndex: 'createdAt', render: timeText },
								{
									title: t('audit.actor'),
									dataIndex: 'actorName',
									render: (value: string) => value || t('audit.anonymous')
								},
								{ title: t('audit.operation'), dataIndex: 'operation', render: operationText },
								{
									title: t('audit.target'),
									dataIndex: 'resourceId',
									render: (value: string) => <code>{value}</code>
								},
								{
									title: t('audit.outcome'),
									dataIndex: 'outcome',
									render: (value: AuditEvent['outcome']) => (
										<StatusTag kind={value === 'success' ? 'success' : 'danger'}>
											{t(value === 'success' ? 'audit.success' : 'audit.failure')}
										</StatusTag>
									)
								},
								{ title: 'HTTP', dataIndex: 'status' }
							]}
						/>
					</TablePanel>
				)
			)}
		</div>
	);
}
