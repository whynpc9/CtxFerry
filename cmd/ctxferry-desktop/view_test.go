package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestNavigateOverviewAndSync(t *testing.T) {
	d := desktop{page: "设备总览", data: app.DesktopSnapshot{
		Device:   app.Device{Name: "test-device", OS: "darwin", Arch: "arm64"},
		Bindings: []app.DesktopBinding{{Project: "sample", Path: "/checkout/sample"}},
		Sync:     app.SyncResult{Config: &app.SyncConfig{Remote: "https://github.com/example/workspace.git", Branch: "main"}},
	}}
	window := ui.NewTester(d.view, 1180, 750)
	if _, ok := window.Access("/checkout/sample"); !ok {
		t.Fatal("missing local checkout")
	}
	if err := window.Click("同步"); err != nil {
		t.Fatal(err)
	}
	if _, ok := window.Access("https://github.com/example/workspace.git"); !ok {
		t.Fatal("missing sync repository")
	}
	if err := window.Click("设备总览"); err != nil {
		t.Fatal(err)
	}
	if _, ok := window.Access("/checkout/sample"); !ok {
		t.Fatal("return navigation lost overview")
	}
}
