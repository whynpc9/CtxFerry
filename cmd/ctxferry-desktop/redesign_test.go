package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestCompactBackupReviewAndWrite(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	core := app.App{DataDir: filepath.Join(root, "data")}
	if _, err := core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := core.ReadDesktopSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan func(), 1)
	d := desktop{
		core: core, ctx: ctx, data: snapshot, route: desktopRoute{Page: pageSync},
		syncState: syncPageState{tab: syncBackupTab, backupPath: filepath.Join(root, "backup"), includeLocal: true},
		update:    func(f func()) { updates <- f },
	}
	d.reviewBackup()
	window := ui.NewTester(d.view, 1024, 680)
	window.Frame()
	control, ok := window.Access("Write backup")
	if !ok {
		t.Fatal("compact Write backup is not pinned")
	}
	if box := control.Bounds; box.Y < 0 || box.Y+box.H > 680 {
		t.Fatalf("Write backup is outside the compact window: %+v", box)
	}
	if err := window.Click("Acknowledge plaintext backup exposure"); err != nil {
		t.Fatal(err)
	}
	if err := window.Click("Write backup"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		update()
		window.Frame()
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for backup")
	}
	if d.syncState.backupAt.IsZero() || d.syncState.backup.Destination != filepath.Join(root, "backup") {
		t.Fatalf("backup state: %+v", d.syncState)
	}
	if _, err := os.Stat(filepath.Join(root, "backup", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if !d.syncState.backup.IncludesLocalOnly || !strings.Contains(d.syncState.backup.Boundary, "Skill installation receipts") {
		t.Fatalf("backup scope or exclusions: %+v", d.syncState.backup)
	}
	if _, ok := window.Access("Backup written"); !ok {
		t.Fatal("backup receipt is missing")
	}
}

func TestBackupLocksUncertainPublication(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageSync}, syncState: syncPageState{tab: syncBackupTab}, data: app.DesktopSnapshot{
		ReadAt: time.Now(), PublicationPending: true,
	}}
	window := ui.NewTester(d.view, 1024, 680)
	if _, ok := window.Access("Backup is temporarily locked"); !ok {
		t.Fatal("uncertain publication did not lock Backup")
	}
	if _, ok := window.Access("Write backup"); ok {
		t.Fatal("uncertain publication exposed a backup write")
	}
}
