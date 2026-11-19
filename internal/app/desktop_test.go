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
	if len(s.PendingIDs) != s.Sync.Pending {
		t.Fatalf("pending records %v, count %d", s.PendingIDs, s.Sync.Pending)
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
