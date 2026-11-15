// Layout and component calls from robot-platform/features/api/ApiPage.tsx.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Collapse, Input, Select, Table } from 'antd';
import { TablePanel, Tag } from '../components/Widgets';
import PageLoading from '../components/PageLoading';
import { usePageHeader } from '../components/PageHeader';
import { api } from '../lib/api/client';
import { useAdminQuery } from '../lib/admin/requests';
import { t } from '../lib/i18n';
import {
	apiAuthentication,
	apiGroup,
	apiVersionLabel,
	apiVersionDescription,
	filterApiOperations,
	parseApiCatalog,
	type ApiOperation
} from '../lib/apiCatalog';
import '../styles/api.css';

export default function ApiPage() {
	const load = useCallback(
		async (signal: AbortSignal) =>
			parseApiCatalog((await api<unknown>('GET', '/api/v1/openapi', { signal })).data),
		[]
	);
	const result = useAdminQuery(load);
	const catalog = result.data;
	const [query, setQuery] = useState('');
	const [group, setGroup] = useState('');
	const [method, setMethod] = useState('');
	const [copied, setCopied] = useState<Record<string, boolean>>({});
	const [copyError, setCopyError] = useState('');
	const [copyStatus, setCopyStatus] = useState('');
	const copyTimers = useRef(new Map<string, ReturnType<typeof setTimeout>>());
	const downloads = useRef(new Map<string, ReturnType<typeof setTimeout>>());
	const alive = useRef(true);
	const copyGeneration = useRef(0);
	useEffect(() => {
		alive.current = true;
		const timers = copyTimers.current;
		const blobs = downloads.current;
		return () => {
			alive.current = false;
			timers.forEach(clearTimeout);
			timers.clear();
			blobs.forEach((timer, url) => {
				clearTimeout(timer);
				URL.revokeObjectURL(url);
			});
			blobs.clear();
		};
	}, []);
	const operations = useMemo(
		() => filterApiOperations(catalog?.operations ?? [], query, group, method),
		[catalog, query, group, method]
	);
	const groups = useMemo(
		() => [
			...new Map(
				(catalog?.operations ?? []).map((operation) => {
					const value = apiGroup(operation);
					return [value.id, { value: value.id, label: value.name }];
				})
			).values()
		],
		[catalog]
	);
	const methods = useMemo(
		() => [...new Set((catalog?.operations ?? []).map((operation) => operation.method))].sort(),
		[catalog]
	);
	function resetFilters() {
		setQuery('');
		setGroup('');
		setMethod('');
	}
	function downloadContract() {
		if (!catalog) return;
		const url = URL.createObjectURL(
			new Blob([catalog.source], { type: 'application/json;charset=utf-8' })
		);
		const anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = 'fuyao-manager-openapi.json';
		anchor.click();
		downloads.current.set(
			url,
			setTimeout(() => {
				URL.revokeObjectURL(url);
				downloads.current.delete(url);
			}, 1000)
		);
	}
	async function copyPath(operation: ApiOperation) {
		const generation = ++copyGeneration.current;
		const previous = copyTimers.current.get(operation.id);
		if (previous) clearTimeout(previous);
		setCopied((current) => ({ ...current, [operation.id]: false }));
		setCopyStatus('');
		try {
			await navigator.clipboard.writeText(operation.path);
			if (!alive.current || generation !== copyGeneration.current) return;
			setCopied((current) => ({ ...current, [operation.id]: true }));
			setCopyError('');
			setCopyStatus(t('api.copied'));
			copyTimers.current.set(
				operation.id,
				setTimeout(() => {
					setCopied((current) => ({ ...current, [operation.id]: false }));
					copyTimers.current.delete(operation.id);
				}, 2000)
			);
		} catch {
			if (!alive.current || generation !== copyGeneration.current) return;
			// The visible role=alert region announces failures; the sr-only
			// status stays reserved for success so errors are not announced twice.
			setCopyError(t('api.copyError'));
			setCopyStatus('');
		}
	}
	const hasFilter = Boolean(query || group || method);
	const searchInput = (
		<Input
			aria-label={t('api.search')}
			placeholder={t('api.searchPlaceholder')}
			value={query}
			allowClear
			onChange={(event) => setQuery(event.target.value)}
		/>
	);
	const filterControls = (
		<>
			<Select
				aria-label={t('api.group')}
				value={group}
				options={[{ value: '', label: t('api.allGroups') }, ...groups]}
				onChange={setGroup}
			/>
			<Select
				aria-label={t('api.method')}
				value={method}
				options={[
					{ value: '', label: t('api.allMethods') },
					...methods.map((value) => ({ value, label: value }))
				]}
				onChange={setMethod}
			/>
			<Button onClick={resetFilters} disabled={!hasFilter}>
				{t('api.clear')}
			</Button>
		</>
	);
	const header = usePageHeader({
		content: (
			<div className="topbar-api-content">
				{searchInput}
				{filterControls}
				{catalog && (
					<span className="topbar-api-count" role="status">
						{t('api.count', { total: catalog.operations.length, shown: operations.length })}
					</span>
				)}
				{catalog && (
					<span className="topbar-api-version">
						{t('api.contractVersion')} <span className="version-text">{catalog.version}</span> ·
						OpenAPI <span className="version-text">{catalog.openapi}</span>
					</span>
				)}
			</div>
		),
		actions: (
			<>
				<Button onClick={result.reload} loading={result.loading}>
					{t('admin.refresh')}
				</Button>
				<Button onClick={downloadContract} disabled={!catalog}>
					{t('api.download')}
				</Button>
			</>
		)
	});
	return (
		<div className="page-stack page-stack--fill api-catalog">
			<span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
				{copyStatus}
			</span>
			<TablePanel className="fill-height">
				<div className="panel-body api-catalog-body">
					{!header && (
						<div className="api-catalog-filters">
							{searchInput}
							{filterControls}
							<Button onClick={result.reload} loading={result.loading}>
								{t('admin.refresh')}
							</Button>
							<Button onClick={downloadContract} disabled={!catalog}>
								{t('api.download')}
							</Button>
						</div>
					)}
					{result.error && (
						<Alert
							type="error"
							showIcon
							title={t(result.error)}
							action={<Button onClick={result.reload}>{t('common.retry')}</Button>}
						/>
					)}
					{result.loading && !catalog && <PageLoading label={t('api.loading')} />}
					{catalog && operations.length === 0 ? (
						<p className="subtle">{t('api.empty')}</p>
					) : (
						catalog && (
							<Collapse
								className="api-catalog-list"
								bordered={false}
								expandIconPlacement="start"
								items={operations.map((operation) => ({
									key: operation.id,
									label: (
										<span className="api-row">
											<span className="api-route-line">
												<Tag
													className="api-method"
													tone={
														operation.method === 'GET'
															? 'success'
															: operation.method === 'POST'
																? 'info'
																: ['PUT', 'PATCH'].includes(operation.method)
																	? 'warning'
																	: operation.method === 'DELETE'
																		? 'danger'
																		: 'neutral'
													}
												>
													{operation.method}
												</Tag>
												<code className="api-path">{operation.path}</code>
												<span className="api-row-group">{apiGroup(operation).name}</span>
											</span>
											<span className="api-row-meta">
												<span className="api-version-label version-text">
													{apiVersionLabel(operation)}
												</span>
												<span className="api-summary">{operation.summary || t('api.details')}</span>
											</span>
										</span>
									),
									children: (
										<div className="api-operation-detail">
											<div className="api-operation-heading">
												<code className="api-path">
													{operation.method} {operation.path}
												</code>
												<Button size="small" onClick={() => void copyPath(operation)}>
													{copied[operation.id] ? t('api.copied') : t('api.copyPath')}
												</Button>
											</div>
											{copyError && <p role="alert">{copyError}</p>}
											{operation.description && (
												<p className="api-description">{operation.description}</p>
											)}
											<section>
												<h3>{t('api.version')}</h3>
												<p>
													<span className="api-version-label version-text">
														{apiVersionLabel(operation)}
													</span>
												</p>
												<p className="api-description">{apiVersionDescription(operation)}</p>
											</section>
											<section>
												<h3>{t('api.authentication')}</h3>
												<p>{apiAuthentication(operation, catalog)}</p>
												{operation.permissions.length > 0 && (
													<p className="api-permissions">
														{operation.permissions.map((scope) => (
															<code key={scope}>{scope}</code>
														))}
													</p>
												)}
												{operation.permissionNote && (
													<p className="api-description">{operation.permissionNote}</p>
												)}
											</section>
											<section>
												<h3>{t('api.parameters')}</h3>
												{operation.parameters.length === 0 ? (
													<p className="subtle">{t('api.noParameters')}</p>
												) : (
													<Table
														size="small"
														pagination={false}
														rowKey={(parameter) => `${parameter.in}:${parameter.name}`}
														dataSource={operation.parameters}
														columns={[
															{
																title: t('api.parameter'),
																dataIndex: 'name',
																render: (name: string) => <code>{name}</code>
															},
															{ title: t('api.location'), dataIndex: 'in' },
															{
																title: t('api.required'),
																dataIndex: 'required',
																render: (required: boolean) => t(required ? 'api.yes' : 'api.no')
															},
															{
																title: t('api.constraints'),
																render: (_, parameter) => (
																	<>
																		{parameter.description}
																		<pre className="api-schema">
																			{JSON.stringify(parameter.schema ?? {}, null, 2)}
																		</pre>
																	</>
																)
															}
														]}
													/>
												)}
											</section>
											<section>
												<h3>{t('api.body')}</h3>
												{operation.requestBody ? (
													<pre className="api-schema" tabIndex={0} aria-label={t('api.body')}>
														{JSON.stringify(operation.requestBody, null, 2)}
													</pre>
												) : (
													<p className="subtle">{t('api.noBody')}</p>
												)}
											</section>
											<section>
												<h3>{t('api.responses')}</h3>
												{Object.entries(operation.responses).map(([status, response]) => (
													<div key={status}>
														<h4>
															<code>{status}</code>{' '}
															{typeof response.description === 'string' ? response.description : ''}
														</h4>
														<pre
															className="api-schema"
															tabIndex={0}
															aria-label={t('api.responseStatus', { status })}
														>
															{JSON.stringify(response, null, 2)}
														</pre>
													</div>
												))}
											</section>
										</div>
									)
								}))}
							/>
						)
					)}
				</div>
			</TablePanel>
		</div>
	);
}
