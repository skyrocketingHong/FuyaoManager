import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Input, Popconfirm, Space, Typography, message } from 'antd';
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
	const [messageApi, contextHolder] = message.useMessage();
	const epoch = useRef(0);
	const scopeEpoch = useRef(0);

	const refresh = useCallback(
		async (pageArg?: number, queryArg?: string, sizeArg?: number) => {
			if (!target) return;
			const current = ++epoch.current;
			try {
				const result = await listPosts(
					target,
					queryArg ?? query,
					pageArg ?? page,
					sizeArg ?? pageSize
				);
				if (current !== epoch.current) return;
				setPosts(result.posts);
				setTotal(result.total);
				setErrorKey(null);
			} catch (err) {
				if (current !== epoch.current) return;
				const key = apiErrorKey(err);
				if (key === 'error.unauthenticated') navigate('/login');
				else setErrorKey(key ?? 'error.generic');
			}
		},
		[target, navigate, page, query, pageSize]
	);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		scopeEpoch.current++;
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
		void refresh(1, '');
		return () => {
			epoch.current++;
			scopeEpoch.current++;
			active = false;
		};
	}, [scope.key]);

	async function handleDelete(post: Post) {
		if (!target) return;
		const currentScope = scopeEpoch.current;
		try {
			const { etag } = await getPost(target, post.path);
			if (currentScope !== scopeEpoch.current) return;
			await deletePost(target, post.path, etag);
			if (currentScope === scopeEpoch.current) await refresh();
		} catch (err) {
			if (currentScope !== scopeEpoch.current) return;
			const key = apiErrorKey(err);
			if (key) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	const searchBox = (
		<Input.Search
			className="posts-search"
			aria-label={t('posts.searchPlaceholder')}
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
					action={<Button onClick={() => void refresh()}>{t('common.retry')}</Button>}
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
						onChange: (next, size) => {
							if (size !== pageSize) {
								setPageSize(size);
								setPage(1);
								void refresh(1, query, size);
							} else {
								setPage(next);
								void refresh(next);
							}
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
			</TablePanel>
		</div>
	);
}
