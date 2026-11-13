package caddyinventory

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

const Version = 1
const maxSites = 2048

var siteIDPattern = regexp.MustCompile(`^caddy-[a-f0-9]{64}(-[a-f0-9]{16}-[1-9][0-9]*)?$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Envelope struct {
	Version int                `json:"fuyaoInventoryVersion"`
	Sites   []caddyconfig.Site `json:"sites"`
}

// WriteSafe parses the official adapter output before anything crosses the
// privileged helper boundary. Source text and handler payloads never escape.
// ASVS 2.2.1/5.3.2: a bounded allowlist replaces raw privileged configuration.
func WriteSafe(reader io.Reader, writer io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(reader, caddyconfig.MaxConfigBytes+1))
	if err != nil {
		return caddyconfig.ErrInvalid
	}
	sites, err := caddyconfig.Parse(data)
	if err != nil {
		return err
	}
	for _, site := range sites {
		if !validSite(site) {
			return caddyconfig.ErrInvalid
		}
	}
	encoded, err := json.Marshal(Envelope{Version: Version, Sites: sites})
	if err != nil {
		return caddyconfig.ErrInvalid
	}
	if len(encoded)+1 > caddyconfig.MaxConfigBytes {
		return caddyconfig.ErrTooLarge
	}
	_, err = writer.Write(append(encoded, '\n'))
	return err
}

// ParseOutput accepts the versioned helper protocol and, for previously
// configured fixed read commands, Caddy's original adapter document.
func ParseOutput(data []byte) ([]caddyconfig.Site, error) {
	if len(data) > caddyconfig.MaxConfigBytes {
		return nil, caddyconfig.ErrTooLarge
	}
	if len(data) == 0 || !utf8.Valid(data) {
		return nil, caddyconfig.ErrInvalid
	}
	var header map[string]json.RawMessage
	if json.Unmarshal(data, &header) != nil || header == nil {
		return nil, caddyconfig.ErrInvalid
	}
	if _, found := header["fuyaoInventoryVersion"]; !found {
		return caddyconfig.Parse(data)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	budget := 100000
	if !validValue(decoder, 0, &budget) {
		return nil, caddyconfig.ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, caddyconfig.ErrInvalid
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope Envelope
	if decoder.Decode(&envelope) != nil || envelope.Version != Version || envelope.Sites == nil || len(envelope.Sites) > maxSites {
		return nil, caddyconfig.ErrInvalid
	}
	if _, ok := header["sites"]; !ok || len(header) != 2 {
		return nil, caddyconfig.ErrInvalid
	}
	ids := make(map[string]bool, len(envelope.Sites))
	for _, site := range envelope.Sites {
		if !validSite(site) || ids[site.ID] {
			return nil, caddyconfig.ErrInvalid
		}
		ids[site.ID] = true
	}
	return envelope.Sites, nil
}

func validSite(site caddyconfig.Site) bool {
	if !siteIDPattern.MatchString(site.ID) || len(site.ID) > 128 || !digestPattern.MatchString(site.Digest) || !safeText(site.Server, 256) || !safeText(site.RoutePath, 8192) || !strings.HasPrefix(site.RoutePath, "/") || !safeText(site.Root, 8192) || !safeText(site.Reason, 8192) || len(site.Hosts) > 4096 || len(site.Listen) > 4096 || site.DuplicateCount < 0 || site.DuplicateCount > maxSites {
		return false
	}
	switch site.Kind {
	case caddyconfig.KindStatic, caddyconfig.KindRedirect, caddyconfig.KindReverseProxy, caddyconfig.KindUnsupported:
	default:
		return false
	}
	for _, host := range site.Hosts {
		if host == "" || !safeText(host, 1024) {
			return false
		}
	}
	for _, listener := range site.Listen {
		if listener == "" || !safeText(listener, 8192) {
			return false
		}
	}
	if redirect := site.Redirect; redirect != nil {
		if !safeText(redirect.Location, 8192) || redirect.StatusCode < 300 || redirect.StatusCode > 399 {
			return false
		}
	}
	return true
}

func safeText(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validValue(decoder *json.Decoder, depth int, budget *int) bool {
	*budget--
	if depth > 64 || *budget < 0 {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return true
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return false
			}
			seen[name] = true
			if !validValue(decoder, depth+1, budget) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !validValue(decoder, depth+1, budget) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}
