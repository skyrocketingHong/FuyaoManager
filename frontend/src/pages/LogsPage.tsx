// Generic table, filters and inline detail anatomy from robot-platform's LogsPage.
import { useCallback, useEffect, useState, type ComponentProps } from 'react';
import { Alert, DatePicker, Input, Select, Space } from 'antd';
import ActionButton from '../components/ActionButton';
import PageLoading from '../components/PageLoading';
import { DataTable, TablePanel, Tag, VersionText, timeText } from '../components/Widgets';
import { usePageHeader } from '../components/PageHeader';
import { useAdminQuery } from '../lib/admin/requests';
import { auditLabels, changeValueText, operationText } from '../lib/admin/presentation';
import { t } from '../lib/i18n';
import { listAudit, queryString, type AuditEvent } from '../lib/api/admin';
import { selectedTableSort, sortColumn, type TableSort } from '../lib/table-sort';
import '../styles/logs.css';

function recordDetail(event: AuditEvent) {
	const field = event.details?.field;
	const facts = [
		[t('admin.username'), event.details?.targetUsername],
		...(field
			? [
					[t('audit.before'), changeValueText(field, event.details?.before)],
					[t('audit.after'), changeValueText(field, event.details?.after)]
				]
			: []),
		[t('audit.sessionId'), event.details?.sessionId],
		[t('admin.passkeyId'), event.details?.passkeyId],
		[t('audit.eventId'), event.id],
		[t('audit.operation'), event.operation],
		[t('audit.actorId'), event.actorId],
		[t('admin.sites'), event.siteId]
	].filter(([, value]) => value !== undefined && value !== null && value !== '');
	return (
		<div className="log-summary">
			{field ? (
				<span>
					{changeValueText(field, event.details?.before)} →{' '}
					{changeValueText(field, event.details?.after)}
				</span>
			) : (
				<span>
					{event.method} {event.route}
				</span>
			)}
			<span className="subtle">HTTP {event.status}</span>
			<details className="log-record-detail">
				<summary>{t('audit.recordDetail')}</summary>
				<dl>
					{facts.map(([label, value]) => (
						<div key={label}>
							<dt>{label}</dt>
							<dd>
								<VersionText>{value}</VersionText>
							</dd>
						</div>
					))}
					<div>
						<dt>{t('audit.route')}</dt>
						<dd>
							<VersionText>
								{event.method} {event.route}
							</VersionText>
						</dd>
					</div>
				</dl>
			</details>
		</div>
	);
}

export default function LogsPage() {
	const [search, setSearch] = useState('');
	const [q, setQuery] = useState('');
	const [actorInput, setActorInput] = useState('');
	const [actor, setActor] = useState('');
	const [operation, setOperation] = useState('');
	const [outcome, setOutcome] = useState('');
	const [range, setRange] = useState<ComponentProps<typeof DatePicker.RangePicker>['value']>(null);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(50);
	const [ordering, setOrdering] = useState<TableSort>();
	useEffect(() => {
		if (search === q && actorInput === actor) return;
		const timer = setTimeout(() => {
			setQuery(search);
			setActor(actorInput);
			setPage(1);
		}, 250);
		return () => clearTimeout(timer);
	}, [search, actorInput, q, actor]);
	const params = queryString({
		q,
		actor,
		operation,
		outcome,
		page,
		pageSize,
		sort: ordering?.field,
		order: ordering?.direction,
		from: range?.[0]?.toISOString(),
		until: range?.[1]?.toISOString()
	});
	const load = useCallback((signal: AbortSignal) => listAudit(params, signal), [params]);
	const query = useAdminQuery(load);
	const entries = query.data?.entries ?? [];
	const operations = [...new Set([...Object.keys(auditLabels), ...(query.data?.operations ?? [])])];
	const filters = (
		<>
			<Select
				aria-label={t('audit.operation')}
				placeholder={t('audit.operation')}
				allowClear
				showSearch
				optionFilterProp="label"
				value={operation || undefined}
				options={operations.map((value) => ({ value, label: operationText(value) }))}
				onChange={(value) => {
					setOperation(value ?? '');
					setPage(1);
				}}
			/>
			<Select
				aria-label={t('audit.outcome')}
				placeholder={t('audit.outcome')}
				allowClear
				value={outcome || undefined}
				options={[
					{ value: 'success', label: t('audit.success') },
					{ value: 'failure', label: t('audit.failure') }
				]}
				onChange={(value) => {
					setOutcome(value ?? '');
					setPage(1);
				}}
			/>
			<Input
				aria-label={t('audit.actor')}
				placeholder={t('audit.actor')}
				allowClear
				maxLength={128}
				value={actorInput}
				onChange={(event) => setActorInput(event.target.value)}
				onPressEnter={() => {
					setActor(actorInput);
					setPage(1);
				}}
			/>
			<Input
				aria-label={t('audit.search')}
				placeholder={t('audit.search')}
				allowClear
				maxLength={128}
				value={search}
				onChange={(event) => setSearch(event.target.value)}
				onPressEnter={() => {
					setQuery(search);
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
			<Select
				aria-label={t('audit.pageSize')}
				value={pageSize}
				onChange={(value) => {
					setPageSize(value);
					setPage(1);
				}}
				options={[25, 50, 100, 200].map((value) => ({
					value,
					label: t('audit.pageRows', { count: value })
				}))}
			/>
		</>
	);
	const actions = (
		<ActionButton onClick={query.reload} loading={query.loading}>
			{t('admin.refresh')}
		</ActionButton>
	);
	const header = usePageHeader({
		content: <div className="topbar-log-content">{filters}</div>,
		actions
	});
	return (
		<div className="page-stack page-stack--fill"><TablePanel className="fill-height">
			{!header && (
				<div className="panel-body">
					<Space wrap>
						{filters}
						{actions}
					</Space>
				</div>
			)}
			{query.error && (
				<Alert
					type="error"
					showIcon
					title={t(query.error)}
					action={<ActionButton onClick={query.reload}>{t('common.retry')}</ActionButton>}
				/>
			)}
			<DataTable<AuditEvent>
				className="operation-logs-table"
				manualSorting
				styles={{ content: { overflowY: 'auto' } }}
				scroll={{ x: 1540 }}
				rowKey="id"
				dataSource={entries}
				locale={{ emptyText: query.loading ? <PageLoading spec="section" label={t('common.loading')} /> : t('audit.empty') }}
				onChange={(_, __, sorter, extra) => {
					if (extra.action === 'sort') {
						setOrdering(selectedTableSort(sorter));
						setPage(1);
					}
				}}
				pagination={{
					current: page,
					pageSize,
					total: query.data?.total ?? 0,
					showSizeChanger: false,
					onChange: (next) => setPage(next)
				}}
				columns={[
					{ title: t('audit.time'), dataIndex: 'createdAt', width: 164, ...sortColumn('createdAt', ordering), render: timeText },
					{ title: t('audit.actor'), dataIndex: 'actorName', width: 140, ...sortColumn('actorName', ordering), render: (value: string) => <VersionText>{value || t('audit.anonymous')}</VersionText> },
					{ title: t('audit.operation'), dataIndex: 'operation', width: 160, ...sortColumn('operation', ordering), render: (value: string) => <span className="log-operation">{operationText(value)}</span> },
					{ title: t('audit.target'), width: 190, ...sortColumn('resourceId', ordering), render: (_, event) => <Space orientation="vertical" size={0}>
						{event.details?.targetUsername && <VersionText>{event.details.targetUsername}</VersionText>}
						<VersionText>{event.resourceId || t('admin.never')}</VersionText>
					</Space> },
					{ title: t('audit.outcome'), dataIndex: 'outcome', width: 100, ...sortColumn('outcome', ordering), render: (value: string) => <Tag tone={value === 'success' ? 'success' : 'danger'}>{t(value === 'success' ? 'audit.success' : 'audit.failure')}</Tag> },
					{ title: t('audit.recordDetail'), render: (_, event) => recordDetail(event) },
					{ title: t('audit.peer'), dataIndex: 'peerIp', width: 175, ...sortColumn('peerIp', ordering), render: (value: string) => <VersionText className="log-address">{value || t('admin.never')}</VersionText> }
				]}
			/>
		</TablePanel></div>
	);
}
