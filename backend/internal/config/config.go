// Package config loads and validates backend configuration from environment
// variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment variable names.
const (
	EnvDatabaseURL       = "FUYAO_ADMIN_DATABASE_URL"
	EnvAddr              = "FUYAO_ADMIN_ADDR"
	EnvTrustedHosts      = "FUYAO_ADMIN_TRUSTED_HOSTS"
	EnvRPID              = "FUYAO_ADMIN_RP_ID"
	EnvRPName            = "FUYAO_ADMIN_RP_DISPLAY_NAME"
	EnvRPOrigins         = "FUYAO_ADMIN_RP_ORIGINS"
	EnvSessionTTL        = "FUYAO_ADMIN_SESSION_TTL"
	EnvChallengeTTL      = "FUYAO_ADMIN_CHALLENGE_TTL"
	EnvPermitTTL         = "FUYAO_ADMIN_PERMIT_TTL"
	EnvAuthCodeTTL       = "FUYAO_ADMIN_AUTH_CODE_TTL"
	EnvThemeContractsDir = "FUYAO_ADMIN_THEME_CONTRACTS"
	EnvPreviewBind       = "FUYAO_ADMIN_PREVIEW_BIND"
	EnvPreviewBase       = "FUYAO_ADMIN_PREVIEW_EXTERNAL_BASE"
	EnvEdgeSnippetDir    = "FUYAO_ADMIN_EDGE_SNIPPET_DIR"
	EnvEdgeApplyCmd      = "FUYAO_ADMIN_EDGE_APPLY_CMD"
	EnvEdgePrivateBind   = "FUYAO_ADMIN_EDGE_PRIVATE_BIND"
	EnvEdgePublicBind    = "FUYAO_ADMIN_EDGE_PUBLIC_BIND"
	EnvEdgePublicHost    = "FUYAO_ADMIN_EDGE_PUBLIC_HOST"
	EnvBuildsRoot        = "FUYAO_ADMIN_BUILDS_ROOT"
	EnvReleasesRoot      = "FUYAO_ADMIN_RELEASES_ROOT"
	EnvPortMin           = "FUYAO_ADMIN_PORT_MIN"
	EnvPortMax           = "FUYAO_ADMIN_PORT_MAX"
	EnvRepoRoot          = "FUYAO_ADMIN_REPO_ROOT"
	EnvSitesRoot         = "FUYAO_ADMIN_SITES_ROOT"
	EnvAdminUIDir        = "FUYAO_ADMIN_UI_DIR"
	EnvBuildMode         = "FUYAO_ADMIN_BUILD_MODE"
	EnvRequireCaps       = "FUYAO_ADMIN_REQUIRE_CAPABILITIES"
	EnvRequireURLs       = "FUYAO_ADMIN_REQUIRE_URL_CONVENTIONS"
)

// Config is the fully validated runtime configuration. CookieSecure is
// derived from the origin schemes: only an https origin marks cookies
// Secure, because browsers refuse Secure cookies over the plain-HTTP
// topologies this server supports (loopback development and the explicitly
// trusted private-network address).
type Config struct {
	DatabaseURL         string
	Addr                string
	TrustedHosts        []string
	RPID                string
	RPDisplayName       string
	RPOrigins           []string
	SessionTTL          time.Duration
	ChallengeTTL        time.Duration
	PermitTTL           time.Duration
	AuthCodeTTL         time.Duration
	CookieSecure        bool
	ThemeContractsDir   string
	RepoRoot            string
	SitesRoot           string
	AdminUIDir          string
	PreviewBind         string
	PreviewExternalBase string
	EdgeSnippetDir      string
	EdgeApplyCmd        []string
	EdgePrivateBind     string
	EdgePublicBind      string
	EdgePublicHost      string
	PortMin             int
	PortMax             int
	BuildsRoot          string
	ReleasesRoot        string
	BuildMode           string
	RequireCaps         []string
	RequireURLs         []string
}

// Load reads the environment and validates it.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:         os.Getenv(EnvDatabaseURL),
		Addr:                envDefault(EnvAddr, "127.0.0.1:8787"),
		TrustedHosts:        csvEnv(EnvTrustedHosts, ""),
		RPID:                envDefault(EnvRPID, "localhost"),
		RPDisplayName:       envDefault(EnvRPName, "FuyaoManager"),
		ThemeContractsDir:   envDefault(EnvThemeContractsDir, ""),
		RepoRoot:            envDefault(EnvRepoRoot, ""),
		SitesRoot:           envDefault(EnvSitesRoot, ""),
		AdminUIDir:          envDefault(EnvAdminUIDir, ""),
		PreviewBind:         envDefault(EnvPreviewBind, "127.0.0.1"),
		PreviewExternalBase: envDefault(EnvPreviewBase, ""),
		EdgeSnippetDir:      envDefault(EnvEdgeSnippetDir, ""),
		EdgePrivateBind:     envDefault(EnvEdgePrivateBind, "127.0.0.1"),
		EdgePublicBind:      envDefault(EnvEdgePublicBind, ""),
		EdgePublicHost:      envDefault(EnvEdgePublicHost, ""),
		PortMin:             20000,
		PortMax:             20100,
		BuildsRoot:          envDefault(EnvBuildsRoot, ""),
		ReleasesRoot:        envDefault(EnvReleasesRoot, ""),
		BuildMode:           envDefault(EnvBuildMode, "ci"),
		RequireCaps:         csvEnv(EnvRequireCaps, "blog"),
		RequireURLs:         csvEnv(EnvRequireURLs, "posts,blog-rss,sitemap"),
	}

	applyCmdRaw := envDefault(EnvEdgeApplyCmd, "sudo -n /usr/local/sbin/fuyao-caddy-apply")
	for _, part := range strings.Fields(applyCmdRaw) {
		cfg.EdgeApplyCmd = append(cfg.EdgeApplyCmd, part)
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
	setTTL(EnvAuthCodeTTL, &cfg.AuthCodeTTL, 10*time.Minute)
	if ttlErr != nil {
		return Config{}, ttlErr
	}
	var intErr error
	setInt := func(envName string, dst *int, fallback int) {
		if intErr != nil {
			return
		}
		*dst, intErr = envInt(envName, fallback)
	}
	setInt(EnvPortMin, &cfg.PortMin, 20000)
	setInt(EnvPortMax, &cfg.PortMax, 20100)
	if intErr != nil {
		return Config{}, intErr
	}
	if ttlErr != nil {
		return Config{}, ttlErr
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// envInt parses an integer setting. A present-but-invalid value is a
// configuration error, never a silent fallback.
func envInt(key string, fallback int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a valid integer", key, v)
	}
	return n, nil
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("%s is required", EnvDatabaseURL)
	}
	if err := c.validateAddr(c.Addr); err != nil {
		return err
	}
	if c.PortMin < 1024 || c.PortMax > 65535 || c.PortMin > c.PortMax {
		return fmt.Errorf("%s/%s must define a port range within 1024-65535 with min <= max", EnvPortMin, EnvPortMax)
	}
	if c.RPID == "" {
		return fmt.Errorf("%s must not be empty", EnvRPID)
	}
	if len(c.RPOrigins) == 0 {
		return fmt.Errorf("%s must contain at least one origin", EnvRPOrigins)
	}

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
		case "http":
			if !isLoopbackHost(host) && !c.isTrustedHost(host) {
				return fmt.Errorf("plain-HTTP origin %q is only allowed on a loopback host or a host listed in %s", raw, EnvTrustedHosts)
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

	// Cookies carry the Secure attribute only when an https origin is in
	// play. A plain-HTTP topology (loopback development or the explicitly
	// trusted private-network address) must NOT set Secure: browsers refuse to
	// store or return Secure cookies over HTTP, which would break sessions
	// while the API keeps answering 200.
	c.CookieSecure = anyHTTPS
	return nil
}

// validateAddr accepts loopback listen addresses anywhere and non-loopback
// hosts only when they are explicitly trusted. A trusted non-loopback host is
// the private-network deployment topology (Tailscale address); it must never
// be guessed from request headers.
func (c *Config) validateAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q is not a valid host:port address", EnvAddr, addr)
	}
	if isLoopbackHost(host) {
		return nil
	}
	if c.isTrustedHost(host) {
		return nil
	}
	return fmt.Errorf("%s %q must be a loopback address or a host listed in %s; non-loopback binding is otherwise not supported", EnvAddr, addr, EnvTrustedHosts)
}

func (c *Config) isTrustedHost(host string) bool {
	for _, trusted := range c.TrustedHosts {
		if trusted == host {
			return true
		}
	}
	return false
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

// csvEnv reads a comma-separated list setting with a fallback; empty entries
// are dropped. An empty fallback yields a nil list.
func csvEnv(key, fallback string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		raw = fallback
	}
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
