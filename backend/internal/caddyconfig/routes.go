package caddyconfig

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func discover(base Site, routes []route, path string, scope []json.RawMessage, root string, reasons []string) []candidate {
	hasHost := false
	for _, value := range routes {
		if routeHasHost(value) {
			hasHost = true
			break
		}
	}
	if !hasHost {
		initialRoot := root
		var nested []candidate
		var residual []route
		for index, value := range routes {
			currentRoot := routeRoot(value, root)
			hasNestedHost := false
			for handlerIndex, handler := range value.Handle {
				if stringField(handler, "handler") != "subroute" {
					continue
				}
				children, err := decodeRoutes(handler["routes"])
				if err != nil {
					continue
				}
				if !containsHost(children) {
					continue
				}
				hasNestedHost = true
				nextReasons := append([]string{}, reasons...)
				if len(routes) != 1 || len(value.Handle) != 1 || len(value.Match) > 0 || value.unknown || value.Group != "" || !onlyKeys(handler, "handler", "routes", "@id") {
					nextReasons = append(nextReasons, "inherited_scope")
				}
				nextPath := path + "/" + strconv.Itoa(index) + "/handle/" + strconv.Itoa(handlerIndex) + "/routes"
				nested = append(nested, discover(base, children, nextPath, appendScope(scope, value.Match), currentRoot, nextReasons)...)
			}
			if !hasNestedHost {
				residual = append(residual, value)
			}
			if len(value.Match) == 0 {
				root = currentRoot
			}
		}
		if len(nested) > 0 {
			if hasResponse(residual) {
				item := base
				item.RoutePath = path
				nested = append(nested, candidate{site: item, routes: residual, initialRoot: initialRoot, initialReasons: append(append([]string{}, reasons...), "shared_routes"), matchScope: scope})
			}
			return nested
		}
		base.RoutePath = path
		return []candidate{{site: base, routes: routes, initialRoot: initialRoot, initialReasons: reasons, matchScope: scope}}
	}
	var out []candidate
	var hostless []route
	for index, value := range routes {
		if !routeHasHost(value) {
			hostless = append(hostless, value)
			root = routeRoot(value, root)
			continue
		}
		item := base
		item.RoutePath = path + "/" + strconv.Itoa(index)
		item.Hosts = hosts(value)
		nextReasons := append([]string{}, reasons...)
		if len(item.Hosts) == 0 {
			nextReasons = append(nextReasons, "unsupported_host")
		}
		out = append(out, candidate{site: item, routes: []route{value}, initialRoot: root, initialReasons: nextReasons, matchScope: appendScope(scope, value.Match)})
	}
	if len(hostless) > 0 {
		for index := range out {
			out[index].initialReasons = append(out[index].initialReasons, "shared_routes")
		}
		if hasResponse(hostless) {
			item := base
			item.RoutePath = path
			out = append(out, candidate{site: item, routes: hostless, initialRoot: root, initialReasons: append(append([]string{}, reasons...), "shared_routes"), matchScope: scope})
		}
	}
	return out
}

func routeHasHost(value route) bool {
	for _, match := range value.Match {
		if _, exists := match["host"]; exists {
			return true
		}
	}
	return false
}
func containsHost(routes []route) bool {
	for _, value := range routes {
		if routeHasHost(value) {
			return true
		}
		for _, handler := range value.Handle {
			if stringField(handler, "handler") == "subroute" {
				nested, _ := decodeRoutes(handler["routes"])
				if containsHost(nested) {
					return true
				}
			}
		}
	}
	return false
}
func hosts(value route) []string {
	var out []string
	for _, match := range value.Match {
		var entries []string
		if raw, exists := match["host"]; exists && json.Unmarshal(raw, &entries) == nil {
			for _, entry := range entries {
				if entry != "" && safeText(entry, 253) && !strings.ContainsAny(entry, "{}@/\\?# ") {
					out = append(out, entry)
				}
			}
		}
	}
	return normalized(out, true)
}

type terminal struct {
	kind     Kind
	root     string
	redirect *Redirect
}
type facts struct {
	terminals       []terminal
	reasons         []string
	primaryRoot     string
	rootAssignments int
}

func (value *facts) reason(reason string) {
	if value == nil || reason == "" {
		return
	}
	for _, old := range value.reasons {
		if old == reason {
			return
		}
	}
	value.reasons = append(value.reasons, reason)
}

func analyze(item *candidate) {
	value := &facts{reasons: append([]string{}, item.initialReasons...)}
	if item.initialRoot != "" {
		value.primaryRoot = item.initialRoot
	}
	analyzeRoutes(item.routes, item.initialRoot, value, len(item.site.Hosts) > 0, false)
	static, redirect, proxy := 0, 0, 0
	for _, leaf := range value.terminals {
		switch leaf.kind {
		case KindStatic:
			static++
		case KindRedirect:
			redirect++
		case KindReverseProxy:
			proxy++
		}
	}
	switch {
	case static > 0:
		item.site.Kind = KindStatic
	case redirect > 0:
		item.site.Kind = KindRedirect
	case proxy > 0:
		item.site.Kind = KindReverseProxy
	default:
		item.site.Kind = KindUnsupported
		value.reason("no_supported_response")
	}
	if len(value.terminals) != 1 {
		value.reason("multiple_responses")
	}
	if (static > 0 && redirect+proxy > 0) || (redirect > 0 && proxy > 0) {
		value.reason("mixed_handlers")
	}
	if static > 0 {
		roots := map[string]bool{}
		for _, leaf := range value.terminals {
			if leaf.kind == KindStatic {
				roots[leaf.root] = true
			}
		}
		if value.primaryRoot != "" && roots[value.primaryRoot] && !roots[""] {
			item.site.Root = value.primaryRoot
		} else if len(roots) == 1 {
			for root := range roots {
				item.site.Root = root
			}
		}
		if !safeText(item.site.Root, 4096) {
			item.site.Root = ""
			value.reason("unsupported_root")
		}
		item.site.RootHasPlaceholder = strings.ContainsAny(item.site.Root, "{}")
		if item.site.RootHasPlaceholder {
			value.reason("placeholder_root")
		}
		if item.site.Root == "" {
			value.reason("implicit_or_multiple_roots")
		} else if !item.site.RootHasPlaceholder && !filepath.IsAbs(item.site.Root) {
			value.reason("relative_root")
		}
	}
	if redirect > 0 {
		for _, leaf := range value.terminals {
			if leaf.redirect == nil {
				continue
			}
			if item.site.Redirect == nil {
				copy := *leaf.redirect
				item.site.Redirect = &copy
			} else if *item.site.Redirect != *leaf.redirect {
				item.site.Redirect = nil
				value.reason("multiple_redirect_targets")
				break
			}
		}
	}
	item.site.ReadOnly = len(value.reasons) > 0
	if item.site.ReadOnly {
		item.site.Reason = strings.Join(value.reasons, ",")
	}
	rawRoutes := make([]json.RawMessage, 0, len(item.routes))
	for _, route := range item.routes {
		rawRoutes = append(rawRoutes, route.raw)
	}
	encoded, _ := json.Marshal(rawRoutes)
	item.contentDigest, _ = canonicalDigest(encoded)
}

func analyzeRoutes(routes []route, root string, value *facts, outerHost bool, conditional bool) string {
	if len(routes) > 1 {
		value.reason("multiple_routes")
	}
	for _, current := range routes {
		matched := len(current.Match) > 0
		if matched && !(outerHost && hostOnly(current.Match)) {
			value.reason("conditional_matcher")
		}
		if current.unknown {
			value.reason("unsupported_route_fields")
		}
		if current.Group != "" {
			value.reason("grouped_routes")
		}
		localConditional := conditional || (matched && !outerHost)
		localRoot := root
		for _, handler := range current.Handle {
			switch stringField(handler, "handler") {
			case "vars":
				if !onlyKeys(handler, "handler", "root", "@id") {
					value.reason("custom_variables")
				}
				if raw, exists := handler["root"]; exists {
					value.rootAssignments++
					if value.rootAssignments > 1 {
						value.reason("multiple_root_assignments")
					}
					var next string
					if json.Unmarshal(raw, &next) != nil {
						value.reason("unsupported_root")
						localRoot = ""
						if !localConditional && len(value.terminals) == 0 {
							value.primaryRoot = ""
						}
					} else {
						localRoot = next
						if !localConditional && len(value.terminals) == 0 {
							value.primaryRoot = next
						}
					}
				}
			case "encode":
				if !onlyKeys(handler, "handler", "encodings", "prefer", "minimum_length", "@id") {
					value.reason("custom_encoding")
				}
			case "file_server":
				if !onlyKeys(handler, "handler", "root", "@id") {
					value.reason("custom_static_options")
				}
				next := stringField(handler, "root")
				if raw, exists := handler["root"]; exists && json.Unmarshal(raw, &next) != nil {
					value.reason("unsupported_root")
					next = ""
					localRoot = ""
				}
				if next != "" && next != "{http.vars.root}" && localRoot != "" {
					value.reason("explicit_root_override")
				}
				if next == "" || next == "{http.vars.root}" {
					next = localRoot
				}
				if !localConditional && len(value.terminals) == 0 {
					value.primaryRoot = next
				}
				value.terminals = append(value.terminals, terminal{kind: KindStatic, root: next})
			case "static_response":
				redirect, reason := redirectOf(handler)
				if redirect == nil {
					value.reason("static_response")
					continue
				}
				value.reason(reason)
				value.terminals = append(value.terminals, terminal{kind: KindRedirect, redirect: redirect})
			case "reverse_proxy":
				value.reason("reverse_proxy_read_only")
				value.terminals = append(value.terminals, terminal{kind: KindReverseProxy})
			case "subroute":
				if !onlyKeys(handler, "handler", "routes", "@id") {
					value.reason("subroute_policy")
				}
				children, err := decodeRoutes(handler["routes"])
				if err != nil {
					value.reason("invalid_subroute")
					continue
				}
				localRoot = analyzeRoutes(children, localRoot, value, false, localConditional)
			default:
				value.reason("unsupported_handler")
			}
		}
		if !matched || outerHost {
			root = localRoot
		}
		outerHost = false
	}
	return root
}

func redirectOf(handler map[string]json.RawMessage) (*Redirect, string) {
	var code int
	if json.Unmarshal(handler["status_code"], &code) != nil {
		code, _ = strconv.Atoi(stringField(handler, "status_code"))
	}
	if code < 300 || code > 399 || code == 304 {
		return nil, ""
	}
	var headers map[string][]string
	if json.Unmarshal(handler["headers"], &headers) != nil {
		return nil, ""
	}
	var locations []string
	for name, entries := range headers {
		if strings.EqualFold(name, "Location") {
			locations = append(locations, entries...)
		}
	}
	if len(locations) != 1 || !safeText(locations[0], 8192) {
		return nil, ""
	}
	result := &Redirect{StatusCode: code, Location: locations[0]}
	reason := ""
	if !onlyKeys(handler, "handler", "status_code", "headers", "@id") || len(headers) != 1 {
		reason = "custom_redirect_response"
	}
	checked := strings.NewReplacer(
		"{http.request.uri}", "/placeholder", "{uri}", "/placeholder",
		"{http.request.orig_uri}", "/placeholder", "{http.request.uri.path}", "/placeholder", "{path}", "/placeholder",
		"{http.request.host}", "example.invalid", "{host}", "example.invalid",
		"{http.request.hostport}", "example.invalid:443", "{http.request.scheme}", "https", "{http.request.port}", "443",
	).Replace(result.Location)
	parsed, err := url.Parse(checked)
	if strings.ContainsAny(checked, "{}") {
		err = ErrInvalid
	}
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https") {
		result.Location = ""
		result.TargetRedacted = true
		return result, "redacted_redirect_target"
	}
	if strings.ContainsAny(result.Location, "{}") {
		return result, "placeholder_redirect"
	}
	return result, reason
}

func routeRoot(value route, root string) string {
	if len(value.Match) > 0 {
		return root
	}
	for _, handler := range value.Handle {
		if stringField(handler, "handler") == "vars" {
			if next := stringField(handler, "root"); next != "" {
				root = next
			}
		}
	}
	return root
}
func hostOnly(matches []map[string]json.RawMessage) bool {
	for _, match := range matches {
		if len(match) != 1 {
			return false
		}
		var hosts []string
		if json.Unmarshal(match["host"], &hosts) != nil || len(hosts) == 0 {
			return false
		}
		for _, host := range hosts {
			if host == "" || !safeText(host, 253) || strings.ContainsAny(host, "{}@/\\?# ") {
				return false
			}
		}
	}
	return len(matches) > 0
}

func hasResponse(routes []route) bool {
	for _, route := range routes {
		for _, handler := range route.Handle {
			switch stringField(handler, "handler") {
			case "vars", "headers", "encode", "rewrite", "request_body", "authentication":
			case "subroute":
				children, _ := decodeRoutes(handler["routes"])
				if hasResponse(children) {
					return true
				}
			default:
				return true
			}
		}
	}
	return false
}
func appendScope(scope []json.RawMessage, matches []map[string]json.RawMessage) []json.RawMessage {
	result := append([]json.RawMessage{}, scope...)
	if len(matches) == 0 {
		return result
	}
	if hostOnly(matches) {
		values := hosts(route{Match: matches})
		raw, _ := json.Marshal(map[string][]string{"host": values})
		return append(result, raw)
	}
	raw, _ := json.Marshal(matches)
	return append(result, raw)
}
