import { api } from './client';
import type { components } from './schema';

export type UpdateStatus = components['schemas']['PlatformUpdateStatus'];
export type UpdateAction = components['schemas']['PlatformUpdateAction'];
export type UpdateVersion = components['schemas']['PlatformUpdateVersion'];

export async function readUpdates(signal?: AbortSignal): Promise<UpdateStatus> {
	return (await api<UpdateStatus>('GET', '/api/v1/admin/updates', { signal })).data;
}

export async function manageUpdates(body: UpdateAction): Promise<UpdateStatus> {
	return (await api<UpdateStatus>('POST', '/api/v1/admin/updates', { body })).data;
}
