package main

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/store"
)

func requireLabels(t *testing.T, window *ui.Tester, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if _, ok := window.Access(label); !ok {
			t.Errorf("missing %q", label)
		}
	}
}

func TestNavigateHomeAndSync(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageOverview}, data: app.DesktopSnapshot{
		ReadAt:       time.Now(),
		QueuePending: 1,
		Device:       app.Device{ID: "dev", Name: "test-device", OS: "darwin", Arch: "arm64"},
		Bindings:     []app.DesktopBinding{{ProjectID: "project", Project: "sample", Path: "/checkout/sample"}},
		Sync: app.SyncResult{
			Config: &app.SyncConfig{Remote: "https://github.com/example/workspace.git", Branch: "main"},
			State:  "conflict", Pending: 1, Conflicts: []app.ConflictInfo{{ID: "conflict"}},
		},
		Queue: []app.DesktopQueueRecord{
			{ID: "pending", ProjectID: "project", Project: "sample", Title: "pending title", Pending: true},
			{ID: "conflict", ProjectID: "project", Project: "sample", Title: "conflict title", Conflict: true},
		},
	}}
	window := ui.NewTester(d.view, 1280, 800)
	requireLabels(t, window, "/checkout/sample", "Queue · Conflicts")
	if err := window.Click("Queue · Conflicts"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "conflict title", "Open comparison")
	if _, ok := window.Access("pending title"); ok {
		t.Fatal("conflict filter exposed a pending-only record")
	}
	if err := window.Click("Home"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "/checkout/sample")
	if err := window.Click("Sync"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "conflict title")
	if _, ok := window.Access("pending title"); ok {
		t.Fatal("return navigation lost the queue filter")
	}
}

func TestSyncWithoutRemoteShowsLocalEvidence(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageSync}, syncState: syncPageState{tab: syncQueueTab}, data: app.DesktopSnapshot{
		QueuePending: 1,
		ReadAt:       time.Date(2026, 11, 20, 12, 34, 56, 0, time.Local),
		Sync:         app.SyncResult{State: "disabled", Pending: 1},
		Queue:        []app.DesktopQueueRecord{{ID: "local-pending-record", Pending: true}},
	}}
	window := ui.NewTester(d.view, 1280, 800)
	requireLabels(t, window, "local-pending-record", "Read 12:34:56")
	if err := window.Click("Repository"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "No private repository configured", "Connect a private repository")
}

func TestCatalogFailuresAndIncompleteSnapshot(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageOverview}, err: store.ErrNotInitialized}
	window := ui.NewTester(d.view, 1024, 680)
	requireLabels(t, window, "Welcome to CtxFerry")
	d.err = store.ErrUnsupportedSchema
	window = ui.NewTester(d.view, 1024, 680)
	requireLabels(t, window, "This catalog is from a newer CtxFerry")
	d.err = errors.New("sensitive database diagnostics")
	d.data = app.DesktopSnapshot{ReadAt: time.Now(), SyncErr: d.err}
	window = ui.NewTester(d.view, 1024, 680)
	requireLabels(t, window, "Refresh failed; showing a stale snapshot", "Sync evidence is unavailable")
	if _, ok := window.Access("sensitive database diagnostics"); ok {
		t.Fatal("raw diagnostics appeared in the desktop")
	}
}
