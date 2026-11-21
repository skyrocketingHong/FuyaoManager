import { api } from './client';
import type { components } from './schema';

export type ThemeSource = components['schemas']['ThemeSource'];
export type ThemeCandidate = components['schemas']['ThemeCandidate'];
export type ThemeInstall = components['schemas']['ThemeInstall'];

export async function readThemeSources(signal?: AbortSignal): Promise<ThemeSource[]> {
	return (await api<{ sources: ThemeSource[] }>('GET', '/api/v1/themes/sources', { signal })).data.sources;
}

export async function checkThemeSource(id: string): Promise<ThemeCandidate> {
	return (await api<ThemeCandidate>('GET', `/api/v1/themes/sources/${encodeURIComponent(id)}/candidate`)).data;
}

export async function installThemeSource(id: string, commit: string): Promise<ThemeInstall> {
	return (await api<ThemeInstall>('POST', `/api/v1/themes/sources/${encodeURIComponent(id)}/install`, {
		body: { commit }
	})).data;
}
