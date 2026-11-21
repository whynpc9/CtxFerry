package app

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/store"
)

func TestMutateCatalogQueuesSharedChanges(t *testing.T) {
	ctx := t.Context()
	a := App{DataDir: filepath.Join(t.TempDir(), "data")}
	init, err := a.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		"INSERT INTO sync_state(singleton,epoch) VALUES(1,'00000000-0000-4000-8000-000000000000')",
		"INSERT INTO sync_config VALUES (1,'https://github.com/a/b.git','" + init.Workspace.ID + "','main','identity')",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	bound, err := a.BindProject(ctx, t.TempDir(), "queued")
	if err != nil {
		t.Fatal(err)
	}
	var queued bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sync_outbox WHERE id=?)", bound.Project.ID).Scan(&queued); err != nil || !queued {
		t.Fatalf("shared Project write did not reach the outbox: %v", err)
	}

	if _, err := db.ExecContext(ctx, "UPDATE sync_state SET pending='{}'"); err != nil {
		t.Fatal(err)
	}
	local := func(c *sql.Conn) (bool, error) { return false, nil }
	if err := a.mutateCatalog(ctx, db, local); err != nil {
		t.Fatalf("local-only write blocked by a pending publication: %v", err)
	}
	shared := func(c *sql.Conn) (bool, error) { return true, nil }
	if err := a.mutateCatalog(ctx, db, shared); Classify(err).Code != "push-pending" {
		t.Fatalf("shared write during a pending publication: %v", err)
	}
}
