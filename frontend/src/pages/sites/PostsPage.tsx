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
	Button,
	Form,
	Input,
	Modal,
	Popconfirm,
	Space,
	Switch,
	Table,
	Typography,
	message
} from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import StatusTag from '../../components/StatusTag';
import { TablePanel } from '../../components/Panel';
import {
	createPost,
	deletePost,
	getPost,
	getSite,
	listPosts,
	putPost,
	type Post,
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
	categories?: string;
	tags?: string;
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
		categories: (post.categories ?? []).join(', '),
		tags: (post.tags ?? []).join(', '),
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
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [posts, setPosts] = useState<Post[] | null>(null);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [query, setQuery] = useState('');
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

	const refresh = useCallback(
		async (pageArg?: number, queryArg?: string) => {
			if (!siteId) return;
			const my = ++epoch.current;
			try {
				const list = await listPosts(siteId, queryArg ?? query, pageArg ?? page, 10);
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
		[siteId, navigate, page, query]
	);

	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setPosts(null);
		setErrorKey(null);
		let cancelled = false;
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
	}, [siteId]);

	const editorEpoch = useRef(0);

	async function openEditor(postPath?: string) {
		setConflict(false);
		if (!siteId) return;
		const my = ++editorEpoch.current;
		setTouched(new Set());
		if (postPath) {
			try {
				const { post, etag } = await getPost(siteId, postPath);
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
		if (!siteId || !editing) return;
		setSaving(true);
		try {
			const body: Partial<Post> = toBody(values);
			await putPost(siteId, editing.path, body, digest);
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
			categories: (values.categories ?? '')
				.split(',')
				.map((s) => s.trim())
				.filter(Boolean),
			tags: (values.tags ?? '')
				.split(',')
				.map((s) => s.trim())
				.filter(Boolean)
		};
	}

	// After a 412, adopt the server version as the new baseline and ETag
	// while preserving every locally-touched field: only untouched fields
	// refresh from the server. Discarding local edits is a separate action.
	async function syncAfterConflict() {
		if (!siteId || !editing) return;
		const my = ++editorEpoch.current;
		try {
			const { post, etag } = await getPost(siteId, editing.path);
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
		if (!siteId) return;
		setSaving(true);
		try {
			const body: Partial<Post> = {
				...values,
				categories: (values.categories ?? '')
					.split(',')
					.map((s) => s.trim())
					.filter(Boolean),
				tags: (values.tags ?? '')
					.split(',')
					.map((s) => s.trim())
					.filter(Boolean)
			};
			await createPost(siteId, body);
			void messageApi.success(t('posts.saved'));
			setEditorOpen(false);
			await refresh();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setSaving(false);
		}
	}

	async function handleDelete(post: Post) {
		if (!siteId) return;
		try {
			const { etag } = await getPost(siteId, post.path);
			await deletePost(siteId, post.path, etag);
			await refresh();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable = site !== null && (site.role === 'owner' || site.role === 'editor');

	const searchBox = (
		<Input.Search
			placeholder={t('posts.searchPlaceholder')}
			allowClear
			style={{ width: 260 }}
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
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<Space style={{ width: '100%', justifyContent: 'space-between' }}>
					{searchBox}
					{newButton}
				</Space>
			) : null}
			{errorKey !== null ? <Alert type="error" showIcon title={t(errorKey)} /> : null}
			<TablePanel>
			<Table<Post>
				rowKey="path"
				dataSource={posts ?? []}
				loading={posts === null}
				pagination={{
					current: page,
					pageSize: 10,
					total,
					showSizeChanger: false,
					onChange: (p) => {
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
								<Typography.Text type="secondary" style={{ fontSize: 12 }}>
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
					{ title: t('posts.dateColumn'), dataIndex: 'date', width: 170 },
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
						width: 170,
						render: (_, post) => (
							<Space>
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
									<Input />
								</Form.Item>
								<Form.Item
									name="slug"
									label="Slug"
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
					<Space size={12} style={{ display: 'flex' }} align="start">
						<Form.Item name="categories" label={t('posts.fieldCategories')} style={{ flex: 1 }}>
							<Input placeholder="技术开发, 生活" />
						</Form.Item>
						<Form.Item name="tags" label={t('posts.fieldTags')} style={{ flex: 1 }}>
							<Input placeholder="Svelte, Go" />
						</Form.Item>
					</Space>
					<Form.Item name="description" label={t('posts.fieldDescription')}>
						<Input.TextArea rows={2} maxLength={500} />
					</Form.Item>
					<Form.Item name="cover" label={t('posts.fieldCover')}>
						<Input placeholder="https://…" />
					</Form.Item>
					<Form.Item name="body" label="Markdown">
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
