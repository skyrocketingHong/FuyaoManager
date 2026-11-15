package contentbind

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/content"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/store"
)

// InitializeDatabasePosts imports each existing scope before the API starts.
// A completed scope is never read from its legacy posts tree again.
func InitializeDatabasePosts(ctx context.Context, pool *pgxpool.Pool, local, platform *content.Store, blog *store.BlogStore, media *store.PostMediaStore) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('fuyao-blog-global-migration',0))`); err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.Hijack().Close(closeCtx)
		return err
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended('fuyao-blog-global-migration',0))`); err != nil {
			_ = conn.Hijack().Close(unlockCtx)
		} else {
			conn.Release()
		}
	}()
	var complete bool
	if err := conn.QueryRow(ctx, `SELECT complete FROM blog_migration_state WHERE singleton=true`).Scan(&complete); err != nil {
		return err
	}
	if complete {
		local.Posts = blog
		platform.Posts = blog
		return nil
	}
	// Incomplete rows are staging data from a previous failed startup. No
	// server is admitted until the global completion marker is set.
	if _, err := conn.Exec(ctx, `DELETE FROM blog_scopes`); err != nil {
		return err
	}
	rows, err := pool.Query(ctx, `SELECT id,kind FROM content_scopes ORDER BY kind,id`)
	if err != nil {
		return err
	}
	type scopeRecord struct {
		id   uuid.UUID
		kind string
	}
	var scopes []scopeRecord
	for rows.Next() {
		var scope scopeRecord
		if err := rows.Scan(&scope.id, &scope.kind); err != nil {
			rows.Close()
			return err
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, scope := range scopes {
		imported, err := blog.Imported(ctx, scope.id)
		if err != nil {
			return err
		}
		if imported {
			continue
		}
		source := local
		if scope.kind == "platform" {
			source = platform
		}
		if source == nil {
			return fmt.Errorf("blog source unavailable for scope %s", scope.id)
		}
		if err := SyncPostMedia(ctx, source, media, scope.id); err != nil {
			return fmt.Errorf("import blog media for scope %s: %w", scope.id, err)
		}
		if err := VerifyLegacyPostMedia(ctx, source, media, scope.id); err != nil {
			return fmt.Errorf("verify blog media for scope %s: %w", scope.id, err)
		}
		if err := blog.ImportLegacyScope(ctx, source, scope.id); err != nil {
			return fmt.Errorf("import blog for scope %s: %w", scope.id, err)
		}
	}
	for _, scope := range scopes {
		source := local
		if scope.kind == "platform" {
			source = platform
		}
		if err := blog.VerifyLegacyScope(ctx, source, scope.id); err != nil {
			return fmt.Errorf("verify blog for scope %s: %w", scope.id, err)
		}
		if err := VerifyLegacyPostMedia(ctx, source, media, scope.id); err != nil {
			return fmt.Errorf("verify blog media for scope %s: %w", scope.id, err)
		}
	}
	var missing bool
	if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM content_scopes c LEFT JOIN blog_scopes b ON b.scope_id=c.id WHERE b.scope_id IS NULL)`).Scan(&missing); err != nil {
		return err
	}
	if missing {
		return fmt.Errorf("content scopes changed during blog import")
	}
	if _, err := conn.Exec(ctx, `UPDATE blog_migration_state SET complete=true WHERE singleton=true`); err != nil {
		return err
	}
	local.Posts = blog
	platform.Posts = blog
	return nil
}
