package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

type blogCategory struct {
	Slug, PublicationPath, Title, Digest string
	Aliases                              []string
	Original                             []byte
}

func emptyBlogVocabulary() content.PostVocabulary {
	return content.PostVocabulary{Version: 1, Categories: []string{}, Tags: []string{}}
}

func (s *BlogStore) GetPostVocabulary(scope uuid.UUID) (content.PostVocabulary, string, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.PostVocabulary{}, "", err
	}
	var data []byte
	var digest string
	err := s.pool.QueryRow(ctx, `SELECT document,digest FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&data, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return emptyBlogVocabulary(), content.Digest(nil), nil
	}
	if err != nil {
		return content.PostVocabulary{}, "", err
	}
	var doc content.PostVocabulary
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, "", err
	}
	if err := doc.Validate(); err != nil {
		return doc, "", err
	}
	return doc, digest, nil
}

func lockBlogScope(ctx context.Context, tx pgx.Tx, scope uuid.UUID) error {
	var value uuid.UUID
	return tx.QueryRow(ctx, `SELECT scope_id FROM blog_scopes WHERE scope_id=$1 FOR UPDATE`, scope).Scan(&value)
}

func (s *BlogStore) SavePostVocabulary(scope uuid.UUID, doc content.PostVocabulary, expected string) (content.PostVocabulary, string, error) {
	if err := doc.Validate(); err != nil {
		return content.PostVocabulary{}, "", err
	}
	if len(doc.Categories) != 0 {
		return content.PostVocabulary{}, "", content.ErrInvalidPath
	}
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.PostVocabulary{}, "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.PostVocabulary{}, "", err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return content.PostVocabulary{}, "", err
	}
	current := content.Digest(nil)
	err = tx.QueryRow(ctx, `SELECT digest FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return content.PostVocabulary{}, "", err
	}
	if expected != "" && expected != current {
		return content.PostVocabulary{}, "", content.ErrConflict
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return content.PostVocabulary{}, "", err
	}
	digest := content.Digest(data)
	_, err = tx.Exec(ctx, `INSERT INTO blog_vocabulary(scope_id,document,digest) VALUES($1,$2,$3)
		ON CONFLICT(scope_id) DO UPDATE SET document=EXCLUDED.document,digest=EXCLUDED.digest`, scope, data, digest)
	if err != nil {
		return content.PostVocabulary{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.PostVocabulary{}, "", err
	}
	return doc, digest, nil
}

func blogCategories(ctx context.Context, tx pgx.Tx, scope uuid.UUID) ([]blogCategory, error) {
	rows, err := tx.Query(ctx, `SELECT slug,publication_path,title,aliases,original,digest FROM blog_categories WHERE scope_id=$1 ORDER BY slug`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	categories := []blogCategory{}
	for rows.Next() {
		var cat blogCategory
		if err := rows.Scan(&cat.Slug, &cat.PublicationPath, &cat.Title, &cat.Aliases, &cat.Original, &cat.Digest); err != nil {
			return nil, err
		}
		categories = append(categories, cat)
	}
	return categories, rows.Err()
}

func (s *BlogStore) GetPostDirectory(scope uuid.UUID, slug string) (content.PostDirectoryDocument, string, error) {
	if !content.ValidPostCategory(slug) {
		return content.PostDirectoryDocument{}, "", content.ErrInvalidPath
	}
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	var title, digest string
	err := s.pool.QueryRow(ctx, `SELECT title,digest FROM blog_categories WHERE scope_id=$1 AND slug=$2`, scope, slug).Scan(&title, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		digest = content.Digest(nil)
		return content.PostDirectoryDocument{Slug: slug, Title: slug, Digest: digest}, digest, nil
	}
	if err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	return content.PostDirectoryDocument{Slug: slug, Title: title, Digest: digest}, digest, nil
}

func (s *BlogStore) SavePostDirectory(scope uuid.UUID, slug, title, expected string) (content.PostDirectoryDocument, string, error) {
	if !content.ValidPostCategory(slug) || strings.TrimSpace(title) == "" {
		return content.PostDirectoryDocument{}, "", content.ErrInvalidPath
	}
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	cat := blogCategory{Slug: slug, PublicationPath: slug, Title: slug, Digest: content.Digest(nil), Aliases: []string{slug}}
	err = tx.QueryRow(ctx, `SELECT publication_path,title,aliases,original,digest FROM blog_categories WHERE scope_id=$1 AND slug=$2`, scope, slug).Scan(&cat.PublicationPath, &cat.Title, &cat.Aliases, &cat.Original, &cat.Digest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return content.PostDirectoryDocument{}, "", err
	}
	if expected != "" && cat.Digest != expected {
		return content.PostDirectoryDocument{}, "", content.ErrConflict
	}
	var duplicate bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blog_categories WHERE scope_id=$1 AND slug<>$2 AND
		(slug=$3 OR title=$3 OR $3=ANY(aliases) OR $2=ANY(aliases) OR publication_path=$2))`, scope, slug, title).Scan(&duplicate); err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	if duplicate {
		return content.PostDirectoryDocument{}, "", content.ErrExists
	}
	if cat.Title != title && cat.Title != "" {
		cat.Aliases = append(cat.Aliases, cat.Title)
	}
	cat.Aliases = uniqueStrings(append(cat.Aliases, slug, title))
	data, err := content.RenderPostDirectory(cat.Original, title)
	if err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	digest := content.Digest(data)
	_, err = tx.Exec(ctx, `INSERT INTO blog_categories(scope_id,slug,publication_path,title,aliases,original,digest) VALUES($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT(scope_id,slug) DO UPDATE SET title=EXCLUDED.title,aliases=EXCLUDED.aliases,original=EXCLUDED.original,digest=EXCLUDED.digest`, scope, slug, cat.PublicationPath, title, cat.Aliases, data, digest)
	if err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.PostDirectoryDocument{}, "", err
	}
	return content.PostDirectoryDocument{Slug: slug, Title: title, Digest: digest}, digest, nil
}

func uniqueStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

// addBlogTags runs under the scope lock held by the article transaction.
func addBlogTags(ctx context.Context, tx pgx.Tx, scope uuid.UUID, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	doc := emptyBlogVocabulary()
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT document FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		if err := doc.Validate(); err != nil {
			return err
		}
	}
	merged := uniqueStrings(append(append([]string{}, doc.Tags...), tags...))
	if len(merged) == len(doc.Tags) {
		return nil
	}
	doc.Tags = merged
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO blog_vocabulary(scope_id,document,digest) VALUES($1,$2,$3)
		ON CONFLICT(scope_id) DO UPDATE SET document=EXCLUDED.document,digest=EXCLUDED.digest`, scope, data, content.Digest(data))
	return err
}

func (s *BlogStore) GetPostTaxonomy(scope uuid.UUID) (content.PostTaxonomy, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.PostTaxonomy{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return content.PostTaxonomy{}, err
	}
	defer tx.Rollback(ctx)
	categories, err := blogCategories(ctx, tx, scope)
	if err != nil {
		return content.PostTaxonomy{}, err
	}
	vocab := emptyBlogVocabulary()
	var vocabulary []byte
	err = tx.QueryRow(ctx, `SELECT document FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&vocabulary)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return content.PostTaxonomy{}, err
	}
	if err == nil {
		if err := json.Unmarshal(vocabulary, &vocab); err != nil {
			return content.PostTaxonomy{}, err
		}
		if err := vocab.Validate(); err != nil {
			return content.PostTaxonomy{}, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT document,digest FROM blog_posts WHERE scope_id=$1`, scope)
	if err != nil {
		return content.PostTaxonomy{}, err
	}
	posts := []content.Post{}
	for rows.Next() {
		var data []byte
		var digest string
		if err := rows.Scan(&data, &digest); err != nil {
			rows.Close()
			return content.PostTaxonomy{}, err
		}
		post, err := decodeBlogPost(data, digest)
		if err != nil {
			rows.Close()
			return content.PostTaxonomy{}, err
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return content.PostTaxonomy{}, err
	}
	rows.Close()
	tax := content.PostTaxonomy{Directories: []content.PostDirectory{}, Categories: []content.TaxonomyTerm{}, CategoryGroups: []content.PostCategoryGroup{}, Tags: []content.TaxonomyTerm{}, PostCount: len(posts)}
	catCounts, tagCounts := map[string]int{}, map[string]int{}
	aliases := map[string]string{}
	for _, cat := range categories {
		aliases[cat.Slug] = cat.Slug
		for _, alias := range cat.Aliases {
			if _, exists := aliases[alias]; !exists {
				aliases[alias] = cat.Slug
			}
		}
	}
	for _, post := range posts {
		seen := map[string]bool{}
		for _, cat := range append(append([]string{}, post.Categories...), post.Category) {
			if canonical, ok := aliases[cat]; ok {
				seen[canonical] = true
			}
		}
		for cat := range seen {
			catCounts[cat]++
		}
		for _, tag := range uniqueStrings(post.Tags) {
			tagCounts[tag]++
		}
	}
	for _, cat := range categories {
		count := catCounts[cat.Slug]
		tax.Directories = append(tax.Directories, content.PostDirectory{Slug: cat.Slug, Title: cat.Title, PostCount: count})
		tax.Categories = append(tax.Categories, content.TaxonomyTerm{Name: cat.Slug, PostCount: count})
		tax.CategoryGroups = append(tax.CategoryGroups, content.PostCategoryGroup{Name: cat.Slug, Title: cat.Title, Directory: cat.Slug, Aliases: uniqueStrings(append([]string{cat.Slug}, cat.Aliases...)), PostCount: count})
	}
	for _, tag := range vocab.Tags {
		if _, ok := tagCounts[tag]; !ok {
			tagCounts[tag] = 0
		}
	}
	names := make([]string, 0, len(tagCounts))
	for name := range tagCounts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		tax.Tags = append(tax.Tags, content.TaxonomyTerm{Name: name, PostCount: tagCounts[name]})
	}
	sort.Slice(tax.CategoryGroups, func(i, j int) bool {
		if tax.CategoryGroups[i].Title != tax.CategoryGroups[j].Title {
			return tax.CategoryGroups[i].Title < tax.CategoryGroups[j].Title
		}
		return tax.CategoryGroups[i].Name < tax.CategoryGroups[j].Name
	})
	if err := tx.Commit(ctx); err != nil {
		return content.PostTaxonomy{}, err
	}
	return tax, nil
}

// RenamePostTag updates vocabulary and all article usages in one transaction.
// ASVS 2.3.3/15.4.2: the scope lock makes the operation atomic with post writes.
// It does not revalidate or reserialize unrelated historical frontmatter.
func (s *BlogStore) RenamePostTag(scope uuid.UUID, from string, to *string, expected string) (int, content.PostVocabulary, string, error) {
	if strings.TrimSpace(from) == "" || (to != nil && strings.TrimSpace(*to) == "") {
		return 0, content.PostVocabulary{}, "", content.ErrInvalidPath
	}
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return 0, content.PostVocabulary{}, "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, content.PostVocabulary{}, "", err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return 0, content.PostVocabulary{}, "", err
	}
	doc := emptyBlogVocabulary()
	current := content.Digest(nil)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT document,digest FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&raw, &current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, doc, "", err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return 0, doc, "", err
		}
		if err := doc.Validate(); err != nil {
			return 0, doc, "", err
		}
	}
	if expected != "" && expected != current {
		return 0, doc, "", content.ErrConflict
	}
	type candidate struct {
		id       uuid.UUID
		post     content.Post
		original []byte
	}
	var changed []candidate
	found, targetExists := false, false
	for _, tag := range doc.Tags {
		if tag == from {
			found = true
		}
		if to != nil && tag == *to && *to != from {
			targetExists = true
		}
	}
	rows, err := tx.Query(ctx, `SELECT id,document,original,digest FROM blog_posts WHERE scope_id=$1 ORDER BY id FOR UPDATE`, scope)
	if err != nil {
		return 0, doc, "", err
	}
	for rows.Next() {
		var id uuid.UUID
		var data, original []byte
		var digest string
		if err := rows.Scan(&id, &data, &original, &digest); err != nil {
			rows.Close()
			return 0, doc, "", err
		}
		post, err := decodeBlogPost(data, digest)
		if err != nil {
			rows.Close()
			return 0, doc, "", err
		}
		uses := false
		for _, tag := range post.Tags {
			if tag == from {
				uses = true
				found = true
			}
			if to != nil && tag == *to && *to != from {
				targetExists = true
			}
		}
		if uses {
			changed = append(changed, candidate{id: id, post: post, original: original})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, doc, "", err
	}
	rows.Close()
	if !found {
		return 0, doc, "", content.ErrNotFound
	}
	if targetExists {
		return 0, doc, "", content.ErrExists
	}
	if to != nil && *to == from {
		return 0, doc, current, nil
	}
	for _, item := range changed {
		tags := make([]string, 0, len(item.post.Tags))
		for _, tag := range item.post.Tags {
			if tag == from {
				if to != nil {
					tags = append(tags, *to)
				}
			} else {
				tags = append(tags, tag)
			}
		}
		item.post.Tags = uniqueStrings(tags)
		item.post.Lastmod = time.Now().UTC().Format(time.RFC3339)
		original, err := content.RenderPostWithSource(item.post, item.original)
		if err != nil {
			return 0, doc, "", err
		}
		data, digest, err := encodeBlogPost(item.post)
		if err != nil {
			return 0, doc, "", err
		}
		if _, err := tx.Exec(ctx, `UPDATE blog_posts SET document=$1,original=$2,digest=$3 WHERE scope_id=$4 AND id=$5`, data, original, digest, scope, item.id); err != nil {
			return 0, doc, "", err
		}
	}
	tags := make([]string, 0, len(doc.Tags)+1)
	for _, tag := range doc.Tags {
		if tag != from {
			tags = append(tags, tag)
		}
	}
	if to != nil {
		tags = append(tags, *to)
	}
	doc.Tags = uniqueStrings(tags)
	data, err := json.Marshal(doc)
	if err != nil {
		return 0, doc, "", err
	}
	digest := content.Digest(data)
	_, err = tx.Exec(ctx, `INSERT INTO blog_vocabulary(scope_id,document,digest) VALUES($1,$2,$3)
		ON CONFLICT(scope_id) DO UPDATE SET document=EXCLUDED.document,digest=EXCLUDED.digest`, scope, data, digest)
	if err != nil {
		return 0, doc, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, doc, "", err
	}
	return len(changed), doc, digest, nil
}

// DeletePostCategory removes only an unused category and its unshared legacy names.
func (s *BlogStore) DeletePostCategory(scope uuid.UUID, slug, expected string) error {
	if !content.ValidPostCategory(slug) {
		return content.ErrInvalidPath
	}
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return err
	}
	var digest string
	var aliases []string
	err = tx.QueryRow(ctx, `SELECT digest,aliases FROM blog_categories WHERE scope_id=$1 AND slug=$2`, scope, slug).Scan(&digest, &aliases)
	if errors.Is(err, pgx.ErrNoRows) {
		return content.ErrNotFound
	}
	if err != nil {
		return err
	}
	if expected != "" && digest != expected {
		return content.ErrConflict
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blog_posts WHERE scope_id=$1 AND (document->>'category'=$2 OR (document->'categories') ? $2))`, scope, slug).Scan(&used); err != nil {
		return err
	}
	if used {
		return content.ErrInUse
	}
	other, err := blogCategories(ctx, tx, scope)
	if err != nil {
		return err
	}
	shared := map[string]bool{}
	for _, cat := range other {
		if cat.Slug == slug {
			continue
		}
		shared[cat.Slug] = true
		for _, alias := range cat.Aliases {
			shared[alias] = true
		}
	}
	removed := map[string]bool{slug: true}
	for _, alias := range aliases {
		if !shared[alias] {
			removed[alias] = true
		}
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT document FROM blog_vocabulary WHERE scope_id=$1`, scope).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		var doc content.PostVocabulary
		if err := json.Unmarshal(raw, &doc); err != nil {
			return err
		}
		if err := doc.Validate(); err != nil {
			return err
		}
		names := make([]string, 0, len(doc.Categories))
		for _, name := range doc.Categories {
			if !removed[name] {
				names = append(names, name)
			}
		}
		if len(names) != len(doc.Categories) {
			doc.Categories = names
			data, err := json.Marshal(doc)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE blog_vocabulary SET document=$1,digest=$2 WHERE scope_id=$3`, data, content.Digest(data), scope); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM blog_categories WHERE scope_id=$1 AND slug=$2`, scope, slug); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
