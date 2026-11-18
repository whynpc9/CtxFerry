package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestConnectionAndAtomicity(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, t.TempDir(), true)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	for key, want := range map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1", "synchronous": "2"} {
		var got string
		if e = db.QueryRowContext(ctx, "PRAGMA "+key).Scan(&got); e != nil || got != want {
			t.Fatalf("%s: %s %v", key, got, e)
		}
	}
	sentinel := errors.New("insertion failed")
	e = Write(ctx, db, func(c *sql.Conn) error {
		if _, e := c.ExecContext(ctx, "INSERT INTO projects VALUES('id','slug','time')"); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRowContext(ctx, "SELECT count(*) FROM projects").Scan(&count); e != nil || count != 0 {
		t.Fatal("transaction did not roll back", count, e)
	}
}

func TestV1MigrationPreservesBindings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, e := Open(ctx, root, true)
	if e != nil {
		t.Fatal(e)
	}
	// Recreate precisely the old schema rather than merely changing its version.
	_, e = db.Exec(`DROP TABLE context_fts; DROP TABLE context_bodies; DROP TABLE records; DROP TABLE checkout_bindings; DROP TABLE memberships; DROP TABLE repository_aliases; DROP TABLE repositories;` + schema + `
 INSERT INTO projects VALUES('project','old','time');
 INSERT INTO checkout_bindings VALUES('binding','project','device','/old/path','time');`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = Open(ctx, root, false)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var id string
	var membership sql.NullString
	if e = db.QueryRow("SELECT id,membership_id FROM checkout_bindings WHERE project_id='project'").Scan(&id, &membership); e != nil || id != "binding" || membership.Valid {
		t.Fatal(id, membership, e)
	}
	var v int
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 3 {
		t.Fatal(v, e)
	}
}
