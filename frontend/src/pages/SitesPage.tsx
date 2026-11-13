/**
 * SitesPage: the platform-scope landing page. Lists the caller's sites with
 * provisioning state, offers in-browser site creation (basics, theme pick,
 * visibility defaulting to private), and retries failed provisioning.
 */
import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import { Alert, Button, Col, Empty, Form, Input, Modal, Row, Select } from 'antd';
import {
	createSite,
	listThemes,
	provisionSite,
	type Site,
	type Theme
} from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { currentAuthGeneration } from '../lib/api/client';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import {
	loadSiteList,
	siteListSnapshot,
	subscribeSiteList,
	upsertSite
} from '../lib/sites/site-list';
import { usePageHeader } from '../components/PageHeader';
import PageLoading from '../components/PageLoading';
import DataTable from '../components/DataTable';
import StatusTag, { type StatusKind } from '../components/StatusTag';
import { TablePanel } from '../components/Panel';

interface CreateForm {
	slug: string;
	name: string;
	description?: string;
	defaultLocale: 'zh-CN' | 'en-US';
	visibility: 'public' | 'private';
	theme?: string;
}

function provisionKind(site: Site): StatusKind {
	if (site.provisionState === 'ready') return 'success';
	if (site.provisionState === 'failed') return 'danger';
	return 'info';
}

export default function SitesPage() {
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const siteList = useSyncExternalStore(subscribeSiteList, siteListSnapshot);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [creating, setCreating] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const [createError, setCreateError] = useState<MessageKey | null>(null);
	const [themes, setThemes] = useState<Theme[] | null>(null);
	const [retrying, setRetrying] = useState<string | null>(null);
	const [form] = Form.useForm<CreateForm>();
	const authGeneration = currentAuthGeneration();
	async function reloadSites() {
		const ownGeneration = currentAuthGeneration();
		setErrorKey(null);
		await loadSiteList(true);
		if (ownGeneration !== currentAuthGeneration()) return;
		setErrorKey(siteListSnapshot().status === 'error' ? 'error.generic' : null);
	}

	useEffect(() => {
		if (session.status !== 'authenticated') return;
		void reloadSites();
	}, [session.status, session.user?.id, authGeneration]);

	useEffect(() => {
		if (!createOpen || themes !== null) return;
		let cancelled = false;
		listThemes()
			.then((list) => {
				if (!cancelled) setThemes(list);
			})
			.catch(() => {
				if (!cancelled) setThemes([]);
			});
		return () => {
			cancelled = true;
		};
	}, [createOpen, themes]);

	const createButton = (
		<Button type="primary" onClick={() => setCreateOpen(true)}>
			{t('sites.create')}
		</Button>
	);
	// The shell's top bar carries the primary action when the provider is
	// present; standalone rendering (tests) keeps it local.
	const headerActive = usePageHeader({ actions: createButton });

	if (
		session.status === 'loading' ||
		(siteList.status === 'loading' && siteList.sites.length === 0)
	) {
		return <PageLoading spec="page" label={t('common.loading')} />;
	}
	if (errorKey !== null) {
		return (
			<Alert
				type="error"
				showIcon
				title={t(errorKey)}
				action={
					<Button size="small" onClick={() => void reloadSites()}>
						{t('common.retry')}
					</Button>
				}
			/>
		);
	}

	async function handleCreate(values: CreateForm) {
		const ownGeneration = currentAuthGeneration();
		setCreating(true);
		setCreateError(null);
		try {
			const body: Record<string, unknown> = {
				slug: values.slug.trim(),
				name: values.name.trim(),
				description: values.description?.trim() ?? '',
				defaultLocale: values.defaultLocale,
				visibility: values.visibility ?? 'private'
			};
			if (values.theme) {
				const [themeId, themeVersion] = values.theme.split('@');
				body.themeId = themeId;
				body.themeVersion = themeVersion;
				body.themeConfig = {};
			}
			const site = await createSite(body as never);
			if (ownGeneration !== currentAuthGeneration()) return;
			upsertSite(site, ownGeneration);
			setCreateOpen(false);
			form.resetFields();
			if (site.provisionState === 'failed') {
				setErrorKey(null);
			}
		} catch (err) {
			if (ownGeneration !== currentAuthGeneration()) return;
			setCreateError(apiErrorKey(err));
		} finally {
			if (ownGeneration === currentAuthGeneration()) setCreating(false);
		}
	}

	async function handleRetry(site: Site) {
		const ownGeneration = currentAuthGeneration();
		setRetrying(site.siteId);
		try {
			const refreshed = await provisionSite(site.siteId);
			upsertSite(refreshed, ownGeneration);
		} catch {
			// Keep the row as-is; the failure record stays visible.
		} finally {
			if (ownGeneration === currentAuthGeneration()) setRetrying(null);
		}
	}

	const sites = siteList.sites;

	return (
		<div className="page-stack">
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'flex-end' }}>{createButton}</div>
			) : null}
			{sites.length === 0 ? (
				<TablePanel>
					<Empty description={t('sites.empty')} />
				</TablePanel>
			) : (
				<TablePanel>
					<DataTable<Site>
						rowKey="siteId"
						pagination={false}
						dataSource={sites}
						columns={[
							{
								title: t('site.name'),
								dataIndex: 'name',
								render: (name: string, site) => (
									<>
										<div>
											<Link to={`/sites/${site.siteId}`}>{name}</Link>
										</div>
										<div className="version-text" style={{ color: 'var(--muted-foreground)' }}>
											{site.slug}
										</div>
									</>
								)
							},
							{
								title: t('site.role'),
								dataIndex: 'role',
								width: 110,
								render: (role: string) => (
									<StatusTag kind="neutral">{t(`role.${role}` as never)}</StatusTag>
								)
							},
							{
								title: t('site.visibility'),
								dataIndex: 'visibility',
								width: 100,
								render: (v: string) => (
									<StatusTag kind={v === 'public' ? 'warning' : 'neutral'}>
										{t(`site.visibility.${v}` as never)}
									</StatusTag>
								)
							},
							{
								title: t('site.provisionState'),
								dataIndex: 'provisionState',
								width: 190,
								render: (_: string, site) => (
									<span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
										<StatusTag kind={provisionKind(site)}>
											{t(`site.provision.${site.provisionState}` as never)}
										</StatusTag>
										{site.provisionState === 'failed' ? (
											<Button
												size="small"
												loading={retrying === site.siteId}
												onClick={() => void handleRetry(site)}
											>
												{t('sites.retryProvision')}
											</Button>
										) : null}
									</span>
								)
							}
						]}
					/>
				</TablePanel>
			)}
			<Modal
				title={t('sites.createTitle')}
				open={createOpen}
				onCancel={() => setCreateOpen(false)}
				confirmLoading={creating}
				onOk={() => form.submit()}
				okText={t('sites.createConfirm')}
				destroyOnClose
			>
				{createError ? (
					<Alert type="error" showIcon title={t(createError)} style={{ marginBottom: 16 }} />
				) : null}
				<Form<CreateForm>
					form={form}
					layout="vertical"
					onFinish={handleCreate}
					initialValues={{ defaultLocale: 'zh-CN', visibility: 'private' }}
				>
					<Form.Item
						name="name"
						label={t('site.name')}
						rules={[{ required: true, message: t('sites.nameRequired') }]}
					>
						<Input maxLength={200} />
					</Form.Item>
					<Form.Item
						name="slug"
						label={t('site.slug')}
						rules={[
							{ required: true, message: t('sites.slugRequired') },
							{
								pattern: /^[a-z0-9][a-z0-9-]{0,62}$/,
								message: t('sites.slugPattern')
							}
						]}
					>
						<Input maxLength={63} />
					</Form.Item>
					<Form.Item name="description" label={t('site.description')}>
						<Input.TextArea rows={2} maxLength={1000} />
					</Form.Item>
					<Row gutter={12}>
						<Col span={12}>
							<Form.Item name="defaultLocale" label={t('site.locale')}>
								<Select
									options={[
										{ value: 'zh-CN', label: 'zh-CN' },
										{ value: 'en-US', label: 'en-US' }
									]}
								/>
							</Form.Item>
						</Col>
						<Col span={12}>
							<Form.Item name="visibility" label={t('site.visibility')}>
								<Select
									options={[
										{ value: 'private', label: t('site.visibility.private') },
										{ value: 'public', label: t('site.visibility.public') }
									]}
								/>
							</Form.Item>
						</Col>
					</Row>
					<Form.Item name="theme" label={t('sites.initialTheme')}>
						<Select
							allowClear
							placeholder="-"
							loading={themes === null}
							options={(themes ?? []).map((theme) => ({
								value: `${theme.themeId}@${theme.version}`,
								label: `${theme.name ?? theme.themeId} @ ${theme.version}`
							}))}
						/>
					</Form.Item>
				</Form>
			</Modal>
		</div>
	);
}
