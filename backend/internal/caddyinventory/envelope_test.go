package caddyinventory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/caddyconfig"
)

const sourceFixture = `{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[{"match":[{"host":["static.example"]}],"handle":[{"handler":"rewrite","uri":"/asset?token=private-query-credential"},{"handler":"headers","request":{"set":{"Authorization":["private-auth-credential"]}}},{"handler":"vars","root":"/srv/site"},{"handler":"file_server"}]}]}}}}}`

func safeFixture(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := WriteSafe(strings.NewReader(sourceFixture), &output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestSafeEnvelopePreservesInventoryAndRedactsPrivilegedSource(t *testing.T) {
	output := safeFixture(t)
	for _, secret := range []string{"private-query-credential", "private-auth-credential", "Authorization", "rewrite", "upstreams"} {
		if bytes.Contains(output, []byte(secret)) {
			t.Fatal("privileged source or credential escaped the inventory helper")
		}
	}
	var envelope Envelope
	if err := json.Unmarshal(output, &envelope); err != nil || envelope.Version != Version || len(envelope.Sites) != 1 {
		t.Fatalf("invalid safe protocol: %v", err)
	}
	rawSites, err := caddyconfig.Parse([]byte(sourceFixture))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseOutput(output)
	if err != nil || !reflect.DeepEqual(decoded, rawSites) {
		t.Fatalf("safe inventory changed identity or public site fields: %v", err)
	}
	legacy, err := ParseOutput([]byte(sourceFixture))
	if err != nil || !reflect.DeepEqual(legacy, decoded) {
		t.Fatalf("fixed legacy adapter output compatibility changed: %v", err)
	}
}

func TestSafeEnvelopeRejectsMalformedVersionFieldsAndSites(t *testing.T) {
	valid := safeFixture(t)
	var envelope map[string]any
	if err := json.Unmarshal(valid, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"future-version", func(v map[string]any) { v["fuyaoInventoryVersion"] = 2 }},
		{"string-version", func(v map[string]any) { v["fuyaoInventoryVersion"] = "1" }},
		{"unknown-envelope-field", func(v map[string]any) { v["source"] = sourceFixture }},
		{"null-sites", func(v map[string]any) { v["sites"] = nil }},
		{"missing-sites", func(v map[string]any) { delete(v, "sites") }},
		{"unknown-site-field", func(v map[string]any) { v["sites"].([]any)[0].(map[string]any)["command"] = "arbitrary" }},
		{"site-key-traversal", func(v map[string]any) { v["sites"].([]any)[0].(map[string]any)["id"] = "../../etc/caddy/Caddyfile" }},
		{"unknown-site-kind", func(v map[string]any) { v["sites"].([]any)[0].(map[string]any)["kind"] = "shell" }},
		{"invalid-digest", func(v map[string]any) { v["sites"].([]any)[0].(map[string]any)["digest"] = "wrong" }},
		{"control-character", func(v map[string]any) { v["sites"].([]any)[0].(map[string]any)["root"] = "/srv/site\nimport secret" }},
		{"duplicate-site-id", func(v map[string]any) { sites := v["sites"].([]any); v["sites"] = append(sites, sites[0]) }},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(valid, &value); err != nil {
				t.Fatal(err)
			}
			sample.edit(value)
			data, _ := json.Marshal(value)
			if _, err := ParseOutput(data); err == nil {
				t.Fatal("unsafe inventory envelope was accepted")
			}
		})
	}
	for _, data := range [][]byte{
		[]byte(`{"fuyaoInventoryVersion":1,"fuyaoInventoryVersion":1,"sites":[]}`),
		[]byte(`{"fuyaoInventoryVersion":1,"sites":[]} {}`),
		[]byte(`{"fuyaoInventoryVersion":1,"sites":[],"sites":[]}`),
		bytes.Replace(valid, []byte(`"kind":"static"`), []byte(`"kind":"static","kind":"static"`), 1),
	} {
		if _, err := ParseOutput(data); err == nil {
			t.Fatal("duplicate keys or trailing inventory document was accepted")
		}
	}
	if sites, err := ParseOutput([]byte(`{"fuyaoInventoryVersion":1,"sites":[]}`)); err != nil || len(sites) != 0 {
		t.Fatalf("empty but valid inventory rejected: %v", err)
	}
}

func TestSafeEnvelopeBoundsAndNoPartialOutput(t *testing.T) {
	for _, source := range []string{"not JSON with private-query-credential", sourceFixture + " {}", strings.Repeat(" ", caddyconfig.MaxConfigBytes+1)} {
		var output bytes.Buffer
		if err := WriteSafe(strings.NewReader(source), &output); err == nil || output.Len() != 0 {
			t.Fatal("failed source parsing emitted partial privileged data")
		}
	}
	if _, err := ParseOutput(bytes.Repeat([]byte{' '}, caddyconfig.MaxConfigBytes+1)); !errors.Is(err, caddyconfig.ErrTooLarge) {
		t.Fatal("oversized capability output was accepted")
	}
	data := safeFixture(t)
	var value Envelope
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	value.Sites = make([]caddyconfig.Site, maxSites+1)
	data, _ = json.Marshal(value)
	if _, err := ParseOutput(data); err == nil {
		t.Fatal("excessive inventory site count was accepted")
	}
}

func TestReadFixedCommandAcceptsSafeAndLegacyProtocols(t *testing.T) {
	for name, output := range map[string][]byte{"safe": safeFixture(t), "legacy": []byte(sourceFixture), "invalid": []byte(`{"fuyaoInventoryVersion":2,"sites":[]}`)} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "output.json")
			if err := os.WriteFile(path, output, 0600); err != nil {
				t.Fatal(err)
			}
			sites, err := Read(context.Background(), []string{"/bin/cat", path})
			if name == "invalid" {
				if err == nil {
					t.Fatal("fixed capability unknown protocol was accepted")
				}
			} else if err != nil || len(sites) != 1 {
				t.Fatalf("fixed %s command: count=%d err=%v", name, len(sites), err)
			}
		})
	}
	if _, err := Read(context.Background(), []string{"/bin/sh", "-c", "exit 1"}); err == nil {
		t.Fatal("failed fixed read command was accepted")
	}
}
