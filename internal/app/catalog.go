package app

import (
	"context"
	"database/sql"

	"github.com/whynpc9/CtxFerry/internal/store"
)

// mutateCatalog is the only way to change shared catalog state. It upgrades a
// legacy published baseline first, then runs fn in one write transaction and,
// when fn reports a shared change, queues that change for sync before commit.
// A mutation therefore cannot change shared records without reaching the outbox.
// fn returns false for local-only and no-op edits, which leave the outbox alone
// and stay possible while a publication is pending.
func (a App) mutateCatalog(ctx context.Context, db *sql.DB, fn func(c *sql.Conn) (shared bool, err error)) error {
	if err := a.prepareBaseline(ctx, db); err != nil {
		return err
	}
	return store.Write(ctx, db, func(c *sql.Conn) error {
		shared, err := fn(c)
		if err != nil || !shared {
			return err
		}
		return queueChanges(ctx, c)
	})
}
