package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/testauth"
)

func adminClient(env *testEnv) (model.User, *tclient) {
	user, token, csrf := env.createUserSession("platform-admin")
	setUserRole(env.t, env, user.ID, model.PlatformRoleAdmin)
	return user, env.clientFor(token, csrf)
}
func adminRequest(t *testing.T, client *tclient, method, path string, body any, expected int) (http.Header, []byte) {
	t.Helper()
	status, headers, raw := client.do(method, path, body, nil)
	if status != expected {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, status, expected, raw)
	}
	return headers, raw
}
func createManagedUser(t *testing.T, client *tclient, username string) accountCodeDTO {
	t.Helper()
	headers, raw := adminRequest(t, client, "POST", "/api/v1/admin/users", map[string]string{"username": username, "displayName": username}, 201)
	if headers.Get("Cache-Control") != "no-store" {
		t.Fatal("registration code response can be cached")
	}
	var result accountCodeDTO
	decodeJSONBody(t, raw, &result)
	if result.User.PlatformRole != "member" || result.Code == "" || !result.ExpiresAt.After(time.Now()) {
		t.Fatal("account was not created as a member with a live code")
	}
	return result
}

func TestAdminAPIRequiresFullAdminAndCSRF(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	member, token, csrf := env.createUserSession("regular-member")
	client := env.clientFor(token, csrf)
	path := "/api/v1/admin/users/" + member.ID.String()
	for _, target := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/admin/users", nil}, {"GET", "/api/v1/admin/audit", nil}, {"GET", path, nil},
		{"POST", "/api/v1/admin/users", map[string]string{"username": "blocked", "displayName": "Blocked"}},
		{"PUT", path + "/role", map[string]string{"platformRole": "admin"}}, {"DELETE", path + "/sessions", nil},
	} {
		adminRequest(t, client, target.method, target.path, target.body, 403)
		adminRequest(t, env.newClient(), target.method, target.path, target.body, 401)
	}
	restricted, restrictedCSRF, _, err := env.store.Sessions.CreateWithKind(t.Context(), member.ID, model.SessionKindRestricted, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, env.clientFor(restricted, restrictedCSRF), "GET", "/api/v1/admin/users", nil, 403)
	adminRequest(t, env.clientFor(admin.token, ""), "POST", "/api/v1/admin/users", map[string]string{"username": "blocked", "displayName": "Blocked"}, 403)
	adminRequest(t, admin, "POST", "/api/v1/admin/users", map[string]string{"username": "blocked", "displayName": "Blocked", "platformRole": "admin"}, 400)
	adminRequest(t, admin, "POST", "/api/v1/admin/users", map[string]string{"username": "blocked"}, 422)
}

func TestAdminAPICreateBindAndAudit(t *testing.T) {
	env := setup(t)
	op, client := adminClient(env)
	created := createManagedUser(t, client, "new-member")
	path := "/api/v1/admin/users/" + created.User.ID
	_, raw := adminRequest(t, client, "GET", path, nil, 200)
	if strings.Contains(string(raw), "webauthn") || strings.Contains(string(raw), "csrfToken") || strings.Contains(string(raw), created.Code) {
		t.Fatal("account detail exposed authentication material")
	}
	member := env.newClient()
	_, raw = adminRequest(t, member, "POST", "/api/v1/auth/exchange", map[string]string{"code": created.Code}, 200)
	var exchanged ceremonyDone
	decodeJSONBody(t, raw, &exchanged)
	if exchanged.SessionScope != model.SessionKindRestricted {
		t.Fatal("new account bypassed passkey binding")
	}
	member.csrf = exchanged.CSRFToken
	adminRequest(t, member, "GET", "/api/v1/sites", nil, 403)
	_, raw = adminRequest(t, member, "POST", "/api/v1/auth/passkey/register/options", map[string]string{}, 200)
	var options creationOptionsResp
	decodeJSONBody(t, raw, &options)
	authenticator := testauth.New()
	_, raw = adminRequest(t, member, "POST", "/api/v1/auth/passkey/register/verify", authenticator.CreateCredential("localhost", env.base, options.PublicKey.Challenge), 200)
	var bound ceremonyDone
	decodeJSONBody(t, raw, &bound)
	member.csrf = bound.CSRFToken
	adminRequest(t, member, "GET", "/api/v1/sites", nil, 200)
	adminRequest(t, member, "GET", "/api/v1/admin/users", nil, 403)
	_, raw = adminRequest(t, client, "GET", "/api/v1/admin/audit?operation=createUser", nil, 200)
	var audit store.AuditPage
	decodeJSONBody(t, raw, &audit)
	if audit.Total != 1 || audit.Entries[0].ActorID == nil || *audit.Entries[0].ActorID != op.ID || audit.Entries[0].Status != 201 || audit.Entries[0].ResourceID != created.User.ID {
		t.Fatalf("wrong create audit: %+v", audit)
	}
	if strings.Contains(string(raw), created.Code) || strings.Contains(string(raw), client.token) || strings.Contains(string(raw), client.csrf) {
		t.Fatal("audit retained a secret")
	}
	_, raw = adminRequest(t, client, "GET", "/api/v1/admin/audit?outcome=failure", nil, 200)
	decodeJSONBody(t, raw, &audit)
	if audit.Total < 2 {
		t.Fatal("restricted or unauthorized attempts were not audited")
	}
}

func TestAdminAPILifecycleAndSiteOwnerGuards(t *testing.T) {
	env := setup(t)
	op, admin := adminClient(env)
	owner, oldToken, oldCSRF := env.createUserSession("site-owner")
	site := createSite(t.Context(), t, env, "protected-site", "Protected", "", "zh-CN", owner.ID)
	opPath := "/api/v1/admin/users/" + op.ID.String()
	path := "/api/v1/admin/users/" + owner.ID.String()
	_, raw := adminRequest(t, admin, "PUT", opPath+"/role", map[string]string{"platformRole": "member"}, 409)
	if !strings.Contains(string(raw), "last_admin") {
		t.Fatal(string(raw))
	}
	adminRequest(t, admin, "PUT", opPath+"/status", map[string]string{"status": "disabled"}, 409)
	_, raw = adminRequest(t, admin, "PUT", path+"/status", map[string]string{"status": "disabled"}, 409)
	if !strings.Contains(string(raw), "last_owner") {
		t.Fatal(string(raw))
	}
	_, raw = adminRequest(t, admin, "GET", path, nil, 200)
	if !strings.Contains(string(raw), "Protected") {
		t.Fatal("site access catalog did not load")
	}
	accessPath := path + "/site-access/" + site.Site.ID.String()
	adminRequest(t, admin, "PUT", accessPath, map[string]string{"role": "viewer"}, 409)
	adminRequest(t, admin, "PUT", opPath+"/site-access/"+site.Site.ID.String(), map[string]string{"role": "owner"}, 200)
	adminRequest(t, admin, "PUT", accessPath, map[string]string{"role": "viewer"}, 200)
	adminRequest(t, admin, "PUT", path+"/status", map[string]string{"status": "disabled"}, 200)
	old := env.clientFor(oldToken, oldCSRF)
	adminRequest(t, old, "GET", "/api/v1/sites", nil, 401)
	adminRequest(t, admin, "PUT", path+"/status", map[string]string{"status": "active"}, 200)
	adminRequest(t, old, "GET", "/api/v1/sites", nil, 401)
	adminRequest(t, admin, "DELETE", path, nil, 204)
	adminRequest(t, admin, "PUT", path+"/status", map[string]string{"status": "active"}, 409)
	adminRequest(t, admin, "PATCH", path+"/profile", map[string]string{"username": "deleted-edit", "displayName": "Deleted"}, 404)
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/users?status=deleted", nil, 200)
	if !strings.Contains(string(raw), owner.ID.String()) {
		t.Fatal("explicit deleted history missing")
	}
}

func TestAdminAPIRecoveryAndScopedSessionRevocation(t *testing.T) {
	env := setup(t)
	op, admin := adminClient(env)
	created := createManagedUser(t, admin, "recover-member")
	path := "/api/v1/admin/users/" + created.User.ID
	_, raw := adminRequest(t, admin, "POST", path+"/login-code", nil, 200)
	var replacement accountCodeDTO
	decodeJSONBody(t, raw, &replacement)
	adminRequest(t, env.newClient(), "POST", "/api/v1/auth/exchange", map[string]string{"code": created.Code}, 409)
	adminRequest(t, env.newClient(), "POST", "/api/v1/auth/exchange", map[string]string{"code": replacement.Code}, 200)
	var adminSessionID uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT id FROM sessions WHERE user_id=$1 AND token_hash=$2`, op.ID, sha256Sum64(admin.token)).Scan(&adminSessionID); err != nil {
		t.Fatal(err)
	}
	adminRequest(t, admin, "DELETE", path+"/sessions/"+adminSessionID.String(), nil, 404)
	adminRequest(t, admin, "GET", "/api/v1/admin/users", nil, 200)
	adminRequest(t, admin, "DELETE", path+"/sessions", nil, 204)
	var live int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, created.User.ID).Scan(&live); err != nil || live != 0 {
		t.Fatalf("live sessions after revocation: %d %v", live, err)
	}
}

func TestAdminAPIPasskeyRevocationRejectsStaleVerifiedCredential(t *testing.T) {
	env := setup(t)
	op, admin := adminClient(env)
	user, token, csrf := env.createUserSession("key-owner")
	keys, err := env.store.Passkeys.ListByUser(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	old := keys[0]
	env.bindPasskey(user.ID, testauth.New())
	opKeys, err := env.store.Passkeys.ListByUser(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, admin, "DELETE", "/api/v1/admin/users/"+op.ID.String()+"/passkeys/"+opKeys[0].ID.String(), nil, 409)
	path := "/api/v1/admin/users/" + user.ID.String() + "/passkeys/"
	adminRequest(t, admin, "DELETE", path+opKeys[0].ID.String(), nil, 404)
	adminRequest(t, admin, "DELETE", path+old.ID.String(), nil, 204)
	adminRequest(t, env.clientFor(token, csrf), "GET", "/api/v1/sites", nil, 401)
	if _, _, _, err := env.store.Sessions.CreateForPasskey(t.Context(), user.ID, "localhost", old.CredentialID, time.Hour); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("stale verified key created a session: %v", err)
	}
	if count, err := env.store.Passkeys.CountByUserRP(t.Context(), user.ID, "localhost"); err != nil || count != 1 {
		t.Fatalf("other key removed: %d %v", count, err)
	}
	response := httptest.NewRecorder()
	api := &API{store: env.store, cfg: Config{RPID: "localhost", SessionTTL: time.Hour}}
	api.finishLogin(response, httptest.NewRequest("POST", "/api/v1/auth/login/verify", nil), user, old.CredentialID)
	if response.Code != 401 {
		t.Fatal("stale authentication completion was accepted")
	}
}

func TestAdminAPIAuditFailureRollsBackAccountCreation(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	if _, err := env.pool.Exec(t.Context(), `ALTER TABLE audit_events ADD CONSTRAINT reject_success CHECK(operation<>'createUser' OR outcome<>'success')`); err != nil {
		t.Fatal(err)
	}
	adminRequest(t, admin, "POST", "/api/v1/admin/users", map[string]string{"username": "must-rollback", "displayName": "Rollback"}, 500)
	if _, err := env.store.Users.GetByUsername(context.Background(), "must-rollback"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("account survived failed audit: %v", err)
	}
	_, raw := adminRequest(t, admin, "GET", "/api/v1/admin/audit?operation=createUser", nil, 200)
	var audit store.AuditPage
	decodeJSONBody(t, raw, &audit)
	if audit.Total != 1 || audit.Entries[0].Outcome != "failure" {
		t.Fatalf("misleading success event: %+v", audit)
	}
}

func TestAdminAPIFiltersAndValidation(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	createManagedUser(t, admin, "filter-one")
	createManagedUser(t, admin, "filter-two")
	_, raw := adminRequest(t, admin, "GET", "/api/v1/admin/users?q=filter-&pageSize=1&page=2", nil, 200)
	var page struct {
		Users []adminUserDTO `json:"users"`
		Total int            `json:"total"`
	}
	decodeJSONBody(t, raw, &page)
	if page.Total != 2 || len(page.Users) != 1 {
		t.Fatalf("pagination failed: %s", raw)
	}
	for _, test := range []struct{ order, first, second string }{
		{"asc", "filter-one", "filter-two"},
		{"desc", "filter-two", "filter-one"},
	} {
		for index, expected := range []string{test.first, test.second} {
			_, raw = adminRequest(t, admin, "GET", fmt.Sprintf("/api/v1/admin/users?q=filter-&pageSize=1&page=%d&sort=username&order=%s", index+1, test.order), nil, 200)
			decodeJSONBody(t, raw, &page)
			if len(page.Users) != 1 || page.Users[0].Username != expected {
				t.Fatalf("sorted account page: %s", raw)
			}
		}
	}
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/audit?sort=operation&order=asc&pageSize=1", nil, 200)
	var sortedAudit store.AuditPage
	decodeJSONBody(t, raw, &sortedAudit)
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/audit?sort=operation&order=desc&pageSize=1", nil, 200)
	var reverseAudit store.AuditPage
	decodeJSONBody(t, raw, &reverseAudit)
	if len(sortedAudit.Entries) != 1 || len(reverseAudit.Entries) != 1 || sortedAudit.Entries[0].Operation > reverseAudit.Entries[0].Operation {
		t.Fatalf("audit sorting failed: %+v %+v", sortedAudit, reverseAudit)
	}
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/users?q=%25", nil, 200)
	decodeJSONBody(t, raw, &page)
	if page.Total != 0 {
		t.Fatal("literal LIKE search became a wildcard")
	}
	for _, path := range []string{"/api/v1/admin/users?page=0", "/api/v1/admin/users?status=other", "/api/v1/admin/users?pageSize=201", "/api/v1/admin/users?sort=username", "/api/v1/admin/users?sort=unknown&order=asc", "/api/v1/admin/audit?sort=operation&order=bad", "/api/v1/admin/audit?outcome=other", "/api/v1/admin/audit?from=wrong", "/api/v1/admin/audit?from=2027-01-01T00:00:00Z&until=2026-01-01T00:00:00Z"} {
		adminRequest(t, admin, "GET", path, nil, 400)
	}
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/audit?q=%25", nil, 200)
	var audit store.AuditPage
	decodeJSONBody(t, raw, &audit)
	if audit.Total != 0 {
		t.Fatal("audit LIKE filter became a wildcard")
	}
}

func TestAdminAccessTransferAndDisableKeepAnActiveOwner(t *testing.T) {
	env := setup(t)
	op, _ := adminClient(env)
	first, _, _ := env.createUserSession("first-owner")
	second, _, _ := env.createUserSession("second-owner")
	site := createSite(t.Context(), t, env, "transfer-race", "Transfer", "", "zh-CN", first.ID)
	start := make(chan struct{})
	grant := make(chan error, 1)
	disable := make(chan error, 1)
	go func() {
		<-start
		grant <- env.store.SetAccountSiteAccess(t.Context(), op.ID, second.ID, site.Site.ID, model.RoleOwner)
	}()
	go func() { <-start; _, err := env.store.DisableAccount(t.Context(), op.ID, first.ID); disable <- err }()
	close(start)
	if err := <-grant; err != nil {
		t.Fatal(err)
	}
	if err := <-disable; err != nil && !errors.Is(err, store.ErrLastOwner) {
		t.Fatal(err)
	}
	var owners int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_memberships m JOIN users u ON u.id=m.user_id WHERE m.site_id=$1 AND m.role='owner' AND u.status='active'`, site.Site.ID).Scan(&owners); err != nil || owners < 1 {
		t.Fatalf("site lost every effective owner: %d %v", owners, err)
	}
}

func TestAdminPasskeyRevokeRacesVerifiedSessionIssuance(t *testing.T) {
	env := setup(t)
	op, _ := adminClient(env)
	user, _, _ := env.createUserSession("assertion-race")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for round := 0; round < 20; round++ {
		if round > 0 {
			env.bindPasskey(user.ID, testauth.New())
		}
		keys, err := env.store.Passkeys.ListByUser(ctx, user.ID)
		if err != nil || len(keys) != 1 {
			t.Fatalf("fixture key: %v", err)
		}
		key := keys[0]
		type issuance struct {
			token string
			err   error
		}
		start := make(chan struct{})
		issued := make(chan issuance, 1)
		revoked := make(chan error, 1)
		go func() {
			<-start
			token, _, _, err := env.store.Sessions.CreateForPasskey(ctx, user.ID, "localhost", key.CredentialID, time.Hour)
			issued <- issuance{token, err}
		}()
		go func() { <-start; revoked <- env.store.AdminRevokePasskey(ctx, op.ID, user.ID, key.ID, "localhost") }()
		close(start)
		result := <-issued
		if err := <-revoked; err != nil {
			t.Fatal(err)
		}
		if result.err != nil && !errors.Is(result.err, store.ErrConflict) {
			t.Fatal(result.err)
		}
		if result.err == nil {
			if _, err := env.store.Sessions.GetAndTouch(ctx, sha256Sum64(result.token)); !errors.Is(err, store.ErrNotFound) {
				t.Fatal("a concurrently issued revoked-key session survived")
			}
		}
	}
}

func TestAdminAuditIgnoresCancelledRead(t *testing.T) {
	env := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	api := &API{store: env.store}
	route := routeSpec{Method: http.MethodGet, Pattern: "/cancelled", OperationID: "cancelledRead", Handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }}
	api.auditRoute(route)(httptest.NewRecorder(), httptest.NewRequest("GET", "/cancelled", nil).WithContext(ctx))
	var count int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cancelled read entered audit history: %d %v", count, err)
	}
}

func TestAdminAuditCapturesCanonicalChangesAndCredentialTargets(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	user, token, _ := env.createUserSession("detail-target")
	path := "/api/v1/admin/users/" + user.ID.String()
	adminRequest(t, admin, "PUT", path+"/role", map[string]string{"platformRole": "admin"}, 200)
	_, raw := adminRequest(t, admin, "GET", "/api/v1/admin/audit?operation=setUserRole&q="+user.ID.String(), nil, 200)
	var page store.AuditPage
	decodeJSONBody(t, raw, &page)
	if page.Total != 1 {
		t.Fatal("role event missing")
	}
	details := page.Entries[0].Details
	if details.TargetUsername != "detail-target" || details.Field != "platformRole" || details.Before == nil || *details.Before != "member" || details.After == nil || *details.After != "admin" {
		t.Fatalf("wrong change details: %+v", details)
	}
	auth, err := env.store.Sessions.GetAndTouch(t.Context(), sha256Sum64(token))
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, admin, "DELETE", path+"/sessions/"+auth.Session.ID.String(), nil, 204)
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/audit?operation=revokeUserSession", nil, 200)
	decodeJSONBody(t, raw, &page)
	if page.Total != 1 || page.Entries[0].Details.SessionID == nil || *page.Entries[0].Details.SessionID != auth.Session.ID {
		t.Fatal("session target missing from audit")
	}
	keys, err := env.store.Passkeys.ListByUser(t.Context(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminRequest(t, admin, "DELETE", path+"/passkeys/"+keys[0].ID.String(), nil, 204)
	_, raw = adminRequest(t, admin, "GET", "/api/v1/admin/audit?operation=revokeUserPasskey&q=detail-target", nil, 200)
	decodeJSONBody(t, raw, &page)
	if page.Total != 1 || page.Entries[0].Details.PasskeyID == nil || *page.Entries[0].Details.PasskeyID != keys[0].ID {
		t.Fatal("passkey target or canonical name filter missing")
	}
}
