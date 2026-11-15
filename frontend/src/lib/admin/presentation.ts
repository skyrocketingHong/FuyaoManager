import { t, type MessageKey } from '../i18n';
import { formatTime } from '../time';

export function timeText(value: string | null): string {
	if (!value) return t('admin.never');
	return formatTime(value);
}

// Same action-label lookup pattern as robot-platform/settings/LogsPage.tsx;
// only manager operations belong in this catalogue.
export const auditLabels: Record<string, MessageKey> = {
	createUser: 'audit.action.createUser',
	updateUserProfile: 'audit.action.updateUserProfile',
	setUserRole: 'audit.action.setUserRole',
	setUserStatus: 'audit.action.setUserStatus',
	deleteUser: 'audit.action.deleteUser',
	issueUserLoginCode: 'audit.action.issueUserLoginCode',
	setUserSiteAccess: 'audit.action.setUserSiteAccess',
	revokeUserSessions: 'audit.action.revokeUserSessions',
	revokeUserSession: 'audit.action.revokeUserSession',
	revokeUserPasskey: 'audit.action.revokeUserPasskey',
	exchangeAuthCode: 'audit.action.exchangeAuthCode',
	verifyPasskeyLogin: 'audit.action.verifyPasskeyLogin',
	verifyPasskeyBinding: 'audit.action.verifyPasskeyBinding',
	verifyPasskeyEnrollment: 'audit.action.verifyPasskeyBinding',
	startPasskeyLogin: 'audit.action.startPasskeyLogin',
	startPasskeyBinding: 'audit.action.startPasskeyBinding',
	startPasskeyEnrollment: 'audit.action.startPasskeyBinding',
	logout: 'audit.action.logout',
	updateAccountProfile: 'audit.action.updateUserProfile',
	createSite: 'audit.action.createSite',
	provisionSite: 'audit.action.provisionSite',
	putSiteVisibility: 'audit.action.putSiteVisibility',
	updateSiteSettings: 'audit.action.updateSiteSettings',
	putSiteTheme: 'audit.action.putSiteTheme',
	createPost: 'audit.action.createPost',
	putPost: 'audit.action.putPost',
	putPostVocabulary: 'audit.action.putPostVocabulary',
	platformPutPostVocabulary: 'audit.action.putPostVocabulary',
	putPostDirectory: 'audit.action.putPostDirectory',
	platformPutPostDirectory: 'audit.action.putPostDirectory',
	deletePost: 'audit.action.deletePost',
	putFriends: 'audit.action.putFriends',
	putSocialLinks: 'audit.action.putSocialLinks',
	reapplySiteEdge: 'audit.action.reapplySiteEdge',
	putSiteConfig: 'audit.action.putSiteConfig',
	uploadAlbumPhoto: 'audit.action.uploadAlbumPhoto',
	processAlbums: 'audit.action.processAlbums',
	putAlbumYear: 'audit.action.putAlbumYear',
	deleteAlbumPhoto: 'audit.action.deleteAlbumPhoto',
	putFootprints: 'audit.action.putFootprints',
	createBuild: 'audit.action.createBuild',
	createPreviewLink: 'audit.action.createPreviewLink',
	closePreviewLink: 'audit.action.closePreviewLink',
	createRelease: 'audit.action.createRelease',
	rollbackRelease: 'audit.action.rollbackRelease'
};
export function operationText(operation: string): string {
	return auditLabels[operation] ? t(auditLabels[operation]) : operation;
}

export function changeValueText(field: string | undefined, value: string | undefined): string {
	if (value === undefined) return t('admin.never');
	if (field === 'siteRole' && value === '') return t('admin.noAccess');
	if (field === 'siteRole' && ['owner', 'editor', 'viewer'].includes(value))
		return t(`role.${value}` as MessageKey);
	if (field === 'platformRole' && ['admin', 'member'].includes(value))
		return t(`admin.role.${value}` as MessageKey);
	if (field === 'status' && ['pending', 'active', 'disabled', 'deleted'].includes(value))
		return t(`admin.status.${value}` as MessageKey);
	return value;
}
