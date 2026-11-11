package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/db"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/model"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/store"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/themecontract"
	"github.com/skyrocketingHong/FuyaoHomepage/admin/backend/internal/wa"
)

// repoRoot resolves the repository root from this test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// testEnv is one integration-test world: a dedicated PostgreSQL schema with
// real migrations, the real handler stack, and a loopback HTTP server that
// the synthetic authenticator targets as http://localhost:<port>.
type testEnv struct {
	t     *testing.T
	base  string
	pool  *pgxpool.Pool
	store *store.Store
}

// testDatabaseName is the only database the integration tests may touch.
const testDatabaseName = "fuyao_mvp_test"

// testDSN resolves the test DSN from FUYAO_ADMIN_TEST_DSN or, when unset,
// from the task-local PostgreSQL descriptor at the repository root. Tests
// skip when neither source is available.
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("FUYAO_ADMIN_TEST_DSN"); dsn != "" {
		return dsn
	}
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".fuyao", "cms-mvp", "postgres-test.json"))
	if err != nil {
		t.Skip("FUYAO_ADMIN_TEST_DSN unset and no local test PostgreSQL descriptor; skipping real-PostgreSQL tests")
	}
	var descriptor struct {
		DatabaseURL string `json:"database_url"`
	}
	if err := json.Unmarshal(raw, &descriptor); err != nil || descriptor.DatabaseURL == "" {
		t.Skip("local test PostgreSQL descriptor unusable; skipping real-PostgreSQL tests")
	}
	return descriptor.DatabaseURL
}

// setup provisions the schema, all migrations, and the server for one test.
func setup(t *testing.T) *testEnv {
	t.Helper()
	return setupApply(t, func(ctx context.Context, env *testEnv) {
		if _, err := db.ApplyMigrations(ctx, env.pool); err != nil {
			t.Fatalf("apply migrations: %v", err)
		}
	})
}

// setupApply provisions schema and server, deferring migration application
// to the callback so tests can exercise upgrade paths.
func setupApply(t *testing.T, apply func(ctx context.Context, env *testEnv)) *testEnv {
	t.Helper()
	dsn := testDSN(t)
	ctx := context.Background()

	// Guard: only the dedicated test database may be touched.
	guard, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test PostgreSQL: %v", err)
	}
	var database string
	if err := guard.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if database != testDatabaseName {
		t.Fatalf("refusing to run against database %q; only %q is allowed", database, testDatabaseName)
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	schema := fmt.Sprintf("cms_test_%s", hex.EncodeToString(suffix))
	if _, err := guard.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA %s`, schema)); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if err := guard.Close(ctx); err != nil {
		t.Fatalf("close guard connection: %v", err)
	}

	pool, err := db.OpenPoolWithSearchPath(ctx, dsn, schema)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	origin := fmt.Sprintf("http://localhost:%d", listener.Addr().(*net.TCPAddr).Port)

	apply(ctx, &testEnv{t: t, base: "", pool: pool, store: nil})

	webAuthn, err := wa.New(wa.Params{
		RPID:          "localhost",
		RPDisplayName: "Fuyao Admin Test",
		RPOrigins:     []string{origin},
		ChallengeTTL:  5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("init webauthn: %v", err)
	}
	st := store.New(pool)
	contracts, err := themecontract.Load(filepath.Join(repoRoot(t), "contracts", "theme"))
	if err != nil {
		t.Fatalf("load theme contracts: %v", err)
	}
	handler := New(st, webAuthn, Config{
		RPID:           "localhost",
		RPOrigins:      []string{origin},
		CookieSecure:   false,
		SessionTTL:     time.Hour,
		ChallengeTTL:   5 * time.Minute,
		ThemeContracts: contracts,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	env := &testEnv{t: t, base: origin, pool: pool, store: st}
	t.Cleanup(func() {
		_ = server.Close()
		if _, err := pool.Exec(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, schema)); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
		pool.Close()
		select {
		case err := <-serveErr:
			if err != nil && err != http.ErrServerClosed {
				t.Errorf("test server: %v", err)
			}
		default:
		}
	})
	return env
}

// tclient is an HTTP client bound to the test origin with a cookie jar and,
// once authenticated, a CSRF token.
type tclient struct {
	env   *testEnv
	hc    *http.Client
	csrf  string
	token string // explicit session token override (test-constructed sessions)
}

func (e *testEnv) newClient() *tclient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatalf("cookie jar: %v", err)
	}
	return &tclient{env: e, hc: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// do performs a request. Unsafe methods carry Origin and X-CSRF-Token by
// default; header overrides may replace either (Origin: "none" omits it).
func (c *tclient) do(method, path string, body any, hdr map[string]string) (int, http.Header, []byte) {
	c.env.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.env.t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.env.base+path, reader)
	if err != nil {
		c.env.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		if origin, ok := hdr["Origin"]; ok {
			if origin != "none" {
				req.Header.Set("Origin", origin)
			}
		} else {
			req.Header.Set("Origin", c.env.base)
		}
		if csrf, ok := hdr["X-CSRF-Token"]; ok {
			req.Header.Set("X-CSRF-Token", csrf)
		} else if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	for name, value := range hdr {
		if name == "Origin" || name == "X-CSRF-Token" {
			continue
		}
		req.Header.Set(name, value)
	}
	if c.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: c.token})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.env.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.env.t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// readCookie returns a cookie currently held by the client jar.
func readCookie(c *tclient, name, path string) string {
	c.env.t.Helper()
	u, err := url.Parse(c.env.base + path)
	if err != nil {
		c.env.t.Fatalf("parse base URL: %v", err)
	}
	for _, ck := range c.hc.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	c.env.t.Fatalf("cookie %s not present in jar", name)
	return ""
}

// doRaw performs a request whose body is an exact string (used to test
// strict parsing of malformed or oversized payloads).
func (c *tclient) doRaw(method, path, rawBody string, hdr map[string]string) (int, http.Header, []byte) {
	c.env.t.Helper()
	req, err := http.NewRequest(method, c.env.base+path, bytes.NewReader([]byte(rawBody)))
	if err != nil {
		c.env.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Origin", c.env.base)
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	if c.token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: c.token})
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.env.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.env.t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, resp.Header, raw
}

// createUserSession constructs an active user and a session directly through
// the store. This helper exists only in _test.go so tests can exercise role
// and membership scenarios without a passkey ceremony for every user; the
// runtime code has no equivalent backdoor.
func (e *testEnv) createUserSession(username string) (model.User, string, string) {
	e.t.Helper()
	ctx := context.Background()
	user, err := e.store.Users.Create(ctx, username, username)
	if err != nil {
		e.t.Fatalf("create user %s: %v", username, err)
	}
	if err := e.store.Users.Activate(ctx, user.ID); err != nil {
		e.t.Fatalf("activate user %s: %v", username, err)
	}
	token, csrf, _, err := e.store.Sessions.Create(ctx, user.ID, time.Hour)
	if err != nil {
		e.t.Fatalf("create session for %s: %v", username, err)
	}
	return user, token, csrf
}

// clientFor returns a client whose requests use an explicitly constructed
// session token.
func (e *testEnv) clientFor(token, csrf string) *tclient {
	c := e.newClient()
	c.token = token
	c.csrf = csrf
	return c
}

// JSON response shapes used across the tests.

type creationOptionsResp struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		User      struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
		RP struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"rp"`
	} `json:"publicKey"`
}

type requestOptionsResp struct {
	PublicKey struct {
		Challenge        string `json:"challenge"`
		UserVerification string `json:"userVerification"`
	} `json:"publicKey"`
}

type ceremonyDoneResp struct {
	User struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	CSRFToken string `json:"csrfToken"`
}

type sessionResp struct {
	Authenticated bool   `json:"authenticated"`
	CSRFToken     string `json:"csrfToken"`
}

type siteListResp struct {
	Sites []siteDTO `json:"sites"`
}

type problemResp struct {
	Status int    `json:"status"`
	Code   string `json:"code"`
}

func decodeJSONBody(t *testing.T, raw []byte, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode response %q: %v", raw, err)
	}
}
