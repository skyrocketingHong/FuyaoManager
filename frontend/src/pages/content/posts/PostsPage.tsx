import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Input, Popconfirm, Select, Space, Typography, message, type TableRef } from 'antd';
import Trans from '../../../components/i18n/Trans';
import { usePageHeader } from '../../../components/PageHeader';
import DataTable, { tableScrollContent } from '../../../components/DataTable';
import { timeText } from '../../../components/Widgets';
import StatusTag from '../../../components/StatusTag';
import { TablePanel } from '../../../components/Panel';
import { useContentScope } from '../../../components/ContentScopeProvider';
import PostWorkspaceTabs from '../../../features/posts/PostWorkspaceTabs';
import {
	deletePost,
	getPost,
	getPostTaxonomy,
	getSite,
	listPosts,
	type Post,
	type PostTaxonomy,
	type PostListSort,
	type Site
} from '../../../lib/api/management';
import { apiErrorKey } from '../../../lib/i18n/errors';
import { t, type MessageKey } from '../../../lib/i18n';
import { selectedTableSort, sortColumn } from '../../../lib/table-sort';
import { RetryButton } from '../../../components/ActionButton';

export default function PostsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const basePath = siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
	const [site, setSite] = useState<Site | null>(null);
	const [taxonomy, setTaxonomy] = useState<PostTaxonomy | null>(null);
	const [posts, setPosts] = useState<Post[] | null>(null);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(20);
	const [ordering, setOrdering] = useState<PostListSort>();
	const [query, setQuery] = useState('');
	const [search, setSearch] = useState('');
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [failedPage, setFailedPage] = useState(1);
	const [loading, setLoading] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();
	const tableRef = useRef<TableRef>(null);
	const epoch = useRef(0);
	const scopeEpoch = useRef(0);
	const loadingRef = useRef(false);

	const refresh = useCallback(
		async (pageArg: number, queryArg: string, sizeArg: number, replace: boolean, order?: PostListSort) => {
			if (!target) return;
			if (!replace && loadingRef.current) return;
			const current = ++epoch.current;
			loadingRef.current = true;
			setLoading(true);
			setErrorKey(null);
			if (replace) {
				setPosts(null);
				setTotal(0);
				setPage(1);
				const content = tableScrollContent(tableRef.current);
				if (content) content.scrollTop = 0;
			}
			try {
				const result = order ? await listPosts(target, queryArg, pageArg, sizeArg, order) : await listPosts(target, queryArg, pageArg, sizeArg);
				if (current !== epoch.current) return;
				setPosts((previous) => {
					if (replace) return result.posts;
					const seen = new Set((previous ?? []).map((post) => post.path));
					return [...(previous ?? []), ...result.posts.filter((post) => !seen.has(post.path))];
				});
				setTotal(result.total);
				setPage(pageArg);
				setErrorKey(null);
			} catch (err) {
				if (current !== epoch.current) return;
				if (replace) setPosts([]);
				setFailedPage(pageArg);
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') navigate('/login');
				else setErrorKey(key ?? 'error.generic');
			} finally {
				if (current === epoch.current) {
					loadingRef.current = false;
					setLoading(false);
				}
			}
		},
		[target, navigate]
	);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		scopeEpoch.current++;
		loadingRef.current = false;
		setSite(null);
		setTaxonomy(null);
		setPosts(null);
		setErrorKey(null);
		setPage(1);
		setOrdering(undefined);
		setQuery('');
		setSearch('');
		let active = true;
		const controller = new AbortController();
		void getPostTaxonomy(target, controller.signal)
			.then((value) => { if (active) setTaxonomy(value); })
			.catch(() => {});
		if (siteId && scope.canEdit === undefined) {
			getSite(siteId)
				.then((value) => {
					if (active) setSite(value);
				})
				.catch(() => {});
		}
		void refresh(1, '', pageSize, true);
		return () => {
			controller.abort();
			epoch.current++;
			scopeEpoch.current++;
			loadingRef.current = false;
			active = false;
		};
	}, [scope.key]);

	const hasMore = page * pageSize < total;
	const loadMore = useCallback(() => {
		if (!hasMore || loading || errorKey) return;
		void refresh(page + 1, query, pageSize, false, ordering);
	}, [hasMore, loading, errorKey, page, query, pageSize, ordering, refresh]);

	async function handleDelete(post: Post) {
		if (!target) return;
		const currentScope = scopeEpoch.current;
		try {
			const { etag } = await getPost(target, post.path);
			if (currentScope !== scopeEpoch.current) return;
			await deletePost(target, post.path, etag);
			if (currentScope === scopeEpoch.current) await refresh(1, query, pageSize, true, ordering);
		} catch (err) {
			if (currentScope !== scopeEpoch.current) return;
			const key = apiErrorKey(err);
			if (key) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	function categoryLabels(post: Post): string[] {
		const groups = taxonomy?.categoryGroups ?? [];
		return Array.from(new Set([post.category, ...(post.categories ?? [])].filter(Boolean).map((name) =>
			groups.find((group) => group.name === name || group.directory === name || group.aliases.includes(name))?.title ?? name
		)));
	}
	const searchBox = (
		<Space wrap>
			<Input.Search
				className="posts-search"
				aria-label={t('posts.searchPlaceholder')}
				placeholder={t('posts.searchPlaceholder')}
				allowClear
				value={search}
				onChange={(event) => setSearch(event.target.value)}
				onSearch={(value) => {
					setQuery(value);
					void refresh(1, value, pageSize, true, ordering);
				}}
			/>
			<Select
				aria-label={t('posts.batchSize')}
				value={pageSize}
				onChange={(value) => {
					setPageSize(value);
					void refresh(1, query, value, true, ordering);
				}}
				options={[10, 20, 50, 100].map((value) => ({
					value,
					label: t('posts.batchRows', { count: value })
				}))}
			/>
		</Space>
	);
	const newButton = (
		<Space wrap>
			<Button type="primary" disabled={!editable} onClick={() => navigate(`${basePath}/new`)}>
				<Trans text={t('posts.new')} />
			</Button>
		</Space>
	);
	const headerTabs = <PostWorkspaceTabs current="posts" />;
	const headerActive = usePageHeader({
		leading: headerTabs,
		content: searchBox,
		actions: newButton
	});

	return (
		<div className="page-stack page-stack--fill">
			{contextHolder}
			{!headerActive ? (
				<Space wrap style={{ width: '100%' }}>
					{headerTabs}
					{newButton}
					{searchBox}
				</Space>
			) : null}
			{errorKey ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={
						<RetryButton onClick={() => void refresh(failedPage, query, pageSize, failedPage === 1, ordering)} />
					}
				/>
			) : null}
			<TablePanel className="fill-height">
				<DataTable<Post>
					className="posts-table"
					tableRef={tableRef}
					manualSorting
					rowKey="path"
					dataSource={posts ?? []}
					loading={loading && posts === null}
					pagination={false}
					loadMore={{
						active: hasMore && !errorKey,
						loading,
						onLoad: loadMore,
						scrollLabel: t('nav.posts')
					}}
					onChange={(_, __, sorter, extra) => {
						if (extra.action !== 'sort') return;
						const next = selectedTableSort(sorter) as PostListSort | undefined;
						setOrdering(next);
						void refresh(1, query, pageSize, true, next);
					}}
					columns={[
						{
							title: t('posts.titleColumn'),
							...sortColumn('title', ordering),
							render: (_, post) => (
								<>
									<div>{post.title}</div>
									<div className="post-row-meta">
										{categoryLabels(post).map((label) => <StatusTag key={label} kind="neutral">{label}</StatusTag>)}
										<Typography.Text type="secondary" className="post-identifier">{post.slug}</Typography.Text>
									</div>
								</>
							)
						},
						{
							title: t('posts.draftColumn'),
							...sortColumn('status', ordering),
							dataIndex: 'draft',
							width: 90,
							render: (isDraft: boolean) =>
								isDraft ? (
									<StatusTag kind="warning">{t('posts.draft')}</StatusTag>
								) : (
									<StatusTag kind="success">{t('posts.published')}</StatusTag>
								)
						},
						{
							title: t('posts.dateColumn'),
							...sortColumn('date', ordering),
							dataIndex: 'date',
							width: 170,
							render: (_, post) => (
								<time dateTime={post.date} title={post.date}>
									{timeText(post.date)}
								</time>
							)
						},
						{
							title: t('posts.tagsColumn'),
							...sortColumn('tags', ordering),
							dataIndex: 'tags',
							render: (tags: string[]) => (
								<span className="panel-tags">
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
									<Button
										size="small"
										onClick={() => navigate(`${basePath}/${encodeURIComponent(post.path)}/edit`)}
									>
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
		</div>
	);
}
