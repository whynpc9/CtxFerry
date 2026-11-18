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
