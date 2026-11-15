import { useCallback, useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Alert, Button, Input, Modal, Popconfirm, Space, message } from 'antd';
import { useContentScope } from '../../components/ContentScopeProvider';
import DataTable from '../../components/DataTable';
import { usePageHeader } from '../../components/PageHeader';
import { TablePanel } from '../../components/Panel';
import PostWorkspaceTabs from '../../features/posts/PostWorkspaceTabs';
import {
	getPostDirectory,
	getPostTaxonomy,
	getPostVocabulary,
	putPostDirectory,
	putPostVocabulary,
	type PostTaxonomy,
	type PostVocabulary,
	type PostVocabularyUpdate
} from '../../lib/api/management';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

type CategoryGroup = NonNullable<PostTaxonomy['categoryGroups']>[number];
type DirectoryDraft = {
	slug: string;
	title: string;
	originalTitle: string;
	etag: string;
	creating: boolean;
};
const EMPTY_CONTENT_DIGEST =
	'sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

function fallbackGroups(taxonomy: PostTaxonomy): CategoryGroup[] {
	const rows: CategoryGroup[] = taxonomy.directories.map((entry) => ({
		name: entry.slug,
		title: entry.title,
		directory: entry.slug,
		aliases: [entry.slug],
		postCount: entry.postCount
	}));
	for (const term of taxonomy.categories) {
		if (!rows.some((entry) => entry.name === term.name))
			rows.push({
				name: term.name,
				title: term.name,
				aliases: [term.name],
				postCount: term.postCount
			});
	}
	return rows;
}

export default function PostVocabularyPage() {
	const [params] = useSearchParams();
	const kind = params.get('view') === 'tags' ? 'tags' : 'categories';
	const scope = useContentScope();
	const target = scope.target;
	const [messageApi, contextHolder] = message.useMessage();
	const [modal, modalContext] = Modal.useModal();
	const [query, setQuery] = useState('');
	const [input, setInput] = useState('');
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
	const directoryDirty =
		!!directory &&
		(directory.creating
			? !!directory.slug.trim() || !!directory.title.trim()
			: directory.title !== directory.originalTitle);
	useUnsavedGuard(input.trim() !== '' || directoryDirty || saving);

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
		setInput('');
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

	async function saveTerms(next: PostVocabularyUpdate, clearInput = false) {
		if (!target || !vocabulary || !editable || saving) return;
		const current = scopeEpoch.current;
		setSaving(true);
		try {
			const saved = await putPostVocabulary(target, next, etag);
			if (current !== scopeEpoch.current) return;
			setVocabulary(saved.vocabulary);
			setEtag(saved.etag);
			if (clearInput) setInput('');
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

	function update(names: string[], remove: boolean) {
		if (!vocabulary || !names.length) return;
		const existing = vocabulary[kind];
		const next = remove
			? existing.filter((entry) => !names.includes(entry))
			: Array.from(new Set([...existing, ...names]));
		if (!remove && next.length === existing.length) {
			void messageApi.warning(t('taxonomy.exists'));
			return;
		}
		void saveTerms(
			{
				version: 1,
				categories: kind === 'categories' ? next : vocabulary.categories,
				tags: kind === 'tags' ? next : vocabulary.tags
			},
			kind === 'tags' && !remove
		);
	}

	function importExisting() {
		if (!taxonomy || !vocabulary) return;
		const groups = taxonomy.categoryGroups ?? fallbackGroups(taxonomy);
		const categories = Array.from(
			new Set([...vocabulary.categories, ...groups.flatMap((group) => group.aliases)])
		);
		const tags = Array.from(
			new Set([...vocabulary.tags, ...taxonomy.tags.map((term) => term.name)])
		);
		if (
			categories.length === vocabulary.categories.length &&
			tags.length === vocabulary.tags.length
		) {
			void messageApi.info(t('taxonomy.imported'));
			return;
		}
		void saveTerms({ version: 1, categories, tags });
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
		if (!target || !directory || !vocabulary || !editable || saving) return;
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
			let registered = true;
			if (directory.creating && !vocabulary.categories.includes(slug)) {
				try {
					await putPostVocabulary(
						target,
						{ version: 1, categories: [...vocabulary.categories, slug], tags: vocabulary.tags },
						etag
					);
				} catch {
					registered = false;
				}
			}
			if (current !== scopeEpoch.current) return;
			setDirectory(null);
			if (registered) void messageApi.success(t('taxonomy.directorySaved'));
			else void messageApi.warning(t('taxonomy.registrationFailed'));
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
					/* Keep the draft for retry. */
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

	const tabs = <PostWorkspaceTabs current={kind} />;
	const search = (
		<Input.Search
			aria-label={t('taxonomy.search')}
			placeholder={t('taxonomy.search')}
			value={query}
			onChange={(event) => setQuery(event.target.value)}
			allowClear
		/>
	);
	const actions = (
		<Space wrap>
			{kind === 'categories' ? (
				<Button
					type="primary"
					disabled={!editable || saving}
					onClick={() =>
						setDirectory({ slug: '', title: '', originalTitle: '', etag: '', creating: true })
					}
				>
					{t('taxonomy.addCategory')}
				</Button>
			) : (
				<Space.Compact>
					<Input
						aria-label={t('taxonomy.tagInput')}
						placeholder={t('taxonomy.tagInput')}
						value={input}
						onChange={(event) => setInput(event.target.value)}
						onPressEnter={() => {
							const name = input.trim();
							if (name) update([name], false);
						}}
						disabled={!editable || saving}
					/>
					<Button
						type="primary"
						loading={saving}
						disabled={!editable || !input.trim()}
						onClick={() => update([input.trim()], false)}
					>
						{t('taxonomy.add')}
					</Button>
				</Space.Compact>
			)}
			<Button disabled={!editable || saving || !taxonomy} onClick={importExisting}>
				{t('taxonomy.importAll')}
			</Button>
		</Space>
	);
	const header = usePageHeader({ leading: tabs, content: search, actions });
	const groups = (taxonomy?.categoryGroups ?? (taxonomy ? fallbackGroups(taxonomy) : [])).filter(
		(entry) =>
			`${entry.name} ${entry.title} ${entry.aliases.join(' ')}`
				.toLowerCase()
				.includes(query.trim().toLowerCase())
	);
	const terms = (taxonomy?.tags ?? []).filter((term) =>
		term.name.toLowerCase().includes(query.trim().toLowerCase())
	);

	return (
		<div className="page-stack page-stack--fill">
			{contextHolder}
			{modalContext}
			{!header ? (
				<Space wrap>
					{tabs}
					{actions}
					{search}
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
				{kind === 'categories' ? (
					<DataTable<CategoryGroup>
						className="taxonomy-table"
						rowKey="name"
						dataSource={groups}
						loading={loading}
						locale={{ emptyText: t('taxonomy.empty') }}
						pagination={false}
						columns={[
							{
								title: t('taxonomy.categoryName'),
								dataIndex: 'title',
								render: (_, group) => (
									<>
										{group.title}
										{group.directory && group.directory !== group.title ? (
											<div className="subtle">{group.directory}</div>
										) : null}
									</>
								)
							},
							{ title: t('taxonomy.count'), dataIndex: 'postCount', width: 110 },
							{
								title: t('admin.actions'),
								width: 220,
								render: (_, group) => {
									const registered = group.aliases.some((name) =>
										vocabulary?.categories.includes(name)
									);
									return (
										<Space wrap>
											{group.directory ? (
												<Button
													size="small"
													disabled={!editable || saving}
													onClick={() => void editDirectory(group.directory!)}
												>
													{t('posts.edit')}
												</Button>
											) : null}
											{registered ? (
												<Popconfirm
													title={t('taxonomy.removeConfirm')}
													onConfirm={() => update(group.aliases, true)}
												>
													<Button danger size="small" disabled={!editable || saving}>
														{t('taxonomy.remove')}
													</Button>
												</Popconfirm>
											) : (
												<Button
													size="small"
													disabled={!editable || saving}
													onClick={() => update(group.aliases, false)}
												>
													{t('taxonomy.register')}
												</Button>
											)}
										</Space>
									);
								}
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
						pagination={false}
						columns={[
							{ title: t('taxonomy.name'), dataIndex: 'name' },
							{ title: t('taxonomy.count'), dataIndex: 'postCount', width: 110 },
							{
								title: t('admin.actions'),
								width: 180,
								render: (_, term) =>
									vocabulary?.tags.includes(term.name) ? (
										<Popconfirm
											title={t('taxonomy.removeConfirm')}
											onConfirm={() => update([term.name], true)}
										>
											<Button danger size="small" disabled={!editable || saving}>
												{t('taxonomy.remove')}
											</Button>
										</Popconfirm>
									) : (
										<Button
											size="small"
											disabled={!editable || saving}
											onClick={() => update([term.name], false)}
										>
											{t('taxonomy.register')}
										</Button>
									)
							}
						]}
					/>
				)}
			</TablePanel>
			<Modal
				title={t(directory?.creating ? 'taxonomy.addCategory' : 'taxonomy.editCategory')}
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
						<label htmlFor="directory-slug">{t('taxonomy.categorySlug')}</label>
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
