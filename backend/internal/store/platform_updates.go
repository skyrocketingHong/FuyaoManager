package store

import (
	"context"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// AdmitPlatformUpdate serializes admission with account revocation. External
// deployment results live in the independent supervisor's durable task receipt.
func (s *Store) AdmitPlatformUpdate(ctx context.Context, auth AuthSession, admit func(context.Context) error) error {
	tx, err := s.beginAccountLifecycle(ctx, auth.User.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE id=$1 AND user_id=$2 AND kind=$3 AND expires_at>now() AND revoked_at IS NULL FOR SHARE)`, auth.Session.ID, auth.User.ID, model.SessionKindFull).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrOperatorForbidden
	}
	return admit(ctx)
}
