import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Input, Popconfirm, Select, Space, Typography, message } from 'antd';
import Trans from '../../components/i18n/Trans';
import { usePageHeader } from '../../components/PageHeader';
import DataTable from '../../components/DataTable';
import { timeText } from '../../components/Widgets';
import StatusTag from '../../components/StatusTag';
import { TablePanel } from '../../components/Panel';
import { useContentScope } from '../../components/ContentScopeProvider';
import {
	deletePost,
	getPost,
	getSite,
	listPosts,
	type Post,
	type Site
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

function postDateText(value: string): string {
	if (/^\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?)?$/.test(value)) {
		return value.replace('T', ' ');
	}
	return timeText(value);
}

export default function PostsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const basePath = siteId ? `/sites/${encodeURIComponent(siteId)}/posts` : '/content/posts';
	const [site, setSite] = useState<Site | null>(null);
	const [posts, setPosts] = useState<Post[] | null>(null);
	const [total, setTotal] = useState(0);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(20);
	const [query, setQuery] = useState('');
	const [search, setSearch] = useState('');
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [failedPage, setFailedPage] = useState(1);
	const [loading, setLoading] = useState(false);
	const [messageApi, contextHolder] = message.useMessage();
	const epoch = useRef(0);
	const scopeEpoch = useRef(0);
	const loadingRef = useRef(false);

	const refresh = useCallback(
		async (pageArg: number, queryArg: string, sizeArg: number, replace: boolean) => {
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
				const content = document.querySelector<HTMLElement>('.posts-table .ant-table-content');
				if (content) content.scrollTop = 0;
			}
			try {
				const result = await listPosts(target, queryArg, pageArg, sizeArg);
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
		setPosts(null);
		setErrorKey(null);
		setPage(1);
		setQuery('');
		setSearch('');
		let active = true;
		if (siteId && scope.canEdit === undefined) {
			getSite(siteId)
				.then((value) => {
					if (active) setSite(value);
				})
				.catch(() => {});
		}
		void refresh(1, '', pageSize, true);
		return () => {
			epoch.current++;
			scopeEpoch.current++;
			loadingRef.current = false;
			active = false;
		};
	}, [scope.key]);

	const hasMore = page * pageSize < total;
	const loadMore = useCallback(() => {
		if (!hasMore || loading || errorKey) return;
		void refresh(page + 1, query, pageSize, false);
	}, [hasMore, loading, errorKey, page, query, pageSize, refresh]);

	useEffect(() => {
		if (
			!hasMore ||
			loading ||
			errorKey ||
			!posts?.length ||
			typeof IntersectionObserver === 'undefined'
		)
			return;
		const row = document.querySelector('.posts-table tbody tr[data-row-key]:last-of-type');
		if (!row) return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) loadMore();
			},
			{ rootMargin: '200px 0px' }
		);
		observer.observe(row);
		return () => observer.disconnect();
	}, [hasMore, loading, errorKey, posts, loadMore]);

	async function handleDelete(post: Post) {
		if (!target) return;
		const currentScope = scopeEpoch.current;
		try {
			const { etag } = await getPost(target, post.path);
			if (currentScope !== scopeEpoch.current) return;
			await deletePost(target, post.path, etag);
			if (currentScope === scopeEpoch.current) await refresh(1, query, pageSize, true);
		} catch (err) {
			if (currentScope !== scopeEpoch.current) return;
			const key = apiErrorKey(err);
			if (key) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
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
					void refresh(1, value, pageSize, true);
				}}
			/>
			<Select
				aria-label={t('posts.batchSize')}
				value={pageSize}
				onChange={(value) => {
					setPageSize(value);
					void refresh(1, query, value, true);
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
			<Button onClick={() => navigate(`${basePath}/vocabulary`)}>{t('taxonomy.manage')}</Button>
			<Button type="primary" disabled={!editable} onClick={() => navigate(`${basePath}/new`)}>
				<Trans text={t('posts.new')} />
			</Button>
		</Space>
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
			{errorKey ? (
				<Alert
					type="error"
					showIcon
					title={t(errorKey)}
					action={
						<Button onClick={() => void refresh(failedPage, query, pageSize, failedPage === 1)}>
							{t('common.retry')}
						</Button>
					}
				/>
			) : null}
			<TablePanel className="fill-height">
				<DataTable<Post>
					className="posts-table"
					rowKey="path"
					dataSource={posts ?? []}
					loading={loading && posts === null}
					pagination={false}
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
							render: (isDraft: boolean) =>
								isDraft ? (
									<StatusTag kind="warning">{t('posts.draft')}</StatusTag>
								) : (
									<StatusTag kind="success">{t('posts.published')}</StatusTag>
								)
						},
						{
							title: t('posts.dateColumn'),
							dataIndex: 'date',
							width: 170,
							render: (_, post) => (
								<time dateTime={post.date} title={post.date}>
									{postDateText(post.date)}
								</time>
							)
						},
						{
							title: t('posts.tagsColumn'),
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
				{hasMore && !errorKey ? (
					<div className="posts-load-more">
						<Button loading={loading} onClick={loadMore}>
							{t('posts.loadMore')}
						</Button>
					</div>
				) : null}
			</TablePanel>
		</div>
	);
}
