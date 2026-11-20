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

func TestNavigateOverviewAndSync(t *testing.T) {
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
	window := ui.NewTester(d.view, 1180, 750)
	requireLabels(t, window, "/checkout/sample", "查看冲突")
	if err := window.Click("查看冲突"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "https://github.com/example/workspace.git", "conflict title", "创建备份…")
	if _, ok := window.Access("pending title"); ok {
		t.Fatal("conflict filter exposed a pending-only record")
	}
	if err := window.Click("设备总览"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "/checkout/sample")
	if err := window.Click("同步"); err != nil {
		t.Fatal(err)
	}
	requireLabels(t, window, "conflict title")
	if _, ok := window.Access("pending title"); ok {
		t.Fatal("return navigation lost the queue filter")
	}
}

func TestSyncWithoutRemoteShowsLocalEvidence(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageSync}, data: app.DesktopSnapshot{
		QueuePending: 1,
		ReadAt:       time.Date(2026, 11, 20, 12, 34, 56, 0, time.Local),
		Sync:         app.SyncResult{State: "disabled", Pending: 1},
		Queue:        []app.DesktopQueueRecord{{ID: "local-pending-record", Pending: true}},
	}}
	window := ui.NewTester(d.view, 1180, 750)
	requireLabels(t, window, "尚未配置同步仓库", "local-pending-record", "连接私有仓库",
		"本机快照 · 2026-11-20 12:34:56", "ctxferry sync init --preview PREVIEW_ID")
}

func TestCatalogFailuresAndIncompleteSnapshot(t *testing.T) {
	d := desktop{route: desktopRoute{Page: pageOverview}, err: store.ErrNotInitialized}
	window := ui.NewTester(d.view, 780, 480)
	requireLabels(t, window, "尚未初始化工作区")
	d.err = store.ErrUnsupportedSchema
	window = ui.NewTester(d.view, 780, 480)
	requireLabels(t, window, "本机目录来自较新版本")
	d.err = errors.New("sensitive database diagnostics")
	d.data = app.DesktopSnapshot{ReadAt: time.Now(), SyncErr: d.err}
	window = ui.NewTester(d.view, 780, 480)
	requireLabels(t, window, "刷新失败，正在显示上次读取的本机快照。请检查当前工作区后重试。", "未知")
	if _, ok := window.Access("sensitive database diagnostics"); ok {
		t.Fatal("raw diagnostics appeared in the desktop")
	}
}
