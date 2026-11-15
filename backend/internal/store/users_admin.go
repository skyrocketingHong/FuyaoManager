// Administrative account scanning: a filtered, paged query over every
// account (the store layer only; the HTTP surface wires the operator's
// admin check on top).

package store

import (
	"context"

	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// UserAdminQuery filters and pages the administrative account list. Deleted
// accounts are excluded unless IncludeDeleted is set (history stays
// queryable through the explicit flag, never by accident).
type UserAdminQuery struct {
	Page           int
	PageSize       int
	PlatformRole   string // empty = any
	Status         string // empty = any
	Search         string // substring of username or display name
	Sort, Order    string
	IncludeDeleted bool
}

// UserAdminPage is one page of the administrative account list.
type UserAdminPage struct {
	Users      []model.User
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// The two list statements are single complete string constants: every
// optional condition is a fixed parameter slot (NULL switches it off), and
// the search term arrives pre-wrapped as a bound LIKE pattern, so nothing is
// ever assembled into the SQL text.
const listAdminCountSQL = `
 SELECT count(*) FROM users
 WHERE ($1::boolean OR status <> 'deleted')
   AND ($2::text IS NULL OR platform_role = $2::text)
   AND ($3::text IS NULL OR status = $3::text)
   AND ($4::text IS NULL OR username ILIKE $4::text
        OR display_name ILIKE $4::text)`

const listAdminPageSQL = `
 SELECT id, username, display_name, webauthn_user_handle, status, platform_role, deleted_at, created_at, updated_at
 FROM users
 WHERE ($1::boolean OR status <> 'deleted')
   AND ($2::text IS NULL OR platform_role = $2::text)
   AND ($3::text IS NULL OR status = $3::text)
   AND ($4::text IS NULL OR username ILIKE $4::text
        OR display_name ILIKE $4::text)
 ORDER BY
 CASE WHEN $7::text = 'username' AND $8::text = 'asc' THEN username END ASC,
 CASE WHEN $7::text = 'username' AND $8::text = 'desc' THEN username END DESC,
 CASE WHEN $7::text = 'displayName' AND $8::text = 'asc' THEN display_name END ASC,
 CASE WHEN $7::text = 'displayName' AND $8::text = 'desc' THEN display_name END DESC,
 CASE WHEN $7::text = 'platformRole' AND $8::text = 'asc' THEN platform_role END ASC,
 CASE WHEN $7::text = 'platformRole' AND $8::text = 'desc' THEN platform_role END DESC,
 CASE WHEN $7::text = 'status' AND $8::text = 'asc' THEN status END ASC,
 CASE WHEN $7::text = 'status' AND $8::text = 'desc' THEN status END DESC,
 CASE WHEN $7::text = 'createdAt' AND $8::text = 'asc' THEN created_at END ASC,
 CASE WHEN ($7::text = 'createdAt' AND $8::text = 'desc') OR $7::text = '' THEN created_at END DESC,
 username ASC, id ASC LIMIT $5 OFFSET $6`

// ListForAdmin returns one filtered, paged page of accounts plus the total
// count. Read-only; the caller's admin status is enforced by the HTTP layer.
func (s *UserStore) ListForAdmin(ctx context.Context, q UserAdminQuery) (UserAdminPage, error) {
	page, size := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	args := []any{q.IncludeDeleted, nilIfEmpty(q.PlatformRole), nilIfEmpty(q.Status), literalPattern(q.Search)}

	var total int
	if err := s.pool.QueryRow(ctx, listAdminCountSQL, args...).Scan(&total); err != nil {
		return UserAdminPage{}, err
	}

	args = append(args, size, (page-1)*size, q.Sort, q.Order)
	rows, err := s.pool.Query(ctx, listAdminPageSQL, args...)
	if err != nil {
		return UserAdminPage{}, err
	}
	defer rows.Close()

	out := UserAdminPage{Users: []model.User{}, Total: total, Page: page, PageSize: size}
	if size > 0 {
		out.TotalPages = (total + size - 1) / size
	}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return UserAdminPage{}, err
		}
		out.Users = append(out.Users, u)
	}
	return out, rows.Err()
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
