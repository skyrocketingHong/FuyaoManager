package caddymanage

import (
	"bytes"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/publicationmedia"
)

// AttachPublication replaces only complete literal root directive arguments
// selected by the root operator. Other handlers, routes, secrets, snippets,
// matchers and root values remain byte-identical in the preserve rendering.
func AttachPublication(source []byte, primaryRoot string, publication Publication, mediaRoots map[string]string) ([]byte, *Publication, error) {
	if len(publication.Paths) != 0 {
		return nil, nil, ErrPermission
	}
	if !filepath.IsAbs(primaryRoot) || strings.ContainsAny(primaryRoot, "{}\r\n") {
		return nil, nil, ErrUnsupported
	}
	for relative, path := range mediaRoots {
		if (relative != "." && !oneOf(relative, publicationmedia.Paths...)) || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "{}\r\n") {
			return nil, nil, ErrPermission
		}
	}
	tokens, err := sourceTokens(source)
	if err != nil {
		return nil, nil, err
	}
	type replacement struct {
		start, end int
		marker     string
	}
	var replacements []replacement
	primary := false
	matchedMedia := map[string]bool{}
	for index, token := range tokens {
		if token.text != "root" || (index > 0 && !oneOf(tokens[index-1].text, "\n", "{")) {
			continue
		}
		end := index + 1
		for end < len(tokens) && !oneOf(tokens[end].text, "\n", "}") {
			end++
		}
		if end-index != 2 && end-index != 3 {
			return nil, nil, ErrUnsupported
		}
		argument := tokens[end-1]
		relative := ""
		found := argument.text == primaryRoot
		if found {
			primary = true
		} else {
			for media, path := range mediaRoots {
				if argument.text == path {
					if found {
						return nil, nil, ErrPermission
					}
					found = true
					relative = media
					matchedMedia[media] = true
				}
			}
		}
		if !found {
			continue
		}
		marker := "__FUYAO_PUBLISH_PATH_" + strconv.Itoa(len(replacements)) + "__"
		publication.Paths = append(publication.Paths, PublishPath{Marker: marker, Preserve: string(source[argument.start:argument.end]), MediaRelative: relative})
		replacements = append(replacements, replacement{argument.start, argument.end, marker})
	}
	if !primary || len(matchedMedia) != len(mediaRoots) {
		return nil, nil, ErrUnsupported
	}
	if err := validatePublication(&publication); err != nil {
		return nil, nil, err
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	result := append([]byte(nil), source...)
	for _, item := range replacements {
		result = bytes.Join([][]byte{result[:item.start], []byte(item.marker), result[item.end:]}, nil)
	}
	return result, &publication, nil
}
