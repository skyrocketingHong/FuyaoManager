// Package config loads and validates backend configuration from environment
// variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment variable names.
const (
	EnvDatabaseURL       = "FUYAO_ADMIN_DATABASE_URL"
	EnvAddr              = "FUYAO_ADMIN_ADDR"
	EnvRPID              = "FUYAO_ADMIN_RP_ID"
	EnvRPName            = "FUYAO_ADMIN_RP_DISPLAY_NAME"
	EnvRPOrigins         = "FUYAO_ADMIN_RP_ORIGINS"
	EnvSessionTTL        = "FUYAO_ADMIN_SESSION_TTL"
	EnvChallengeTTL      = "FUYAO_ADMIN_CHALLENGE_TTL"
	EnvPermitTTL         = "FUYAO_ADMIN_PERMIT_TTL"
	EnvThemeContractsDir = "FUYAO_ADMIN_THEME_CONTRACTS"
)

// Config is the fully validated runtime configuration. CookieSecure is
// derived from the origin schemes: plain-HTTP origins are only permitted on
// loopback hosts, and cookies are marked Secure unless every origin is a
// loopback HTTP origin.
type Config struct {
	DatabaseURL       string
	Addr              string
	RPID              string
	RPDisplayName     string
	RPOrigins         []string
	SessionTTL        time.Duration
	ChallengeTTL      time.Duration
	PermitTTL         time.Duration
	CookieSecure      bool
	ThemeContractsDir string
}

// Load reads the environment and validates it.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:       os.Getenv(EnvDatabaseURL),
		Addr:              envDefault(EnvAddr, "127.0.0.1:8787"),
		RPID:              envDefault(EnvRPID, "localhost"),
		RPDisplayName:     envDefault(EnvRPName, "Fuyao Admin"),
		ThemeContractsDir: envDefault(EnvThemeContractsDir, ""),
	}

	origins := envDefault(EnvRPOrigins, "http://localhost:5174")
	for _, raw := range strings.Split(origins, ",") {
		origin := strings.TrimSpace(raw)
		if origin != "" {
			cfg.RPOrigins = append(cfg.RPOrigins, origin)
		}
	}

	var ttlErr error
	setTTL := func(envName string, dst *time.Duration, fallback time.Duration) {
		if ttlErr != nil {
			return
		}
		*dst, ttlErr = envDuration(envName, fallback)
	}
	setTTL(EnvSessionTTL, &cfg.SessionTTL, 12*time.Hour)
	setTTL(EnvChallengeTTL, &cfg.ChallengeTTL, 5*time.Minute)
	setTTL(EnvPermitTTL, &cfg.PermitTTL, 10*time.Minute)
	if ttlErr != nil {
		return Config{}, ttlErr
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("%s is required", EnvDatabaseURL)
	}
	if err := validateLoopbackAddr(c.Addr); err != nil {
		return err
	}
	if c.RPID == "" {
		return fmt.Errorf("%s must not be empty", EnvRPID)
	}
	if len(c.RPOrigins) == 0 {
		return fmt.Errorf("%s must contain at least one origin", EnvRPOrigins)
	}

	allHTTPLoopback := true
	anyHTTPS := false
	for _, raw := range c.RPOrigins {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("origin %q is not a valid URL", raw)
		}
		host := u.Hostname()
		switch u.Scheme {
		case "https":
			anyHTTPS = true
			allHTTPLoopback = false
		case "http":
			if !isLoopbackHost(host) {
				return fmt.Errorf("plain-HTTP origin %q is only allowed on a loopback host", raw)
			}
		default:
			return fmt.Errorf("origin %q must use the http or https scheme", raw)
		}
		if host != c.RPID && !strings.HasSuffix(host, "."+c.RPID) {
			return fmt.Errorf("origin %q host %q does not belong to the relying party ID %q", raw, host, c.RPID)
		}
		if u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("origin %q must not contain a path, query, or fragment", raw)
		}
	}

	// Cookies stay unencrypted only for the all-loopback local development
	// setup; any https (or mixed) configuration enforces the Secure attribute.
	c.CookieSecure = anyHTTPS || !allHTTPLoopback
	return nil
}

func validateLoopbackAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q is not a valid host:port address", EnvAddr, addr)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("%s %q must be a loopback address; non-loopback binding is not supported", EnvAddr, addr)
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// envDuration parses a duration setting. A present-but-invalid value is a
// configuration error, never a silent fallback to the default.
func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a valid duration", key, v)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, got %q", key, v)
	}
	return d, nil
}
