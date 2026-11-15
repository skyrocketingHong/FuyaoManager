package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/contentbind"
)

func TestBlogImportExternalFixture(t *testing.T) {
	src := os.Getenv("FUYAO_TEST_LEGACY_POSTS")
	if src == "" {
		t.Skip("FUYAO_TEST_LEGACY_POSTS is unset")
	}
	w := newWorkspaceTestWorld(t)
	dst := w.library.ContentRoot(content.PlatformScopeID)
	var before []content.Post
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		if rel == "." {
			return nil
		}
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0750)
		}
		if !d.Type().IsRegular() {
			t.Fatal("non-regular fixture entry")
		}
		if e = os.MkdirAll(filepath.Dir(to), 0750); e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		out, e := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
		if e != nil {
			in.Close()
			return e
		}
		_, e = io.Copy(out, in)
		in.Close()
		out.Close()
		if e != nil {
			return e
		}
		if strings.HasPrefix(rel, "posts/") && strings.HasSuffix(strings.ToLower(rel), ".md") && !strings.HasPrefix(d.Name(), "_") {
			raw, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			post, e := content.ParsePost(strings.TrimPrefix(filepath.ToSlash(rel), "posts/"), raw)
			if e != nil {
				return e
			}
			before = append(before, post)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := contentbind.InitializeDatabasePosts(context.Background(), w.env.pool, w.local, w.library, w.env.store.Blog, w.env.store.PostMedia); err != nil {
		t.Fatal(err)
	}
	after, total, err := w.library.ListPosts(content.PlatformScopeID, "", 1, 1000)
	if err != nil || total != len(before) {
		t.Fatalf("counts before=%d after=%d error=%v", len(before), total, err)
	}
	lookup := map[string]content.Post{}
	for _, p := range after {
		lookup[p.Slug] = p
	}
	for _, p := range before {
		q, ok := lookup[p.Slug]
		if !ok || p.Title != q.Title || p.Body != q.Body || p.Date != q.Date || p.Lastmod != q.Lastmod || strings.Join(p.Tags, "\x00") != strings.Join(q.Tags, "\x00") {
			t.Fatalf("record mismatch for a migrated post")
		}
	}
	target := filepath.Join(t.TempDir(), "snapshot")
	if err := w.env.store.Blog.ExportPosts(context.Background(), content.PlatformScopeID, target); err != nil {
		t.Fatal(err)
	}
	for _, p := range before {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(p.Category), p.Slug+".md")); err != nil {
			t.Fatalf("category publication path was not preserved: %v", err)
		}
	}
	if theme := os.Getenv("FUYAO_TEST_THEME_ROOT"); theme != "" {
		original := fixtureThemeIndex(t, theme, filepath.Join(src, "posts"))
		migrated := fixtureThemeIndex(t, theme, target)
		if len(original) != len(migrated) {
			t.Fatalf("published post count changed: %d -> %d", len(original), len(migrated))
		}
		for slug, old := range original {
			next, ok := migrated[slug]
			if !ok {
				t.Fatal("a published slug disappeared after import")
			}
			for _, category := range old {
				if !containsString(next, category) {
					t.Fatal("a published category route disappeared after import")
				}
			}
		}
	}
	t.Logf("verified %d fixture records, including nested categories", total)
}

func fixtureThemeIndex(t *testing.T, theme, posts string) map[string][]string {
	t.Helper()
	temp := t.TempDir()
	config := filepath.Join(temp, "site.json")
	if err := os.WriteFile(config, []byte(`{"site":{"url":"https://fixture.invalid","name":"Fixture"},"seo":{"description":""}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", filepath.Join(theme, "scripts/generate-blog-index.js"))
	cmd.Dir = theme
	cmd.Env = append(os.Environ(), "FUYAO_POSTS_SOURCE_DIR="+posts, "FUYAO_BLOG_OUTPUT_DIR="+filepath.Join(temp, "posts"), "FUYAO_STATIC_OUTPUT_DIR="+temp, "FUYAO_SITE_CONFIG_JSON="+config)
	if _, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("theme index generation failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(temp, "posts/all.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Slug       string   `json:"slug"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	result := make(map[string][]string, len(entries))
	for _, entry := range entries {
		result[entry.Slug] = entry.Categories
	}
	return result
}
