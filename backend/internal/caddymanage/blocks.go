package caddymanage

import (
	"bytes"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

type sourceToken struct {
	text       string
	start, end int
}
type sourceBlock struct {
	start, end, open int
	addresses        []string
}

// extractBlocks only locates complete top-level byte ranges. Caddy's official
// adapter remains the syntax and semantic authority; ambiguous ranges fail
// closed instead of partially rewriting a route or expanding an import.
func extractBlocks(data []byte) ([]sourceBlock, error) {
	if len(data) > maxFileBytes || !utf8.Valid(data) {
		return nil, ErrUnsupported
	}
	tokens, err := sourceTokens(data)
	if err != nil {
		return nil, err
	}
	depth := 0
	var header []sourceToken
	var current sourceBlock
	var blocks []sourceBlock
	for _, token := range tokens {
		switch token.text {
		case "\n":
			if depth == 0 && (len(header) == 0 || !strings.HasSuffix(header[len(header)-1].text, ",")) {
				header = nil
			}
		case "{":
			if depth == 0 {
				start := token.start
				if len(header) > 0 {
					start = header[0].start
				}
				start = bytes.LastIndexByte(data[:start], '\n') + 1
				current = sourceBlock{start: start, open: token.end}
				for _, part := range header {
					current.addresses = append(current.addresses, strings.TrimSuffix(part.text, ","))
				}
				header = nil
			}
			depth++
			if depth > 64 {
				return nil, ErrUnsupported
			}
		case "}":
			depth--
			if depth < 0 {
				return nil, ErrUnsupported
			}
			if depth == 0 {
				end := token.end
				if newline := bytes.IndexByte(data[end:], '\n'); newline >= 0 {
					tail := strings.TrimSpace(string(data[end : end+newline]))
					if tail != "" && !strings.HasPrefix(tail, "#") {
						return nil, ErrUnsupported
					}
					end += newline + 1
				}
				current.end = end
				blocks = append(blocks, current)
			}
		default:
			if depth == 0 {
				header = append(header, token)
			}
		}
	}
	if depth != 0 {
		return nil, ErrUnsupported
	}
	return blocks, nil
}

func sourceTokens(data []byte) ([]sourceToken, error) {
	var result []sourceToken
	for index := 0; index < len(data); {
		if len(result) > 100000 {
			return nil, ErrUnsupported
		}
		switch data[index] {
		case ' ', '\t', '\r':
			index++
			continue
		case '\n':
			result = append(result, sourceToken{"\n", index, index + 1})
			index++
			continue
		case '#':
			for index < len(data) && data[index] != '\n' {
				index++
			}
			continue
		}
		start := index
		if data[index] == '"' || data[index] == '`' {
			quote := data[index]
			index++
			closed := false
			for index < len(data) {
				if quote == '"' && data[index] == '\\' {
					index += 2
					continue
				}
				if data[index] == quote {
					index++
					closed = true
					break
				}
				index++
			}
			if !closed || index > len(data) {
				return nil, ErrUnsupported
			}
			// Caddy quotes escape only a quote; they do not use Go's \n or
			// regexp escape rules (v2.11.9 caddyfile/lexer.go).
			value := string(data[start+1 : index-1])
			if quote == '"' {
				value = strings.ReplaceAll(value, `\"`, `"`)
			}
			// Quoted braces and newlines must never become structural tokens.
			if oneOf(value, "{", "}", "\n") {
				value = "quoted:" + value
			}
			result = append(result, sourceToken{value, start, index})
			continue
		}
		for index < len(data) && !bytes.ContainsRune([]byte(" \t\r\n"), rune(data[index])) {
			index++
		}
		value := string(data[start:index])
		if strings.HasPrefix(value, "<<") {
			marker := strings.TrimPrefix(value, "<<")
			if !keyPattern.MatchString(strings.ToLower(marker)) {
				return nil, ErrUnsupported
			}
			closed := false
			for index < len(data) {
				end := bytes.IndexByte(data[index:], '\n')
				if end < 0 {
					end = len(data) - index
				}
				if strings.TrimSpace(string(data[index:index+end])) == marker {
					index += end
					closed = true
					break
				}
				index += end
				if index < len(data) {
					index++
				}
			}
			if !closed {
				return nil, ErrUnsupported
			}
			value = "heredoc"
		}
		result = append(result, sourceToken{value, start, index})
	}
	return result, nil
}

func blockMatches(block sourceBlock, target caddyconfig.Site) bool {
	if len(block.addresses) == 0 {
		return false
	}
	hosts := map[string]bool{}
	ports := map[string]bool{}
	for _, address := range block.addresses {
		if strings.HasPrefix(address, "(") || strings.ContainsAny(address, "{}") {
			return false
		}
		if !strings.Contains(address, "://") {
			address = "https://" + address
		}
		parsed, err := url.Parse(address)
		if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return false
		}
		host := parsed.Hostname()
		if host != "" {
			hosts[strings.ToLower(host)] = true
		}
		port := parsed.Port()
		if port == "" {
			if parsed.Scheme == "http" {
				port = "80"
			} else if parsed.Scheme == "https" {
				port = "443"
			} else {
				return false
			}
		}
		ports[port] = true
	}
	if len(hosts) != len(target.Hosts) {
		return false
	}
	for _, host := range target.Hosts {
		if !hosts[strings.ToLower(host)] {
			return false
		}
	}
	for _, listen := range target.Listen {
		_, port, err := net.SplitHostPort(listen)
		if err != nil || !ports[port] {
			return false
		}
	}
	return len(target.Listen) > 0
}
