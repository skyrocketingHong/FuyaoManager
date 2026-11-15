import {
	lazy,
	Suspense,
	useCallback,
	useDeferredValue,
	useEffect,
	useId,
	useRef,
	useState
} from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	AutoComplete,
	Button,
	Form,
	Input,
	Popconfirm,
	Select,
	Space,
	Switch,
	Typography,
	message
} from 'antd';
import { ArrowLeft } from 'lucide-react';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import { PageTabs, usePageHeader } from '../../../components/PageHeader';
import { useContentScope } from '../../../components/ContentScopeProvider';
import PostDateInput, { isValidPostDate } from '../../../features/posts/PostDateInput';
import PageLoading from '../../../components/PageLoading';
import StatusTag from '../../../components/StatusTag';
import MarkdownToolbar from '../../../features/posts/MarkdownToolbar';
import { useMarkdownSync } from '../../../features/posts/useMarkdownSync';
import {
	createPost,
	getPost,
	getPostTaxonomy,
	getPostVocabulary,
	getSite,
	putPost,
	putPostVocabulary,
	postPreviewImageURL,
	type Post,
	type PostTaxonomy,
	type Site
} from '../../../lib/api/management';
import type { ContentTarget } from '../../../lib/api/content';
import { useUnsavedGuard } from '../../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../../lib/i18n/errors';
import { t, type MessageKey } from '../../../lib/i18n';
import { RetryButton } from '../../../components/ActionButton';

const MarkdownPreview = lazy(() => import('../../../features/posts/MarkdownPreview'));

interface PostForm {
	category: string;
	slug: string;
	title: string;
	date: string;
	lastmod: string;
	draft: boolean;
	categories: string[];
	tags: string[];
	description: string;
	cover: string;
	coverAlt: string;
	coverSource: string;
	coverSourceLink: string;
	body: string;
}

function toForm(post: Partial<Post>): PostForm {
	return {
		category: post.category ?? '',
		slug: post.slug ?? '',
		title: post.title ?? '',
		date: post.date ?? '',
		lastmod: post.lastmod ?? '',
		draft: post.draft ?? false,
		categories: [...(post.categories ?? [])],
		tags: [...(post.tags ?? [])],
		description: post.description ?? '',
		cover: post.cover ?? '',
		coverAlt: post.coverAlt ?? '',
		coverSource: post.coverSource ?? '',
		coverSourceLink: post.coverSourceLink ?? '',
		body: post.body ?? ''
	};
}

function toBody(values: PostForm): Partial<Post> {
	return {
		...values,
		categories: (values.categories ?? []).filter((value) => value.trim() !== ''),
		tags: (values.tags ?? []).filter((value) => value.trim() !== '')
	};
}

function sameForm(a: PostForm, b: PostForm): boolean {
	return JSON.stringify(toBody(a)) === JSON.stringify(toBody(b));
}

async function registerNewTerms(target: ContentTarget, tags: string[]) {
	for (let attempt = 0; attempt < 2; attempt++) {
		const { vocabulary, etag } = await getPostVocabulary(target);
		const missingTags = tags.filter((tag) => !vocabulary.tags.includes(tag));
		if (!missingTags.length) return;
		try {
			await putPostVocabulary(
				target,
				{
					version: 1,
					categories: vocabulary.categories,
					tags: [...vocabulary.tags, ...new Set(missingTags)]
				},
				etag
			);
			return;
		} catch (err) {
			if (attempt === 0 && apiErrorKey(err) === 'error.conflict') continue;
			throw err;
		}
	}
}

export default function PostEditorPage({ mode }: { mode: 'new' | 'edit' }) {
	const { siteId, postPath } = useParams<{ siteId: string; postPath: string }>();
	const scope = useContentScope();
	const editorIdentity = `${scope.key}:${postPath ?? 'new'}`;
	const target = scope.target;
	const navigate = useNavigate();
	const basePath = siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
	const [form] = Form.useForm<PostForm>();
	const bodyRef = useRef<TextAreaRef>(null);
	const previewRef = useRef<HTMLElement>(null);
	const previewCursorRef = useRef<HTMLSpanElement>(null);
	const bodyTextarea = useCallback(() => bodyRef.current?.resizableTextArea?.textArea ?? null, []);
	const bodyId = useId();
	const [titleInvalid, setTitleInvalid] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();
	const [site, setSite] = useState<Site | null>(null);
	const [editing, setEditing] = useState<Post | null>(null);
	const [baseline, setBaseline] = useState<PostForm | null>(null);
	const [draft, setDraft] = useState<PostForm | null>(null);
	const [digest, setDigest] = useState('');
	const [saving, setSaving] = useState(false);
	const [conflict, setConflict] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [reload, setReload] = useState(0);
	const touched = useRef<Set<keyof PostForm>>(new Set());
	const [view, setView] = useState('split');
	const [taxonomy, setTaxonomy] = useState<PostTaxonomy | null>(null);
	const [taxonomyError, setTaxonomyError] = useState<MessageKey | null>(null);
	const [taxonomyLoading, setTaxonomyLoading] = useState(false);
	const editorEpoch = useRef(0);
	const taxonomyEpoch = useRef(0);
	const taxonomyController = useRef<AbortController | null>(null);
	const createdPath = useRef<string | null>(null);
	const dirty = !!baseline && !!draft && !sameForm(baseline, draft);
	const previewBody = useDeferredValue(draft?.body ?? '');
	useMarkdownSync({
		textarea: bodyTextarea,
		preview: previewRef,
		cursor: previewCursorRef,
		source: draft?.body ?? '',
		rendered: previewBody,
		enabled: view === 'split' && !!baseline
	});
	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	useUnsavedGuard(dirty || saving);

	const loadTaxonomy = useCallback(() => {
		if (!target) return;
		taxonomyController.current?.abort();
		const controller = new AbortController();
		taxonomyController.current = controller;
		const epoch = ++taxonomyEpoch.current;
		setTaxonomyLoading(true);
		setTaxonomyError(null);
		getPostTaxonomy(target, controller.signal)
			.then((value) => {
				if (epoch !== taxonomyEpoch.current) return;
				setTaxonomy(value);
				setTaxonomyLoading(false);
			})
			.catch((err) => {
				if (epoch !== taxonomyEpoch.current || controller.signal.aborted) return;
				setTaxonomyError(apiErrorKey(err) ?? 'error.generic');
				setTaxonomyLoading(false);
			});
	}, [target]);

	useEffect(() => {
		setTaxonomy(null);
		loadTaxonomy();
		return () => {
			taxonomyEpoch.current++;
			taxonomyController.current?.abort();
		};
	}, [scope.key, loadTaxonomy]);

	useEffect(() => {
		let active = true;
		setSite(null);
		if (siteId && scope.canEdit === undefined) {
			getSite(siteId)
				.then((value) => {
					if (active) setSite(value);
				})
				.catch(() => {});
		}
		return () => {
			active = false;
		};
	}, [siteId, scope.key, scope.canEdit]);

	useEffect(() => {
		if (!target) return;
		const epoch = ++editorEpoch.current;
		setEditing(null);
		setBaseline(null);
		setDraft(null);
		setDigest('');
		setSaving(false);
		setConflict(false);
		setTitleInvalid(false);
		touched.current = new Set();
		setErrorKey(null);
		createdPath.current = null;
		form.resetFields();
		const apply = (post: Post | null, values: PostForm, etag: string) => {
			if (epoch !== editorEpoch.current) return;
			setEditing(post);
			setBaseline(values);
			setDraft(values);
			setDigest(etag);
			form.setFieldsValue(values);
		};
		if (mode === 'new') {
			apply(null, toForm({ category: 'tech', date: new Date().toISOString() }), '');
		} else if (postPath) {
			getPost(target, postPath)
				.then(({ post, etag }) => apply(post, toForm(post), etag))
				.catch((err) => {
					if (epoch !== editorEpoch.current) return;
					const key = apiErrorKey(err);
					if (key === 'error.unauthenticated') navigate('/login');
					else setErrorKey(key ?? 'error.generic');
				});
		} else setErrorKey('error.generic');
		return () => {
			editorEpoch.current++;
		};
	}, [scope.key, postPath, mode, reload]);

	useEffect(() => {
		if (!dirty && !saving && createdPath.current && mode === 'new') {
			const path = createdPath.current;
			createdPath.current = null;
			navigate(`${basePath}/${encodeURIComponent(path)}/edit`, { replace: true });
		}
	}, [dirty, saving, mode, basePath, navigate, editing]);

	async function save(values: PostForm) {
		if (!target || !editable || saving) return;
		const epoch = editorEpoch.current;
		const original = toBody(values);
		const submitted = {
			...original,
			lastmod: touched.current.has('lastmod') ? original.lastmod : new Date().toISOString()
		};
		const knownTags = new Set([
			...(baseline?.tags ?? []),
			...(taxonomy?.tags ?? []).map((term) => term.name)
		]);
		const newTags = (submitted.tags ?? []).filter((tag) => !knownTags.has(tag));
		setSaving(true);
		try {
			const { post: saved, etag: nextEtag } = editing
				? await putPost(target, editing.path, submitted, digest)
				: await createPost(target, submitted);
			if (epoch !== editorEpoch.current) return;
			const current = form.getFieldsValue(true) as PostForm;
			const savedForm = toForm(saved);
			const newerFields = (Object.keys(current) as (keyof PostForm)[]).filter(
				(key) => JSON.stringify(current[key]) !== JSON.stringify((original as PostForm)[key])
			);
			const next = { ...savedForm };
			for (const field of newerFields) Object.assign(next, { [field]: current[field] });
			setEditing(saved);
			setDigest(nextEtag);
			setBaseline(savedForm);
			setConflict(false);
			form.setFieldsValue(next);
			setDraft(next);
			touched.current = new Set(newerFields);
			if (newerFields.length) void messageApi.info(t('posts.newerEditsKept'));
			if (!editing) createdPath.current = saved.path;
			void messageApi.success(t('posts.saved'));
			if (newTags.length) {
				try {
					await registerNewTerms(target, newTags);
				} catch {
					if (epoch === editorEpoch.current)
						void messageApi.warning(t('posts.vocabularyRegistrationFailed'));
				}
			}
			if (epoch === editorEpoch.current) loadTaxonomy();
		} catch (err) {
			if (epoch !== editorEpoch.current) return;
			if (apiErrorKey(err) === 'error.conflict') setConflict(true);
			else void messageApi.warning(t(apiErrorKey(err) ?? 'error.generic'));
		} finally {
			if (epoch === editorEpoch.current) setSaving(false);
		}
	}

	async function syncAfterConflict(keepChanges = true) {
		if (!target || !editing || saving) return;
		const epoch = editorEpoch.current;
		const beforeRequest = toBody(form.getFieldsValue(true) as PostForm);
		setSaving(true);
		try {
			const { post, etag } = await getPost(target, editing.path);
			if (epoch !== editorEpoch.current) return;
			const server = toForm(post);
			const current = form.getFieldsValue(true) as PostForm;
			const merged = { ...server };
			const keep = keepChanges
				? touched.current
				: new Set(
						(Object.keys(current) as (keyof PostForm)[]).filter(
							(field) => JSON.stringify(current[field]) !== JSON.stringify(beforeRequest[field])
						)
					);
			for (const field of keep) Object.assign(merged, { [field]: current[field] });
			touched.current = new Set(keep);
			setEditing(post);
			setBaseline(server);
			setDigest(etag);
			form.setFieldsValue(merged);
			setDraft(merged);
			setConflict(false);
			void messageApi.info(t(keepChanges ? 'posts.conflictSynced' : 'posts.conflictDiscarded'));
		} catch (err) {
			if (epoch === editorEpoch.current)
				void messageApi.warning(t(apiErrorKey(err) ?? 'error.generic'));
		} finally {
			if (epoch === editorEpoch.current) setSaving(false);
		}
	}

	const headerContent = (
		<Space wrap>
			<Button type="text" icon={<ArrowLeft size={16} />} onClick={() => navigate(basePath)}>
				{t('posts.back')}
			</Button>
			<Input
				className="post-editor-title"
				aria-label={t('posts.fieldTitle')}
				aria-required="true"
				aria-invalid={titleInvalid}
				placeholder={t('posts.fieldTitle')}
				status={titleInvalid ? 'error' : undefined}
				value={draft?.title ?? ''}
				disabled={!editable || !baseline}
				maxLength={300}
				onChange={(event) => {
					form.setFieldsValue({ title: event.target.value });
					setDraft(form.getFieldsValue(true) as PostForm);
					touched.current.add('title');
					setTitleInvalid(false);
				}}
			/>
			{baseline ? (
				<StatusTag kind={dirty || !editing ? 'warning' : 'success'}>
					{t(dirty || !editing ? 'posts.unsaved' : 'posts.savedStatus')}
				</StatusTag>
			) : null}
		</Space>
	);
	const headerActions = (
		<Space wrap>
			<PageTabs
				value={view}
				onChange={setView}
				ariaLabel={t('posts.editorMode')}
				options={[
					{ value: 'write', label: t('posts.write') },
					{ value: 'split', label: t('posts.split') },
					{ value: 'preview', label: t('posts.preview') }
				]}
			/>
			<Button
				type="primary"
				loading={saving}
				disabled={!editable || !baseline || (!!editing && !dirty)}
				onClick={() => form.submit()}
			>
				{t('common.save')}
			</Button>
		</Space>
	);
	const headerActive = usePageHeader({ leading: headerContent, actions: headerActions });
	const dateRule = {
		validator: async (_: unknown, value?: string) => {
			if (value && !isValidPostDate(value)) throw new Error(t('posts.dateInvalid'));
		}
	};
	const categoryOptions = taxonomy?.categoryGroups
		? taxonomy.categoryGroups.map((group) => ({
				value: group.aliases.find((alias) => draft?.categories.includes(alias)) ?? group.name,
				label: group.title
			}))
		: [
				...(taxonomy?.categories ?? []).map((entry) => ({ value: entry.name, label: entry.name })),
				...(taxonomy?.directories ?? [])
					.filter((entry) => !(taxonomy?.categories ?? []).some((term) => term.name === entry.slug))
					.map((entry) => ({ value: entry.slug, label: entry.title }))
			];

	return (
		<div className="page-stack page-stack--fill post-editor-page">
			{contextHolder}
			{!headerActive ? (
				<div className="post-editor-fallback">
					{headerContent}
					{headerActions}
				</div>
			) : null}
			{errorKey ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={
						<RetryButton onClick={() => setReload((value) => value + 1)} />
					}
				/>
			) : null}
			{conflict ? (
				<Alert
					type="warning"
					showIcon
					title={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					action={
						<Space wrap>
							<Button size="small" disabled={saving} onClick={() => void syncAfterConflict()}>
								{t('posts.conflictAction')}
							</Button>
							<Popconfirm
								title={t('posts.discardConfirm')}
								onConfirm={() => void syncAfterConflict(false)}
							>
								<Button size="small" danger disabled={saving}>
									{t('posts.discardLocal')}
								</Button>
							</Popconfirm>
						</Space>
					}
				/>
			) : null}
			{!baseline && !errorKey ? <PageLoading /> : null}
			<Form
				form={form}
				layout="vertical"
				className="post-editor-form fill-height"
				style={baseline ? undefined : { display: 'none' }}
				disabled={!editable}
				onFinish={(values) => void save({ ...form.getFieldsValue(true), ...values } as PostForm)}
				onFinishFailed={({ errorFields }) => {
					if (errorFields.some((field) => field.name[0] === 'title')) {
						setTitleInvalid(true);
						void messageApi.warning(t('posts.titleRequired'));
					}
				}}
				onValuesChange={(changed) => {
					setDraft(form.getFieldsValue(true) as PostForm);
					touched.current = new Set([
						...touched.current,
						...(Object.keys(changed) as (keyof PostForm)[])
					]);
				}}
			>
				<Form.Item
					name="title"
					hidden
					rules={[{ required: true, whitespace: true, message: t('posts.titleRequired') }]}
				>
					<Input />
				</Form.Item>
				<div className="post-editor-layout">
					<section className="post-editor-writing">
						<MarkdownToolbar
							key={editorIdentity}
							value={draft?.body ?? ''}
							disabled={!editable || !baseline || view === 'preview'}
							controls={bodyId}
							textarea={bodyTextarea}
							onEmptyToc={() => void messageApi.info(t('posts.tocEmpty'))}
							onChange={(body) => {
								form.setFieldsValue({ body });
								setDraft(form.getFieldsValue(true) as PostForm);
								touched.current.add('body');
							}}
						/>
						<div className={`post-editor-body post-editor-body--${view}`}>
							<Form.Item
								name="body"
								label={t('posts.fieldBody')}
								htmlFor={bodyId}
								className="post-editor-source"
							>
								<Input.TextArea key={editorIdentity} id={bodyId} ref={bodyRef} spellCheck />
							</Form.Item>
							<section ref={previewRef} className="post-editor-preview" aria-label={t('posts.preview')}>
								<span ref={previewCursorRef} className="post-preview-cursor" hidden aria-hidden="true" />
								{previewBody ? (
									<Suspense fallback={<PageLoading spec="section" />}>
										<MarkdownPreview
											source={previewBody}
											imageURL={
												target ? (source) => postPreviewImageURL(target, source) : undefined
											}
										/>
									</Suspense>
								) : (
									<Typography.Text type="secondary">{t('posts.previewEmpty')}</Typography.Text>
								)}
							</section>
						</div>
					</section>
					<aside className="post-editor-properties" aria-label={t('posts.properties')}>
						<Form.Item
							name="date"
							label={t('posts.fieldDate')}
							rules={[{ required: true, message: t('posts.dateRequired') }, dateRule]}
						>
							<PostDateInput label={t('posts.fieldDate')} disabled={!editable} />
						</Form.Item>
						<Form.Item name="lastmod" label={t('posts.fieldLastmod')} rules={[dateRule]}>
							<PostDateInput label={t('posts.fieldLastmod')} disabled={!editable} />
						</Form.Item>
						{editing ? (
							<Form.Item label={t('posts.fieldPath')}>
								<Input value={editing.path} disabled />
							</Form.Item>
						) : (
							<div className="form-grid">
								<Form.Item
									name="category"
									label={t('posts.fieldCategory')}
									rules={[
										{ required: true },
										{ pattern: /^[a-z0-9][a-z0-9-]{0,62}$/, message: t('posts.slugPattern') }
									]}
								>
									<AutoComplete
										showSearch={{
											filterOption: (input, option) =>
												String(option?.label ?? '')
													.toLowerCase()
													.includes(input.toLowerCase())
										}}
										options={[
											...(taxonomy?.directories ?? []).map((dir) => ({
												value: dir.slug,
												label: `${dir.title} (${dir.slug})`
											})),
											...(taxonomy?.categories ?? [])
												.filter(
													(item) =>
														/^[a-z0-9][a-z0-9-]{0,62}$/.test(item.name) &&
														!(taxonomy?.directories ?? []).some((dir) => dir.slug === item.name)
												)
												.map((item) => ({ value: item.name, label: item.name }))
										]}
									/>
								</Form.Item>
								<Form.Item
									name="slug"
									label={t('posts.fieldSlug')}
									rules={[
										{ required: true },
										{ pattern: /^[a-z0-9][a-z0-9-]{0,120}$/, message: t('posts.slugPattern') }
									]}
								>
									<Input />
								</Form.Item>
							</div>
						)}
						<Form.Item name="draft" label={t('posts.fieldDraft')} valuePropName="checked">
							<Switch checkedChildren={t('posts.draft')} unCheckedChildren={t('posts.published')} />
						</Form.Item>
						{taxonomyError ? (
							<Alert
								type="error"
								showIcon
								title={t('posts.taxonomyError')}
								action={
									<RetryButton size="small" onClick={loadTaxonomy} />
								}
							/>
						) : null}
						<Form.Item
							name="categories"
							label={t('posts.fieldCategories')}
							rules={[
								{
									validator: async (_, values?: string[]) => {
										if (values?.some((value) => !value.trim()))
											throw new Error(t('posts.categoryRequired'));
									}
								}
							]}
						>
							<Select
								mode="multiple"
								showSearch={{
									filterOption: (input, option) =>
										`${option?.value ?? ''} ${option?.label ?? ''}`
											.toLowerCase()
											.includes(input.toLowerCase())
								}}
								allowClear
								loading={taxonomyLoading}
								placeholder={t('posts.categoriesPlaceholder')}
								options={categoryOptions}
							/>
						</Form.Item>
						<Form.Item
							name="tags"
							label={t('posts.fieldTags')}
							rules={[
								{
									validator: async (_, values?: string[]) => {
										if (values?.some((value) => !value.trim()))
											throw new Error(t('posts.tagRequired'));
									}
								}
							]}
						>
							<Select
								mode="tags"
								showSearch={{
									filterOption: (input, option) =>
										String(option?.value ?? '')
											.toLowerCase()
											.includes(input.toLowerCase())
								}}
								allowClear
								loading={taxonomyLoading}
								options={(taxonomy?.tags ?? []).map((entry) => ({
									value: entry.name,
									label: entry.name
								}))}
							/>
						</Form.Item>
						<Form.Item name="description" label={t('posts.fieldDescription')}>
							<Input.TextArea rows={2} maxLength={500} />
						</Form.Item>
						<Form.Item name="cover" label={t('posts.fieldCover')}>
							<Input placeholder="https://…" />
						</Form.Item>
						<Form.Item name="coverAlt" label={t('posts.coverAlt')}>
							<Input />
						</Form.Item>
						<Form.Item name="coverSource" label={t('posts.coverSource')}>
							<Input />
						</Form.Item>
						<Form.Item name="coverSourceLink" label={t('posts.coverSourceLink')}>
							<Input />
						</Form.Item>
					</aside>
				</div>
			</Form>
		</div>
	);
}
