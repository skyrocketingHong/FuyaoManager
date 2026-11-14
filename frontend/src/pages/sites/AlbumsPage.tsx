/**
 * AlbumsPage: year albums under the platform content authority. Uploads
 * queue the theme's own EXIF/thumbnail indexer (task status banner), the
 * photo grid edits titles/descriptions/alt and ordering with digest saves,
 * and deletion respects frozen-release references.
 */
import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, Empty, Input, Modal, Popconfirm, Select, Space, Upload, message } from 'antd';
import type { UploadFile } from 'antd';
import { ArrowDown, ArrowUp } from 'lucide-react';
import Trans from '../../components/i18n/Trans';
import { usePageHeader, PageTabs } from '../../components/PageHeader';
import DataTable from '../../components/DataTable';
import StatusTag from '../../components/StatusTag';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import PageLoading from '../../components/PageLoading';
import { TablePanel } from '../../components/Panel';
import { useContentScope } from '../../components/ContentScopeProvider';
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
	const scope = useContentScope();
	const target = scope.target;
	const navigate = useNavigate();
	const [site, setSite] = useState<Site | null>(null);
	const [index, setIndex] = useState<AlbumIndex | null>(null);
	const [year, setYear] = useState<string>('');
	const [doc, setDoc] = useState<AlbumYear | null>(null);
	const [digest, setDigest] = useState('');
	const [dirty, setDirty] = useState(false);
	const [page, setPage] = useState(1);
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
			if (!target || !yearArg) return;
			const my = ++epoch.current;
			try {
				const env = await getAlbumYear(target, yearArg);
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
		[target, navigate]
	);

	const loadIndex = useCallback(async () => {
		if (!target) return;
		const my = ++epoch.current;
		try {
			const env = await getAlbums(target);
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
	}, [target, navigate, loadYear]);

	useEffect(() => {
		if (!target) return;
		epoch.current++;
		setSite(null);
		setIndex(null);
		setDoc(null);
		let cancelled = false;
		if (siteId && scope.canEdit === undefined)
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
	}, [scope.key]);

	// Poll task status while anything is queued or running. When processing
	// finishes, the index reload is skipped while the user has unsaved
	// metadata edits — a completed background task must never clobber the
	// editor. The stale index stays visible until the user saves or reloads.
	useEffect(() => {
		if (!target) return;
		if (!tasks.some((task) => !terminalTask.has(task.status))) return;
		const timer = window.setInterval(async () => {
			try {
				const list = await listMediaTasks(target);
				setTasks((prev) => (prev.length === 0 && list.length === 0 ? prev : list));
				if (!list.some((task) => !terminalTask.has(task.status))) {
					if (!dirty) {
						void loadIndex();
					}
				}
			} catch {
				// polling continues silently
			}
		}, 3000);
		return () => window.clearInterval(timer);
	}, [tasks, target, loadIndex, dirty]);

	async function refreshTasks() {
		if (!target) return;
		try {
			setTasks(await listMediaTasks(target));
		} catch {
			// surfaced on the next poll
		}
	}

	// Upload beforeUpload: store through the typed API, never auto-upload.
	async function handleUpload(file: File) {
		if (!target) return false;
		setUploading(true);
		try {
			await uploadAlbumPhoto(target, file);
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
		setPage(1);
		setDoc({ ...doc, photos: updater(doc.photos) });
	}

	function patchYear(patch: Partial<Pick<AlbumYear, 'title' | 'cover'>>) {
		if (!doc) return;
		setDirty(true);
		setDoc({ ...doc, ...patch });
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

	// Conflict sync: adopt the server digest (baseline marker) while keeping
	// the local photo edits; discarding them is the separate explicit action.
	async function syncAfterConflict() {
		if (!target || !year) return;
		const my = ++epoch.current;
		try {
			const env = await getAlbumYear(target, year);
			if (my !== epoch.current) return;
			setDigest(env.digest);
			if (!dirty) setDoc(env.year);
			setConflict(false);
			void messageApi.info(t('posts.conflictSynced'));
		} catch {
			// keep the banner
		}
	}

	async function handleSave() {
		if (!target || !doc || !year) return;
		setSaving(true);
		try {
			const photos = doc.photos.map((p) => ({
				filename: p.filename,
				title: p.title ?? '',
				description: p.description ?? '',
				alt: p.alt ?? ''
			}));
			const saved = await putAlbumYear(
				target,
				year,
				photos,
				digest,
				doc.title ?? '',
				doc.cover ?? ''
			);
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

	// Detach removes the photo from the next build only; the files stay for
	// releases that still reference them. Hard delete additionally removes
	// files and is refused while referenced.
	async function handleDelete(photo: AlbumPhotoEntry, mode: 'delete' | 'detach') {
		if (!target || !year) return;
		try {
			await deleteAlbumPhoto(target, year, photo.filename, digest, mode);
			await loadYear(year);
			void messageApi.success(mode === 'detach' ? t('albums.detached') : t('albums.deleted'));
		} catch (err) {
			const key = apiErrorKey(err);
			if (key !== null) void messageApi.warning(t(key));
		}
	}

	const editable =
		scope.canEdit ?? (site !== null && (site.role === 'owner' || site.role === 'editor'));
	useUnsavedGuard(dirty);
	const activeTask = tasks.find((task) => !terminalTask.has(task.status));
	const lastFailure = tasks.find((task) => task.status === 'failed');

	const headerActions = (
		<Space size={8}>
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
	);
	const yearTabs =
		index !== null && (index.years ?? []).length > 0 ? (
			<PageTabs
				value={year}
				options={(index.years ?? []).map((y) => ({
					key: String(y),
					label: String(y),
					value: String(y)
				}))}
				onChange={(key) => {
					setYear(key);
					void loadYear(key);
				}}
				ariaLabel={t('nav.albums')}
			/>
		) : undefined;
	const headerActive = usePageHeader({ content: yearTabs, actions: headerActions });

	return (
		<div className="page-stack">
			{contextHolder}
			{!headerActive ? (
				<div style={{ display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 }}>
					{yearTabs}
					{headerActions}
				</div>
			) : null}
			{activeTask ? (
				<Alert type="info" showIcon title={t('albums.processing')} style={{ marginBottom: 12 }} />
			) : null}
			{lastFailure ? (
				<Alert
					type="error"
					showIcon
					title={t('albums.taskFailed')}
					description={lastFailure.failure?.message ?? ''}
					style={{ marginBottom: 12 }}
					action={
						<Button
							size="small"
							disabled={!editable}
							onClick={async () => {
								if (!target) return;
								const { processAlbums } = await import('../../lib/api/management');
								try {
									await processAlbums(target);
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
					title={t('posts.conflictTitle')}
					description={t('posts.conflictBody')}
					style={{ marginBottom: 12 }}
					action={
						<Space>
							<Button size="small" onClick={() => void syncAfterConflict()}>
								{t('posts.conflictAction')}
							</Button>
							<Button
								size="small"
								danger
								onClick={() => {
									setDirty(false);
									void loadYear(year);
								}}
							>
								{t('posts.discardLocal')}
							</Button>
						</Space>
					}
				/>
			) : null}
			{errorKey !== null ? (
				<Alert type="error" showIcon title={t(errorKey)} style={{ marginBottom: 12 }} />
			) : null}
			{index === null ? (
				<PageLoading spec="page" label={t('common.loading')} />
			) : (
				<TablePanel>
					{doc ? (
						<Space wrap style={{ marginBottom: 8 }}>
							<Input
								size="small"
								style={{ width: 220 }}
								placeholder={t('albums.yearTitlePlaceholder')}
								value={doc.title ?? ''}
								disabled={!editable}
								onChange={(e) => patchYear({ title: e.target.value })}
							/>
							<Select
								size="small"
								style={{ width: 200 }}
								placeholder={t('albums.coverPlaceholder')}
								value={doc.cover || undefined}
								disabled={!editable}
								allowClear
								options={doc.photos.map((p) => ({
									value: p.filename,
									label: p.title || p.filename
								}))}
								onChange={(value: string | undefined) => patchYear({ cover: value ?? '' })}
							/>
						</Space>
					) : null}
					{doc === null ? (
						<PageLoading spec="section" />
					) : doc.photos.length === 0 ? (
						<Empty description={t('albums.empty')} />
					) : (
						<DataTable<AlbumPhotoEntry>
							rowKey="filename"
							size="small"
							dataSource={doc.photos}
							pagination={{
								current: page,
								pageSize: 20,
								showSizeChanger: false,
								onChange: setPage
							}}
							columns={[
								{
									title: t('albums.photoColumn'),
									render: (_, photo) => (
										<>
											<div>{photo.title || photo.filename}</div>
											<span className="subtle">{photo.path}</span>
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
											<StatusTag kind="success">{t('albums.hasAlt')}</StatusTag>
										) : (
											<StatusTag kind="neutral">{t('albums.noAlt')}</StatusTag>
										)
								},
								{
									title: t('admin.actions'),
									width: 220,
									render: (_, photo, i) => (
										<Space>
											<Button size="small" onClick={() => openEditor(photo)}>
												{t('posts.edit')}
											</Button>
											{editable ? (
												<>
													<Button
														size="small"
														type="text"
														aria-label={t('common.moveUp')}
														onClick={() => move(i, -1)}
													>
														<ArrowUp size={14} aria-hidden="true" />
													</Button>
													<Button
														size="small"
														type="text"
														aria-label={t('common.moveDown')}
														onClick={() => move(i, 1)}
													>
														<ArrowDown size={14} aria-hidden="true" />
													</Button>
													<Popconfirm
														title={t('albums.detachConfirm')}
														onConfirm={() => void handleDelete(photo, 'detach')}
													>
														<Button size="small">{t('albums.detach')}</Button>
													</Popconfirm>
													<Popconfirm
														title={t('albums.deleteConfirm')}
														onConfirm={() => void handleDelete(photo, 'delete')}
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
				</TablePanel>
			)}
			<Modal
				title={t('albums.editTitle')}
				open={editing !== null}
				onCancel={() => setEditing(null)}
				onOk={() => void applyEditor()}
				okText={t('common.save')}
				destroyOnClose
			>
				<label className="field">
					<span className="field-label">{t('albums.fieldTitle')}</span>
					<Input
						value={editValues.title}
						onChange={(e) => setEditValues((v) => ({ ...v, title: e.target.value }))}
					/>
				</label>
				<label className="field">
					<span className="field-label">{t('albums.fieldDescription')}</span>
					<Input
						value={editValues.description}
						onChange={(e) => setEditValues((v) => ({ ...v, description: e.target.value }))}
					/>
				</label>
				<label className="field">
					<span className="field-label">{t('albums.fieldAlt')}</span>
					<Input
						value={editValues.alt}
						onChange={(e) => setEditValues((v) => ({ ...v, alt: e.target.value }))}
					/>
				</label>
			</Modal>
		</div>
	);
}
