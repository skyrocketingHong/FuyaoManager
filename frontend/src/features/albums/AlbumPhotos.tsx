import { useEffect, useRef, useState } from 'react';
import { Button, Dropdown, type MenuProps } from 'antd';
import { ArrowDown, ArrowUp, Ellipsis, ImageOff } from 'lucide-react';
import DataTable, { compareTableValues } from '../../components/DataTable';
import { TablePanel } from '../../components/Panel';
import StatusTag from '../../components/StatusTag';
import { getAlbumThumbnailUrl, type AlbumPhotoEntry } from '../../lib/api/management';
import type { ContentTarget } from '../../lib/api/content';
import { t } from '../../lib/i18n';
import { mountAlbumHorizontalScroll, staggerAlbumCards } from './galleryMotion';

export type PhotoRemoval = 'detach' | 'delete';

export function AlbumThumbnail({
	target,
	photo,
	size,
	eager = false
}: {
	target: ContentTarget;
	photo: AlbumPhotoEntry;
	size: 'tile' | 'row' | 'detail';
	eager?: boolean;
}) {
	const width = size === 'detail' ? 960 : size === 'tile' ? 480 : 120;
	const variants = (photo.variants ?? [])
		.filter((variant) => variant.path && variant.width && variant.width > 0)
		.sort((a, b) => (a.width ?? 0) - (b.width ?? 0));
	const variant = variants.find((item) => (item.width ?? 0) >= width) ?? variants.at(-1);
	const url = variant?.path ? getAlbumThumbnailUrl(target, variant.path) : '';
	const [failed, setFailed] = useState('');
	const available = url && failed !== url;

	return (
		<span className={`album-thumbnail album-thumbnail--${size}`}>
			{available ? (
				<img
					src={url}
					alt={size === 'detail' ? photo.alt || photo.title || photo.filename : ''}
					width={variant?.width}
					height={variant?.height}
					loading={eager ? 'eager' : 'lazy'}
					decoding="async"
					onError={() => setFailed(url)}
				/>
			) : (
				<span className="album-thumbnail-empty" title={t('albums.noPreview')}>
					<ImageOff size={size === 'row' ? 16 : 24} aria-hidden="true" />
					{size === 'detail' && t('albums.noPreview')}
				</span>
			)}
		</span>
	);
}

function PhotoTitle({ photo, cover }: { photo: AlbumPhotoEntry; cover: boolean }) {
	return (
		<span className="album-photo-label">
			<span className="album-photo-title" title={photo.title || photo.filename}>
				{photo.title || photo.filename}
			</span>
			<span className="album-photo-meta">
				{photo.date && <time dateTime={photo.date.slice(0, 10)}>{photo.date.slice(0, 10)}</time>}
				{cover && <StatusTag kind="info">{t('albums.coverTag')}</StatusTag>}
			</span>
		</span>
	);
}

interface AlbumPhotosProps {
	target: ContentTarget;
	photos: AlbumPhotoEntry[];
	order: readonly AlbumPhotoEntry[];
	cover?: string;
	savedCover?: string;
	view: 'grid' | 'gallery' | 'list';
	editable: boolean;
	busy: boolean;
	dirty: boolean;
	onOpen: (photo: AlbumPhotoEntry) => void;
	onCover: (filename: string) => void;
	onMove: (filename: string, direction: -1 | 1) => void;
	onRemove: (photo: AlbumPhotoEntry, mode: PhotoRemoval) => void;
}

export default function AlbumPhotos({
	target,
	photos,
	order,
	cover,
	savedCover,
	view,
	editable,
	busy,
	dirty,
	onOpen,
	onCover,
	onMove,
	onRemove
}: AlbumPhotosProps) {
	const viewport = useRef<HTMLElement>(null);
	const track = useRef<HTMLUListElement>(null);
	useEffect(() => {
		if (view === 'list' || !track.current) return;
		const stopScroll =
			view === 'gallery' && viewport.current
				? mountAlbumHorizontalScroll(viewport.current, track.current)
				: undefined;
		const stopStagger = staggerAlbumCards(track.current);
		return () => {
			stopStagger?.();
			stopScroll?.();
		};
	}, [view]);

	const positions = new Map(order.map((photo, i) => [photo.filename, i]));

	function actions(photo: AlbumPhotoEntry) {
		if (!editable) return null;
		const position = positions.get(photo.filename) ?? -1;
		const items: MenuProps['items'] = [
			{
				key: 'cover',
				label: t('albums.setCover'),
				disabled: savedCover === photo.filename,
				onClick: () => onCover(photo.filename)
			},
			...(view !== 'list'
				? [
						{
							key: 'up',
							label: t('common.moveUp'),
							disabled: position <= 0,
							onClick: () => onMove(photo.filename, -1)
						},
						{
							key: 'down',
							label: t('common.moveDown'),
							disabled: position < 0 || position === order.length - 1,
							onClick: () => onMove(photo.filename, 1)
						}
					]
				: []),
			{ type: 'divider' },
			{
				key: 'detach',
				label: t('albums.detach'),
				disabled: dirty,
				onClick: () => onRemove(photo, 'detach')
			},
			{
				key: 'delete',
				label: t('albums.deletePhoto'),
				danger: true,
				disabled: dirty,
				onClick: () => onRemove(photo, 'delete')
			}
		];
		return (
			<Dropdown menu={{ items }} trigger={['click']} placement="bottomRight" disabled={busy}>
				<Button
					type="text"
					className="album-icon-action"
					icon={<Ellipsis size={18} />}
					aria-label={t('albums.photoActions', { photo: photo.title || photo.filename })}
				/>
			</Dropdown>
		);
	}

	if (view !== 'list') {
		const tiles = (
			<ul ref={track} className="album-photo-grid" aria-label={t('albums.photoColumn')}>
				{photos.map((photo, i) => (
					<li className="album-photo-tile" key={photo.filename}>
						<button
							type="button"
							className="album-tile-image"
							onClick={() => onOpen(photo)}
							aria-label={t('albums.openPhoto', { photo: photo.title || photo.filename })}
						>
							<span className="album-book">
								<span className="album-book-front">
									<AlbumThumbnail target={target} photo={photo} size="tile" eager={i === 0} />
								</span>
								<span className="album-book-pages" aria-hidden="true" />
								<span className="album-book-back" aria-hidden="true" />
							</span>
						</button>
						<div className="album-tile-footer">
							<PhotoTitle photo={photo} cover={photo.filename === cover} />
							{actions(photo)}
						</div>
					</li>
				))}
			</ul>
		);
		return view === 'gallery' ? (
			<section ref={viewport} className="album-gallery" aria-label={t('albums.galleryRegion')}>
				<div className="album-gallery-runway">
					<div className="album-gallery-window">{tiles}</div>
				</div>
			</section>
		) : (
			tiles
		);
	}

	return (
		<TablePanel>
			<DataTable<AlbumPhotoEntry>
				scrollLabel={t('nav.albums')}
				className="albums-table"
				rowKey="filename"
				dataSource={photos}
				pagination={false}
				columns={[
					{
						title: t('albums.photoColumn'),
						sorter: (a, b) => compareTableValues(a.title || a.filename, b.title || b.filename),
						render: (_, photo) => (
							<button
								type="button"
								className="album-row-photo"
								onClick={() => onOpen(photo)}
								aria-label={t('albums.openPhoto', { photo: photo.title || photo.filename })}
							>
								<AlbumThumbnail target={target} photo={photo} size="row" />
								<PhotoTitle photo={photo} cover={photo.filename === cover} />
							</button>
						)
					},
					{
						title: t('albums.dateColumn'),
						dataIndex: 'date',
						width: 132,
						render: (date?: string) => date?.slice(0, 10) || '—'
					},
					{
						title: t('albums.altColumn'),
						dataIndex: 'alt',
						width: 116,
						render: (alt?: string) => (
							<StatusTag kind={alt?.trim() ? 'success' : 'neutral'}>
								{t(alt?.trim() ? 'albums.hasAlt' : 'albums.noAlt')}
							</StatusTag>
						)
					},
					...(editable
						? [
								{
									title: t('admin.actions'),
									key: 'actions',
									width: 155,
									render: (_: unknown, photo: AlbumPhotoEntry) => {
										const position = positions.get(photo.filename) ?? -1;
										return (
											<div className="album-list-actions">
												<Button
													type="text"
													className="album-icon-action"
													icon={<ArrowUp size={16} />}
													aria-label={t('common.moveUp')}
													title={t('common.moveUp')}
													disabled={busy || position <= 0}
													onClick={() => onMove(photo.filename, -1)}
												/>
												<Button
													type="text"
													className="album-icon-action"
													icon={<ArrowDown size={16} />}
													aria-label={t('common.moveDown')}
													title={t('common.moveDown')}
													disabled={busy || position < 0 || position === order.length - 1}
													onClick={() => onMove(photo.filename, 1)}
												/>
												{actions(photo)}
											</div>
										);
									}
								}
							]
						: [])
				]}
			/>
		</TablePanel>
	);
}
