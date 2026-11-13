package buildrunner

import (
	"errors"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var ErrPreviewUnsafeOrigin = errors.New("preview wrapper requires independent HTTPS origins")

// PreviewLease names an existing origin incarnation. Looking it up never
// starts a listener or revives a closed preview.
type PreviewLease struct {
	SiteID     uuid.UUID
	BuildID    uuid.UUID
	Host       string
	InstanceID string
	ExpiresAt  time.Time
}
type PreviewFrame struct {
	PreviewLease
	EntryURL string
	Origin   string
}

func (p *PreviewServer) WrapperEnabled() bool {
	origin, err := url.Parse(p.cfg.ManagerOrigin)
	return err == nil && origin.Scheme == "https" && origin.Hostname() != ""
}

func (p *PreviewServer) WrapperHostname() string {
	_, preview, err := p.wrapperOrigins()
	if err != nil {
		return ""
	}
	parsed, _ := url.Parse(preview)
	return strings.ToLower(parsed.Hostname())
}

// Caddy's private TLS listeners need their own range: website listeners may
// already own the allocated backend port on the Tailscale interface.
func (p *PreviewServer) externalOrigin(backendPort int) (string, error) {
	offset := p.cfg.ExternalPortOffset
	if backendPort < 1 || backendPort > 65535 || offset < 0 || offset > 65535-backendPort {
		return "", ErrPreviewUnsafeOrigin
	}
	port := strconv.Itoa(backendPort + offset)
	if p.cfg.ExternalBase == "" {
		return "http://" + net.JoinHostPort(p.cfg.BindAddr, port), nil
	}
	base, err := url.Parse(p.cfg.ExternalBase)
	if err != nil || base.Hostname() == "" || base.Port() != "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return "", ErrPreviewUnsafeOrigin
	}
	base.Host = net.JoinHostPort(strings.ToLower(base.Hostname()), port)
	base.Path = ""
	return base.String(), nil
}

func (p *PreviewServer) wrapperOrigins() (manager, preview string, err error) {
	m, first := url.Parse(p.cfg.ManagerOrigin)
	v, second := url.Parse(p.cfg.ExternalBase)
	if first != nil || second != nil || m == nil || v == nil || m.Scheme != "https" || v.Scheme != "https" ||
		m.Hostname() == "" || v.Hostname() == "" || strings.EqualFold(m.Hostname(), v.Hostname()) ||
		m.User != nil || v.User != nil || (m.Path != "" && m.Path != "/") || v.Path != "" ||
		m.RawQuery != "" || v.RawQuery != "" || m.Fragment != "" || v.Fragment != "" || v.Port() != "" {
		return "", "", ErrPreviewUnsafeOrigin
	}
	m.Host = strings.ToLower(m.Host)
	v.Host = strings.ToLower(v.Host)
	return strings.TrimRight(m.String(), "/"), v.String(), nil
}

func ValidPreviewHost(host string) bool {
	if host == "" || len(host) > 253 || host != strings.ToLower(host) {
		return false
	}
	if !strings.Contains(host, ".") {
		if len(host) > 63 || host[0] == '-' {
			return false
		}
		for _, ch := range host {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
		}
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return false
			}
		}
	}
	return true
}

func (p *PreviewServer) IssueWrapper(siteID, buildID uuid.UUID, host string) (string, time.Time, error) {
	if _, _, err := p.wrapperOrigins(); err != nil {
		return "", time.Time{}, err
	}
	if !ValidPreviewHost(host) {
		return "", time.Time{}, ErrPreviewNotReady
	}
	return p.issue(siteID, buildID, host)
}

func (p *PreviewServer) lookupWrapperLocked(token, host string) (PreviewLease, *previewOrigin, error) {
	lease, ok := p.leases[token]
	if !ok || !ValidPreviewHost(host) || host != lease.Host || !time.Now().Before(lease.ExpiresAt) || p.closed {
		return PreviewLease{}, nil, ErrPreviewClosed
	}
	origin := p.origins[lease.BuildID]
	if origin == nil || origin.siteID != lease.SiteID || origin.instanceID != lease.InstanceID || origin.expired() {
		return PreviewLease{}, nil, ErrPreviewClosed
	}
	return lease, origin, nil
}

func (p *PreviewServer) LookupWrapper(token, host string) (PreviewLease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, _, err := p.lookupWrapperLocked(token, host)
	return lease, err
}

// FrameWrapper renews only the entry token of an existing lease. It cannot
// call Issue: a page reload after CloseOrigin must remain gone.
func (p *PreviewServer) FrameWrapper(token, host, route string) (PreviewFrame, error) {
	_, _, err := p.wrapperOrigins()
	if err != nil {
		return PreviewFrame{}, err
	}
	next, err := previewPath(route)
	if err != nil {
		return PreviewFrame{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, origin, err := p.lookupWrapperLocked(token, host)
	if err != nil {
		return PreviewFrame{}, err
	}
	frameOrigin, err := p.externalOrigin(origin.port)
	if err != nil {
		return PreviewFrame{}, err
	}
	entryToken, err := NewPreviewToken()
	if err != nil {
		return PreviewFrame{}, err
	}
	origin.mu.Lock()
	origin.tokens[entryToken] = lease.ExpiresAt
	origin.entries[entryToken] = struct{}{}
	origin.mu.Unlock()
	return PreviewFrame{PreviewLease: lease, Origin: frameOrigin, EntryURL: frameOrigin + "/__preview__/" + entryToken + "?next=" + url.QueryEscape(next)}, nil
}

// Paths stay local to the isolated origin; reject open redirects and
// browser/server normalization differences before issuing an entry token.
func previewPath(route string) (string, error) {
	if route == "" {
		return "/", nil
	}
	parsed, err := url.Parse(route)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, `\`) {
		return "", ErrPreviewNotReady
	}
	for _, ch := range route {
		if unicode.IsControl(ch) {
			return "", ErrPreviewNotReady
		}
	}
	clean := path.Clean(parsed.Path)
	if clean != strings.TrimSuffix(parsed.Path, "/") && !(clean == "/" && parsed.Path == "/") {
		return "", ErrPreviewNotReady
	}
	return parsed.String(), nil
}
