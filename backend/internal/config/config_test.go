package config

import (
	"testing"
)

func setTestEnv(t *testing.T, addr, origins string) {
	t.Helper()
	t.Setenv(EnvDatabaseURL, "postgres://user:pass@127.0.0.1:5432/db")
	t.Setenv(EnvAddr, addr)
	t.Setenv(EnvRPOrigins, origins)
	t.Setenv(EnvRPID, "localhost")
}

func TestLoadRejectsNonLoopbackAddr(t *testing.T) {
	setTestEnv(t, "0.0.0.0:8787", "http://localhost:5174")
	if _, err := Load(); err == nil {
		t.Fatal("0.0.0.0 must be rejected")
	}
	setTestEnv(t, "192.168.1.10:8787", "http://localhost:5174")
	if _, err := Load(); err == nil {
		t.Fatal("non-loopback IP must be rejected")
	}
}

func TestLoadRejectsPlainHTTPOffLoopback(t *testing.T) {
	setTestEnv(t, "127.0.0.1:8787", "http://admin.example.com:5174")
	if _, err := Load(); err == nil {
		t.Fatal("plain HTTP on a public host must be rejected")
	}
}

func TestLoadAcceptsLoopbackHTTPAndSetsInsecureCookie(t *testing.T) {
	// Both origins share the localhost registrable site, so the cookie is
	// still first-party; 127.0.0.1 origins are impossible here because the
	// relying party ID cannot be an IP literal.
	setTestEnv(t, "127.0.0.1:8787", "http://localhost:5174,http://localhost:5175")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("loopback http must be accepted: %v", err)
	}
	if cfg.CookieSecure {
		t.Fatal("all-loopback http development must not mark cookies Secure")
	}
}

func TestLoadHTTPSRequiresSecureCookie(t *testing.T) {
	t.Setenv(EnvDatabaseURL, "postgres://user:pass@127.0.0.1:5432/db")
	t.Setenv(EnvAddr, "127.0.0.1:8787")
	t.Setenv(EnvRPID, "admin.example.com")
	t.Setenv(EnvRPOrigins, "https://admin.example.com")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("https origin must be accepted: %v", err)
	}
	if !cfg.CookieSecure {
		t.Fatal("https configuration must mark cookies Secure")
	}
}

func TestLoadRejectsOriginOutsideRPID(t *testing.T) {
	setTestEnv(t, "127.0.0.1:8787", "http://other.example:5174")
	if _, err := Load(); err == nil {
		t.Fatal("origin outside the relying party ID must be rejected")
	}
}

func TestLoadRejectsInvalidTTL(t *testing.T) {
	setTestEnv(t, "127.0.0.1:8787", "http://localhost:5174")
	t.Setenv(EnvSessionTTL, "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("an unparseable TTL must fail loudly, not fall back to the default")
	}
	t.Setenv(EnvSessionTTL, "12h")
	t.Setenv(EnvChallengeTTL, "-5m")
	if _, err := Load(); err == nil {
		t.Fatal("a non-positive TTL must fail loudly")
	}
}

func TestLoadTrustedHTTPHostKeepsCookieUsable(t *testing.T) {
	t.Setenv(EnvDatabaseURL, "postgres://x")
	t.Setenv(EnvAddr, "100.117.154.115:8787")
	t.Setenv(EnvTrustedHosts, "100.117.154.115")
	t.Setenv(EnvRPID, "100.117.154.115")
	t.Setenv(EnvRPOrigins, "http://100.117.154.115:8787")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("trusted plain-HTTP host must load: %v", err)
	}
	if cfg.CookieSecure {
		t.Fatal("plain-HTTP origins (loopback or trusted) must not mark cookies Secure; browsers would drop the session")
	}
}
