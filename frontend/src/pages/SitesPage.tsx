/**
 * SitesPage: the platform-scope landing page. Lists the caller's sites with
 * provisioning state, offers in-browser site creation (basics, theme pick,
 * visibility defaulting to private), and retries failed provisioning.
 */
import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useSyncExternalStore } from 'react';
import {
	Alert,
	Button,
	Card,
	Col,
	Empty,
	Form,
	Input,
	Modal,
	Row,
	Select,
	Spin,
	Table,
	Tag
} from 'antd';
import {
	createSite,
	listThemes,
	provisionSite,
	type Site,
	type Theme
} from '../lib/api/management';
import { apiErrorKey } from '../lib/i18n/errors';
import { t, type MessageKey } from '../lib/i18n';
import { sessionSnapshot, subscribeSession } from '../lib/session';
import {
	loadSiteList,
	siteListSnapshot,
	subscribeSiteList,
	upsertSite
} from '../lib/sites/site-list';

interface CreateForm {
	slug: string;
	name: string;
	description?: string;
	defaultLocale: 'zh-CN' | 'en-US';
	visibility: 'public' | 'private';
	theme?: string;
}

function provisionTag(site: Site): TagRender {
	const color =
		site.provisionState === 'ready'
			? 'success'
			: site.provisionState === 'failed'
				? 'error'
				: 'processing';
	return { color, key: site.provisionState };
}

interface TagRender {
	color: 'success' | 'error' | 'processing';
	key: string;
}

export default function SitesPage() {
	const navigate = useNavigate();
	const session = useSyncExternalStore(subscribeSession, sessionSnapshot);
	const siteList = useSyncExternalStore(subscribeSiteList, siteListSnapshot);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [creating, setCreating] = useState(false);
	const [createOpen, setCreateOpen] = useState(false);
	const [createError, setCreateError] = useState<MessageKey | null>(null);
	const [themes, setThemes] = useState<Theme[] | null>(null);
	const [retrying, setRetrying] = useState<string | null>(null);
	const [form] = Form.useForm<CreateForm>();

	useEffect(() => {
		if (session.status === 'anonymous') {
			navigate('/login');
			return;
		}
		if (session.status !== 'authenticated') return;
		void loadSiteList(true).then(() => {
			if (siteListSnapshot().status === 'error') setErrorKey('error.generic');
		});
	}, [session.status, navigate]);

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

	if (
		session.status === 'loading' ||
		(siteList.status === 'loading' && siteList.sites.length === 0)
	) {
		return (
			<p role="status">
				<Spin /> {t('common.loading')}
			</p>
		);
	}
	if (errorKey !== null) {
		return <Alert type="error" showIcon message={t(errorKey)} />;
	}

	async function handleCreate(values: CreateForm) {
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
			upsertSite(site);
			setCreateOpen(false);
			form.resetFields();
			if (site.provisionState === 'failed') {
				setErrorKey(null);
			}
		} catch (err) {
			setCreateError(apiErrorKey(err));
		} finally {
			setCreating(false);
		}
	}

	async function handleRetry(site: Site) {
		setRetrying(site.siteId);
		try {
			const refreshed = await provisionSite(site.siteId);
			upsertSite(refreshed);
		} catch {
			// Keep the row as-is; the failure record stays visible.
		} finally {
			setRetrying(null);
		}
	}

	const sites = siteList.sites;

	return (
		<>
			<div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
				<Button type="primary" onClick={() => setCreateOpen(true)}>
					{t('sites.create')}
				</Button>
			</div>
			{sites.length === 0 ? (
				<Card>
					<Empty description={t('sites.empty')} />
				</Card>
			) : (
				<Table<Site>
					rowKey="siteId"
					pagination={false}
					dataSource={sites}
					onRow={(site) => ({
						onClick: () =>
							site.provisionState !== 'provisioning' && navigate(`/sites/${site.siteId}`)
					})}
					columns={[
						{
							title: t('site.name'),
							dataIndex: 'name',
							render: (name: string, site) => (
								<>
									<div>{name}</div>
									<div style={{ opacity: 0.65 }}>{site.slug}</div>
								</>
							)
						},
						{
							title: t('site.role'),
							dataIndex: 'role',
							width: 110,
							render: (role: string) => <Tag>{t(`role.${role}` as never)}</Tag>
						},
						{
							title: t('site.visibility'),
							dataIndex: 'visibility',
							width: 100,
							render: (v: string) => (
								<Tag color={v === 'public' ? 'warning' : 'default'}>
									{t(`site.visibility.${v}` as never)}
								</Tag>
							)
						},
						{
							title: t('site.provisionState'),
							dataIndex: 'provisionState',
							width: 170,
							render: (_: string, site) => {
								const tag = provisionTag(site);
								return (
									<span>
										<Tag color={tag.color}>
											{t(`site.provision.${site.provisionState}` as never)}
										</Tag>
										{site.provisionState === 'failed' ? (
											<Button
												size="small"
												loading={retrying === site.siteId}
												onClick={(e) => {
													e.stopPropagation();
													void handleRetry(site);
												}}
											>
												{t('sites.retryProvision')}
											</Button>
										) : null}
									</span>
								);
							}
						}
					]}
				/>
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
					<Alert type="error" showIcon message={t(createError)} style={{ marginBottom: 16 }} />
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
		</>
	);
}
