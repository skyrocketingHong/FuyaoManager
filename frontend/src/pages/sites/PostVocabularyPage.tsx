import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Input, Modal, Popconfirm, Space, message } from 'antd';
import { ArrowLeft } from 'lucide-react';
import { PageTabs, usePageHeader } from '../../components/PageHeader';
import { useContentScope } from '../../components/ContentScopeProvider';
import DataTable from '../../components/DataTable';
import { TablePanel } from '../../components/Panel';
import {
	getPostDirectory,
	getPostTaxonomy,
	getPostVocabulary,
	putPostDirectory,
	putPostVocabulary,
	type PostTaxonomy,
	type PostVocabulary
} from '../../lib/api/management';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

type VocabularyKind = 'directories' | 'categories' | 'tags';
type DirectoryDraft = {
	slug: string;
	title: string;
	originalTitle: string;
	etag: string;
	creating: boolean;
};
const EMPTY_CONTENT_DIGEST =
	'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

export default function PostVocabularyPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const basePath = siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
	const navigate = useNavigate();
	const [messageApi, contextHolder] = message.useMessage();
	const [modal, modalContext] = Modal.useModal();
	const [kind, setKind] = useState<VocabularyKind>('categories');
	const [query, setQuery] = useState('');
	const [inputs, setInputs] = useState({ categories: '', tags: '' });
	const [directory, setDirectory] = useState<DirectoryDraft | null>(null);
	const [taxonomy, setTaxonomy] = useState<PostTaxonomy | null>(null);
	const [vocabulary, setVocabulary] = useState<PostVocabulary | null>(null);
	const [etag, setEtag] = useState('');
	const [loading, setLoading] = useState(false);
	const [saving, setSaving] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const epoch = useRef(0);
	const scopeEpoch = useRef(0);
	const controller = useRef<AbortController | null>(null);
	const editable = (scope.canEdit ?? vocabulary?.canEdit) === true && vocabulary?.canEdit === true;
	const input = kind === 'directories' ? '' : inputs[kind];
	const directoryDirty =
		!!directory &&
		(directory.creating
			? !!directory.slug.trim() || !!directory.title.trim()
			: directory.title !== directory.originalTitle);
	useUnsavedGuard(inputs.categories.trim() !== '' || inputs.tags.trim() !== '' || directoryDirty);

	const load = useCallback(() => {
		if (!target) return;
		controller.current?.abort();
		const request = new AbortController();
		controller.current = request;
		const current = ++epoch.current;
		setLoading(true);
		setErrorKey(null);
		Promise.all([
			getPostTaxonomy(target, request.signal),
			getPostVocabulary(target, request.signal)
		])
			.then(([nextTaxonomy, nextVocabulary]) => {
				if (current !== epoch.current) return;
				setTaxonomy(nextTaxonomy);
				setVocabulary(nextVocabulary.vocabulary);
				setEtag(nextVocabulary.etag);
				setLoading(false);
			})
			.catch((err) => {
				if (current !== epoch.current || request.signal.aborted) return;
				setErrorKey(apiErrorKey(err) ?? 'error.generic');
				setLoading(false);
			});
	}, [target]);

	useEffect(() => {
		scopeEpoch.current++;
		setTaxonomy(null);
		setVocabulary(null);
		setEtag('');
		setInputs({ categories: '', tags: '' });
		setDirectory(null);
		setQuery('');
		setSaving(false);
		load();
		return () => {
			epoch.current++;
			scopeEpoch.current++;
			controller.current?.abort();
		};
	}, [scope.key, load]);

	async function update(name: string, remove: boolean) {
		if (!target || !vocabulary || !editable || saving || kind === 'directories') return;
		if (!name.trim()) return;
		const existing = vocabulary[kind];
		if (!remove && existing.includes(name)) {
			void messageApi.warning(t('taxonomy.exists'));
			return;
		}
		const value = {
			version: 1 as const,
			categories:
				kind === 'categories'
					? remove
						? existing.filter((entry) => entry !== name)
						: [...existing, name]
					: vocabulary.categories,
			tags:
				kind === 'tags'
					? remove
						? existing.filter((entry) => entry !== name)
						: [...existing, name]
					: vocabulary.tags
		};
		const current = scopeEpoch.current;
		setSaving(true);
		try {
			const saved = await putPostVocabulary(target, value, etag);
			if (current !== scopeEpoch.current) return;
			setVocabulary(saved.vocabulary);
			setEtag(saved.etag);
			if (!remove)
				setInputs((currentInputs) => ({
					...currentInputs,
					[kind]: currentInputs[kind].trim() === name ? '' : currentInputs[kind]
				}));
			void messageApi.success(t('taxonomy.saved'));
			load();
		} catch (err) {
			if (current !== scopeEpoch.current) return;
			const key = apiErrorKey(err) ?? 'error.generic';
			void messageApi.warning(t(key));
			if (key === 'error.conflict') load();
		} finally {
			if (current === scopeEpoch.current) setSaving(false);
		}
	}

	async function editDirectory(slug: string) {
		if (!target || !editable || saving) return;
		const current = scopeEpoch.current;
		setSaving(true);
		try {
			const result = await getPostDirectory(target, slug);
			if (current === scopeEpoch.current)
				setDirectory({
					slug,
					title: result.directory.title,
					originalTitle: result.directory.title,
					etag: result.etag,
					creating: false
				});
		} catch (err) {
			if (current === scopeEpoch.current)
				void messageApi.warning(t(apiErrorKey(err) ?? 'error.generic'));
		} finally {
			if (current === scopeEpoch.current) setSaving(false);
		}
	}

	async function saveDirectory() {
		if (!target || !directory || !editable || saving) return;
		const slug = directory.slug.trim();
		const title = directory.title.trim();
		if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(slug)) {
			void messageApi.warning(t('taxonomy.invalidCategory'));
			return;
		}
		if (!title) return;
		if (directory.creating && taxonomy?.directories.some((entry) => entry.slug === slug)) {
			void messageApi.warning(t('taxonomy.directoryExists'));
			return;
		}
		const current = scopeEpoch.current;
		setSaving(true);
		try {
			let currentETag = directory.etag;
			if (directory.creating) {
				const latest = await getPostDirectory(target, slug);
				if (current !== scopeEpoch.current) return;
				if (latest.directory.digest !== EMPTY_CONTENT_DIGEST) {
					void messageApi.warning(t('taxonomy.directoryExists'));
					load();
					return;
				}
				currentETag = latest.etag;
			}
			await putPostDirectory(target, slug, title, currentETag);
			if (current !== scopeEpoch.current) return;
			setDirectory(null);
			void messageApi.success(t('taxonomy.directorySaved'));
			load();
		} catch (err) {
			if (current !== scopeEpoch.current) return;
			const key = apiErrorKey(err) ?? 'error.generic';
			void messageApi.warning(t(key));
			if (key === 'error.conflict' && !directory.creating) {
				try {
					const latest = await getPostDirectory(target, slug);
					if (current === scopeEpoch.current)
						setDirectory((draft) =>
							draft && draft.slug === slug
								? { ...draft, etag: latest.etag, originalTitle: latest.directory.title }
								: draft
						);
				} catch {
					/* The draft remains open for a later retry. */
				}
			}
		} finally {
			if (current === scopeEpoch.current) setSaving(false);
		}
	}

	function closeDirectory() {
		if (saving) return;
		if (!directoryDirty) {
			setDirectory(null);
			return;
		}
		modal.confirm({
			title: t('draft.leaveTitle'),
			okText: t('posts.discardLocal'),
			cancelText: t('draft.leaveCancel'),
			okButtonProps: { danger: true },
			onOk: () => setDirectory(null)
		});
	}

	const headerContent = (
		<Space wrap>
			<Button type="text" icon={<ArrowLeft size={16} />} onClick={() => navigate(basePath)}>
				{t('posts.back')}
			</Button>
			<PageTabs
				value={kind}
				onChange={(value) => setKind(value as VocabularyKind)}
				ariaLabel={t('taxonomy.title')}
				options={[
					{ value: 'categories', label: t('taxonomy.categories') },
					{ value: 'tags', label: t('taxonomy.tags') },
					{ value: 'directories', label: t('taxonomy.directories') }
				]}
			/>
			<Input.Search
				aria-label={t('taxonomy.search')}
				placeholder={t('taxonomy.search')}
				value={query}
				onChange={(event) => setQuery(event.target.value)}
				allowClear
			/>
		</Space>
	);
	const headerActions =
		kind === 'directories' ? (
			<Button
				type="primary"
				disabled={!editable || saving}
				onClick={() =>
					setDirectory({ slug: '', title: '', originalTitle: '', etag: '', creating: true })
				}
			>
				{t('taxonomy.addDirectory')}
			</Button>
		) : (
			<Space.Compact>
				<Input
					aria-label={t(kind === 'categories' ? 'taxonomy.categoryInput' : 'taxonomy.tagInput')}
					placeholder={t(kind === 'categories' ? 'taxonomy.categoryInput' : 'taxonomy.tagInput')}
					value={input}
					onChange={(event) => {
						const value = event.target.value;
						setInputs((current) => ({ ...current, [kind]: value }));
					}}
					onPressEnter={() => void update(input.trim(), false)}
					disabled={!editable || saving}
				/>
				<Button
					type="primary"
					loading={saving}
					disabled={!editable || !input.trim()}
					onClick={() => void update(input.trim(), false)}
				>
					{t('taxonomy.add')}
				</Button>
			</Space.Compact>
		);
	const headerActive = usePageHeader({ content: headerContent, actions: headerActions });
	const terms =
		kind === 'directories'
			? []
			: (taxonomy?.[kind] ?? []).filter((term) =>
					term.name.toLowerCase().includes(query.trim().toLowerCase())
				);
	const directories = (taxonomy?.directories ?? []).filter((entry) =>
		`${entry.slug} ${entry.title}`.toLowerCase().includes(query.trim().toLowerCase())
	);

	return (
		<div className="page-stack page-stack--fill">
			{contextHolder}
			{modalContext}
			{!headerActive ? (
				<Space wrap>
					{headerContent}
					{headerActions}
				</Space>
			) : null}
			{errorKey ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={<Button onClick={load}>{t('common.retry')}</Button>}
				/>
			) : null}
			<TablePanel className="fill-height">
				{kind === 'directories' ? (
					<DataTable<(typeof directories)[number]>
						className="taxonomy-table"
						rowKey="slug"
						dataSource={directories}
						loading={loading}
						locale={{ emptyText: t('taxonomy.empty') }}
						pagination={{ pageSize: 20, showSizeChanger: false }}
						columns={[
							{ title: t('taxonomy.directoryTitle'), dataIndex: 'title' },
							{ title: t('taxonomy.directorySlug'), dataIndex: 'slug' },
							{ title: t('taxonomy.count'), dataIndex: 'postCount', width: 110 },
							{
								title: t('admin.actions'),
								width: 120,
								render: (_, entry) => (
									<Button
										size="small"
										disabled={!editable || saving}
										onClick={() => void editDirectory(entry.slug)}
									>
										{t('posts.edit')}
									</Button>
								)
							}
						]}
					/>
				) : (
					<DataTable<(typeof terms)[number]>
						className="taxonomy-table"
						rowKey="name"
						dataSource={terms}
						loading={loading}
						locale={{ emptyText: t('taxonomy.empty') }}
						pagination={{ pageSize: 20, showSizeChanger: false }}
						columns={[
							{ title: t('taxonomy.name'), dataIndex: 'name' },
							{ title: t('taxonomy.count'), dataIndex: 'postCount', width: 110 },
							{
								title: t('admin.actions'),
								width: 180,
								render: (_, term) => {
									const registered = vocabulary?.[kind].includes(term.name) === true;
									return registered ? (
										<Popconfirm
											title={t('taxonomy.removeConfirm')}
											onConfirm={() => void update(term.name, true)}
										>
											<Button danger size="small" disabled={!editable || saving}>
												{t('taxonomy.remove')}
											</Button>
										</Popconfirm>
									) : (
										<Button
											size="small"
											disabled={!editable || saving}
											onClick={() => void update(term.name, false)}
										>
											{t('taxonomy.register')}
										</Button>
									);
								}
							}
						]}
					/>
				)}
			</TablePanel>
			<Modal
				title={t(directory?.creating ? 'taxonomy.addDirectory' : 'taxonomy.editDirectory')}
				open={!!directory}
				onCancel={closeDirectory}
				onOk={() => void saveDirectory()}
				confirmLoading={saving}
				okButtonProps={{ disabled: !directory?.slug.trim() || !directory?.title.trim() }}
				okText={t('common.save')}
				cancelText={t('common.cancel')}
			>
				{directory ? (
					<Space orientation="vertical" style={{ width: '100%' }}>
						<label htmlFor="directory-slug">{t('taxonomy.directorySlug')}</label>
						<Input
							id="directory-slug"
							value={directory.slug}
							disabled={!directory.creating || saving}
							onChange={(event) => setDirectory({ ...directory, slug: event.target.value })}
						/>
						<label htmlFor="directory-title">{t('taxonomy.directoryTitle')}</label>
						<Input
							id="directory-title"
							value={directory.title}
							disabled={saving}
							onChange={(event) => setDirectory({ ...directory, title: event.target.value })}
						/>
					</Space>
				) : null}
			</Modal>
		</div>
	);
}
