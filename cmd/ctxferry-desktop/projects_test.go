package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

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

	d := desktop{
		route: desktopRoute{Page: pageOverview},
		data:  app.DesktopSnapshot{ReadAt: time.Now()},
		project: projectPage{
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

func TestProjectManagementLinksUseRegisteredRoutes(t *testing.T) {
	for _, tc := range []struct{ page, tab, action string }{
		{pageFleet, "资源", "打开设备与资源"},
		{pageSkills, "Skills", "打开 Skills 管理"},
		{pageArtifacts, "产物", "打开产物管理"},
	} {
		t.Run(tc.page, func(t *testing.T) {
			ctx := t.Context()
			core := app.App{DataDir: t.TempDir()}
			if _, err := core.Init(ctx); err != nil {
				t.Fatal(err)
			}
			bound, err := core.BindProject(ctx, t.TempDir(), "sample")
			if err != nil {
				t.Fatal(err)
			}
			prior, alreadyRegistered := desktopPages[tc.page]
			t.Cleanup(func() {
				if alreadyRegistered {
					desktopPages[tc.page] = prior
				} else {
					delete(desktopPages, tc.page)
				}
			})
			delete(desktopPages, tc.page)
			d := desktop{
				route: desktopRoute{Page: pageProjects, ProjectID: bound.Project.ID},
				data:  app.DesktopSnapshot{ReadAt: time.Now()},
				project: projectPage{
					core: core, ctx: ctx, run: func(work func() func()) { work()() },
				},
			}
			window := ui.NewTester(d.view, 1180, 750)
			if _, ok := window.Access("项目 · sample"); !ok {
				t.Fatal("project route did not open the requested Project")
			}
			clickTab := func(name string) {
				t.Helper()
				for _, control := range window.AccessAll() {
					if control.Segment && control.Name == name {
						box := control.Bounds
						window.ClickAt(box.X+box.W/2, box.Y+box.H/2)
						return
					}
				}
				t.Fatalf("missing Project tab %q", name)
			}
			clickTab(tc.tab)
			if _, ok := window.Access(tc.action); ok {
				t.Fatal("showed an entry for an unavailable management page")
			}
			desktopPages[tc.page] = func(d *desktop, c *ui.Context) {
				if ui.Button(c, "返回项目").Clicked() {
					d.navigate(desktopRoute{Page: pageProjects, ProjectID: d.route.ProjectID})
				}
			}
			clickTab("配置")
			clickTab(tc.tab)
			if err := window.Click(tc.action); err != nil {
				t.Fatal(err)
			}
			if want := (desktopRoute{Page: tc.page, ProjectID: bound.Project.ID}); d.route != want {
				t.Fatalf("management route got %#v; want %#v", d.route, want)
			}
			if err := window.Click("返回项目"); err != nil {
				t.Fatal(err)
			}
			if _, ok := window.Access(tc.action); !ok || d.project.target.CheckoutID != bound.Binding.ID {
				t.Fatal("return discarded the project tab or Checkout")
			}
		})
	}
}
