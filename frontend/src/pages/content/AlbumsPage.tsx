import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Empty, Input, Select, Space } from 'antd';
import { LayoutGrid, List, PanelsTopLeft, Search, Upload as UploadIcon } from 'lucide-react';
import { RetryButton } from '../../components/ActionButton';
import { useContentScope } from '../../components/ContentScopeProvider';
import PageLoading from '../../components/PageLoading';
import { PageTabs, usePageHeader } from '../../components/PageHeader';
import StatusTag from '../../components/StatusTag';
import AlbumDialogs, { type AlbumDialog } from '../../features/albums/AlbumDialogs';
import AlbumPhotos, { type PhotoRemoval } from '../../features/albums/AlbumPhotos';
import { mergeAlbumDraft, validAlbumCover } from '../../features/albums/mergeAlbumDraft';
import { useAdminQuery } from '../../lib/admin/requests';
import { currentAuthGeneration } from '../../lib/api/client';
import type { ContentTarget } from '../../lib/api/content';
import {
	deleteAlbumPhoto,
	getAlbums,
	getAlbumYear,
	getSite,
	listMediaTasks,
	processAlbums,
	putAlbumYear,
	uploadAlbumPhoto,
	type AlbumPhotoEntry,
	type AlbumYear
} from '../../lib/api/management';
import { useUnsavedGuard } from '../../lib/editing/useUnsavedGuard';
import { t, type MessageKey } from '../../lib/i18n';
import { apiErrorKey } from '../../lib/i18n/errors';
import { queryChoice, queryValue, usePageQuery } from '../../lib/navigation/query';
import '../../styles/albums.css';

type AlbumSnapshot = Awaited<ReturnType<typeof getAlbumYear>>;
type AlbumWrite = 'upload' | 'retry' | 'save' | 'remove' | 'merge';

function selectedYear(years: string[], params: URLSearchParams) {
	const requested = queryValue(params, 'year');
	return requested && years.includes(requested) ? requested : (years[0] ?? '');
}

export default function AlbumsPage() {
	const scope = useContentScope();
	return (
		<AlbumWorkspace
			key={`${scope.key}:${currentAuthGeneration()}`}
			target={scope.target}
			canEdit={scope.canEdit}
		/>
	);
}

function AlbumWorkspace({ target, canEdit }: { target?: ContentTarget; canEdit?: boolean }) {
	const { modal, message } = App.useApp();
	const [params, setParams] = usePageQuery();
	const view = queryChoice(params, 'view', ['grid', 'gallery', 'list'], 'grid');
	const search = (queryValue(params, 'q') ?? '').slice(0, 150);
	const index = useAdminQuery(
		useCallback((signal) => (target ? getAlbums(target, signal) : Promise.resolve(null)), [target])
	);
	const tasks = useAdminQuery(
		useCallback(
			(signal) => (target ? listMediaTasks(target, signal) : Promise.resolve([])),
			[target]
		)
	);
	const permissions = useAdminQuery(
		useCallback(
			() =>
				typeof target === 'string' && canEdit === undefined
					? getSite(target)
					: Promise.resolve(null),
			[target, canEdit]
		)
	);
	const editable =
		canEdit ?? (permissions.data?.role === 'owner' || permissions.data?.role === 'editor');
	const years = [...new Set(index.data?.index.years ?? [])].sort((a, b) => b - a).map(String);
	const year = selectedYear(years, params);

	const [snapshot, setSnapshot] = useState<{ key: string; data: AlbumSnapshot }>();
	const [draft, setDraft] = useState<{ key: string; album: AlbumYear }>();
	const [yearError, setYearError] = useState<{ key: string; message: MessageKey }>();
	const [reading, setReading] = useState('');
	const [write, setWrite] = useState<AlbumWrite | null>(null);
	const [writeError, setWriteError] = useState('');
	const [conflict, setConflict] = useState(false);
	const [confirming, setConfirming] = useState(false);
	const [dialog, setDialog] = useState<AlbumDialog | null>(null);
	const [progress, setProgress] = useState({ done: 0, total: 0 });
	const fileInput = useRef<HTMLInputElement>(null);
	const activeRead = useRef<AbortController>();
	const writing = useRef(false);
	const mounted = useRef(true);
	const needsRefresh = useRef(false);
	const currentYear = useRef(year);
	currentYear.current = year;

	const saved = snapshot?.key === year ? snapshot.data : undefined;
	const local = draft?.key === year ? draft.album : undefined;
	const album = local ?? saved?.year;
	const dirty = !!local;
	const busy = !!write || (reading === year && !!year);
	const loadYear = useCallback(
		async (key: string) => {
			if (!target || !key) return;
			activeRead.current?.abort();
			const read = new AbortController();
			activeRead.current = read;
			setYearError(undefined);
			setReading(key);
			try {
				const data = await getAlbumYear(target, key, read.signal);
				if (!read.signal.aborted) setSnapshot({ key, data });
			} catch (error) {
				if (!read.signal.aborted)
					setYearError({ key, message: apiErrorKey(error) ?? 'error.generic' });
			} finally {
				if (activeRead.current === read) {
					activeRead.current = undefined;
					setReading('');
				}
			}
		},
		[target]
	);

	useEffect(() => {
		mounted.current = true;
		return () => {
			mounted.current = false;
		};
	}, []);
	useEffect(() => {
		setDraft(undefined);
		setDialog(null);
		setConflict(false);
		setWriteError('');
		void loadYear(year);
		return () => {
			activeRead.current?.abort();
			activeRead.current = undefined;
		};
	}, [year, loadYear]);

	const task = tasks.data?.[0];
	const processing = !!tasks.data?.some(
		({ status }) => status === 'queued' || status === 'running'
	);
	useEffect(() => {
		if (processing) needsRefresh.current = true;
		if (processing && !tasks.loading) {
			const timer = window.setTimeout(tasks.reload, 3000);
			return () => window.clearTimeout(timer);
		}
		if (
			!processing &&
			!tasks.loading &&
			tasks.data &&
			!dirty &&
			!busy &&
			!dialog &&
			!confirming &&
			needsRefresh.current
		) {
			needsRefresh.current = false;
			index.reload();
			void loadYear(year);
		}
	}, [
		processing,
		tasks.loading,
		tasks.data,
		tasks.reload,
		dirty,
		busy,
		dialog,
		confirming,
		index.reload,
		loadYear,
		year
	]);

	const dialogChanged =
		dialog?.kind === 'photo'
			? (['title', 'description', 'alt'] as const).some(
					(field) => (dialog.values[field] ?? '') !== (dialog.photo[field] ?? '')
				)
			: dialog?.kind === 'settings' &&
				((dialog.values.title ?? '') !== (album?.title ?? '') ||
					(dialog.values.cover ?? '') !== (album ? validAlbumCover(album) : ''));
	useUnsavedGuard(
		dirty || !!dialogChanged,
		(from, to) =>
			from.pathname === to.pathname && selectedYear(years, new URLSearchParams(to.search)) === year
	);

	async function runWrite(kind: AlbumWrite, act: () => Promise<void>) {
		if (!editable || writing.current || activeRead.current) return;
		writing.current = true;
		setWrite(kind);
		setWriteError('');
		try {
			await act();
		} catch (error) {
			if (mounted.current) {
				const key = apiErrorKey(error) ?? 'error.generic';
				if (key === 'error.conflict' && currentYear.current === year) setConflict(true);
				else setWriteError(t(key));
			}
		} finally {
			writing.current = false;
			if (mounted.current) setWrite(null);
		}
	}

	function upload(files: File[]) {
		if (!target || files.length === 0) return;
		void runWrite('upload', async () => {
			let accepted = 0;
			const rejected: string[] = [];
			setProgress({ done: 0, total: files.length });
			for (const file of files) {
				try {
					await uploadAlbumPhoto(target, file);
					accepted++;
				} catch (error) {
					rejected.push(`${file.name}（${t(apiErrorKey(error) ?? 'error.generic')}）`);
				}
				if (!mounted.current) return;
				setProgress({ done: accepted + rejected.length, total: files.length });
			}
			if (accepted > 0) {
				needsRefresh.current = true;
				tasks.reload();
			}
			if (rejected.length > 0)
				setWriteError(t('albums.uploadFailed', { files: rejected.join(', ') }));
			else void message.success(t('albums.queuedCount', { count: accepted }));
		});
	}

	function updateAlbum(next: AlbumYear) {
		if (editable && !busy) setDraft({ key: year, album: next });
	}

	function applyDialog() {
		if (!dialog || !album || busy || !editable) return;
		if (dialogChanged)
			updateAlbum(
				dialog.kind === 'settings'
					? { ...album, ...dialog.values }
					: {
							...album,
							photos: album.photos.map((photo) =>
								photo.filename === dialog.photo.filename ? { ...photo, ...dialog.values } : photo
							)
						}
			);
		setDialog(null);
	}

	function move(filename: string, direction: -1 | 1) {
		if (!album) return;
		const photos = [...album.photos];
		const from = photos.findIndex((photo) => photo.filename === filename);
		const to = from + direction;
		if (from < 0 || to < 0 || to >= photos.length) return;
		[photos[from], photos[to]] = [photos[to], photos[from]];
		updateAlbum({ ...album, photos });
	}

	function save() {
		if (!target || !saved || !local || conflict) return;
		void runWrite('save', async () => {
			const data = await putAlbumYear(
				target,
				year,
				local.photos.map(({ filename, title, description, alt }) => ({
					filename,
					title: title ?? '',
					description: description ?? '',
					alt: alt ?? ''
				})),
				saved.digest,
				local.title ?? '',
				validAlbumCover(local)
			);
			if (mounted.current && currentYear.current === year) {
				setSnapshot({ key: year, data });
				setDraft(undefined);
				setConflict(false);
				void message.success(t('albums.saved'));
			}
		});
	}

	function resolveConflict(keepDraft: boolean) {
		if (!target) return;
		void runWrite('merge', async () => {
			const data = await getAlbumYear(target, year);
			if (!mounted.current || currentYear.current !== year) return;
			if (keepDraft && local && saved)
				setDraft({ key: year, album: mergeAlbumDraft(saved.year, local, data.year) });
			else setDraft(undefined);
			setSnapshot({ key: year, data });
			setConflict(false);
		});
	}

	function confirmRemoval(photo: AlbumPhotoEntry, mode: PhotoRemoval) {
		if (!saved || !target || dirty || !editable || busy || conflict || confirming) return;
		setConfirming(true);
		modal.confirm({
			afterClose: () => setConfirming(false),
			title: t(mode === 'detach' ? 'albums.detach' : 'albums.deletePhoto'),
			content: (
				<>
					<p className="version-text">{photo.title || photo.filename}</p>
					<p>{t(mode === 'detach' ? 'albums.detachConfirm' : 'albums.deleteConfirm')}</p>
				</>
			),
			okText: t(mode === 'detach' ? 'albums.detach' : 'albums.deletePhoto'),
			okButtonProps: { danger: mode === 'delete' },
			cancelText: t('common.cancel'),
			focusable: { autoFocusButton: 'cancel' },
			onOk: () =>
				runWrite('remove', async () => {
					if (!mounted.current || currentYear.current !== year) return;
					await deleteAlbumPhoto(target, year, photo.filename, saved.digest, mode);
					if (!mounted.current || currentYear.current !== year) return;
					index.reload();
					await loadYear(year);
					void message.success(t(mode === 'detach' ? 'albums.detached' : 'albums.deleted'));
				})
		});
	}

	const query = search.trim().toLocaleLowerCase();
	const photos =
		album?.photos.filter(
			(photo) =>
				!query ||
				[photo.filename, photo.title, photo.description].some((field) =>
					field?.toLocaleLowerCase().includes(query)
				)
		) ?? [];
	const cover = album ? validAlbumCover(album) || album.photos[0]?.filename : undefined;
	const controls = (
		<div className="album-toolbar">
			{years.length > 0 && (
				<Select<string>
					className="album-year-picker"
					aria-label={t('albums.year')}
					value={year}
					options={years.map((item) => ({ label: item, value: item }))}
					disabled={!!write}
					showSearch={{ optionFilterProp: 'label' }}
					onChange={(value) => setParams({ year: value })}
				/>
			)}
			{album && album.photos.length > 0 && (
				<>
					<Input
						className="album-search"
						prefix={<Search size={15} aria-hidden="true" />}
						aria-label={t('albums.search')}
						placeholder={t('albums.search')}
						allowClear
						value={search}
						maxLength={150}
						onChange={(event) => setParams({ q: event.target.value }, true)}
					/>
					<PageTabs
						value={view}
						ariaLabel={t('albums.view')}
						options={[
							{
								value: 'grid',
								label: (
									<span className="album-view-label">
										<LayoutGrid size={14} />
										{t('albums.grid')}
									</span>
								)
							},
							{
								value: 'gallery',
								label: (
									<span className="album-view-label">
										<PanelsTopLeft size={14} />
										{t('albums.gallery')}
									</span>
								)
							},
							{
								value: 'list',
								label: (
									<span className="album-view-label">
										<List size={15} />
										{t('albums.list')}
									</span>
								)
							}
						]}
						onChange={(value) => setParams({ view: value === 'grid' ? null : value })}
					/>
				</>
			)}
		</div>
	);
	const actions =
		editable && target ? (
			<Space wrap size={8}>
				<input
					ref={fileInput}
					type="file"
					aria-label={t('albums.upload')}
					accept="image/jpeg,image/png,image/webp"
					multiple
					hidden
					onChange={(event) => {
						const files = Array.from(event.currentTarget.files ?? []);
						event.currentTarget.value = '';
						upload(files);
					}}
				/>
				<Button
					type={dirty ? 'default' : 'primary'}
					icon={<UploadIcon size={15} />}
					loading={write === 'upload'}
					disabled={busy}
					onClick={() => fileInput.current?.click()}
				>
					{write === 'upload' ? t('albums.uploadProgress', progress) : t('albums.upload')}
				</Button>
				{dirty && (
					<Button
						type="primary"
						loading={write === 'save'}
						disabled={busy || conflict}
						onClick={save}
					>
						{t('common.save')}
					</Button>
				)}
			</Space>
		) : undefined;
	const header = usePageHeader({ content: controls, actions });
	const error = index.error ?? (yearError?.key === year ? yearError.message : null);

	return (
		<div className="page-stack album-workspace">
			{!header && (
				<div className="album-toolbar album-toolbar--local">
					{controls}
					{actions}
				</div>
			)}
			{writeError && (
				<Alert
					type="error"
					showIcon
					title={writeError}
					closable={{ onClose: () => setWriteError('') }}
				/>
			)}
			{tasks.error && (
				<Alert
					type="warning"
					showIcon
					title={t('albums.taskReadFailed')}
					action={<RetryButton onClick={tasks.reload} />}
				/>
			)}
			{processing && <Alert type="info" showIcon title={t('albums.processing')} />}
			{!processing && task?.status === 'failed' && (
				<Alert
					type="error"
					showIcon
					title={t('albums.taskFailed')}
					description={task.failure?.message}
					action={
						editable && (
							<RetryButton
								loading={write === 'retry'}
								disabled={busy}
								onClick={() => {
									if (target)
										void runWrite('retry', async () => {
											await processAlbums(target);
											if (mounted.current) {
												needsRefresh.current = true;
												tasks.reload();
											}
										});
								}}
							/>
						)
					}
				/>
			)}
			{conflict && (
				<Alert
					type="warning"
					showIcon
					title={t('posts.conflictTitle')}
					description={t('albums.conflictBody')}
					action={
						<Space wrap>
							{dirty && (
								<Button size="small" disabled={busy} onClick={() => resolveConflict(true)}>
									{t('albums.keepDraft')}
								</Button>
							)}
							<Button size="small" disabled={busy} onClick={() => resolveConflict(false)}>
								{t(dirty ? 'posts.discardLocal' : 'common.refresh')}
							</Button>
						</Space>
					}
				/>
			)}
			{error && (
				<Alert
					type="error"
					showIcon
					title={t(error)}
					action={
						<RetryButton onClick={() => (index.error ? index.reload() : void loadYear(year))} />
					}
				/>
			)}
			{(!index.data || (year && !album)) && !error && (
				<PageLoading spec="page" label={t('common.loading')} />
			)}
			{index.data && !year && (
				<Empty
					image={Empty.PRESENTED_IMAGE_SIMPLE}
					description={t(editable ? 'albums.noAlbumsEditable' : 'albums.noAlbums')}
				/>
			)}
			{album && target && (
				<>
					<header className="album-heading">
						<div className="album-heading-identity">
							<h1>{album.title?.trim() || t('albums.yearHeading', { year })}</h1>
							<div className="album-heading-meta">
								{album.title?.trim() && <span>{year}</span>}
								<span>{t('albums.photoCount', { count: album.photos.length })}</span>
								{dirty && <StatusTag kind="warning">{t('albums.unsaved')}</StatusTag>}
							</div>
						</div>
						{editable && (
							<Button
								disabled={busy}
								onClick={() =>
									setDialog({
										kind: 'settings',
										values: { title: album.title ?? '', cover: validAlbumCover(album) }
									})
								}
							>
								{t('albums.settings')}
							</Button>
						)}
					</header>
					{album.photos.length === 0 ? (
						<Empty
							image={Empty.PRESENTED_IMAGE_SIMPLE}
							description={t(editable ? 'albums.empty' : 'albums.noPhotos')}
						/>
					) : photos.length === 0 ? (
						<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('albums.noResults')}>
							<Button onClick={() => setParams({ q: null })}>{t('albums.clearSearch')}</Button>
						</Empty>
					) : (
						<AlbumPhotos
							target={target}
							photos={photos}
							order={album.photos}
							view={view}
							cover={cover}
							savedCover={album.cover}
							editable={editable}
							busy={busy}
							dirty={dirty || conflict}
							onOpen={(photo) =>
								setDialog({
									kind: 'photo',
									photo,
									values: { title: photo.title, description: photo.description, alt: photo.alt }
								})
							}
							onCover={(filename) => updateAlbum({ ...album, cover: filename })}
							onMove={move}
							onRemove={confirmRemoval}
						/>
					)}
					{dialog && (
						<AlbumDialogs
							dialog={dialog}
							target={target}
							album={album}
							editable={editable}
							busy={busy}
							onChange={setDialog}
							onApply={applyDialog}
							onClose={() => setDialog(null)}
						/>
					)}
				</>
			)}
		</div>
	);
}
