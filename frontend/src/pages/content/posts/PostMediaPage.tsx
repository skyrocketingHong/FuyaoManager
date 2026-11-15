import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Input, Space, Typography, message } from 'antd';
import { Copy, FolderUp, Upload } from 'lucide-react';
import { useContentScope } from '../../../components/ContentScopeProvider';
import DataTable from '../../../components/DataTable';
import { usePageHeader } from '../../../components/PageHeader';
import { TablePanel } from '../../../components/Panel';
import PostWorkspaceTabs from '../../../features/posts/PostWorkspaceTabs';
import {
	importPostMedia,
	listPostMedia,
	postMediaURL,
	type PostMedia,
	type PostMediaList
} from '../../../lib/api/management';
import { useUnsavedGuard } from '../../../lib/editing/useUnsavedGuard';
import { apiErrorKey } from '../../../lib/i18n/errors';
import { t, type MessageKey } from '../../../lib/i18n';

function legacyPath(file: File): string | undefined {
	if (!file.webkitRelativePath) return undefined;
	const parts = file.webkitRelativePath.split('/').filter(Boolean);
	const wp = parts.findIndex(
		(part, index) => part === 'wp-content' && parts[index + 1] === 'uploads'
	);
	const uploads = parts.indexOf('uploads');
	const relative = wp >= 0 ? parts.slice(wp + 2) : uploads >= 0 ? parts.slice(uploads + 1) : parts;
	return `/wp-content/uploads/${relative.join('/')}`;
}

export default function PostMediaPage() {
	const scope = useContentScope();
	const target = scope.target;
	const [messageApi, contextHolder] = message.useMessage();
	const [data, setData] = useState<PostMediaList | null>(null);
	const [query, setQuery] = useState('');
	const [error, setError] = useState<MessageKey | null>(null);
	const [failures, setFailures] = useState<string[]>([]);
	const [loading, setLoading] = useState(false);
	const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);
	const input = useRef<HTMLInputElement>(null);
	const folder = useRef<HTMLInputElement>(null);
	const requestEpoch = useRef(0);
	const scopeEpoch = useRef(0);
	const controller = useRef<AbortController | null>(null);
	const editable = (scope.canEdit ?? data?.canEdit) === true && data?.canEdit === true;
	useUnsavedGuard(progress !== null);

	const load = useCallback(() => {
		if (!target) return;
		controller.current?.abort();
		const request = new AbortController();
		controller.current = request;
		const epoch = ++requestEpoch.current;
		setLoading(true);
		setError(null);
		listPostMedia(target, request.signal)
			.then((result) => {
				if (epoch !== requestEpoch.current) return;
				setData(result);
				setLoading(false);
			})
			.catch((err) => {
				if (epoch !== requestEpoch.current || request.signal.aborted) return;
				setError(apiErrorKey(err) ?? 'error.generic');
				setLoading(false);
			});
	}, [target]);

	useEffect(() => {
		folder.current?.setAttribute('webkitdirectory', '');
		scopeEpoch.current++;
		setData(null);
		setQuery('');
		setFailures([]);
		setProgress(null);
		load();
		return () => {
			scopeEpoch.current++;
			requestEpoch.current++;
			controller.current?.abort();
		};
	}, [scope.key, load]);

	async function importFiles(files: File[]) {
		if (!target || !editable || progress || !files.length) return;
		const epoch = scopeEpoch.current;
		const failed: string[] = [];
		setFailures([]);
		setProgress({ done: 0, total: files.length });
		for (const [index, file] of files.entries()) {
			try {
				await importPostMedia(target, file, legacyPath(file));
			} catch {
				failed.push(file.webkitRelativePath || file.name);
			}
			if (epoch !== scopeEpoch.current) return;
			setProgress({ done: index + 1, total: files.length });
		}
		if (epoch !== scopeEpoch.current) return;
		setProgress(null);
		setFailures(failed);
		void messageApi.info(
			t('media.importResult', { success: files.length - failed.length, failed: failed.length })
		);
		load();
	}

	const tabs = <PostWorkspaceTabs current="media" />;
	const actions = (
		<Space wrap>
			<Button
				icon={<Upload size={16} />}
				disabled={!editable || progress !== null}
				onClick={() => input.current?.click()}
			>
				{t('media.importFiles')}
			</Button>
			<Button
				icon={<FolderUp size={16} />}
				disabled={!editable || progress !== null}
				onClick={() => folder.current?.click()}
			>
				{t('media.importFolder')}
			</Button>
		</Space>
	);
	const search = (
		<Input.Search
			aria-label={t('media.search')}
			placeholder={t('media.search')}
			value={query}
			onChange={(event) => setQuery(event.target.value)}
			allowClear
		/>
	);
	const header = usePageHeader({ leading: tabs, actions, content: search });
	const items = (data?.items ?? []).filter((item) =>
		item.path.toLowerCase().includes(query.trim().toLowerCase())
	);

	return (
		<div className="page-stack page-stack--fill post-media-page">
			{contextHolder}
			{!header ? (
				<Space wrap>
					{tabs}
					{actions}
					{search}
				</Space>
			) : null}
			<input
				ref={input}
				type="file"
				multiple
				accept="image/jpeg,image/png,image/webp,image/gif"
				hidden
				onChange={(event) => {
					const files = Array.from(event.target.files ?? []);
					event.target.value = '';
					void importFiles(files);
				}}
			/>
			<input
				ref={folder}
				type="file"
				multiple
				accept="image/jpeg,image/png,image/webp,image/gif"
				hidden
				onChange={(event) => {
					const files = Array.from(event.target.files ?? []);
					event.target.value = '';
					void importFiles(files);
				}}
			/>
			{progress ? (
				<Typography.Text role="status">{t('media.importProgress', progress)}</Typography.Text>
			) : null}
			{error ? (
				<Alert
					type="error"
					showIcon
					title={t(error)}
					action={<Button onClick={load}>{t('common.retry')}</Button>}
				/>
			) : null}
			{failures.length ? (
				<Alert
					type="warning"
					showIcon
					title={t('media.failed')}
					description={
						<ul>
							{failures.map((file) => (
								<li key={file}>{file}</li>
							))}
						</ul>
					}
				/>
			) : null}
			<TablePanel className="fill-height">
				<DataTable<PostMedia>
					className="post-media-table"
					rowKey="path"
					dataSource={items}
					loading={loading}
					pagination={false}
					locale={{ emptyText: t('media.empty') }}
					columns={[
						{
							title: t('media.preview'),
							width: 90,
							render: (_, item) =>
								target ? (
									<img
										className="post-media-thumbnail"
										src={postMediaURL(target, item.path)}
										alt=""
										loading="lazy"
									/>
								) : null
						},
						{
							title: t('media.path'),
							dataIndex: 'path',
							render: (path: string) => <code>{path}</code>
						},
						{
							title: t('media.size'),
							dataIndex: 'size',
							width: 130,
							render: (size: number) => `${(size / 1024).toFixed(1)} KiB`
						},
						{
							title: t('admin.actions'),
							width: 120,
							render: (_, item) => (
								<Button
									size="small"
									icon={<Copy size={14} />}
									onClick={async () => {
										try {
											await navigator.clipboard.writeText(item.path);
											void messageApi.success(t('media.copied'));
										} catch {
											void messageApi.warning(t('media.copyFailed'));
										}
									}}
								>
									{t('media.copy')}
								</Button>
							)
						}
					]}
				/>
			</TablePanel>
		</div>
	);
}
