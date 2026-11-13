package caddymanage

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

// ProtectedEntry is shared by operator registration and the management UI.
func ProtectedEntry(site caddyconfig.Site) bool { return protectedEntry(site) }
func protectedEntry(site caddyconfig.Site) bool {
	for _, host := range site.Hosts {
		if strings.EqualFold(host, "manager.fuyaoskyrocket.ing") {
			return true
		}
	}
	return filepath.Clean(site.Root) == "/srv/fuyao-admin/static-page"
}

// A direct imported document can be migrated as one immutable source while
// adding slots only inside its unique static block. Other hosts in that file
// stay byte-identical. Wildcard and parameterized imports are not expanded.
func (e *Engine) importedBlock(master []byte, target caddyconfig.Site) ([]byte, *sourceBlock, int, int, string, error) {
	tokens, err := sourceTokens(master)
	if err != nil {
		return nil, nil, 0, 0, "", ErrUnsupported
	}
	depth := 0
	var selected *sourceBlock
	var document []byte
	var source string
	start, end := 0, 0
	for index, token := range tokens {
		if token.text == "{" {
			depth++
			continue
		}
		if token.text == "}" {
			depth--
			continue
		}
		if depth != 0 || token.text != "import" || index+1 >= len(tokens) {
			continue
		}
		argument := tokens[index+1]
		if index+2 < len(tokens) && tokens[index+2].text != "\n" {
			continue
		}
		path := argument.text
		if !filepath.IsAbs(path) || strings.ContainsAny(path, "*?[]{}\r\n") {
			continue
		}
		data, err := e.trustedFile(path)
		if err != nil {
			continue
		}
		blocks, err := extractBlocks(data)
		if err != nil {
			continue
		}
		for _, block := range blocks {
			if !blockMatches(block, target) {
				continue
			}
			if selected != nil {
				return nil, nil, 0, 0, "", fmt.Errorf("site matches multiple imported blocks: %w", ErrUnsupported)
			}
			// Additional static roots would need their own independently
			// authorized regions; do not expose them through this site's slots.
			otherTokens, err := sourceTokens(append(append([]byte(nil), data[:block.start]...), data[block.end:]...))
			if err != nil {
				return nil, nil, 0, 0, "", err
			}
			for i, other := range otherTokens {
				if other.text == "root" && (i == 0 || oneOf(otherTokens[i-1].text, "\n", "{")) {
					return nil, nil, 0, 0, "", fmt.Errorf("import has another static root outside the registered block: %w", ErrUnsupported)
				}
			}
			copied := block
			selected = &copied
			document, source = data, path
			start = bytes.LastIndexByte(master[:token.start], '\n') + 1
			end = argument.end
			if newline := bytes.IndexByte(master[end:], '\n'); newline >= 0 {
				end += newline + 1
			}
		}
	}
	if selected == nil {
		return nil, nil, 0, 0, "", fmt.Errorf("site has no unique inline or direct-import static block: %w", ErrUnsupported)
	}
	return document, selected, start, end, source, nil
}
