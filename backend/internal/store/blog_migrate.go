package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

type importedPost struct {
	post content.Post
	raw  []byte
	path string
}

func (s *BlogStore) Imported(ctx context.Context, scope uuid.UUID) (bool, error) {
	var initialized bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blog_scopes WHERE scope_id=$1)`, scope).Scan(&initialized)
	return initialized, err
}

// validPublicationPath permits the theme's existing nested category URLs but
// excludes empty segments, traversal and filesystem-specific separators.
// ASVS 5.3.2: validate each segment before constructing snapshot paths.
func validPublicationPath(value string) bool {
	if value == "" {
		return true
	}
	parts := strings.Split(value, "/")
	if len(parts) == 0 || len(parts) > 32 {
		return false
	}
	for _, part := range parts {
		if !content.ValidPostCategory(part) {
			return false
		}
	}
	return true
}

func scanLegacyBlog(root string) ([]string, []string, error) {
	var files, directoryPaths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
				return content.ErrInvalidPath
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("blog import rejects symlink %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return fmt.Errorf("unsupported hidden blog directory: %s", rel)
			}
			if !validPublicationPath(rel) {
				return fmt.Errorf("invalid category path: %s", rel)
			}
			directoryPaths = append(directoryPaths, rel)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("blog import rejects non-regular file %s", path)
		}
		if entry.Name() == "_index.md" || strings.Contains(entry.Name(), ".tmp-") {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				return fmt.Errorf("unsupported hidden post: %s", rel)
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") || strings.HasPrefix(entry.Name(), "_") {
			return fmt.Errorf("unsupported legacy blog resource: %s", rel)
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	sort.Strings(directoryPaths)
	return files, directoryPaths, nil
}

// ImportLegacyScope imports a complete scope in one transaction. Once marked,
// further calls never inspect or re-import the old files.
func (s *BlogStore) ImportLegacyScope(ctx context.Context, legacy *content.Store, scope uuid.UUID) error {
	initialized, err := s.Imported(ctx, scope)
	if err != nil {
		return err
	}
	if initialized {
		return nil
	}
	if legacy == nil || legacy.Posts != nil {
		return errors.New("legacy blog reader is unavailable")
	}
	root := filepath.Join(legacy.ContentRoot(scope), "posts")
	files, directoryPaths, err := scanLegacyBlog(root)
	if err != nil {
		return err
	}

	vocabulary, vocabularyDigest, err := legacy.GetPostVocabulary(scope)
	if err != nil {
		return err
	}
	_, aliasesDigest, err := legacy.ReadFile(scope, content.RootContent, ".post-category-aliases.json")
	if errors.Is(err, content.ErrNotFound) {
		aliasesDigest, err = content.Digest(nil), nil
	}
	if err != nil {
		return err
	}
	legacyAliases, err := legacy.LegacyPostCategoryAliases(scope)
	if err != nil {
		return err
	}

	directories := make([]content.PostDirectory, 0, len(directoryPaths))
	indexFiles := map[string][]byte{}
	for _, path := range directoryPaths {
		raw, _, err := legacy.ReadFile(scope, content.RootContent, "posts/"+path+"/_index.md")
		if errors.Is(err, content.ErrNotFound) {
			raw, err = []byte{}, nil
		}
		if err != nil {
			return err
		}
		title, err := content.PostDirectoryTitle(raw, filepath.Base(path))
		if err != nil {
			return fmt.Errorf("category %s: %w", path, err)
		}
		indexFiles[path] = raw
		directories = append(directories, content.PostDirectory{Slug: path, Title: title})
	}
	const rootCategory = "__root__"
	rootIndex, _, err := legacy.ReadFile(scope, content.RootContent, "posts/_index.md")
	if errors.Is(err, content.ErrNotFound) {
		rootIndex, err = []byte{}, nil
	}
	if err != nil {
		return err
	}
	rootCategoryAdded := false
	addRootCategory := func() error {
		if rootCategoryAdded {
			return nil
		}
		title, err := content.PostDirectoryTitle(rootIndex, "Uncategorized")
		if err != nil {
			return err
		}
		indexFiles[rootCategory] = rootIndex
		directories = append(directories, content.PostDirectory{Slug: rootCategory, Title: title})
		rootCategoryAdded = true
		return nil
	}
	if len(rootIndex) > 0 {
		if err := addRootCategory(); err != nil {
			return err
		}
	}
	items := make([]importedPost, 0, len(files))
	posts := make([]content.Post, 0, len(files))
	slugs := map[string]bool{}
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, _, err := legacy.ReadFile(scope, content.RootContent, "posts/"+path)
		if err != nil {
			return err
		}
		post, err := content.ParsePost(path, raw)
		if err != nil {
			return fmt.Errorf("parse post %s: %w", path, err)
		}
		if !content.ValidPostSlug(post.Slug) {
			return fmt.Errorf("post %s has an invalid slug", path)
		}
		if slugs[post.Slug] {
			return fmt.Errorf("duplicate post slug %q", post.Slug)
		}
		slugs[post.Slug] = true
		if post.Category == "" {
			post.Category = rootCategory
			if err := addRootCategory(); err != nil {
				return err
			}
		}
		posts = append(posts, post)
		items = append(items, importedPost{post: post, raw: raw, path: path})
	}
	taxonomy := content.BuildPostTaxonomy(directories, posts, vocabulary, legacyAliases)
	// Category terms are represented by the unified category records below.
	vocabulary.Categories = []string{}
	categories := make([]blogCategory, 0, len(taxonomy.CategoryGroups))
	used := map[string]bool{}
	for _, group := range taxonomy.CategoryGroups {
		if content.ValidPostCategory(group.Directory) {
			used[group.Directory] = true
		}
	}
	for _, group := range taxonomy.CategoryGroups {
		slug := ""
		if content.ValidPostCategory(group.Directory) {
			slug = group.Directory
		}
		if slug == "" && group.Directory == "" && content.ValidPostCategory(group.Name) && !used[group.Name] {
			slug = group.Name
		}
		if slug == "" {
			sum := sha256.Sum256([]byte(group.Name))
			slug = "category-" + hex.EncodeToString(sum[:8])
			if used[slug] {
				return fmt.Errorf("blog category identity collision")
			}
		}
		used[slug] = true
		publicationPath := group.Directory
		if publicationPath == rootCategory {
			publicationPath = ""
		} else if publicationPath == "" {
			publicationPath = slug
		}
		if !validPublicationPath(publicationPath) {
			return fmt.Errorf("invalid category publication path")
		}
		raw := indexFiles[group.Directory]
		if raw == nil {
			raw = []byte{}
		}
		categories = append(categories, blogCategory{Slug: slug, PublicationPath: publicationPath, Title: group.Title, Aliases: uniqueStrings(append(append([]string{slug, group.Name, publicationPath}, group.Aliases...), group.Title)), Original: raw, Digest: content.Digest(raw)})
	}
	for alias, path := range legacyAliases {
		found := false
		for index := range categories {
			if categories[index].PublicationPath == path {
				categories[index].Aliases = uniqueStrings(append(categories[index].Aliases, alias))
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("legacy category alias references missing category %q", path)
		}
	}
	aliasMap, publicationPaths := map[string]string{}, map[string]string{}
	ambiguous := map[string]bool{}
	for _, cat := range categories {
		publicationPaths[cat.Slug] = cat.PublicationPath
		for _, alias := range cat.Aliases {
			if old, ok := aliasMap[alias]; ok && old != cat.Slug {
				ambiguous[alias] = true
			} else {
				aliasMap[alias] = cat.Slug
			}
		}
		aliasMap[cat.Slug], aliasMap[cat.PublicationPath] = cat.Slug, cat.Slug
	}
	for i := range items {
		item := &items[i]
		primary, ok := aliasMap[item.post.Category]
		if !ok {
			return fmt.Errorf("post %s has no matching category", item.path)
		}
		mapped := []string{primary}
		for _, category := range item.post.Categories {
			if ambiguous[category] {
				return fmt.Errorf("post %s has an ambiguous category", item.path)
			}
			value, ok := aliasMap[category]
			if !ok {
				return fmt.Errorf("post %s has an unknown category", item.path)
			}
			mapped = append(mapped, value)
		}
		item.post.Category, item.post.Categories = primary, uniqueStrings(mapped)
		exported := item.post
		exported.Category = publicationPaths[primary]
		exported.Categories = make([]string, 0, len(item.post.Categories))
		for _, category := range item.post.Categories {
			if value := publicationPaths[category]; value != "" {
				exported.Categories = append(exported.Categories, value)
			}
		}
		if _, err := content.RenderPostWithSource(exported, item.raw); err != nil {
			return fmt.Errorf("post %s cannot be safely exported: %w", item.path, err)
		}
		id := uuid.NewSHA1(scope, []byte("blog-post:"+item.path))
		item.post.ID, item.post.Path = id.String(), id.String()
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "fuyao-blog-import:"+scope.String()); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blog_scopes WHERE scope_id=$1)`, scope).Scan(&initialized); err != nil {
		return err
	}
	if initialized {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO blog_scopes(scope_id) VALUES($1)`, scope); err != nil {
		return err
	}
	vocabData, err := json.Marshal(vocabulary)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO blog_vocabulary(scope_id,document,digest) VALUES($1,$2,$3)`, scope, vocabData, content.Digest(vocabData)); err != nil {
		return err
	}
	for _, cat := range categories {
		if _, err := tx.Exec(ctx, `INSERT INTO blog_categories(scope_id,slug,publication_path,title,aliases,original,digest) VALUES($1,$2,$3,$4,$5,$6,$7)`, scope, cat.Slug, cat.PublicationPath, cat.Title, cat.Aliases, cat.Original, cat.Digest); err != nil {
			return err
		}
	}
	for _, item := range items {
		data, _, err := encodeBlogPost(item.post)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO blog_posts(id,scope_id,slug,legacy_path,document,original,digest) VALUES($1,$2,$3,$4,$5,$6,$7)`, item.post.ID, scope, item.post.Slug, item.path, data, item.raw, content.Digest(item.raw)); err != nil {
			return blogConflict(err)
		}
		_, current, err := legacy.ReadFile(scope, content.RootContent, "posts/"+item.path)
		if err != nil || current != content.Digest(item.raw) {
			return fmt.Errorf("post %s changed during database import", item.path)
		}
	}
	currentFiles, currentDirs, err := scanLegacyBlog(root)
	if err != nil {
		return err
	}
	if !equalStrings(files, currentFiles) || !equalStrings(directoryPaths, currentDirs) {
		return errors.New("blog files changed during database import")
	}
	_, currentVocabulary, err := legacy.GetPostVocabulary(scope)
	if err != nil || currentVocabulary != vocabularyDigest {
		return errors.New("blog vocabulary changed during database import")
	}
	_, currentAliases, err := legacy.ReadFile(scope, content.RootContent, ".post-category-aliases.json")
	if errors.Is(err, content.ErrNotFound) {
		currentAliases, err = content.Digest(nil), nil
	}
	if err != nil || currentAliases != aliasesDigest {
		return errors.New("blog aliases changed during database import")
	}
	for path, original := range indexFiles {
		indexPath := "posts/" + path + "/_index.md"
		if path == rootCategory {
			indexPath = "posts/_index.md"
		}
		_, current, err := legacy.ReadFile(scope, content.RootContent, indexPath)
		if errors.Is(err, content.ErrNotFound) {
			current, err = content.Digest(nil), nil
		}
		if err != nil || current != content.Digest(original) {
			return fmt.Errorf("category %s changed during database import", path)
		}
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM blog_posts WHERE scope_id=$1`, scope).Scan(&count); err != nil {
		return err
	}
	if count != len(items) {
		return errors.New("database blog import count mismatch")
	}
	return tx.Commit(ctx)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ExportPosts writes a consistent database snapshot for a theme build.
func (s *BlogStore) ExportPosts(ctx context.Context, scope uuid.UUID, root string) error {
	if err := s.ensureScope(ctx, scope); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := os.MkdirAll(root, 0750); err != nil {
		return err
	}
	paths := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT slug,publication_path,title,original FROM blog_categories WHERE scope_id=$1 ORDER BY publication_path`, scope)
	if err != nil {
		return err
	}
	for rows.Next() {
		var slug, path, title string
		var original []byte
		if err := rows.Scan(&slug, &path, &title, &original); err != nil {
			rows.Close()
			return err
		}
		if !content.ValidPostCategory(slug) || !validPublicationPath(path) {
			rows.Close()
			return content.ErrInvalidPath
		}
		paths[slug] = path
		if path == "" && len(original) == 0 {
			continue
		}
		data, err := content.RenderPostDirectory(original, title)
		if err != nil {
			rows.Close()
			return err
		}
		dir := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(dir, 0750); err != nil {
			rows.Close()
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "_index.md"), data, 0640); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT document,original,digest FROM blog_posts WHERE scope_id=$1 ORDER BY id`, scope)
	if err != nil {
		return err
	}
	for rows.Next() {
		var data, original []byte
		var digest string
		if err := rows.Scan(&data, &original, &digest); err != nil {
			rows.Close()
			return err
		}
		post, err := decodeBlogPost(data, digest)
		if err != nil {
			rows.Close()
			return err
		}
		path, ok := paths[post.Category]
		if !ok || !content.ValidPostSlug(post.Slug) {
			rows.Close()
			return content.ErrInvalidPath
		}
		post.Category = path
		categories := make([]string, 0, len(post.Categories))
		for _, category := range post.Categories {
			value, ok := paths[category]
			if !ok {
				rows.Close()
				return content.ErrInvalidPath
			}
			if value != "" {
				categories = append(categories, value)
			}
		}
		post.Categories = categories
		rendered, err := content.RenderPostWithSource(post, original)
		if err != nil {
			rows.Close()
			return err
		}
		dir := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(dir, 0750); err != nil {
			rows.Close()
			return err
		}
		file, err := os.OpenFile(filepath.Join(dir, post.Slug+".md"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if err != nil {
			rows.Close()
			return err
		}
		_, writeErr := file.Write(rendered)
		closeErr := file.Close()
		if writeErr != nil {
			rows.Close()
			return writeErr
		}
		if closeErr != nil {
			rows.Close()
			return closeErr
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return tx.Commit(ctx)
}

// VerifyLegacyScope checks staged rows against the source immediately before
// the global cutover marker is committed.
func (s *BlogStore) VerifyLegacyScope(ctx context.Context, legacy *content.Store, scope uuid.UUID) error {
	root := filepath.Join(legacy.ContentRoot(scope), "posts")
	files, dirs, err := scanLegacyBlog(root)
	if err != nil {
		return err
	}
	rows, err := s.pool.Query(ctx, `SELECT legacy_path,original FROM blog_posts WHERE scope_id=$1`, scope)
	if err != nil {
		return err
	}
	originals := map[string][]byte{}
	for rows.Next() {
		var path string
		var raw []byte
		if err := rows.Scan(&path, &raw); err != nil {
			rows.Close()
			return err
		}
		originals[path] = raw
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(originals) != len(files) {
		return errors.New("blog source changed after import")
	}
	for _, path := range files {
		original, ok := originals[path]
		if !ok {
			return errors.New("blog source changed after import")
		}
		_, digest, err := legacy.ReadFile(scope, content.RootContent, "posts/"+path)
		if err != nil || digest != content.Digest(original) {
			return errors.New("blog source changed after import")
		}
	}
	rows, err = s.pool.Query(ctx, `SELECT publication_path,aliases,original FROM blog_categories WHERE scope_id=$1`, scope)
	if err != nil {
		return err
	}
	indexes := map[string][]byte{}
	aliasPaths := map[string]string{}
	for rows.Next() {
		var path string
		var aliases []string
		var raw []byte
		if err := rows.Scan(&path, &aliases, &raw); err != nil {
			rows.Close()
			return err
		}
		indexes[path] = raw
		for _, alias := range aliases {
			aliasPaths[alias] = path
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, dir := range dirs {
		original, ok := indexes[dir]
		if !ok {
			return errors.New("blog categories changed after import")
		}
		_, digest, err := legacy.ReadFile(scope, content.RootContent, "posts/"+dir+"/_index.md")
		if errors.Is(err, content.ErrNotFound) {
			digest, err = content.Digest(nil), nil
		}
		if err != nil || digest != content.Digest(original) {
			return errors.New("blog category changed after import")
		}
	}
	if original, ok := indexes[""]; ok {
		_, digest, err := legacy.ReadFile(scope, content.RootContent, "posts/_index.md")
		if errors.Is(err, content.ErrNotFound) {
			digest, err = content.Digest(nil), nil
		}
		if err != nil || digest != content.Digest(original) {
			return errors.New("root category changed after import")
		}
	}
	legacyVocabulary, _, err := legacy.GetPostVocabulary(scope)
	if err != nil {
		return err
	}
	databaseVocabulary, _, err := s.GetPostVocabulary(scope)
	if err != nil {
		return err
	}
	if !equalStrings(legacyVocabulary.Tags, databaseVocabulary.Tags) {
		return errors.New("blog vocabulary changed after import")
	}
	for _, category := range legacyVocabulary.Categories {
		if _, ok := aliasPaths[category]; !ok {
			return errors.New("blog category vocabulary changed after import")
		}
	}
	aliases, err := legacy.LegacyPostCategoryAliases(scope)
	if err != nil {
		return err
	}
	for name, path := range aliases {
		if aliasPaths[name] != path {
			return errors.New("blog category aliases changed after import")
		}
	}
	return nil
}
