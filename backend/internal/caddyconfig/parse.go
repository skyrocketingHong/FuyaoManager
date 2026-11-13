// Package caddyconfig inventories the JSON produced by Caddy's own adapter.
// It does not parse Caddyfile syntax, execute handlers, or grant write access.
package caddyconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxConfigBytes = 4 << 20
const maxDepth = 64
const maxTokens = 100000
const maxSites = 2048

var (
	ErrTooLarge   = errors.New("Caddy JSON exceeds the size limit")
	ErrInvalid    = errors.New("invalid Caddy JSON configuration")
	ErrComplexity = errors.New("Caddy JSON exceeds the structural limit")
)

type Kind string

const (
	KindStatic       Kind = "static"
	KindRedirect     Kind = "redirect"
	KindReverseProxy Kind = "reverse_proxy"
	KindUnsupported  Kind = "unsupported"
)

type Redirect struct {
	Location       string `json:"location"`
	StatusCode     int    `json:"statusCode"`
	TargetRedacted bool   `json:"targetRedacted,omitempty"`
}

// ReadOnly reports structural limitations, not filesystem or user permissions.
// A false value is only a simple syntax candidate; callers still authorize,
// validate the live source, and preserve Caddy options before any takeover.
type Site struct {
	ID                 string    `json:"id"`
	Digest             string    `json:"digest"`
	Server             string    `json:"server"`
	RoutePath          string    `json:"routePath"`
	Hosts              []string  `json:"hosts"`
	Listen             []string  `json:"listen"`
	Kind               Kind      `json:"kind"`
	ReadOnly           bool      `json:"readOnly"`
	Reason             string    `json:"reason,omitempty"`
	Root               string    `json:"root,omitempty"`
	RootHasPlaceholder bool      `json:"rootHasPlaceholder"`
	Redirect           *Redirect `json:"redirect,omitempty"`
	DuplicateCount     int       `json:"duplicateCount,omitempty"`
}

type route struct {
	Match    []map[string]json.RawMessage `json:"match"`
	Handle   []map[string]json.RawMessage `json:"handle"`
	Terminal bool                         `json:"terminal"`
	Group    string                       `json:"group"`
	unknown  bool
	raw      json.RawMessage
}

type candidate struct {
	site           Site
	routes         []route
	matchScope     []json.RawMessage
	initialRoot    string
	initialReasons []string
	contentDigest  string
}

// Parse consumes exactly one bounded UTF-8 JSON document. Unknown handler
// payloads stay private; only the explicit inventory fields above escape.
// ASVS 1.5.2/2.2.1: bounded structure, duplicate-key rejection and allowlists.
func Parse(data []byte) ([]Site, error) {
	if len(data) > MaxConfigBytes {
		return nil, ErrTooLarge
	}
	if len(data) == 0 || !utf8.Valid(data) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	budget := maxTokens
	if err := validateValue(decoder, 0, &budget); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(data, &document) != nil || document == nil {
		return nil, ErrInvalid
	}
	appsRaw, hasApps := document["apps"]
	if !hasApps {
		for key := range document {
			if !allowed(key, "admin", "logging", "storage", "identity", "@id") {
				return nil, ErrInvalid
			}
		}
		return []Site{}, nil
	}
	apps, err := object(appsRaw)
	if err != nil {
		return nil, err
	}
	httpRaw, hasHTTP := apps["http"]
	if !hasHTTP {
		return []Site{}, nil
	}
	httpApp, err := object(httpRaw)
	if err != nil {
		return nil, err
	}
	serversRaw, hasServers := httpApp["servers"]
	if !hasServers {
		return []Site{}, nil
	}
	servers, err := object(serversRaw)
	if err != nil {
		return nil, err
	}
	digest, err := canonicalDigest(data)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	var found []candidate
	for _, name := range names {
		server, err := object(servers[name])
		if err != nil {
			return nil, err
		}
		var listen []string
		if raw, exists := server["listen"]; exists && json.Unmarshal(raw, &listen) != nil {
			return nil, ErrInvalid
		}
		listen = normalized(listen, false)
		initial := []string{}
		if len(listen) == 0 {
			initial = append(initial, "missing_listen")
		}
		for _, address := range listen {
			if !safeText(address, 512) || strings.ContainsAny(address, "@?#{}") {
				initial = append(initial, "unsupported_listen")
				listen = []string{}
				break
			}
		}
		for key, raw := range server {
			if !allowed(key, "listen", "routes", "logs", "protocols", "@id") && !isEmpty(raw) {
				initial = append(initial, "server_policy")
				break
			}
		}
		routes, err := decodeRoutes(server["routes"])
		if err != nil {
			return nil, err
		}
		if len(routes) == 0 {
			continue
		}
		label := name
		if !safeText(label, 128) {
			label = "server-" + hash([]byte(name))[:12]
			initial = append(initial, "unsupported_server_name")
		}
		path := "/apps/http/servers/" + pointer(label) + "/routes"
		base := Site{Server: label, RoutePath: path, Hosts: []string{}, Listen: listen, Digest: digest}
		items := discover(base, routes, path, nil, "", initial)
		found = append(found, items...)
		if len(found) > maxSites {
			return nil, ErrComplexity
		}
	}
	for index := range found {
		analyze(&found[index])
	}
	assignIDs(found)
	result := make([]Site, 0, len(found))
	for _, item := range found {
		result = append(result, item.site)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func validateValue(decoder *json.Decoder, depth int, budget *int) error {
	if depth > maxDepth {
		return ErrComplexity
	}
	*budget--
	if *budget < 0 {
		return ErrComplexity
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return ErrInvalid
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return ErrInvalid
			}
			keys[name] = true
			if err := validateValue(decoder, depth+1, budget); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := validateValue(decoder, depth+1, budget); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	closing, err := decoder.Token()
	if err != nil || (delimiter == '{' && closing != json.Delim('}')) || (delimiter == '[' && closing != json.Delim(']')) {
		return ErrInvalid
	}
	return nil
}

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return nil, ErrInvalid
	}
	return value, nil
}

func decodeRoutes(raw json.RawMessage) ([]route, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || entries == nil {
		return nil, ErrInvalid
	}
	out := make([]route, 0, len(entries))
	for _, entry := range entries {
		var value route
		if json.Unmarshal(entry, &value) != nil {
			return nil, ErrInvalid
		}
		fields, err := object(entry)
		if err != nil {
			return nil, err
		}
		value.raw = entry
		for key := range fields {
			if !allowed(key, "match", "handle", "terminal", "group", "@id") {
				value.unknown = true
			}
		}
		out = append(out, value)
	}
	return out, nil
}
