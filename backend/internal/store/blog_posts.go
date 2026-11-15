package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

// BlogStore is the database authority for one or more isolated posts scopes.
type BlogStore struct{ pool *pgxpool.Pool }

func NewBlogStore(pool *pgxpool.Pool) *BlogStore { return &BlogStore{pool: pool} }

func blogContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func (s *BlogStore) ensureScope(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO blog_scopes(scope_id) VALUES($1) ON CONFLICT DO NOTHING`, id)
	return err
}

func decodeBlogPost(data []byte, digest string) (content.Post, error) {
	var post content.Post
	if err := json.Unmarshal(data, &post); err != nil {
		return content.Post{}, fmt.Errorf("decode blog record: %w", err)
	}
	post.Digest = digest
	return post, nil
}

func encodeBlogPost(post content.Post) ([]byte, string, error) {
	post.Digest = ""
	data, err := json.Marshal(post)
	if err != nil {
		return nil, "", err
	}
	return data, content.Digest(data), nil
}

func blogConflict(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return content.ErrExists
	}
	return err
}

func canonicalBlogCategories(ctx context.Context, tx pgx.Tx, scope uuid.UUID, post *content.Post) error {
	rows, err := tx.Query(ctx, `SELECT slug,aliases FROM blog_categories WHERE scope_id=$1`, scope)
	if err != nil {
		return err
	}
	type entry struct {
		slug    string
		aliases []string
	}
	var entries []entry
	for rows.Next() {
		var slug string
		var aliases []string
		if err := rows.Scan(&slug, &aliases); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry{slug: slug, aliases: aliases})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	lookup, ambiguous, canonical := map[string]string{}, map[string]bool{}, map[string]bool{}
	for _, value := range entries {
		lookup[value.slug] = value.slug
		canonical[value.slug] = true
	}
	for _, value := range entries {
		for _, alias := range value.aliases {
			if canonical[alias] {
				continue
			}
			if owner, exists := lookup[alias]; exists && owner != value.slug {
				ambiguous[alias] = true
			} else {
				lookup[alias] = value.slug
			}
		}
	}
	if ambiguous[post.Category] {
		return content.ErrConflict
	}
	primary, ok := lookup[post.Category]
	if !ok {
		return content.ErrNotFound
	}
	categories := []string{primary}
	for _, name := range post.Categories {
		if ambiguous[name] {
			return content.ErrConflict
		}
		slug, ok := lookup[name]
		if !ok {
			return content.ErrNotFound
		}
		categories = append(categories, slug)
	}
	post.Category, post.Categories = primary, uniqueStrings(categories)
	return nil
}

func (s *BlogStore) ListPosts(scope uuid.UUID, query string, page, pageSize int, ordering ...content.PostSort) ([]content.Post, int, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return nil, 0, err
	}
	order := content.PostSort{}
	if len(ordering) != 0 {
		order = ordering[0]
	}
	if !content.ValidPostSort(order) {
		return nil, 0, content.ErrInvalidPath
	}
	if order.Field == "" {
		order = content.PostSort{Field: "date", Direction: "desc"}
	}
	rows, err := s.pool.Query(ctx, `SELECT document,digest FROM blog_posts WHERE scope_id=$1`, scope)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	all := []content.Post{}
	q := strings.ToLower(strings.TrimSpace(query))
	for rows.Next() {
		var data []byte
		var digest string
		if err := rows.Scan(&data, &digest); err != nil {
			return nil, 0, err
		}
		post, err := decodeBlogPost(data, digest)
		if err != nil {
			return nil, 0, err
		}
		if q != "" && !strings.Contains(strings.ToLower(post.Title+" "+post.Slug+" "+post.Category+" "+strings.Join(post.Tags, " ")), q) {
			continue
		}
		all = append(all, post)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	posts, total := content.SortPostsPage(all, page, pageSize, order)
	return posts, total, nil
}

// resolveBlogPost accepts stable database IDs and legacy paths for old clients.
// ASVS 1.2.4/8.2.2: bind the identifier and scope as query parameters.
func resolveBlogPost(ctx context.Context, tx pgx.Tx, scope uuid.UUID, key string) (uuid.UUID, []byte, []byte, string, error) {
	var id uuid.UUID
	var data, original []byte
	var digest string
	err := tx.QueryRow(ctx, `SELECT id,document,original,digest FROM blog_posts
		WHERE scope_id=$1 AND (id::text=$2 OR legacy_path=$2) FOR UPDATE`, scope, key).Scan(&id, &data, &original, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil, nil, "", content.ErrNotFound
	}
	return id, data, original, digest, err
}

func (s *BlogStore) GetPost(scope uuid.UUID, key string) (content.Post, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.Post{}, err
	}
	var data []byte
	var digest string
	err := s.pool.QueryRow(ctx, `SELECT document,digest FROM blog_posts WHERE scope_id=$1 AND (id::text=$2 OR legacy_path=$2)`, scope, key).Scan(&data, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Post{}, content.ErrNotFound
	}
	if err != nil {
		return content.Post{}, err
	}
	return decodeBlogPost(data, digest)
}

func (s *BlogStore) CreatePost(scope uuid.UUID, post content.Post) (content.Post, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.Post{}, err
	}
	if err := post.Validate(); err != nil {
		return content.Post{}, err
	}
	if err := post.NormalizePostDates(); err != nil {
		return content.Post{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.Post{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return content.Post{}, err
	}
	if err := canonicalBlogCategories(ctx, tx, scope, &post); err != nil {
		return content.Post{}, err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return content.Post{}, err
	}
	post.ID, post.Path = id.String(), id.String()
	original, err := content.RenderPostWithSource(post, nil)
	if err != nil {
		return content.Post{}, err
	}
	data, digest, err := encodeBlogPost(post)
	if err != nil {
		return content.Post{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO blog_posts(id,scope_id,slug,legacy_path,document,original,digest)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, id, scope, post.Slug, "database:"+id.String(), data, original, digest)
	if err != nil {
		return content.Post{}, blogConflict(err)
	}
	if err := addBlogTags(ctx, tx, scope, post.Tags); err != nil {
		return content.Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.Post{}, err
	}
	post.Digest = digest
	return post, nil
}

func (s *BlogStore) SavePost(scope uuid.UUID, post content.Post, expected string) (content.Post, error) {
	ctx, cancel := blogContext()
	defer cancel()
	if err := post.Validate(); err != nil {
		return content.Post{}, err
	}
	if err := post.NormalizePostDates(); err != nil {
		return content.Post{}, err
	}
	if err := s.ensureScope(ctx, scope); err != nil {
		return content.Post{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return content.Post{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockBlogScope(ctx, tx, scope); err != nil {
		return content.Post{}, err
	}
	id, _, original, digest, err := resolveBlogPost(ctx, tx, scope, post.Path)
	if err != nil {
		return content.Post{}, err
	}
	if expected != "" && digest != expected {
		return content.Post{}, content.ErrConflict
	}
	if err := canonicalBlogCategories(ctx, tx, scope, &post); err != nil {
		return content.Post{}, err
	}
	post.ID, post.Path = id.String(), id.String()
	original, err = content.RenderPostWithSource(post, original)
	if err != nil {
		return content.Post{}, err
	}
	data, nextDigest, err := encodeBlogPost(post)
	if err != nil {
		return content.Post{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE blog_posts SET slug=$1,document=$2,original=$3,digest=$4 WHERE scope_id=$5 AND id=$6`, post.Slug, data, original, nextDigest, scope, id); err != nil {
		return content.Post{}, blogConflict(err)
	}
	if err := addBlogTags(ctx, tx, scope, post.Tags); err != nil {
		return content.Post{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return content.Post{}, err
	}
	post.Digest = nextDigest
	return post, nil
}

func (s *BlogStore) DeletePost(scope uuid.UUID, key, expected string) error {
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
	id, _, _, digest, err := resolveBlogPost(ctx, tx, scope, key)
	if err != nil {
		return err
	}
	if expected != "" && digest != expected {
		return content.ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM blog_posts WHERE scope_id=$1 AND id=$2`, scope, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
