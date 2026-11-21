import { queryChoice, usePageQuery } from '../../lib/navigation/query';
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { Alert, App, Button, Descriptions, Select, Space, Typography } from 'antd';
import { useParams } from 'react-router-dom';
import { Panel, TablePanel } from '../../components/Panel';
import DataTable from '../../components/DataTable';
import StatusTag from '../../components/StatusTag';
import CaddyIcon from '../../components/CaddyIcon';
import { useCurrentSite } from '../../components/layout/SiteBoundary';
import { PageTabs, usePageHeader } from '../../components/PageHeader';
import PageLoading from '../../components/PageLoading';
import { RefreshButton, RetryButton } from '../../components/ActionButton';
import { useAdminMutation, useAdminQuery } from '../../lib/admin/requests';
import { timeText } from '../../lib/admin/presentation';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { currentAuthGeneration } from '../../lib/api/client';
import {
	changeCaddyConfig,
	getCaddyConfig,
	caddyReasonKey,
	preservedCaddySettings,
	type CaddyEnvelope,
	type CaddyHistory,
	type CaddySettings
} from '../../lib/api/caddy';
import { loadSiteList } from '../../lib/sites/site-list';
import { sessionSnapshot, subscribeSession } from '../../lib/session';
import { t, type MessageKey } from '../../lib/i18n';

const equal = (first: CaddySettings, second: CaddySettings) =>
	first.compression === second.compression &&
	first.cacheControl === second.cacheControl &&
	first.securityHeaders === second.securityHeaders;
type Editor = {
	scope: string;
	settings: CaddySettings;
	baseline: CaddySettings;
	etag: string | null;
};

export default function CaddySitePage() {
	const { siteId = '' } = useParams();
	return <CaddySiteEditor key={siteId} siteId={siteId} />;
}

function CaddySiteEditor({ siteId }: { siteId: string }) {
	const site = useCurrentSite();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const scope = `${currentAuthGeneration()}:${siteId}`;
	const { modal, message } = App.useApp();
	const confirmation = useRef<{ destroy: () => void } | null>(null);
	useEffect(() => () => confirmation.current?.destroy(), []);
	const load = useCallback((signal: AbortSignal) => getCaddyConfig(siteId, signal), [siteId]);
	const query = useAdminQuery(load);
	const mutation = useAdminMutation();
	const [view, setView] = useState<{ scope: string; response: CaddyEnvelope } | null>(null);
	const [editor, setEditor] = useState<Editor | null>(null);
	useEffect(() => {
		const response = query.data;
		if (!response || response.data.siteId !== siteId) return;
		setView({ scope, response });
		setEditor((previous) =>
			previous?.scope === scope && !equal(previous.settings, previous.baseline)
				? previous
				: {
						scope,
						settings: { ...(response.data.management?.settings ?? preservedCaddySettings) },
						baseline: { ...(response.data.management?.settings ?? preservedCaddySettings) },
						etag: response.etag
					}
		);
	}, [query.data, scope, siteId]);
	const response = view?.scope === scope ? view.response : null;
	const config = response?.data;
	const draft = editor?.scope === scope ? editor : null;
	const dirty = !!draft && !equal(draft.settings, draft.baseline);
	const owner = session.user?.platformRole === 'admin' && site?.role === 'owner';
	const capabilities = config?.capabilities;
	const busy = mutation.busy || query.loading;
	const blocked = busy || !!query.error;
	const managed = config?.management;
	const pending = !!managed && managed.revision !== managed.appliedRevision;
	const validated = !!managed && managed.validatedRevision === managed.revision;
	const etag = response?.etag;
	const unavailable = !owner
		? 'caddy.ownerRequired'
		: config?.unavailableReason
			? caddyReasonKey(config.unavailableReason)
			: config && !capabilities
				? 'caddy.managementUnavailable'
				: null;
	useUnsavedGuard(dirty || mutation.busy);
	function install(saved: CaddyEnvelope) {
		if (saved.data.siteId !== siteId) throw new Error('Unexpected Caddy site response');
		setView({ scope, response: saved });
		const settings = { ...(saved.data.management?.settings ?? preservedCaddySettings) };
		setEditor({ scope, settings, baseline: settings, etag: saved.etag });
		void loadSiteList(true);
	}
	async function perform(
		action: 'draft' | 'convert' | 'validate' | 'deploy' | 'rollback',
		revision?: number
	) {
		const match = action === 'draft' ? draft?.etag : etag;
		if (!match || !owner || blocked) return;
		await mutation.run(
			() =>
				changeCaddyConfig(
					siteId,
					action,
					match,
					action === 'draft'
						? { settings: draft!.settings }
						: action === 'rollback'
							? { revision }
							: {}
				),
			(saved) => {
				install(saved);
				void message.success(t('common.saved'));
			}
		);
	}
	function confirm(action: 'convert' | 'deploy' | 'rollback', revision?: number) {
		confirmation.current = modal.confirm({
			title: t(
				action === 'convert'
					? 'caddy.convertTitle'
					: action === 'rollback'
						? 'caddy.rollbackTitle'
						: 'caddy.deployTitle'
			),
			content: t(
				action === 'convert'
					? 'caddy.convertConsequence'
					: action === 'rollback'
						? 'caddy.rollbackConsequence'
						: 'caddy.deployConsequence',
				{ revision: revision ?? managed?.revision ?? '' }
			),
			okText: t('admin.confirm'),
			cancelText: t('admin.cancel'),
			okButtonProps: { danger: action !== 'convert' },
			onOk: () => perform(action, revision)
		});
	}
	function refresh() {
		const reload = () => {
			setEditor((previous) =>
				previous ? { ...previous, settings: { ...previous.baseline } } : previous
			);
			mutation.clearError();
			query.reload();
		};
		if (dirty)
			confirmation.current = modal.confirm({
				title: t('draft.leaveTitle'),
				content: t('draft.leaveBody'),
				okText: t('draft.leaveConfirm'),
				cancelText: t('draft.leaveCancel'),
				onOk: reload
			});
		else reload();
	}
	const actions = (
		<Space size={8}>
			<RefreshButton onClick={refresh} disabled={busy} />
			{owner && capabilities?.convert && (
				<Button type="primary" onClick={() => confirm('convert')} disabled={blocked || !etag}>
					{t('caddy.convert')}
				</Button>
			)}
			{owner && capabilities?.edit && (
				<Button
					type="primary"
					onClick={() => void perform('draft')}
					disabled={blocked || !dirty || !draft?.etag}
				>
					{t('caddy.saveDraft')}
				</Button>
			)}
			{owner && capabilities?.validate && (
				<Button
					onClick={() => void perform('validate')}
					disabled={blocked || dirty || !pending || !etag}
				>
					{t('caddy.validate')}
				</Button>
			)}
			{owner && capabilities?.deploy && (
				<Button
					danger
					onClick={() => confirm('deploy')}
					disabled={blocked || dirty || !pending || !validated || !etag}
				>
					{t('caddy.deploy')}
				</Button>
			)}
		</Space>
	);
	const [params, setParams] = usePageQuery();
	const tab = managed
		? queryChoice(params, 'tab', ['configuration', 'history'], 'configuration')
		: 'configuration';
	const content = (
		<Space size={8} wrap>
			{managed && (
				<PageTabs
					value={tab}
					ariaLabel={t('nav.caddy')}
					options={[
						{ value: 'configuration', label: t('caddy.configTab') },
						{ value: 'history', label: t('caddy.history') }
					]}
					onChange={(value) => setParams({ tab: value === 'configuration' ? null : value })}
				/>
			)}
			<CaddyIcon />
			<span>
				{t(
					config?.managementMode === 'platform'
						? 'sites.platformManagement'
						: 'sites.caddyManagement'
				)}
			</span>
			{managed && (
				<StatusTag kind={pending ? (validated ? 'info' : 'warning') : 'success'}>
					{t(pending ? (validated ? 'caddy.validated' : 'caddy.draft') : 'caddy.applied')} #
					{managed.revision}
				</StatusTag>
			)}
		</Space>
	);
	const header = usePageHeader({ content, actions });
	const definition = config?.definition;
	const target = definition?.redirect?.location;
	const targetBase = target?.split('{')[0];
	const options: {
		field: keyof CaddySettings;
		label: MessageKey;
		values: { value: string; label: string }[];
	}[] = [
		{
			field: 'compression',
			label: 'caddy.compression',
			values: (['preserve', 'none', 'gzip', 'zstd', 'gzip_zstd'] as const).map((value) => ({
				value,
				label: t(`caddy.compression.${value}` as MessageKey)
			}))
		},
		{
			field: 'cacheControl',
			label: 'caddy.cacheControl',
			values: (['preserve', 'no_store'] as const).map((value) => ({
				value,
				label: t(value === 'preserve' ? 'caddy.preserve' : 'caddy.noStore')
			}))
		},
		{
			field: 'securityHeaders',
			label: 'caddy.securityHeaders',
			values: (['preserve', 'baseline'] as const).map((value) => ({
				value,
				label: t(value === 'preserve' ? 'caddy.preserve' : 'caddy.baselineHeaders')
			}))
		}
	];
	return (
		<div className="page-stack">
			{!header && (
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
					action={<RetryButton onClick={refresh} />}
				/>
			)}
			{mutation.error && <Alert type="error" showIcon title={t(mutation.error)} />}
			{!config && query.loading && <PageLoading label={t('common.loading')} />}
			{config && (
				<>
					{!config.present && <Alert type="warning" showIcon title={t('caddy.missing')} />}
					{unavailable && <Alert type="info" showIcon title={t(unavailable)} />}
					{dirty && managed && draft?.etag !== etag && (
						<Alert type="warning" showIcon title={t('caddy.draftConflict')} />
					)}
					{tab === 'configuration' && (
						<div className="workspace-grid">
							<Panel title={site?.name} className={managed && draft ? 'workspace-main' : undefined}>
								<Descriptions
									column={1}
									items={[
										{
											key: 'kind',
											label: t('caddy.kind'),
											children: t(
												config.kind === 'redirect'
													? 'caddy.redirect'
													: config.kind === 'static'
														? 'caddy.static'
														: config.kind === 'reverse_proxy'
															? 'caddy.proxy'
															: 'caddy.complex'
											)
										},
										{
											key: 'hosts',
											label: t('caddy.hosts'),
											children: (
												<Space orientation="vertical">
													{definition?.hosts?.map((host) => (
														<Typography.Text key={host} copyable>
															{host}
														</Typography.Text>
													))}
												</Space>
											)
										},
										{
											key: 'listen',
											label: t('caddy.listen'),
											children: <code>{definition?.listen?.join(', ')}</code>
										},
										...(definition?.root
											? [
													{
														key: 'root',
														label: t('caddy.root'),
														children: <Typography.Text copyable>{definition.root}</Typography.Text>
													}
												]
											: []),
										...(target
											? [
													{
														key: 'redirect',
														label: t('caddy.target'),
														children: (
															<Space orientation="vertical">
																<code>{target}</code>
																{targetBase && /^https?:\/\//.test(targetBase) && (
																	<a href={targetBase} target="_blank" rel="noreferrer">
																		{t('caddy.openTarget')}
																	</a>
																)}
															</Space>
														)
													},
													{
														key: 'status',
														label: 'HTTP',
														children: definition?.redirect?.statusCode
													}
												]
											: []),
										{ key: 'sync', label: t('caddy.synced'), children: timeText(config.syncedAt) }
									]}
								/>
							</Panel>
							{managed && draft && (
								<Panel
									className="workspace-side"
									title={t('caddy.safeSettings')}
									tags={<code>#{managed.revision}</code>}
								>
									<div className="form-grid">
										{options.map((option) => (
											<label className="field" key={option.field}>
												<span className="field-label">{t(option.label)}</span>
												<Select
													aria-label={t(option.label)}
													value={draft.settings[option.field]}
													disabled={
														blocked ||
														!owner ||
														!capabilities?.edit ||
														!config.settingsCapabilities?.[option.field]
													}
													options={option.values}
													onChange={(value) =>
														setEditor((previous) =>
															previous?.scope === scope
																? {
																		...previous,
																		settings: { ...previous.settings, [option.field]: value }
																	}
																: previous
														)
													}
												/>
											</label>
										))}
									</div>
									<details className="log-record-detail">
										<summary>{t('caddy.sourceDigest')}</summary>
										<code>{managed.sourceDigest}</code>
									</details>
								</Panel>
							)}
						</div>
					)}
					{managed && tab === 'history' && (
						<TablePanel title={t('caddy.history')}>
							<DataTable<CaddyHistory>
								scrollLabel={t('caddy.history')}
								rowKey="revision"
								dataSource={managed.history ?? []}
								pagination={false}
								columns={[
									{
										title: t('caddy.revision'),
										dataIndex: 'revision',
										render: (value: number) => <code>#{value}</code>
									},
									{
										title: t('admin.status'),
										dataIndex: 'status',
										render: (status: CaddyHistory['status']) => (
											<StatusTag
												kind={
													status === 'applied'
														? 'success'
														: status === 'validated'
															? 'info'
															: 'neutral'
												}
											>
												{t(`caddy.${status}` as MessageKey)}
											</StatusTag>
										)
									},
									{ title: t('admin.createdAt'), dataIndex: 'createdAt', render: timeText },
									{
										title: t('caddy.settingsColumn'),
										render: (_, item) => (
											<Space orientation="vertical" size={0}>
												{options.map((option) => (
													<span key={option.field}>
														{t(option.label)}:{' '}
														{
															option.values.find(
																(value) => value.value === item.settings[option.field]
															)?.label
														}
													</span>
												))}
											</Space>
										)
									},
									{
										title: t('admin.actions'),
										render: (_, item) =>
											owner &&
											capabilities?.deploy &&
											item.status === 'applied' &&
											item.revision !== managed.revision ? (
												<Button
													danger
													disabled={blocked || dirty || !etag}
													onClick={() => confirm('rollback', item.revision)}
												>
													{t('caddy.rollback')}
												</Button>
											) : null
									}
								]}
							/>
						</TablePanel>
					)}
				</>
			)}
		</div>
	);
}
