package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

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

// live is the catalog visibility rule as a SQL predicate: a row is visible only
// while none of the named record ID columns has a sync tombstone. Columns must
// be qualified, because an unqualified id would bind to the tombstone itself.
func live(columns ...string) string {
	return "NOT EXISTS (SELECT 1 FROM sync_tombstones t WHERE t.id IN (" + strings.Join(columns, ",") + "))"
}

// openProjectReadOnly opens the catalog without creating or migrating it and
// selects a live Project this device is subscribed to.
func (a App) openProjectReadOnly(ctx context.Context, selector string) (*sql.DB, Project, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, Project{}, err
	}
	p, err := selectProject(ctx, db, selector)
	if err == nil {
		err = requireSubscription(ctx, db, p.ID)
	}
	if err != nil {
		db.Close()
		return nil, Project{}, err
	}
	return db, p, nil
}

// ReadProjects lists live Projects, ordered by slug, without creating or
// migrating the catalog. all includes Projects this device unsubscribed from.
func (a App) ReadProjects(ctx context.Context, all bool) ([]Project, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	r, err := listProjects(ctx, db, all)
	return r.Projects, err
}

// CatalogState says why the local catalog could or could not be opened, so
// every front end offers the same recovery for the same failure.
type CatalogState string

const (
	CatalogReady             CatalogState = "ready"
	CatalogUninitialized     CatalogState = "uninitialized"
	CatalogMigrationRequired CatalogState = "migration-required"
	CatalogUnsupportedSchema CatalogState = "unsupported-schema"
	CatalogUnavailable       CatalogState = "unavailable"
)

// ClassifyCatalog maps an error from opening or reading the catalog to its state.
func ClassifyCatalog(err error) CatalogState {
	switch {
	case err == nil:
		return CatalogReady
	case errors.Is(err, store.ErrNotInitialized):
		return CatalogUninitialized
	case errors.Is(err, store.ErrMigrationRequired):
		return CatalogMigrationRequired
	case errors.Is(err, store.ErrUnsupportedSchema):
		return CatalogUnsupportedSchema
	default:
		return CatalogUnavailable
	}
}
