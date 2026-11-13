package httpapi

import (
	"encoding/json"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
	"net/http"
	"testing"
)

func TestCaddySiteImportPreservesBoundariesAndPermissions(t *testing.T) {
	env := setup(t)
	op, admin := adminClient(env)
	entries := []caddyconfig.Site{
		{ID: "short", Kind: caddyconfig.KindStatic, Hosts: []string{"static.example"}, Listen: []string{":443"}, Root: "/srv/real-site", ReadOnly: true},
		{ID: "short-duplicate", Kind: caddyconfig.KindRedirect, Hosts: []string{"redirect.example"}, Listen: []string{":443"}, ReadOnly: true, Redirect: &caddyconfig.Redirect{Location: "https://target.example", StatusCode: 308}},
		{ID: "protected", Kind: caddyconfig.KindStatic, Hosts: []string{"manager.fuyaoskyrocket.ing"}, Listen: []string{":443"}, ReadOnly: true},
	}
	count, err := env.store.SyncCaddySites(t.Context(), op.ID, entries)
	if err != nil || count != 2 {
		t.Fatalf("sync: %d %v", count, err)
	}
	if _, err = env.store.SyncCaddySites(t.Context(), op.ID, entries); err != nil {
		t.Fatal(err)
	}
	sites, err := env.store.Sites.ListForUser(t.Context(), op.ID)
	if err != nil || len(sites) != 2 {
		t.Fatalf("site count %d %v", len(sites), err)
	}
	for _, site := range sites {
		if site.Kind == "theme" || site.Role != model.RoleOwner {
			t.Fatal("import lost type or owner")
		}
		path := "/api/v1/sites/" + site.Site.ID.String()
		_, data := adminRequest(t, admin, http.MethodGet, path+"/caddy", nil, 200)
		var value map[string]any
		if err = json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		if value["present"] != true {
			t.Fatal("missing live flag")
		}
		adminRequest(t, admin, "POST", path+"/builds", map[string]string{}, 409)
		adminRequest(t, admin, "GET", path+"/theme", nil, 409)
		_, token, csrf := env.createUserSession("other-" + site.Site.ID.String()[:8])
		adminRequest(t, env.clientFor(token, csrf), "GET", path+"/caddy", nil, 404)
	}
	if _, err = env.store.SyncCaddySites(t.Context(), op.ID, entries[:1]); err != nil {
		t.Fatal(err)
	}
	var missing int
	if err = env.pool.QueryRow(t.Context(), `SELECT count(*) FROM caddy_sites WHERE NOT present`).Scan(&missing); err != nil || missing != 1 {
		t.Fatalf("removed source not marked absent: %d %v", missing, err)
	}
}
func TestCaddySyncRejectsMembersAndUnavailableSource(t *testing.T) {
	env := setup(t)
	_, admin := adminClient(env)
	_, token, csrf := env.createUserSession("nonadmin")
	adminRequest(t, env.clientFor(token, csrf), "POST", "/api/v1/admin/caddy/sync", nil, 403)
	adminRequest(t, admin, "POST", "/api/v1/admin/caddy/sync", nil, 503)
}
