/**
 * AlbumsPage: year albums under the platform content authority. Uploads
 * queue the theme's own EXIF/thumbnail indexer (task status banner), the
 * photo grid edits titles/descriptions/alt and ordering with digest saves,
 * and deletion respects frozen-release references.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
	Alert,
	Button,
	Card,
	Input,
	Modal,
	Popconfirm,
	Space,
	Spin,
	Table,
	Tag,
	Tabs,
	Upload,
	message
} from 'antd';
import type { UploadFile } from 'antd';
import Trans from '../../components/i18n/Trans';
import {
	deleteAlbumPhoto,
	getAlbums,
	getAlbumYear,
	getSite,
	listMediaTasks,
	putAlbumYear,
	uploadAlbumPhoto,
	type AlbumIndex,
	type AlbumPhotoEntry,
	type AlbumYear,
	type MediaTask,
	type Site
} from '../../lib/api/management';
import { apiErrorKey } from '../../lib/i18n/errors';
import { t, type MessageKey } from '../../lib/i18n';

const terminalTask = new Set(['succeeded', 'failed']);

export default function AlbumsPage() {
	const { siteId } = useParams<{ siteId: string }>();
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [index, setIndex] = useState<AlbumIndex | null>(null);
	const [year, setYear] = useState<string>('');
	const [doc, setDoc] = useState<AlbumYear | null>(null);
	const [digest, setDigest] = useState('');
	const [dirty, setDirty] = useState(false);
	const [tasks, setTasks] = useState<MediaTask[]>([]);
	const [uploading, setUploading] = useState(false);
	const [saving, setSaving] = useState(false);
	const [editing, setEditing] = useState<AlbumPhotoEntry | null>(null);
	const [editValues, setEditValues] = useState({ title: '', description: '', alt: '' });
	const [conflict, setConflict] = useState(false);
	const [errorKey, setErrorKey] = useState<MessageKey | null>(null);
	const [messageApi, contextHolder] = message.useMessage();
	const epoch = useRef(0);

	const loadYear = useCallback(
		async (yearArg: string) => {
			if (!siteId || !yearArg) return;
			const my = ++epoch.current;
			try {
				const env = await getAlbumYear(siteId, yearArg);
				if (my !== epoch.current) return;
				setDoc(env.year);
				setDigest(env.digest);
				setDirty(false);
				setConflict(false);
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
		[siteId, navigate]
	);

	const loadIndex = useCallback(async () => {
		if (!siteId) return;
		const my = ++epoch.current;
		try {
			const env = await getAlbums(siteId);
			if (my !== epoch.current) return;
			setIndex(env.index);
			const years = env.index.years ?? [];
			const latest = years.length > 0 ? String(years[years.length - 1]) : '';
			setYear(latest);
			if (latest) void loadYear(latest);
		} catch (err) {
			if (my !== epoch.current) return;
			const key = apiErrorKey(err);
			if (key === 'error.unauthenticated') {
				navigate('/login');
				return;
			}
			setErrorKey(key);
		}
	}, [siteId, navigate, loadYear]);

	useEffect(() => {
		if (!siteId) return;
		epoch.current++;
		setSite(null);
		setIndex(null);
		setDoc(null);
		let cancelled = false;
		getSite(siteId)
			.then((s) => {
				if (!cancelled) setSite(s);
			})
			.catch(() => {});
		void loadIndex();
		return () => {
			epoch.current++;
			cancelled = true;
		};
		// eslint-disable-next-line react-hooks/exhaustive-deps
	}, [siteId]);

	// Poll task status while anything is queued or running.
	useEffect(() => {
		if (!siteId) return;
		if (!tasks.some((task) => !terminalTask.has(task.status))) return;
		const timer = window.setInterval(async () => {
			try {
				const list = await listMediaTasks(siteId);
				setTasks((prev) => (prev.length === 0 && list.length === 0 ? prev : list));
				if (!list.some((task) => !terminalTask.has(task.status))) {
					void loadIndex();
				}
			} catch {
				// polling continues silently
			}
		}, 3000);
		return () => window.clearInterval(timer);
	}, [tasks, siteId, loadIndex]);

	async function refreshTasks() {
		if (!siteId) return;
		try {
			setTasks(await listMediaTasks(siteId));
		} catch {
			// surfaced on the next poll
		}
	}

	// Upload beforeUpload: store through the typed API, never auto-upload.
	async function handleUpload(file: File) {
		if (!siteId) return false;
		setUploading(true);
		try {
			await uploadAlbumPhoto(siteId, file);
			void messageApi.success(t('albums.uploadQueued'));
			await refreshTasks();
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		} finally {
			setUploading(false);
		}
		return false; // prevent antd's own upload
	}

	function mutateDoc(updater: (photos: AlbumPhotoEntry[]) => AlbumPhotoEntry[]) {
		if (!doc) return;
		setDirty(true);
		setDoc({ ...doc, photos: updater(doc.photos) });
	}

	function openEditor(photo: AlbumPhotoEntry) {
		setEditing(photo);
		setEditValues({
			title: photo.title ?? '',
			description: photo.description ?? '',
			alt: photo.alt ?? ''
		});
	}

	async function applyEditor() {
		if (!editing) return;
		mutateDoc((photos) =>
			photos.map((p) => (p.filename === editing.filename ? { ...p, ...editValues } : p))
		);
		setEditing(null);
	}

	function move(indexOf: number, delta: -1 | 1) {
		mutateDoc((photos) => {
			const next = [...photos];
			const target = indexOf + delta;
			if (target < 0 || target >= next.length) return next;
			[next[indexOf], next[target]] = [next[target], next[indexOf]];
			return next;
		});
	}

	async function handleSave() {
		if (!siteId || !doc || !year) return;
		setSaving(true);
		try {
			const photos = doc.photos.map((p) => ({
				filename: p.filename,
				title: p.title ?? '',
				description: p.description ?? '',
				alt: p.alt ?? ''
			}));
			const saved = await putAlbumYear(siteId, year, photos, digest);
			setDoc(saved.year);
			setDigest(saved.digest);
			setDirty(false);
			setConflict(false);
			void messageApi.success(t('albums.saved'));
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

	async function handleDelete(photo: AlbumPhotoEntry) {
		if (!siteId || !year) return;
		try {
			await deleteAlbumPhoto(siteId, year, photo.filename, digest);
			await loadYear(year);
			void messageApi.success(t('albums.deleted'));
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable = site !== null && (site.role === 'owner' || site.role === 'editor');
	const activeTask = tasks.find((task) => !terminalTask.has(task.status));
	const lastFailure = tasks.find((task) => task.status === 'failed');

	return (
		<>
			{contextHolder}
			<div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
				<Space>
					<Upload
						beforeUpload={(file) => {
							void handleUpload(file);
							return false;
						}}
						showUploadList={false}
						accept="image/jpeg,image/png,image/webp"
						disabled={!editable}
						fileList={[] as UploadFile[]}
					>
						<Button loading={uploading} disabled={!editable}>
							<Trans text={t('albums.upload')} />
						</Button>
					</Upload>
					<Button
						type="primary"
						loading={saving}
						disabled={!editable || !dirty}
						onClick={() => void handleSave()}
					>
						<Trans text={t('common.save')} />
					</Button>
				</Space>
			</div>
			{activeTask ? (
				<Alert type="info" showIcon message={t('albums.processing')} style={{ marginBottom: 12 }} />
			) : null}
			{lastFailure ? (
				<Alert
					type="error"
					showIcon
					message={t('albums.taskFailed')}
					description={lastFailure.failure?.message ?? ''}
					style={{ marginBottom: 12 }}
					action={
						<Button
							size="small"
							onClick={async () => {
								const { processAlbums } = await import('../../lib/api/management');
								try {
									await processAlbums(siteId ?? '');
									await refreshTasks();
								} catch (err) {
									const key = apiErrorKey(err);
									if (key !== null) void messageApi.warning(t(key));
								}
							}}
						>
							{t('albums.retry')}
						</Button>
					}
				/>
			) : null}
			{conflict ? (
				<Alert
					type="warning"
					showIcon
					message={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
					action={
						<Button size="small" onClick={() => void loadYear(year)}>
							{t('posts.conflictAction')}
						</Button>
					}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon message={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{index === null ? (
				<p role="status">
					<Spin /> {t('common.loading')}
				</p>
			) : (
				<Card size="small">
					<Tabs
						activeKey={year}
						items={(index.years ?? []).map((y) => ({ key: String(y), label: String(y) }))}
						onChange={(key) => {
							setYear(key);
							void loadYear(key);
						}}
					/>
					{doc === null ? (
						<Spin size="small" />
					) : doc.photos.length === 0 ? (
						<p>{t('albums.empty')}</p>
					) : (
						<Table<AlbumPhotoEntry>
							rowKey="filename"
							size="small"
							dataSource={doc.photos}
							pagination={false}
							columns={[
								{
									title: t('albums.photoColumn'),
									render: (_, photo) => (
										<>
											<div>{photo.title || photo.filename}</div>
											<span style={{ fontSize: 12, opacity: 0.65 }}>{photo.path}</span>
										</>
									)
								},
								{
									title: t('albums.dateColumn'),
									dataIndex: 'date',
									width: 180,
									render: (date: string) => (date ?? '').slice(0, 10)
								},
								{
									title: t('albums.altColumn'),
									width: 90,
									render: (_, photo) =>
										photo.alt ? (
											<Tag color="green">{t('albums.hasAlt')}</Tag>
										) : (
											<Tag>{t('albums.noAlt')}</Tag>
										)
								},
								{
									width: 220,
									render: (_, photo, i) => (
										<Space>
											<Button size="small" onClick={() => openEditor(photo)}>
												{t('posts.edit')}
											</Button>
											{editable ? (
												<>
													<Button size="small" type="text" onClick={() => move(i, -1)}>
														↑
													</Button>
													<Button size="small" type="text" onClick={() => move(i, 1)}>
														↓
													</Button>
													<Popconfirm
														title={t('albums.deleteConfirm')}
														onConfirm={() => void handleDelete(photo)}
													>
														<Button size="small" danger>
															{t('posts.delete')}
														</Button>
													</Popconfirm>
												</>
											) : null}
										</Space>
									)
								}
							]}
						/>
					)}
				</Card>
			)}
			<Modal
				title={t('albums.editTitle')}
				open={editing !== null}
				onCancel={() => setEditing(null)}
				onOk={() => void applyEditor()}
				okText={t('common.save')}
				destroyOnClose
			>
				<label style={{ display: 'block', marginBottom: 8 }}>
					<span style={{ display: 'block', fontSize: 12 }}>{t('albums.fieldTitle')}</span>
					<Input
						value={editValues.title}
						onChange={(e) => setEditValues((v) => ({ ...v, title: e.target.value }))}
					/>
				</label>
				<label style={{ display: 'block', marginBottom: 8 }}>
					<span style={{ display: 'block', fontSize: 12 }}>{t('albums.fieldDescription')}</span>
					<Input
						value={editValues.description}
						onChange={(e) => setEditValues((v) => ({ ...v, description: e.target.value }))}
					/>
				</label>
				<label style={{ display: 'block' }}>
					<span style={{ display: 'block', fontSize: 12 }}>{t('albums.fieldAlt')}</span>
					<Input
						value={editValues.alt}
						onChange={(e) => setEditValues((v) => ({ ...v, alt: e.target.value }))}
					/>
				</label>
			</Modal>
		</>
	);
}
