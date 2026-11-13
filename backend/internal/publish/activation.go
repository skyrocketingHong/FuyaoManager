package publish

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/skyrocketingHong/FuyaoManager/backend/internal/model"
)

// ActivationBridge joins the serving capability to the database transaction.
// A nil session means a non-Caddy site should use the existing edge flow.
type ActivationBridge interface {
	Begin(context.Context, uuid.UUID, uuid.UUID) (ActivationSession, error)
}
type ActivationSession interface {
	Tx() pgx.Tx
	Activate(context.Context, model.Release) error
	Commit(context.Context) error
	Abort(context.Context) error
}
