import dayjs from 'dayjs';
import { queryChoice, queryValue, usePageQuery } from '../lib/navigation/query';
// Generic table, filters and inline detail anatomy from robot-platform's LogsPage.
import { useCallback, useEffect, useState } from 'react';
import { Alert, DatePicker, Input, Select, Space } from 'antd';
import { RefreshButton, RetryButton } from '../components/ActionButton';
import PageLoading from '../components/PageLoading';
import { DataTable, TablePanel, Tag, VersionText, timeText } from '../components/Widgets';
import { usePageHeader } from '../components/PageHeader';
import { useAdminBatches } from '../lib/admin/requests';
import { auditLabels, changeValueText, operationText } from '../lib/admin/presentation';
import { t } from '../lib/i18n';
import { listAudit, queryString, type AuditEvent, type AuditPage } from '../lib/api/admin';
import { selectedTableSort, sortColumn, type TableSort } from '../lib/table-sort';
import '../styles/logs.css';

const entriesOf = (data: AuditPage) => data.entries;
const entryKey = (event: AuditEvent) => event.id;

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
	const [url, setURL] = usePageQuery();
	const q = (queryValue(url, 'q') ?? '').slice(0, 128);
	const actor = (queryValue(url, 'actor') ?? '').slice(0, 128);
	const [search, setSearch] = useState(q);
	const [actorInput, setActorInput] = useState(actor);
	const operation = (queryValue(url, 'operation') ?? '').slice(0, 128);
	const outcome = queryChoice(url, 'outcome', ['success', 'failure', ''], '');
	const from = dayjs(queryValue(url, 'from') ?? '');
	const until = dayjs(queryValue(url, 'until') ?? '');
	const range: [typeof from, typeof until] | null = from.isValid() && until.isValid() && until.isAfter(from) ? [from, until] : null;
	const pageSize = Number(queryChoice(url, 'size', ['25', '50', '100', '200'], '50'));
	const field = queryValue(url, 'sort');
	const direction = queryValue(url, 'order');
	const ordering: TableSort | undefined = field && ['createdAt', 'actorName', 'operation', 'resourceId', 'outcome', 'status', 'peerIp'].includes(field) &&
		(direction === 'asc' || direction === 'desc') ? { field, direction } : undefined;
	useEffect(() => setSearch(q), [q]);
	useEffect(() => setActorInput(actor), [actor]);
	useEffect(() => {
		if (search === q && actorInput === actor) return;
		const timer = setTimeout(() => {
			setURL({ q: search, actor: actorInput }, true);
		}, 250);
		return () => clearTimeout(timer);
	}, [search, actorInput, q, actor, setURL]);
	const params = queryString({
		q,
		actor,
		operation,
		outcome,
		pageSize,
		sort: ordering?.field,
		order: ordering?.direction,
		from: range?.[0]?.toISOString(),
		until: range?.[1]?.toISOString()
	});
	const load = useCallback((page: number, signal: AbortSignal) => listAudit(`${params}&page=${page}`, signal), [params]);
	const query = useAdminBatches(params, load, entriesOf, entryKey);
	const entries = query.rows;
	const operations = [...new Set([...Object.keys(auditLabels), ...(query.data?.operations ?? [])])];
	const filters = (
		<>
			<Select
				aria-label={t('audit.operation')}
				placeholder={t('audit.operation')}
				allowClear
				showSearch={{ optionFilterProp: 'label' }}
				value={operation || undefined}
				options={operations.map((value) => ({ value, label: operationText(value) }))}
				onChange={(value) => {
					setURL({ operation: value });
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
					setURL({ outcome: value });
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
					setURL({ q: search, actor: actorInput });
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
					setURL({ q: search, actor: actorInput });
				}}
			/>
			<DatePicker.RangePicker
				aria-label={t('audit.timeRange')}
				showTime
				value={range}
				onChange={(value) => {
					setURL({ from: value?.[0]?.toISOString(), until: value?.[1]?.toISOString() });
				}}
			/>
			<Select
				aria-label={t('audit.pageSize')}
				value={pageSize}
				onChange={(value) => {
					setURL({ size: value === 50 ? null : String(value) });
				}}
				options={[25, 50, 100, 200].map((value) => ({
					value,
					label: t('audit.pageRows', { count: value })
				}))}
			/>
		</>
	);
	const actions = (
		<RefreshButton onClick={query.reload} loading={query.loading} />
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
					action={<RetryButton onClick={query.retry} />}
				/>
			)}
			<DataTable<AuditEvent>
				className="operation-logs-table"
				scrollLabel={t('nav.logs')}
				manualSorting
				styles={{ content: { overflowY: 'auto' } }}
				scroll={{ x: 1540 }}
				rowKey="id"
				dataSource={entries}
				locale={{ emptyText: query.loading ? <PageLoading spec="section" label={t('common.loading')} /> : t('audit.empty') }}
				onChange={(_, __, sorter, extra) => {
					if (extra.action === 'sort') {
						const next = selectedTableSort(sorter);
						setURL({ sort: next?.field, order: next?.direction });
					}
				}}
				pagination={false}
				loadMore={{
					active: query.hasMore && !query.error,
					loading: query.loading,
					onLoad: query.loadMore,
					scrollLabel: t('nav.logs')
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
