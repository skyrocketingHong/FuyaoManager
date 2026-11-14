/**
 * PostsPage: the site's markdown posts under the platform content
 * authority. List with search and pagination, a full editor for the
 * frontmatter fields the theme consumes plus the markdown body, digest-based
 * conflict recovery (412 keeps your edits and offers a resync), and delete
 * with confirmation.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	AutoComplete,
	Button,
	Form,
	Input,
	Modal,
	Popconfirm,
	Select,
	Space,
	Switch,
	Typography,
	message
} from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import DataTable from '../../components/DataTable';
import { timeText } from '../../components/Widgets';
import StatusTag from '../../components/StatusTag';
import { TablePanel } from '../../components/Panel';
import { useContentScope } from '../../components/ContentScopeProvider';
import {
	createPost,
	deletePost,
	getPost,
	getPostTaxonomy,
	getSite,
	listPosts,
	putPost,
	type Post,
	type PostTaxonomy,
	type Site
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

interface PostForm {
	category: string;
	slug: string;
	title: string;
	date: string;
	lastmod?: string;
	draft: boolean;
	categories?: string[];
	tags?: string[];
	description?: string;
	cover?: string;
	coverAlt?: string;
	coverSource?: string;
	coverSourceLink?: string;
	body?: string;
}

function toForm(post: Post): PostForm {
	return {
		category: post.category,
		slug: post.slug,
		title: post.title,
		date: post.date,
		lastmod: post.lastmod || undefined,
		draft: post.draft,
		categories: post.categories ?? [],
		tags: post.tags ?? [],
		description: post.description || undefined,
		cover: post.cover || undefined,
		coverAlt: post.coverAlt || undefined,
		coverSource: post.coverSource || undefined,
		coverSourceLink: post.coverSourceLink || undefined,
		body: post.body
	};
}

export default function PostsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [posts, setPosts] = useState<Post[] | null>(null);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(20);
	const [query, setQuery] = useState('');
	const [search, setSearch] = useState('');
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [editorOpen, setEditorOpen] = useState(false);
	const [editing, setEditing] = useState<Post | null>(null);
	const [digest, setDigest] = useState('');
	const [saving, setSaving] = useState(false);
	const [conflict, setConflict] = useState(false);
	// Server baseline and locally-touched fields for the open editor. The
	// baseline drives 412 recovery (sync refreshes it without overwriting
	// touched fields); untouched fields follow the server version.
	const [baseline, setBaseline] = useState<PostForm | null>(null);
	const [touched, setTouched] = useState<Set<keyof PostForm>>(new Set());
	const [messageApi, contextHolder] = message.useMessage();
	const [form] = Form.useForm<PostForm>();
	// Request epoch guards against late responses after site switches.
	const epoch = useRef(0);
	// The taxonomy loads on its own epoch with its own abort controller so
	// editor saves never get reported through it and scope switches discard
	// stale candidate responses.
	const [taxonomy, setTaxonomy] = useState<PostTaxonomy | null>(null);
	const [taxonomyError, setTaxonomyError] = useState<MessageKey | null>(null);
	const [taxonomyLoading, setTaxonomyLoading] = useState(false);
	const taxonomyEpoch = useRef(0);
	const loadTaxonomy = useCallback(() => {
		if (!target) return;
		const my = ++taxonomyEpoch.current;
		const controller = new AbortController();
		setTaxonomyLoading(true);
		setTaxonomyError(null);
		getPostTaxonomy(target, controller.signal)
			.then((result) => {
				if (my !== taxonomyEpoch.current) return;
				setTaxonomy(result);
				setTaxonomyLoading(false);
			})
			.catch((err) => {
				if (my !== taxonomyEpoch.current || controller.signal.aborted) return;
				setTaxonomyError(apiErrorKey(err));
				setTaxonomyLoading(false);
			});
	}, [target]);

	const refresh = useCallback(
		async (pageArg?: number, queryArg?: string, pageSizeArg?: number) => {
			if (!target) return;
			const my = ++epoch.current;
			try {
				const list = await listPosts(
					target,
					queryArg ?? query,
					pageArg ?? page,
					pageSizeArg ?? pageSize
				);
				if (my !== epoch.current) return;
				setPosts(list.posts);
				setTotal(list.total);
				setErrorKey(null);
			} catch (err) {
				if (my !== epoch.current) return;
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') {
					navigate('/login');
					return;
				}
				setErrorKey(key);
			}
		},
		[target, navigate, page, query, pageSize]
	);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		setSite(null);
		setPosts(null);
		setErrorKey(null);
		setPage(1);
		setQuery('');
		setSearch('');
		taxonomyEpoch.current++;
		setTaxonomy(null);
		setTaxonomyError(null);
		loadTaxonomy();
		let cancelled = false;
		if (siteId && scope.canEdit === undefined)
			getSite(siteId)
				.then((s) => {
					if (!cancelled) setSite(s);
				})
				.catch(() => {});
		void refresh(1, '');
		return () => {
			epoch.current++;
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [scope.key]);

	const editorEpoch = useRef(0);

	async function openEditor(postPath?: string) {
		setConflict(false);
		if (!target) return;
		const my = ++editorEpoch.current;
		setTouched(new Set());
		if (postPath) {
			try {
				const { post, etag } = await getPost(target, postPath);
				if (my !== editorEpoch.current) return; // user closed/switched meanwhile
				setEditing(post);
				setDigest(etag);
				const formValues = toForm(post);
				setBaseline(formValues);
				form.setFieldsValue(formValues);
			} catch (err) {
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
				return;
			}
		} else {
			setEditing(null);
			setDigest('');
			form.resetFields();
			const initial: PostForm = {
				category: 'tech',
				slug: '',
				title: '',
				date: new Date().toISOString(),
				draft: false
			};
			setBaseline(initial);
			form.setFieldsValue(initial);
		}
		setEditorOpen(true);
	}

	async function handleSave(values: PostForm) {
		if (!target || !editing) return;
		setSaving(true);
		try {
			const body: Partial<Post> = toBody(values);
			await putPost(target, editing.path, body, digest);
			void messageApi.success(t('posts.saved'));
			// If the user kept editing while the request was in flight, the
			// newer local values differ from what was submitted: keep the
			// editor open with those values instead of silently discarding
			// them behind a close.
			const current = form.getFieldsValue();
			if (JSON.stringify(toBody(current as PostForm)) !== JSON.stringify(body)) {
				void messageApi.info(t('posts.newerEditsKept'));
			} else {
				setEditorOpen(false);
			}
			// The saved version becomes the new baseline for further edits.
			const savedForm = toForm({ ...editing, ...body } as Post);
			setBaseline(savedForm);
			setDigest('');
			await refresh();
			loadTaxonomy();
		} catch (err) {
			if (apiErrorKey(err) === 'error.conflict') {
				setConflict(true);
			} else {
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
			}
		} finally {
			setSaving(false);
		}
	}

function toBody(values: PostForm): Partial<Post> {
	return {
		...values,
		categories: (values.categories ?? [])
			.map((s) => s.trim())
			.filter((s) => s !== ''),
		tags: (values.tags ?? [])
			.map((s) => s.trim())
			.filter((s) => s !== '')
	};
}

	// After a 412, adopt the server version as the new baseline and ETag
	// while preserving every locally-touched field: only untouched fields
	// refresh from the server. Discarding local edits is a separate action.
	async function syncAfterConflict() {
		if (!target || !editing) return;
		const my = ++editorEpoch.current;
		try {
			const { post, etag } = await getPost(target, editing.path);
			if (my !== editorEpoch.current) return;
			const serverForm = toForm(post);
			setBaseline(serverForm);
			setDigest(etag);
			setEditing(post);
			const merged: Record<string, unknown> = {
				...(serverForm as unknown as Record<string, unknown>)
			};
			const rawForm = form as unknown as { getFieldValue: (name: string) => unknown };
			for (const field of touched) {
				merged[field as string] = rawForm.getFieldValue(field as string) ?? '';
			}
			form.setFieldsValue(merged as never);
			setConflict(false);
			void messageApi.info(t('posts.conflictSynced'));
		} catch {
			// keep the conflict banner; the user can retry manually
		}
	}

	function discardLocal() {
		if (!baseline) return;
		form.setFieldsValue(baseline);
		setTouched(new Set());
		setConflict(false);
	}

	async function handleCreate(values: PostForm) {
		if (!target) return;
		setSaving(true);
		try {
			await createPost(target, toBody(values));
			void messageApi.success(t('posts.saved'));
			setEditorOpen(false);
			await refresh();
			loadTaxonomy();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setSaving(false);
		}
	}

	async function handleDelete(post: Post) {
		if (!target) return;
		try {
			const { etag } = await getPost(target, post.path);
			await deletePost(target, post.path, etag);
			await refresh();
			loadTaxonomy();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));

	const searchBox = (
		<Input.Search
			className="posts-search"
			placeholder={t('posts.searchPlaceholder')}
			allowClear
			value={search}
			onChange={(event) => setSearch(event.target.value)}
			onSearch={(value) => {
				setQuery(value);
				setPage(1);
				void refresh(1, value);
			}}
		/>
	);
	const newButton = (
		<Button type="primary" disabled={!editable} onClick={() => void openEditor()}>
			<Trans text={t('posts.new')} />
		</Button>
	);
	const headerActive = usePageHeader({ content: searchBox, actions: newButton });

	return (
		<div className="page-stack page-stack--fill">
			{contextHolder}
			{!headerActive ? (
				<Space wrap style={{ width: '100%', justifyContent: 'space-between' }}>
					{searchBox}
					{newButton}
				</Space>
			) : null}
			{errorKey !== null ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={
						<Button onClick={() => void refresh()}>{t('common.retry')}</Button>
					}
				/>
			) : null}
			<TablePanel className="fill-height">
				<DataTable<Post>
					className="posts-table"
					rowKey="path"
					dataSource={posts ?? []}
					loading={posts === null}
					pagination={{
						current: page,
						pageSize,
						total,
						showSizeChanger: true,
						pageSizeOptions: [10, 20, 50],
						onChange: (p, size) => {
							if (size !== pageSize) {
								setPageSize(size);
								setPage(1);
								void refresh(1, query, size);
								return;
							}
							setPage(p);
							void refresh(p);
						}
					}}
					columns={[
						{
							title: t('posts.titleColumn'),
								render: (_, post) => (
									<>
										<div>{post.title}</div>
										<Typography.Text type="secondary" className="text-caption">
											{post.path}
										</Typography.Text>
									</>
								)
						},
						{
							title: t('posts.draftColumn'),
							dataIndex: 'draft',
							width: 90,
							render: (draft: boolean) =>
								draft ? (
									<StatusTag kind="warning">{t('posts.draft')}</StatusTag>
								) : (
									<StatusTag kind="success">{t('posts.published')}</StatusTag>
								)
						},
						{
							title: t('posts.dateColumn'),
							dataIndex: 'date',
							width: 170,
							render: (_, post) => timeText(post.date)
						},
						{
							title: t('posts.tagsColumn'),
							dataIndex: 'tags',
							render: (tags: string[]) => (
								<span style={{ display: 'inline-flex', flexWrap: 'wrap', gap: 4 }}>
									{(tags ?? []).map((tag) => (
										<StatusTag key={tag} kind="neutral">
											{tag}
										</StatusTag>
									))}
								</span>
							)
						},
						{
							title: t('admin.actions'),
							width: 170,
							render: (_, post) => (
								<Space wrap size={4}>
									<Button size="small" onClick={() => void openEditor(post.path)}>
										{t('posts.edit')}
									</Button>
									{editable ? (
										<Popconfirm
											title={t('posts.deleteConfirm')}
											onConfirm={() => void handleDelete(post)}
										>
											<Button size="small" danger>
												{t('posts.delete')}
											</Button>
										</Popconfirm>
									) : null}
								</Space>
							)
						}
					]}
				/>
			</TablePanel>
			<Modal
				title={editing ? t('posts.editTitle') : t('posts.newTitle')}
				open={editorOpen}
				onCancel={() => {
					editorEpoch.current++;
					setEditorOpen(false);
				}}
				confirmLoading={saving}
				onOk={() => form.submit()}
				okButtonProps={{ disabled: !editable }}
				okText={t('common.save')}
				width={720}
				destroyOnClose
			>
				{conflict ? (
					<Alert
						type="warning"
						showIcon
						title={t('posts.conflictTitle')}
						description={t('posts.conflictBody')}
						action={
							<Space>
								<Button size="small" onClick={() => void syncAfterConflict()}>
									{t('posts.conflictAction')}
								</Button>
								<Button size="small" danger onClick={discardLocal}>
									{t('posts.discardLocal')}
								</Button>
							</Space>
						}
						style={{ marginBottom: 16 }}
					/>
				) : null}
				<Form
					form={form}
					layout="vertical"
					onFinish={editing ? handleSave : handleCreate}
					onValuesChange={(changed) => {
						for (const key of Object.keys(changed)) {
							setTouched((prev) => new Set(prev).add(key as keyof PostForm));
						}
					}}
					disabled={!editable}
				>
					<Space size={12} style={{ display: 'flex' }} align="start">
						<Form.Item
							name="title"
							label={t('posts.fieldTitle')}
							rules={[{ required: true, message: t('posts.titleRequired') }]}
							style={{ flex: 2 }}
						>
							<Input maxLength={300} />
						</Form.Item>
						<Form.Item
							name="date"
							label={t('posts.fieldDate')}
							rules={[{ required: true, message: t('posts.dateRequired') }]}
							style={{ flex: 1 }}
						>
							<Input placeholder="2026-01-01T10:00:00.000Z" />
						</Form.Item>
					</Space>
					<Space size={12} style={{ display: 'flex' }} align="start">
						{editing ? (
							<Form.Item label={t('posts.fieldPath')} style={{ flex: 1 }}>
								<Input value={editing.path} disabled />
							</Form.Item>
						) : (
							<>
								<Form.Item
									name="category"
									label={t('posts.fieldCategory')}
									rules={[
										{ required: true },
										{ pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: t('posts.slugPattern') }
									]}
									style={{ flex: 1 }}
								>
									<AutoComplete
										style={{ width: '100%' }}
										options={(taxonomy?.directories ?? []).map((dir) => ({
											value: dir.slug,
											label: `${dir.title} (${dir.slug})`
										}))}
										filterOption={(input, option) =>
											String(option?.label ?? '')
												.toLowerCase()
												.includes(input.toLowerCase()) ||
											String(option?.value ?? '').includes(input.toLowerCase())
										}
									/>
								</Form.Item>
								<Form.Item
									name="slug"
									label={t('posts.fieldSlug')}
									rules={[
										{ required: true },
										{ pattern: /^[a-z0-9][a-z0-9-]{0,120}$/, message: t('posts.slugPattern') }
									]}
									style={{ flex: 1 }}
								>
									<Input />
								</Form.Item>
							</>
						)}
						<Form.Item name="draft" label={t('posts.fieldDraft')} valuePropName="checked">
							<Switch checkedChildren={t('posts.draft')} unCheckedChildren={t('posts.published')} />
						</Form.Item>
					</Space>
					{taxonomyError !== null ? (
						<Alert
							type="error"
							showIcon
							title={t('posts.taxonomyError')}
							description={t(taxonomyError)}
							action={
								<Button size="small" onClick={loadTaxonomy}>
									{t('common.retry')}
								</Button>
							}
						/>
					) : null}
					<Space size={12} style={{ display: 'flex' }} align="start">
						<Form.Item
							name="categories"
							label={t('posts.fieldCategories')}
							style={{ flex: 1 }}
						>
							<Select
								mode="multiple"
								showSearch
								allowClear
								loading={taxonomyLoading}
								style={{ width: '100%' }}
								placeholder={t('posts.categoriesPlaceholder')}
								options={(taxonomy?.categories ?? []).map((entry) => ({
									value: entry.name,
									label: entry.name
								}))}
								filterOption={(input, option) =>
									String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
								}
							/>
						</Form.Item>
						<Form.Item name="tags" label={t('posts.fieldTags')} style={{ flex: 1 }}>
							<Select
								mode="tags"
								showSearch
								loading={taxonomyLoading}
								style={{ width: '100%' }}
								options={(taxonomy?.tags ?? []).map((entry) => ({
									value: entry.name,
									label: entry.name
								}))}
								filterOption={(input, option) =>
									String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
								}
								onChange={(values: string[]) => {
									const cleaned = values.filter((v) => v.trim() !== '');
									if (cleaned.length !== values.length) {
										form.setFieldValue('tags', cleaned);
									}
								}}
							/>
							</Form.Item>
						</Space>
					<Form.Item name="description" label={t('posts.fieldDescription')}>
						<Input.TextArea rows={2} maxLength={500} />
					</Form.Item>
					<Form.Item name="cover" label={t('posts.fieldCover')}>
						<Input placeholder="https://…" />
					</Form.Item>
					<Form.Item name="body" label={t('posts.fieldBody')}>
						<Input.TextArea rows={10} style={{ fontFamily: 'monospace' }} />
					</Form.Item>
					<Form.Item name="lastmod" label={t('posts.fieldLastmod')} hidden>
						<Input />
					</Form.Item>
				</Form>
			</Modal>
		</div>
	);
}
