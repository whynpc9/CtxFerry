package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/store"
)

func TestDesktopLocalSnapshot(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "data")
	a := App{DataDir: root}
	if _, err := a.ReadDesktopSnapshot(ctx); !errors.Is(err, store.ErrNotInitialized) {
		t.Fatalf("missing catalog: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("desktop created missing data directory: %v", err)
	}
	init, err := a.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.BindProject(ctx, t.TempDir(), "bound")
	if err != nil {
		t.Fatal(err)
	}
	s, err := a.ReadDesktopSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Device.ID != init.Device.ID || len(s.Bindings) != 1 || s.Bindings[0].Project != "bound" || s.Sync.State != "disabled" {
		t.Fatalf("unexpected initial snapshot: %+v", s)
	}
	db, err := store.Open(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		"INSERT INTO sync_config VALUES(1,'https://github.com/example/workspace.git','workspace','main','identity')",
		"INSERT INTO sync_state(singleton,epoch,confirmed_head,history_bytes) VALUES(1,'test','head',4096)",
		"INSERT INTO sync_conflicts(token,id,local,remote,head,active) VALUES('conflict-id','record',X'00',X'00','head',1)",
		"INSERT INTO sync_outbox(id,proposed,operation_id,epoch) VALUES('pending-record',X'00','operation','test')",
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	s, err = a.ReadDesktopSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Sync.State != "conflict" || s.Sync.Config.Branch != "main" || s.Sync.Head != "head" || s.Sync.History.RawBytes != 4096 || len(s.Sync.Conflicts) != 1 {
		t.Fatalf("unexpected sync evidence: %+v", s.Sync)
	}
	if len(s.PendingIDs) != s.Sync.Pending || s.QueuePending != 1 {
		t.Fatalf("pending records %v, count %d, waiting %d", s.PendingIDs, s.Sync.Pending, s.QueuePending)
	}
	found := false
	for _, id := range s.PendingIDs {
		if id == "pending-record" {
			found = true
		}
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM sync_config"); err != nil {
		t.Fatal(err)
	}
	offline, err := a.ReadDesktopSnapshot(ctx)
	if err != nil || offline.Sync.Config != nil || offline.Sync.Pending != s.Sync.Pending || len(offline.Sync.Conflicts) != 1 {
		t.Fatalf("local evidence without a remote: %+v, %v", offline, err)
	}
	if !found {
		t.Fatal("missing pending record")
	}
}

// A conflict may still be in the outbox. It is counted in the raw CLI
// backlog, but should appear in the desktop conflict count only.
func TestDesktopQueueAndPartialSnapshot(t *testing.T) {
	ctx := t.Context()
	a := App{DataDir: filepath.Join(t.TempDir(), "data")}
	init, err := a.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindProject(ctx, t.TempDir(), "retained-project"); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		"INSERT INTO sync_outbox VALUES ('conflict',NULL,X'00','first','epoch'),('waiting',NULL,X'00','second','epoch'),('blocked',NULL,X'00','third','epoch')",
		"INSERT INTO sync_conflicts VALUES ('token','conflict',NULL,X'00',X'00','head',1)",
		"INSERT INTO sync_blocked VALUES ('blocked','dependent record has a conflict')",
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	s, err := a.ReadDesktopSnapshot(ctx)
	if err != nil || s.SyncErr != nil || s.QueuePending != 1 || s.Sync.Pending != 3 || len(s.Queue) != 3 ||
		!s.Queue[0].Conflict || !s.Queue[1].Blocked || !s.Queue[2].Waiting() {
		t.Fatalf("queue projection: %+v, %v", s, err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO sync_state(singleton,epoch) VALUES(1,'epoch')"); err != nil {
		t.Fatal(err)
	}
	// A bad sync section cannot erase the established catalog or bindings.
	if _, err := db.ExecContext(ctx, "INSERT INTO sync_config VALUES (1,'https://redacted@github.com/a/b.git',?,'main','identity')", init.Workspace.ID); err != nil {
		t.Fatal(err)
	}
	s, err = a.ReadDesktopSnapshot(ctx)
	if err != nil || s.SyncErr == nil || s.ReadAt.IsZero() || len(s.Bindings) != 1 || s.Sync.Config != nil {
		t.Fatalf("partial snapshot: %+v, %v", s, err)
	}
}
