import { expect, it } from 'vitest';
import { mergeAlbumDraft } from '../src/features/albums/mergeAlbumDraft';
import type { AlbumPhotoEntry, AlbumYear } from '../src/lib/api/management';

function photo(name: string, fields: Partial<AlbumPhotoEntry> = {}): AlbumPhotoEntry {
	return { filename: `${name}.jpg`, path: `2026/${name}.jpg`, ...fields };
}

it('rebases only the managed fields edited locally and keeps incoming photos', () => {
	const first = photo('first', { title: 'First' });
	const second = photo('second');
	const removed = photo('removed');
	const base: AlbumYear = { year: 2026, cover: removed.filename, photos: [first, second, removed] };
	const draft = {
		...base,
		photos: [{ ...second, alt: 'Local caption' }, first, removed]
	};
	const incoming = {
		...base,
		title: 'Server album',
		photos: [
			{ ...first, title: 'Server name' },
			{ ...second, description: 'Server description' },
			photo('new')
		]
	};
	const merged = mergeAlbumDraft(base, draft, incoming);
	expect(merged.title).toBe('Server album');
	expect(merged.cover).toBe('');
	expect(merged.photos).toEqual([
		{ ...second, description: 'Server description', alt: 'Local caption' },
		{ ...first, title: 'Server name' },
		photo('new')
	]);
});
