package content

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

func legacyFootprintsStore(t *testing.T) (*Store, uuid.UUID, []byte) {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	raw, err := os.ReadFile("testdata/footprints-legacy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.ContentRoot(id), "data", "footprints.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return store, id, raw
}
func footprintYAMLValue(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var result map[string]any
	if err := yaml.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestLegacyFootprintsProjectsStringNamesAndPlaceTypesWithoutRewriting(t *testing.T) {
	store, id, original := legacyFootprintsStore(t)
	doc, digest, err := store.GetFootprints(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Cities) != 2 || doc.Cities[0].Name != (FootprintName{Zh: "Legacy City", En: "Legacy City"}) || doc.Cities[0].Places[0].Type != "spot" || doc.Cities[0].Places[0].Name.Zh != "Legacy Place" {
		t.Fatal("theme-compatible legacy name/type projection failed")
	}
	if err := doc.Validate(); err != nil {
		t.Fatalf("valid legacy document cannot be edited: %v", err)
	}
	if _, savedDigest, err := store.SaveFootprints(id, doc, digest); err != nil || savedDigest != digest {
		t.Fatalf("unchanged save altered the product: %v", err)
	}
	current, _, err := store.ReadFile(id, RootContent, "data/footprints.yaml")
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("an unchanged save rewrote legacy source bytes")
	}
}

func TestLegacyFootprintsSaveKeepsUnknownGeographyByIdentity(t *testing.T) {
	store, id, original := legacyFootprintsStore(t)
	doc, digest, err := store.GetFootprints(id)
	if err != nil {
		t.Fatal(err)
	}
	// Reordering must carry metadata with its stable ID, rather than with
	// its former array index. Only this requested description changes.
	doc.Cities[0].Description = "Updated description"
	doc.Cities[0].Places[0], doc.Cities[0].Places[1] = doc.Cities[0].Places[1], doc.Cities[0].Places[0]
	doc.Cities[0], doc.Cities[1] = doc.Cities[1], doc.Cities[0]
	_, nextDigest, err := store.SaveFootprints(id, doc, digest)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := store.ReadFile(id, RootContent, "data/footprints.yaml")
	if err != nil {
		t.Fatal(err)
	}
	expected := footprintYAMLValue(t, original)
	cities := expected["cities"].([]any)
	legacy := cities[0].(map[string]any)
	legacy["description"] = "Updated description"
	places := legacy["places"].([]any)
	places[0], places[1] = places[1], places[0]
	cities[0], cities[1] = cities[1], cities[0]
	if !reflect.DeepEqual(expected, footprintYAMLValue(t, raw)) {
		t.Fatal("save discarded or reassigned unmodeled geographic data or legacy name/type nodes")
	}
	if _, _, err := store.SaveFootprints(id, doc, digest); !errors.Is(err, ErrConflict) {
		t.Fatal("stale save bypassed the original digest")
	}
	reread, readDigest, err := store.GetFootprints(id)
	if err != nil || readDigest != nextDigest || reread.Cities[1].Places[1].Type != "spot" {
		t.Fatal("saved legacy document failed its canonical read projection")
	}
}

func TestLegacyFootprintsChangedNameKeepsOtherLocalesAndGeographicMetadata(t *testing.T) {
	store, id, original := legacyFootprintsStore(t)
	doc, digest, err := store.GetFootprints(id)
	if err != nil {
		t.Fatal(err)
	}
	doc.Cities[1].Name.Zh = "更新城市"
	if _, _, err := store.SaveFootprints(id, doc, digest); err != nil {
		t.Fatal(err)
	}
	raw, _, err := store.ReadFile(id, RootContent, "data/footprints.yaml")
	if err != nil {
		t.Fatal(err)
	}
	expected := footprintYAMLValue(t, original)
	expected["cities"].([]any)[1].(map[string]any)["name"].(map[string]any)["zh"] = "更新城市"
	if !reflect.DeepEqual(expected, footprintYAMLValue(t, raw)) {
		t.Fatal("editing one locale discarded unrelated locales or geographic fields")
	}
}
