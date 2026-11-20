package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestProjectResetThroughNativeView(t *testing.T) {
	ctx := t.Context()
	core := app.App{DataDir: t.TempDir()}
	checkout, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	project, err := core.BindProject(ctx, checkout, "sample")
	if err != nil {
		t.Fatal(err)
	}
	for layer, value := range map[string]string{"project": "60", "device": "90"} {
		_, err := core.ChangeConfiguration(ctx, project.Project.ID, checkout, app.ConfigurationChange{
			Layer: layer, Field: "timeoutSeconds", Value: json.RawMessage(value),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	d := desktop{page: "设备总览", project: projectPage{
		core: core, ctx: ctx, run: func(work func() func()) { work()() },
	}}
	window := ui.NewTester(d.view, 1180, 750)
	click := func(label string) {
		t.Helper()
		if err := window.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	access := func(label string) {
		t.Helper()
		if _, ok := window.Access(label); !ok {
			t.Fatalf("missing %q", label)
		}
	}
	click("项目")
	access("sample")
	click("sample")
	access("项目 · sample")
	click("预览恢复继承…")
	access("90 秒 → 60 秒")
	click("取消")
	before, err := core.ExplainConfiguration(ctx, project.Project.ID, checkout, "")
	if err != nil || len(before.Values) != 1 || before.Values[0].Value != 90 {
		t.Fatalf("cancel changed the configuration: %#v, %v", before, err)
	}
	click("预览恢复继承…")
	click("确认恢复继承")
	access("已恢复继承 · timeoutSeconds · 设备层 · 仅本机")

	after, err := core.ExplainConfiguration(ctx, project.Project.ID, checkout, "")
	if err != nil || len(after.Values) != 1 || after.Values[0].Value != 60 || after.Values[0].Origin.Layer != "project" {
		t.Fatalf("context explain did not read the restored Project value: %#v, %v", after, err)
	}
}
