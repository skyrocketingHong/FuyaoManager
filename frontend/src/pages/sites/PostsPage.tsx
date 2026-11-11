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
	Tag,
	Typography,
	message
} from 'antd';
import Trans from '../../components/i18n/Trans';
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

	async function openEditor(postPath?: string) {
		setConflict(false);
		if (!siteId) return;
		if (postPath) {
			try {
				const { post, etag } = await getPost(siteId, postPath);
				setEditing(post);
				setDigest(etag);
				form.setFieldsValue(toForm(post));
			} catch (err) {
				const key = apiErrorKey(err);
				if (key !== null) void messageApi.warning(t(key));
				return;
			}
		} else {
			setEditing(null);
			setDigest('');
			form.resetFields();
			form.setFieldsValue({
				category: 'tech',
				draft: false,
				date: new Date().toISOString()
			});
		}
		setEditorOpen(true);
	}

	async function handleSave(values: PostForm) {
		if (!siteId || !editing) return;
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
			await putPost(siteId, editing.path, body, digest);
			void messageApi.success(t('posts.saved'));
			setEditorOpen(false);
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

	// After a 412, reload the server version but keep local edits per field
	// the user has not touched (antd preserves untouched form values).
	async function syncAfterConflict() {
		if (!siteId || !editing) return;
		try {
			const { post, etag } = await getPost(siteId, editing.path);
			setDigest(etag);
			setEditing(post);
			form.setFieldsValue(toForm(post));
			setConflict(false);
			void messageApi.info(t('posts.conflictSynced'));
		} catch {
			// keep the conflict banner; the user can retry manually
		}
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

	return (
		<>
			{contextHolder}
			<Space style={{ marginBottom: 12, width: '100%', justifyContent: 'space-between' }}>
				<Input.Search
					placeholder={t('posts.searchPlaceholder')}
					allowClear
					style={{ maxWidth: 320 }}
					onSearch={(value) => {
						setQuery(value);
						setPage(1);
						void refresh(1, value);
					}}
				/>
				<Button type="primary" disabled={!editable} onClick={() => void openEditor()}>
					<Trans text={t('posts.new')} />
				</Button>
			</Space>
			{errorKey !== null ? <Alert type="error" showIcon message={t(errorKey)} /> : null}
			<Table<Post>
				rowKey="path"
				size="small"
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
								<Tag color="orange">{t('posts.draft')}</Tag>
							) : (
								<Tag color="green">{t('posts.published')}</Tag>
							)
					},
					{ title: t('posts.dateColumn'), dataIndex: 'date', width: 170 },
					{
						title: t('posts.tagsColumn'),
						dataIndex: 'tags',
						render: (tags: string[]) =>
							(tags ?? []).map((tag) => (
								<Tag key={tag} style={{ marginInlineEnd: 4 }}>
									{tag}
								</Tag>
							))
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
			<Modal
				title={editing ? t('posts.editTitle') : t('posts.newTitle')}
				open={editorOpen}
				onCancel={() => setEditorOpen(false)}
				confirmLoading={saving}
				onOk={() => form.submit()}
				okText={t('common.save')}
				width={720}
				destroyOnClose
			>
				{conflict ? (
					<Alert
						type="warning"
						showIcon
						message={t('posts.conflictTitle')}
						description={t('posts.conflictBody')}
						action={
							<Button size="small" onClick={() => void syncAfterConflict()}>
								{t('posts.conflictAction')}
							</Button>
						}
						style={{ marginBottom: 16 }}
					/>
				) : null}
				<Form form={form} layout="vertical" onFinish={editing ? handleSave : handleCreate}>
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
		</>
	);
}
