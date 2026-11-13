package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddymanage"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

const (
	caddyFakeValidationID = "11111111111111111111111111111111"
	caddyFakeAdoptID      = "22222222222222222222222222222222"
	caddyFakeDeployID     = "33333333333333333333333333333333"
	caddyFakeRollbackID   = "44444444444444444444444444444444"
)

type caddyControlFake struct {
	mu             sync.Mutex
	digest         string
	capabilities   caddymanage.Capabilities
	requests       []caddymanage.Request
	applied        int
	adopted        bool
	stateDigest    string
	rootRevisionID string
	uncertain      bool
}

func newCaddyControlFake() *caddyControlFake {
	return &caddyControlFake{
		digest:       strings.Repeat("a", 64),
		capabilities: caddymanage.Capabilities{Compression: true, CacheControl: true, SecurityHeaders: true},
	}
}

func (f *caddyControlFake) control(_ context.Context, req caddymanage.Request) (caddymanage.Result, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return caddymanage.Result{}, err
	}
	req, err = caddymanage.DecodeRequest(bytes.NewReader(raw))
	if err != nil {
		return caddymanage.Result{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	if req.Operation != "inspect" && req.ExpectedDigest != "" && req.ExpectedDigest != f.digest {
		return caddymanage.Result{}, caddymanage.ErrConflict
	}
	result := caddymanage.Result{
		Registered: true, Adopted: f.adopted, Capabilities: f.capabilities, SourceDigest: f.digest, Drifted: f.adopted && f.stateDigest != f.digest, RuntimeUncertain: f.uncertain, RootRevisionID: f.rootRevisionID,
		Settings: caddymanage.Settings{Compression: "preserve", CacheControl: "preserve", SecurityHeaders: "preserve"},
	}
	switch req.Operation {
	case "inspect":
	case "adopt":
		f.adopted = true
		f.stateDigest = f.digest
		f.rootRevisionID = caddyFakeAdoptID
		result.Adopted = true
		result.RootRevisionID = caddyFakeAdoptID
	case "validate":
		if result.Drifted || f.uncertain {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		result.ValidationID = caddyFakeValidationID
	case "deploy":
		if result.Drifted || f.uncertain {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		if req.ValidationID != caddyFakeValidationID {
			return caddymanage.Result{}, caddymanage.ErrValidation
		}
		f.applied++
		f.digest = strings.Repeat("b", 64)
		f.stateDigest = f.digest
		result.SourceDigest, result.RootRevisionID = f.digest, caddyFakeDeployID
		f.rootRevisionID = result.RootRevisionID
	case "rollback":
		if req.RevisionID != caddyFakeAdoptID {
			return caddymanage.Result{}, caddymanage.ErrConflict
		}
		f.applied++
		f.digest = strings.Repeat("a", 64)
		f.stateDigest = f.digest
		result.SourceDigest, result.RootRevisionID = f.digest, caddyFakeRollbackID
		f.rootRevisionID = result.RootRevisionID
	default:
		return caddymanage.Result{}, caddymanage.ErrInvalidRequest
	}
	return result, nil
}

func (f *caddyControlFake) snapshot() ([]caddymanage.Request, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]caddymanage.Request(nil), f.requests...), f.applied
}

func setupCaddyManagement(t *testing.T, enabled bool) (*testEnv, *caddyControlFake, model.User, *tclient, store.CaddySite) {
	t.Helper()
	control := newCaddyControlFake()
	env := setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatal(err)
		}
		env.mutateConfig = func(cfg *Config) {
			cfg.CaddyControl = control.control
			cfg.CaddyManagementEnabled = enabled
		}
	})
	op, client := adminClient(env)
	entry := caddyconfig.Site{ID: "caddy-managed-fixture", Kind: caddyconfig.KindStatic, Hosts: []string{"managed.example"}, Listen: []string{":443"}, Root: t.TempDir(), ReadOnly: true}
	if _, err := env.store.SyncCaddySites(t.Context(), op.ID, []caddyconfig.Site{entry}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := env.pool.QueryRow(t.Context(), `SELECT site_id FROM caddy_sites WHERE source_key=$1`, entry.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	c, err := env.store.GetCaddySite(t.Context(), id, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	return env, control, op, client, c
}

func caddyRequest(t *testing.T, client *tclient, method, path string, body any, etag string, want int) (http.Header, []byte) {
	t.Helper()
	headers := map[string]string{}
	if etag != "" {
		headers["If-Match"] = etag
	}
	status, responseHeaders, raw := client.do(method, path, body, headers)
	if status != want {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, raw)
	}
	for _, secret := range []string{caddyFakeValidationID, caddyFakeAdoptID, caddyFakeDeployID, caddyFakeRollbackID} {
		if bytes.Contains(bytes.ToLower(raw), []byte(strings.ToLower(secret))) {
			t.Fatal("Caddy response exposed an internal capability token")
		}
	}
	if status == http.StatusOK && (bytes.Contains(raw, []byte(`"validationId":`)) || bytes.Contains(raw, []byte(`"rootRevisionId":`))) {
		t.Fatal("Caddy JSON exposed an internal capability field")
	}
	return responseHeaders, raw
}

func TestCaddyManagementLifecyclePersistsAuditAndSurvivesSync(t *testing.T) {
	env, control, op, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	headers, raw := caddyRequest(t, client, "GET", path, nil, "", 200)
	var initial caddyState
	decodeJSONBody(t, raw, &initial)
	if initial.ManagementMode != "caddy" || initial.Management != nil || !initial.Capabilities.Convert || headers.Get("Cache-Control") != "no-store" {
		t.Fatal("unmanaged site did not expose conversion with an uncached state")
	}
	headers, raw = caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, headers.Get("ETag"), 200)
	var adopted caddyState
	decodeJSONBody(t, raw, &adopted)
	if adopted.ManagementMode != "platform" || adopted.Management == nil || adopted.Management.Revision != 1 || adopted.Management.AppliedRevision == nil || *adopted.Management.AppliedRevision != 1 {
		t.Fatal("conversion did not persist the original applied revision")
	}
	if _, applies := control.snapshot(); applies != 0 {
		t.Fatal("conversion changed the running source")
	}
	settings := caddymanage.Settings{Compression: "gzip_zstd", CacheControl: "no_store", SecurityHeaders: "baseline"}
	headers, raw = caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": settings}, headers.Get("ETag"), 200)
	var draft caddyState
	decodeJSONBody(t, raw, &draft)
	if draft.Management.Revision != 2 || draft.Management.ValidatedRevision != nil || *draft.Management.AppliedRevision != 1 {
		t.Fatal("draft did not invalidate validation while retaining the live revision")
	}
	if _, applies := control.snapshot(); applies != 0 {
		t.Fatal("saving a draft applied Caddy")
	}
	caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, headers.Get("ETag"), 409)
	headers, raw = caddyRequest(t, client, "POST", path+"/validate", map[string]any{}, headers.Get("ETag"), 200)
	var validated caddyState
	decodeJSONBody(t, raw, &validated)
	if validated.Management.ValidatedRevision == nil || *validated.Management.ValidatedRevision != 2 {
		t.Fatal("validation did not persist the exact draft revision")
	}
	beforeDeploy, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || beforeDeploy.ValidationID != caddyFakeValidationID {
		t.Fatalf("internal validation ticket was not persisted: %v", err)
	}
	headers, raw = caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, headers.Get("ETag"), 200)
	var deployed caddyState
	decodeJSONBody(t, raw, &deployed)
	if *deployed.Management.AppliedRevision != 2 || deployed.Management.SourceDigest != strings.Repeat("b", 64) {
		t.Fatal("deployment did not advance the applied revision and digest")
	}
	headers, raw = caddyRequest(t, client, "POST", path+"/rollback", map[string]any{"revision": 1}, headers.Get("ETag"), 200)
	var rolledBack caddyState
	decodeJSONBody(t, raw, &rolledBack)
	if rolledBack.Management.Revision != 3 || *rolledBack.Management.AppliedRevision != 3 || rolledBack.Management.SourceDigest != strings.Repeat("a", 64) || len(rolledBack.Management.History) != 3 {
		t.Fatal("rollback did not create a new applied revision preserving history")
	}
	var restored caddymanage.Settings
	decodeJSONBody(t, rolledBack.Management.Settings, &restored)
	if restored.Compression != "preserve" || restored.CacheControl != "preserve" || restored.SecurityHeaders != "preserve" {
		t.Fatal("rollback did not restore the selected applied settings")
	}
	_, applies := control.snapshot()
	if applies != 2 {
		t.Fatalf("apply count = %d, want deploy and rollback only", applies)
	}
	var releaseID *uuid.UUID
	var entries, successfulEvents int
	if err := env.pool.QueryRow(t.Context(), `SELECT current_release_id FROM sites WHERE id=$1`, c.SiteID).Scan(&releaseID); err != nil {
		t.Fatal(err)
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM site_network_entries WHERE site_id=$1`, c.SiteID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if releaseID != nil || entries != 0 {
		t.Fatal("Caddy management provisioned or published a theme site")
	}
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE actor_id=$1 AND site_id=$2 AND resource_id=$2::text AND outcome='success' AND operation IN ('convertCaddySite','saveCaddyDraft','validateCaddyDraft','deployCaddyDraft','rollbackCaddyConfiguration')`, op.ID, c.SiteID).Scan(&successfulEvents); err != nil || successfulEvents != 5 {
		t.Fatalf("Caddy success audit did not retain actor/site/operation: count=%d err=%v", successfulEvents, err)
	}
	beforeSync, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	var definition caddyconfig.Site
	decodeJSONBody(t, c.Definition, &definition)
	if _, err := env.store.SyncCaddySites(t.Context(), op.ID, []caddyconfig.Site{definition}); err != nil {
		t.Fatal(err)
	}
	afterSync, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || !reflect.DeepEqual(beforeSync, afterSync) {
		t.Fatal("inventory sync discarded management state or revision history")
	}
}

func TestCaddyManagementPreconditionsAndStrictInput(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	etag := store.CaddyETag(c.SiteID, nil)
	caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, "", 428)
	caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, `"stale"`, 412)
	caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, "*", 412)
	for _, key := range []string{"path", "command", "raw", "sourceFile", "template"} {
		caddyRequest(t, client, "POST", path+"/convert", map[string]any{key: "/etc/caddy/Caddyfile"}, etag, 400)
	}
	requests, applies := control.snapshot()
	if len(requests) != 0 || applies != 0 {
		t.Fatal("invalid input reached the privileged capability")
	}
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, etag, 200)
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "gzip", CacheControl: "preserve", SecurityHeaders: "preserve"}}, etag, 412)
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": map[string]any{"compression": "gzip", "cacheControl": "preserve", "securityHeaders": "preserve", "raw": "arbitrary configuration"}}, headers.Get("ETag"), 400)
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "shell", CacheControl: "preserve", SecurityHeaders: "preserve"}}, headers.Get("ETag"), 422)
	m, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || m.Revision != 1 || len(m.History) != 1 {
		t.Fatal("rejected mutations changed the configuration history")
	}
}

func TestCaddyManagementRequiresFullAdminOwnerAndCSRF(t *testing.T) {
	env, control, op, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	etag := store.CaddyETag(c.SiteID, nil)
	member, token, csrf := env.createUserSession("caddy-member-owner")
	if _, err := env.pool.Exec(t.Context(), `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,'owner')`, c.SiteID, member.ID); err != nil {
		t.Fatal(err)
	}
	caddyRequest(t, env.clientFor(token, csrf), "POST", path+"/convert", map[string]any{}, etag, 403)
	viewer, viewerToken, viewerCSRF := env.createUserSession("caddy-admin-viewer")
	setUserRole(t, env, viewer.ID, model.PlatformRoleAdmin)
	if _, err := env.pool.Exec(t.Context(), `INSERT INTO site_memberships(site_id,user_id,role) VALUES($1,$2,'viewer')`, c.SiteID, viewer.ID); err != nil {
		t.Fatal(err)
	}
	caddyRequest(t, env.clientFor(viewerToken, viewerCSRF), "POST", path+"/convert", map[string]any{}, etag, 403)
	outsider, outsiderToken, outsiderCSRF := env.createUserSession("caddy-admin-outsider")
	setUserRole(t, env, outsider.ID, model.PlatformRoleAdmin)
	caddyRequest(t, env.clientFor(outsiderToken, outsiderCSRF), "POST", path+"/convert", map[string]any{}, etag, 404)
	caddyRequest(t, env.clientFor(client.token, ""), "POST", path+"/convert", map[string]any{}, etag, 403)
	caddyRequest(t, env.newClient(), "POST", path+"/convert", map[string]any{}, etag, 401)
	restricted, restrictedCSRF, _, err := env.store.Sessions.CreateWithKind(t.Context(), op.ID, model.SessionKindRestricted, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caddyRequest(t, env.clientFor(restricted, restrictedCSRF), "GET", path, nil, "", 403)
	caddyRequest(t, env.clientFor(restricted, restrictedCSRF), "POST", path+"/convert", map[string]any{}, etag, 403)
	requests, applies := control.snapshot()
	if len(requests) != 0 || applies != 0 {
		t.Fatal("unauthorized requests reached the privileged capability")
	}
}

func TestCaddyManagementStaticRootsAndSettingsCapabilities(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	var original caddyconfig.Site
	decodeJSONBody(t, c.Definition, &original)
	for _, sample := range []struct {
		name        string
		kind        caddyconfig.Kind
		root        string
		placeholder bool
	}{
		{"redirect", caddyconfig.KindRedirect, "/srv/static", false},
		{"proxy", caddyconfig.KindReverseProxy, "/srv/static", false},
		{"missing-root", caddyconfig.KindStatic, "", false},
		{"dynamic-root", caddyconfig.KindStatic, "{http.vars.root}", true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			definition := original
			definition.Kind, definition.Root, definition.RootHasPlaceholder = sample.kind, sample.root, sample.placeholder
			raw, _ := json.Marshal(definition)
			if _, err := env.pool.Exec(t.Context(), `UPDATE caddy_sites SET kind=$2,definition=$3 WHERE site_id=$1`, c.SiteID, sample.kind, raw); err != nil {
				t.Fatal(err)
			}
			headers, raw := caddyRequest(t, client, "GET", path, nil, "", 200)
			var state caddyState
			decodeJSONBody(t, raw, &state)
			if state.Capabilities.Convert || state.UnavailableReason != "site_kind_unsupported" {
				t.Fatal("unsupported site advertised conversion")
			}
			caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, headers.Get("ETag"), 409)
		})
	}
	if requests, _ := control.snapshot(); len(requests) != 0 {
		t.Fatal("unsupported site reached the privileged capability")
	}
	if _, err := env.pool.Exec(t.Context(), `UPDATE caddy_sites SET kind='static',definition=$2 WHERE site_id=$1`, c.SiteID, c.Definition); err != nil {
		t.Fatal(err)
	}
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, store.CaddyETag(c.SiteID, nil), 200)
	control.mu.Lock()
	control.capabilities.CacheControl = false
	control.mu.Unlock()
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "preserve", CacheControl: "no_store", SecurityHeaders: "preserve"}}, headers.Get("ETag"), 409)
}

func TestCaddyManagementSourceDriftAndDeploymentIsolation(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, false)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, store.CaddyETag(c.SiteID, nil), 200)
	headers, _ = caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "gzip", CacheControl: "preserve", SecurityHeaders: "preserve"}}, headers.Get("ETag"), 200)
	caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, headers.Get("ETag"), 503)
	caddyRequest(t, client, "POST", path+"/rollback", map[string]any{"revision": 1}, headers.Get("ETag"), 503)
	before, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.digest = strings.Repeat("c", 64)
	control.mu.Unlock()
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "zstd", CacheControl: "preserve", SecurityHeaders: "preserve"}}, headers.Get("ETag"), 409)
	caddyRequest(t, client, "POST", path+"/validate", map[string]any{}, headers.Get("ETag"), 409)
	after, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("source drift changed the database or revision history")
	}
	_, raw := caddyRequest(t, client, "GET", path, nil, "", 200)
	var state caddyState
	decodeJSONBody(t, raw, &state)
	if state.UnavailableReason != "source_config_changed" || state.Capabilities.Validate || state.Capabilities.Deploy {
		t.Fatal("source drift did not disable validation and deployment")
	}
	if _, applies := control.snapshot(); applies != 0 {
		t.Fatal("disabled deployment or source drift changed the live configuration")
	}
}

func TestCaddyManagementValidationCannotReplaceAppliedHistoryOrAuthorizeNewDraft(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, store.CaddyETag(c.SiteID, nil), 200)
	headers, _ = caddyRequest(t, client, "POST", path+"/validate", map[string]any{}, headers.Get("ETag"), 200)
	applied, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || len(applied.History) != 1 || applied.History[0].Status != "applied" || applied.History[0].RootRevisionID != caddyFakeAdoptID {
		t.Fatal("validating the applied revision destroyed its rollback identity")
	}
	settings := caddymanage.Settings{Compression: "gzip", CacheControl: "preserve", SecurityHeaders: "preserve"}
	headers, _ = caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": settings}, headers.Get("ETag"), 200)
	headers, _ = caddyRequest(t, client, "POST", path+"/validate", map[string]any{}, headers.Get("ETag"), 200)
	validatedETag := headers.Get("ETag")
	settings.Compression = "zstd"
	headers, _ = caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": settings}, validatedETag, 200)
	caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, validatedETag, 412)
	caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, headers.Get("ETag"), 409)
	changed, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || changed.Revision != 3 || changed.ValidationID != "" || changed.ValidatedRevision != nil || *changed.AppliedRevision != 1 {
		t.Fatal("a previous validation authorized a changed draft")
	}
	headers, _ = caddyRequest(t, client, "POST", path+"/validate", map[string]any{}, headers.Get("ETag"), 200)
	before, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.digest = strings.Repeat("c", 64)
	control.mu.Unlock()
	caddyRequest(t, client, "POST", path+"/deploy", map[string]any{}, headers.Get("ETag"), 409)
	after, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("a failed deployment on external source drift changed the database")
	}
	if _, applies := control.snapshot(); applies != 0 {
		t.Fatal("invalidated validation or source drift applied a configuration")
	}
}

func TestCaddyManagementConcurrentDraftsHonorOneExactETag(t *testing.T) {
	env, _, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, store.CaddyETag(c.SiteID, nil), 200)
	etag := headers.Get("ETag")
	type response struct {
		status int
		raw    []byte
	}
	results := make(chan response, 2)
	start := make(chan struct{})
	for _, compression := range []string{"gzip", "zstd"} {
		go func() {
			var result response
			defer func() { results <- result }()
			<-start
			result.status, _, result.raw = client.do("PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: compression, CacheControl: "preserve", SecurityHeaders: "preserve"}}, map[string]string{"If-Match": etag})
		}()
	}
	close(start)
	statuses := map[int]int{}
	for range 2 {
		result := <-results
		statuses[result.status]++
		if result.status == http.StatusPreconditionFailed {
			var problem problemResp
			decodeJSONBody(t, result.raw, &problem)
			if problem.Code != "precondition_failed" {
				t.Fatal("concurrent loser did not report an exact revision conflict")
			}
		}
	}
	if statuses[http.StatusOK] != 1 || statuses[http.StatusPreconditionFailed] != 1 {
		t.Fatalf("concurrent draft statuses = %v, want one 200 and one 412", statuses)
	}
	management, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || management.Revision != 2 || len(management.History) != 2 {
		t.Fatal("concurrent drafts lost an update or created a duplicate revision")
	}
	var successfulDrafts int
	if err := env.pool.QueryRow(t.Context(), `SELECT count(*) FROM audit_events WHERE site_id=$1 AND operation='saveCaddyDraft' AND outcome='success'`, c.SiteID).Scan(&successfulDrafts); err != nil || successfulDrafts != 1 {
		t.Fatalf("successful concurrent draft audit count=%d err=%v", successfulDrafts, err)
	}
}

func TestCaddyManagementRuntimeUncertaintyCannotBeAdoptedOrSavedAway(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	control.mu.Lock()
	control.uncertain = true
	control.mu.Unlock()
	headers, raw := caddyRequest(t, client, "GET", path, nil, "", 200)
	var state caddyState
	decodeJSONBody(t, raw, &state)
	if state.UnavailableReason != "caddy_runtime_uncertain" || state.Capabilities.Convert || state.Capabilities.Validate || state.Capabilities.Deploy {
		t.Fatal("uncertain root runtime advertised safe conversion or deployment")
	}
	caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, headers.Get("ETag"), 409)
	if requests, _ := control.snapshot(); len(requests) != 2 || requests[0].Operation != "inspect" || requests[1].Operation != "inspect" {
		t.Fatal("conversion attempted to reset uncertain root state")
	}
	control.mu.Lock()
	control.uncertain = false
	control.mu.Unlock()
	headers, _ = caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, headers.Get("ETag"), 200)
	before, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	control.mu.Lock()
	control.uncertain = true
	control.mu.Unlock()
	caddyRequest(t, client, "PUT", path+"/draft", map[string]any{"settings": caddymanage.Settings{Compression: "gzip", CacheControl: "preserve", SecurityHeaders: "preserve"}}, headers.Get("ETag"), 409)
	after, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("saving a draft cleared or hid uncertain runtime state")
	}
}

func TestCaddyManagementSameSourceDigestCannotHideRootRevisionDrift(t *testing.T) {
	env, control, _, client, c := setupCaddyManagement(t, true)
	path := "/api/v1/sites/" + c.SiteID.String() + "/caddy"
	headers, _ := caddyRequest(t, client, "POST", path+"/convert", map[string]any{}, store.CaddyETag(c.SiteID, nil), 200)
	before, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil {
		t.Fatal(err)
	}
	requestsBefore, _ := control.snapshot()
	control.mu.Lock()
	control.rootRevisionID = "55555555555555555555555555555555"
	control.mu.Unlock()
	_, raw := caddyRequest(t, client, "GET", path, nil, "", 200)
	var state caddyState
	decodeJSONBody(t, raw, &state)
	if state.UnavailableReason != "source_config_changed" || state.Capabilities.Validate || state.Capabilities.Deploy {
		t.Fatal("same-byte root apply was mistaken for the database's applied revision")
	}
	for _, action := range []struct {
		method, suffix string
		body           any
	}{
		{"POST", "/convert", map[string]any{}},
		{"PUT", "/draft", map[string]any{"settings": caddymanage.Settings{Compression: "gzip", CacheControl: "preserve", SecurityHeaders: "preserve"}}},
		{"POST", "/validate", map[string]any{}},
		{"POST", "/deploy", map[string]any{}},
		{"POST", "/rollback", map[string]any{"revision": 1}},
	} {
		caddyRequest(t, client, action.method, path+action.suffix, action.body, headers.Get("ETag"), 409)
	}
	after, err := env.store.GetCaddyManagement(t.Context(), c.SiteID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("root revision drift modified database state or history")
	}
	requestsAfter, applies := control.snapshot()
	for _, request := range requestsAfter[len(requestsBefore):] {
		if request.Operation != "inspect" {
			t.Fatal("root revision drift reached a privileged mutation")
		}
	}
	if applies != 0 {
		t.Fatal("root revision mismatch triggered an apply")
	}
}
