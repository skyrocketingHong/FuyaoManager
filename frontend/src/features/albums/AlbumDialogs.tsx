import { Input, Modal, Select } from 'antd';
import { AlbumThumbnail } from './AlbumPhotos';
import type { ContentTarget } from '../../lib/api/content';
import type { AlbumPhotoEntry, AlbumYear } from '../../lib/api/management';
import { t } from '../../lib/i18n';

export type PhotoFields = Pick<AlbumPhotoEntry, 'title' | 'description' | 'alt'>;
export type AlbumFields = Pick<AlbumYear, 'title' | 'cover'>;
export type AlbumDialog =
	| { kind: 'photo'; photo: AlbumPhotoEntry; values: PhotoFields }
	| { kind: 'settings'; values: AlbumFields };

export default function AlbumDialogs({
	dialog,
	target,
	album,
	editable,
	busy,
	onChange,
	onApply,
	onClose
}: {
	dialog: AlbumDialog;
	target: ContentTarget;
	album: AlbumYear;
	editable: boolean;
	busy: boolean;
	onChange: (dialog: AlbumDialog) => void;
	onApply: () => void;
	onClose: () => void;
}) {
	if (dialog.kind === 'settings') {
		const { title, cover } = dialog.values;
		return (
			<Modal
				open
				title={t('albums.settings')}
				onCancel={onClose}
				onOk={onApply}
				okText={t('albums.apply')}
				okButtonProps={{ disabled: !editable || busy }}
			>
				<div className="album-dialog-fields">
					<label className="field">
						<span className="field-label">{t('albums.albumTitle')}</span>
						<Input
							value={title ?? ''}
							placeholder={String(album.year)}
							disabled={!editable || busy}
							onChange={(event) =>
								onChange({ ...dialog, values: { ...dialog.values, title: event.target.value } })
							}
						/>
					</label>
					<label className="field">
						<span className="field-label">{t('albums.coverPlaceholder')}</span>
						<Select
							aria-label={t('albums.coverPlaceholder')}
							value={cover || undefined}
							placeholder={t('albums.automaticCover')}
							allowClear
							showSearch={{ optionFilterProp: 'label' }}
							disabled={!editable || busy || album.photos.length === 0}
							options={album.photos.map((photo) => ({
								label: photo.title || photo.filename,
								value: photo.filename
							}))}
							onChange={(value) =>
								onChange({ ...dialog, values: { ...dialog.values, cover: value ?? '' } })
							}
						/>
					</label>
				</div>
			</Modal>
		);
	}

	const { photo, values } = dialog;
	const fields = ['title', 'description', 'alt'] as const;
	const labels = {
		title: 'albums.fieldTitle',
		description: 'albums.fieldDescription',
		alt: 'albums.fieldAlt'
	} as const;
	return (
		<Modal
			open
			title={editable ? t('albums.editTitle') : t('albums.photoDetails')}
			width={720}
			onCancel={onClose}
			onOk={onApply}
			okText={t('albums.apply')}
			okButtonProps={{ disabled: busy }}
			footer={editable ? undefined : null}
		>
			<div className="album-photo-detail">
				<AlbumThumbnail target={target} photo={photo} size="detail" eager />
				<div className="album-dialog-fields">
					<div className="album-file-facts">
						<span className="version-text">{photo.path || photo.filename}</span>
						{photo.date && (
							<time dateTime={photo.date.slice(0, 10)}>{photo.date.slice(0, 10)}</time>
						)}
					</div>
					{editable ? (
						fields.map((field) => {
							const Control = field === 'title' ? Input : Input.TextArea;
							return (
								<label className="field" key={field}>
									<span className="field-label">{t(labels[field])}</span>
									<Control
										value={values[field] ?? ''}
										disabled={busy}
										onChange={(event) =>
											onChange({ ...dialog, values: { ...values, [field]: event.target.value } })
										}
									/>
								</label>
							);
						})
					) : (
						<dl className="album-photo-properties">
							{fields
								.filter((field) => values[field]?.trim())
								.map((field) => (
									<div key={field}>
										<dt>{t(labels[field])}</dt>
										<dd>{values[field]}</dd>
									</div>
								))}
						</dl>
					)}
				</div>
			</div>
		</Modal>
	);
}
