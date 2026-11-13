package caddyconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func assignIDs(items []candidate) {
	groups := map[string][]int{}
	for index := range items {
		scope := items[index].matchScope
		if scope == nil {
			scope = []json.RawMessage{}
		}
		identity := struct {
			Listen []string
			Hosts  []string
			Match  []json.RawMessage
		}{items[index].site.Listen, items[index].site.Hosts, scope}
		encoded, _ := json.Marshal(identity)
		id := "caddy-" + hash(encoded)
		items[index].site.ID = id
		groups[id] = append(groups[id], index)
	}
	for id, indices := range groups {
		if len(indices) < 2 {
			continue
		}
		sort.Slice(indices, func(i, j int) bool { return items[indices[i]].contentDigest < items[indices[j]].contentDigest })
		counts := map[string]int{}
		for _, index := range indices {
			item := &items[index].site
			counts[items[index].contentDigest]++
			item.ID = id + "-" + items[index].contentDigest[:16] + "-" + strconv.Itoa(counts[items[index].contentDigest])
			item.DuplicateCount = len(indices)
			item.ReadOnly = true
			if item.Reason != "" {
				item.Reason += ","
			}
			item.Reason += "duplicate_match_boundary"
		}
	}
}

func canonicalDigest(data []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", ErrInvalid
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalid
	}
	return hash(encoded), nil
}
func hash(data []byte) string     { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func pointer(value string) string { return strings.NewReplacer("~", "~0", "/", "~1").Replace(value) }
func stringField(fields map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(fields[key], &value)
	return value
}
func onlyKeys(fields map[string]json.RawMessage, keys ...string) bool {
	for key := range fields {
		if !allowed(key, keys...) {
			return false
		}
	}
	return true
}
func allowed(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
func safeText(value string, max int) bool {
	if len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}
func normalized(values []string, lower bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if lower {
			value = strings.ToLower(value)
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
func isEmpty(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) || bytes.Equal(trimmed, []byte("[]"))
}
