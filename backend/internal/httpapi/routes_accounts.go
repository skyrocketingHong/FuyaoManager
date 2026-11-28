package httpapi

import "net/http"

func (a *API) routesAccounts() []routeSpec {
	return []routeSpec{
		// WebAuthn ceremonies (no prior session; protected by permit,
		// challenge, and Origin checks). On IP-literal deployments the RP ID
		// cannot be a domain string, so the relying party is unavailable and
		// these routes answer 503; one-time-code login is unaffected.
		{http.MethodPost, "/api/v1/auth/register/options", "startPasskeyEnrollment", a.passkeyRoute(a.handleRegisterOptions)},
		{http.MethodPost, "/api/v1/auth/register/verify", "verifyPasskeyEnrollment", a.passkeyRoute(a.handleRegisterVerify)},
		{http.MethodPost, "/api/v1/auth/login/options", "startPasskeyLogin", a.passkeyRoute(a.handleLoginOptions)},
		{http.MethodPost, "/api/v1/auth/login/verify", "verifyPasskeyLogin", a.passkeyRoute(a.handleLoginVerify)},

		// One-time CLI-issued code redemption (initialization and recovery).
		{http.MethodPost, "/api/v1/auth/exchange", "exchangeAuthCode", a.handleExchangeCode},

		// Session-bound passkey binding: the only registration path once the
		// platform has an active administrator. The restricted registration
		// session (issued when the account has no credential for the current
		// RP) may drive exactly these two endpoints plus session read/logout.
		{http.MethodPost, "/api/v1/auth/passkey/register/options", "startPasskeyBinding", a.passkeyRoute(a.requireAuth(a.withCSRF(a.handleBindOptions)))},
		{http.MethodPost, "/api/v1/auth/passkey/register/verify", "verifyPasskeyBinding", a.passkeyRoute(a.requireAuth(a.withCSRF(a.handleBindVerify)))},

		{http.MethodGet, "/api/v1/session", "getSession", a.handleSession},
		{http.MethodGet, "/api/v1/admin/users", "listUsers", a.requireFullScope(a.adminRoute(a.handleListUsers))},
		{http.MethodPost, "/api/v1/admin/users", "createUser", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleCreateUser)))},
		{http.MethodGet, "/api/v1/admin/users/{userId}", "getUser", a.requireFullScope(a.adminRoute(a.handleGetUser))},
		{http.MethodPatch, "/api/v1/admin/users/{userId}/profile", "updateUserProfile", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleAdminUpdateProfile)))},
		{http.MethodPut, "/api/v1/admin/users/{userId}/role", "setUserRole", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleSetUserRole)))},
		{http.MethodPut, "/api/v1/admin/users/{userId}/status", "setUserStatus", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleSetUserStatus)))},
		{http.MethodDelete, "/api/v1/admin/users/{userId}", "deleteUser", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleDeleteUser)))},
		{http.MethodPost, "/api/v1/admin/users/{userId}/login-code", "issueUserLoginCode", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleIssueUserCode)))},
		{http.MethodPut, "/api/v1/admin/users/{userId}/site-access/{siteId}", "setUserSiteAccess", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleSetUserSiteAccess)))},
		{http.MethodDelete, "/api/v1/admin/users/{userId}/sessions", "revokeUserSessions", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleRevokeUserSessions)))},
		{http.MethodDelete, "/api/v1/admin/users/{userId}/sessions/{sessionId}", "revokeUserSession", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleRevokeUserSessions)))},
		{http.MethodDelete, "/api/v1/admin/users/{userId}/passkeys/{passkeyId}", "revokeUserPasskey", a.requireFullScope(a.withCSRF(a.adminRoute(a.handleRevokeUserPasskey)))},
		{http.MethodGet, "/api/v1/admin/audit", "listAuditEvents", a.requireFullScope(a.adminRoute(a.handleListAudit))},
		{http.MethodPost, "/api/v1/auth/logout", "logout", a.requireAuth(a.withCSRF(a.handleLogout))},

		// Self-service account profile: the target is always the caller's own
		// account — the endpoint never accepts a user id.
		{http.MethodPatch, "/api/v1/account/profile", "updateAccountProfile", a.requireFullScope(a.withCSRF(a.handleUpdateAccountProfile))},
	}
}
