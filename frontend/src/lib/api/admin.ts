import { api } from './client';
import type { components } from './schema';

export type AdminUser = components['schemas']['AdminUser'];
export type AdminUserPage = components['schemas']['AdminUserPage'];
export type AdminUserDetail = components['schemas']['AdminUserDetail'];
export type AdminProfile = components['schemas']['AdminProfileInput'];
export type AccountCode = components['schemas']['AccountCode'];
export type AuditPage = components['schemas']['AuditPage'];
export type AuditEvent = components['schemas']['AuditEvent'];
export type SiteAccess = AdminUserDetail['access']['sites'][number];

export function queryString(values: Record<string, string | number | undefined>): string {
	const query = new URLSearchParams();
	for (const [key, value] of Object.entries(values))
		if (value !== undefined && value !== '') query.set(key, String(value));
	return query.toString();
}
const userPath = (id: string) => `/api/v1/admin/users/${encodeURIComponent(id)}`;
export async function listUsers(query: string, signal?: AbortSignal): Promise<AdminUserPage> {
	return (await api<AdminUserPage>('GET', `/api/v1/admin/users?${query}`, { signal })).data;
}
export async function getUser(id: string, signal?: AbortSignal): Promise<AdminUserDetail> {
	return (await api<AdminUserDetail>('GET', userPath(id), { signal })).data;
}
export async function createUser(body: AdminProfile): Promise<AccountCode> {
	return (await api<AccountCode>('POST', '/api/v1/admin/users', { body })).data;
}
export async function updateUser(id: string, body: AdminProfile): Promise<AdminUser> {
	return (await api<AdminUser>('PATCH', `${userPath(id)}/profile`, { body })).data;
}
export async function setUserRole(
	id: string,
	platformRole: AdminUser['platformRole']
): Promise<AdminUser> {
	return (await api<AdminUser>('PUT', `${userPath(id)}/role`, { body: { platformRole } })).data;
}
export async function setUserStatus(id: string, status: 'active' | 'disabled'): Promise<AdminUser> {
	return (await api<AdminUser>('PUT', `${userPath(id)}/status`, { body: { status } })).data;
}
export async function deleteUser(id: string): Promise<void> {
	await api('DELETE', userPath(id));
}
export async function issueUserCode(id: string): Promise<AccountCode> {
	return (await api<AccountCode>('POST', `${userPath(id)}/login-code`)).data;
}
export async function setSiteAccess(
	id: string,
	siteId: string,
	role: SiteAccess['role']
): Promise<void> {
	await api('PUT', `${userPath(id)}/site-access/${encodeURIComponent(siteId)}`, { body: { role } });
}
export async function revokeSessions(id: string, sessionId?: string): Promise<void> {
	await api(
		'DELETE',
		`${userPath(id)}/sessions${sessionId ? `/${encodeURIComponent(sessionId)}` : ''}`
	);
}
export async function revokePasskey(id: string, keyId: string): Promise<void> {
	await api('DELETE', `${userPath(id)}/passkeys/${encodeURIComponent(keyId)}`);
}
export async function listAudit(query: string, signal?: AbortSignal): Promise<AuditPage> {
	return (await api<AuditPage>('GET', `/api/v1/admin/audit?${query}`, { signal })).data;
}
