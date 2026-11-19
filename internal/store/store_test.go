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
	_, e = db.Exec(`DROP TABLE execution_trust; DROP TABLE resource_observations; DROP TABLE resource_routes; DROP TABLE sync_baselines; DROP TABLE sync_outbox; DROP TABLE project_subscriptions; DROP TABLE sync_config; DROP TABLE sync_state; DROP TABLE sync_previews; DROP TABLE export_identities; DROP TABLE context_fts; DROP TABLE context_bodies; DROP TABLE records; DROP TABLE checkout_bindings; DROP TABLE memberships; DROP TABLE repository_aliases; DROP TABLE repositories;` + schema + `
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
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 9 {
		t.Fatal(v, e)
	}
}

func TestV5MigrationPreservesPendingOperations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, e := Open(ctx, root, true)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`DROP TABLE resource_observations; DROP TABLE resource_routes; DROP TABLE sync_conflicts; DROP TABLE sync_objects; DROP TABLE sync_tombstones; DROP TABLE sync_blocked;
 INSERT INTO sync_state(singleton,epoch,pending) VALUES(1,'epoch','pinned candidate');
 INSERT INTO sync_outbox VALUES('record',x'62617365',x'6c6f63616c','operation','epoch'); PRAGMA user_version=5;`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = Open(ctx, root, false)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var base, local []byte
	var pending string
	if e = db.QueryRow("SELECT base,proposed FROM sync_outbox WHERE id='record'").Scan(&base, &local); e != nil || string(base) != "base" || string(local) != "local" {
		t.Fatal(e, string(base), string(local))
	}
	if e = db.QueryRow("SELECT pending FROM sync_state").Scan(&pending); e != nil || pending != "pinned candidate" {
		t.Fatal(e, pending)
	}
}

// A v8 store gains an empty device-local trust table and keeps pending work.
func TestV8MigrationStartsWithoutApprovals(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, e := Open(ctx, root, true)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`DROP TABLE execution_trust;
 INSERT INTO sync_state(singleton,epoch,pending) VALUES(1,'epoch','');
 INSERT INTO sync_outbox VALUES('record',x'62617365',x'6c6f63616c','operation','epoch'); PRAGMA user_version=8;`)
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = Open(ctx, root, false)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var v, approvals int
	var local []byte
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil || v != 9 {
		t.Fatal(v, e)
	}
	if e = db.QueryRow("SELECT count(*) FROM execution_trust").Scan(&approvals); e != nil || approvals != 0 {
		t.Fatal(approvals, e)
	}
	if e = db.QueryRow("SELECT proposed FROM sync_outbox WHERE id='record'").Scan(&local); e != nil || string(local) != "local" {
		t.Fatal(e, string(local))
	}
}
