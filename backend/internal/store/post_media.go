package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
)

type PostMediaStore struct{ pool *pgxpool.Pool }

// Put is idempotent for identical bytes and never overwrites a different image.
func (s *PostMediaStore) Put(ctx context.Context, scope uuid.UUID, path string, data []byte) error {
	kind, err := content.ValidatePostMedia(path, data)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	tag, err := s.pool.Exec(ctx, `INSERT INTO post_media(scope_id,path,mime_type,content,digest)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(scope_id,path) DO NOTHING`, scope, path, kind, data, digest[:])
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	var saved, savedDigest []byte
	if err := s.pool.QueryRow(ctx, `SELECT content,digest FROM post_media WHERE scope_id=$1 AND path=$2`, scope, path).Scan(&saved, &savedDigest); err != nil {
		return err
	}
	if !bytes.Equal(savedDigest, digest[:]) || !bytes.Equal(saved, data) {
		return content.ErrExists
	}
	return nil
}

func (s *PostMediaStore) Get(ctx context.Context, scope uuid.UUID, path string) ([]byte, string, error) {
	var data []byte
	var kind string
	err := s.pool.QueryRow(ctx, `SELECT content,mime_type FROM post_media WHERE scope_id=$1 AND path=$2`, scope, path).Scan(&data, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", content.ErrNotFound
	}
	return data, kind, err
}

func (s *PostMediaStore) List(ctx context.Context, scope uuid.UUID) ([]content.PostMedia, error) {
	rows, err := s.pool.Query(ctx, `SELECT path,octet_length(content) FROM post_media WHERE scope_id=$1 ORDER BY path`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []content.PostMedia{}
	for rows.Next() {
		var item content.PostMedia
		if err := rows.Scan(&item.Path, &item.Size); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Visit uses a single database snapshot and hands one image at a time to the build.
func (s *PostMediaStore) Visit(ctx context.Context, scope uuid.UUID, visit func(string, []byte) error) error {
	rows, err := s.pool.Query(ctx, `SELECT path,content FROM post_media WHERE scope_id=$1 ORDER BY path`, scope)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		var data []byte
		if err := rows.Scan(&path, &data); err != nil {
			return err
		}
		if err := visit(path, data); err != nil {
			return err
		}
	}
	return rows.Err()
}
