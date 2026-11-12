package buildrunner

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedFaviconRoot covers the production favicon seeding contract: an
// empty site favicon directory receives the theme's default set byte for
// byte, a populated directory is never touched, and a theme without a
// static/favicon directory seeds nothing.
func TestSeedFaviconRoot(t *testing.T) {
	seed := t.TempDir()
	if err := os.WriteFile(filepath.Join(seed, "favicon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "favicon.ico"), []byte("ico-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("seeds empty target", func(t *testing.T) {
		target := t.TempDir()
		if err := seedFaviconRoot(target, seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		for _, name := range []string{"favicon.svg", "favicon.ico"} {
			data, err := os.ReadFile(filepath.Join(target, name))
			if err != nil {
				t.Fatalf("seeded %s missing: %v", name, err)
			}
			want, _ := os.ReadFile(filepath.Join(seed, name))
			if string(data) != string(want) {
				t.Fatalf("seeded %s differs from the theme default", name)
			}
		}
	})

	t.Run("populated target untouched", func(t *testing.T) {
		target := t.TempDir()
		if err := os.WriteFile(filepath.Join(target, "custom.svg"), []byte("custom"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := seedFaviconRoot(target, seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "custom.svg" {
			t.Fatalf("populated favicon dir was modified: %v", entries)
		}
	})

	t.Run("missing seed directory is a no-op", func(t *testing.T) {
		target := t.TempDir()
		if err := seedFaviconRoot(target, filepath.Join(t.TempDir(), "absent")); err != nil {
			t.Fatalf("absent seed must not fail: %v", err)
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("nothing should be seeded: %v", entries)
		}
	})

	t.Run("creates missing target directory", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "nested", "favicon")
		if err := seedFaviconRoot(target, seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "favicon.svg")); err != nil {
			t.Fatalf("target not created: %v", err)
		}
	})
}
