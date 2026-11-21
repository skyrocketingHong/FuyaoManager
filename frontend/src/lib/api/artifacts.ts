import { api } from './client';
import type { components } from './schema';

export type ArtifactKind = 'builds' | 'releases';
export type ArtifactState = NonNullable<components['schemas']['BuildTask']['artifactState']>;

export async function deleteArtifacts(siteId: string, kind: ArtifactKind, ids: string[]): Promise<void> {
	await api('POST', `/api/v1/sites/${encodeURIComponent(siteId)}/${kind}/artifacts/delete`, { body: { ids } });
}
