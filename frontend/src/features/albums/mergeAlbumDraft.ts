import type { AlbumYear } from '../../lib/api/management';

export function validAlbumCover(album: AlbumYear): string {
	return album.photos.some((photo) => photo.filename === album.cover) ? (album.cover ?? '') : '';
}

export function mergeAlbumDraft(base: AlbumYear, draft: AlbumYear, incoming: AlbumYear): AlbumYear {
	const originals = new Map(base.photos.map((photo) => [photo.filename, photo]));
	const edits = new Map(draft.photos.map((photo) => [photo.filename, photo]));
	const fresh = new Map(incoming.photos.map((photo) => [photo.filename, photo]));
	for (const [name, photo] of fresh) {
		const original = originals.get(name);
		const edit = edits.get(name);
		if (!original || !edit) continue;
		fresh.set(name, {
			...photo,
			title: edit.title !== original.title ? edit.title : photo.title,
			description: edit.description !== original.description ? edit.description : photo.description,
			alt: edit.alt !== original.alt ? edit.alt : photo.alt
		});
	}
	const reordered = draft.photos.some((photo, i) => photo.filename !== base.photos[i]?.filename);
	// Preserve the locally chosen sequence of surviving photos, with new photos appended.
	const order = reordered
		? [...new Set([...draft.photos.map((photo) => photo.filename), ...fresh.keys()])]
		: [...fresh.keys()];
	const merged = {
		...incoming,
		title: draft.title !== base.title ? draft.title : incoming.title,
		cover: draft.cover !== base.cover ? draft.cover : incoming.cover,
		photos: order.flatMap((name) => {
			const photo = fresh.get(name);
			return photo ? [photo] : [];
		})
	};
	return { ...merged, cover: validAlbumCover(merged) };
}
