package caddymanage

import (
	"bytes"
)

const PublicationFallbackMarker = "__FUYAO_PUBLICATION_FALLBACK__"

// This fixed rule is rendered only after sealing an actual 200.html. Static
// and API requests keep their ordinary file-server result, including 404.
const publicationFallback = `
@fuyao_publication_page {
 not path /api /api/* /_AMapService /_AMapService/* /.well-known /.well-known/* /_app /_app/* /assets /assets/* /js /js/* /css /css/* /font /font/* /fonts /fonts/* /images /images/* /wp-content /wp-content/* /media /media/* /posts /posts/* /data /data/* /favicon /favicon/*
 not path_regexp fuyao_publication_static (?i)\.(js|mjs|cjs|css|map|json|xml|txt|html|htm|ico|png|jpe?g|gif|webp|avif|svg|bmp|tiff?|woff2?|ttf|otf|eot|wasm|mp4|webm|mp3|ogg|wav|pdf|zip|gz|br|webmanifest|csv|yaml|yml|lrc|tar|7z|rar|exe|dmg|apk|bin|epub)($|/)
}
route @fuyao_publication_page {
 try_files {path} {path}/index.html {path}.html /200.html
}
`

func maskSettingsSlots(source []byte) []byte {
	view := append([]byte(nil), source...)
	for _, marker := range []string{"__FUYAO_COMPRESSION__", "__FUYAO_CACHE_CONTROL__", "__FUYAO_SECURITY_HEADERS__"} {
		view = bytes.ReplaceAll(view, []byte(marker), bytes.Repeat([]byte(" "), len(marker)))
	}
	return view
}

func simpleStaticBlock(source []byte) bool {
	tokens, err := sourceTokens(source)
	if err != nil {
		return false
	}
	depth, roots, servers := 0, 0, 0
	lineStart := true
	section := ""
	for index, token := range tokens {
		switch token.text {
		case "\n":
			lineStart = true
			continue
		case "{":
			depth++
			lineStart = true
			continue
		case "}":
			depth--
			lineStart = false
			continue
		}
		if !lineStart {
			continue
		}
		lineStart = false
		if depth == 1 {
			section = token.text
			if !oneOf(section, "root", "file_server", "bind", "encode", "header", "tls", "log", "log_skip", "skip_log") {
				return false
			}
			if section == "root" || section == "file_server" {
				end := index + 1
				for end < len(tokens) && !oneOf(tokens[end].text, "\n", "{", "}") {
					end++
				}
				if section == "root" {
					roots++
					if end-index != 2 && (end-index != 3 || tokens[index+1].text != "*") {
						return false
					}
				} else {
					servers++
					if end-index != 1 && (end-index != 2 || tokens[index+1].text != "browse") {
						return false
					}
				}
			}
		} else if depth == 2 && section == "file_server" {
			if !oneOf(token.text, "hide", "index", "precompressed", "browse", "disable_canonical_uris", "etag_file_extensions") {
				return false
			}
		}
	}
	return depth == 0 && roots == 1 && servers == 1
}
