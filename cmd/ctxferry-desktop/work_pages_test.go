package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestNavigateSkillsAndArtifacts(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	core := app.App{DataDir: t.TempDir()}
	init, err := core.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	checkout := t.TempDir()
	binding, err := core.BindProject(ctx, checkout, "sample")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	skillFile := filepath.Join(source, "SKILL.md")
	const first = "---\nname: sample-skill\n---\nSynthetic instructions\n"
	if err := os.WriteFile(skillFile, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := core.AddSkill(ctx, app.SkillImport{Local: source, Share: "private"}); err != nil {
		t.Fatal(err)
	}
	projectID := binding.Project.ID
	if _, err := core.SetSkillGroup(ctx, "project:"+projectID, []string{"sample-skill"}); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(filepath.Join(checkout, filename), []byte(filename), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := core.AddArtifact(ctx, projectID, checkout, filename, app.ArtifactRegistration{
			Type: "text/plain", Sensitivity: "internal", CacheLimit: app.DefaultArtifactCacheLimit,
			Provenance: &app.RunProvenance{SourceRevision: "test-revision", Profile: "none",
				Command: []string{"test"}, Summary: filename, Completed: []string{"synthetic complete"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := core.ReadDesktopSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d := desktop{core: core, page: "设备总览", data: snapshot}
	window := ui.NewTester(d.view, 1180, 750)
	click := func(label string) {
		t.Helper()
		if err := window.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	see := func(label string) {
		t.Helper()
		if _, ok := window.Access(label); !ok {
			t.Fatalf("missing %q", label)
		}
	}
	click("Skills")
	see("预览安装…")
	d.openProjectSkills(projectID)
	window.Frame()
	if d.skills.project != projectID || d.skills.name != "sample-skill" {
		t.Fatalf("Skills route did not retain Project ID: %+v", d.skills.entries)
	}
	click("预览安装…")
	see("安装计划")
	see("确认安装")
	if d.skills.preview.Confirmation == "" || len(d.skills.preview.Files) != 1 || d.skills.preview.Files[0].Path != "SKILL.md" {
		t.Fatal("view did not store the core preview")
	}
	target := d.skills.preview.Path
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("preview touched target: %v", err)
	}
	click("确认安装")
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if d.skills.targetState.Installed == nil {
		t.Fatal("view did not reload the installation receipt")
	}
	see("候选 pin")
	if err := os.WriteFile(skillFile, []byte(first+"Approved change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	approved, err := core.AddSkill(ctx, app.SkillImport{Local: source, Share: "private"})
	if err != nil {
		t.Fatal(err)
	}
	d.loadSkills()
	window.Frame()
	click("预览更新…")
	click("确认更新")
	prior := d.skills.targetState.Previous
	if prior == nil {
		t.Fatal("update did not retain the previous pin")
	}
	click("共享 · " + skillShortPin(&approved))
	click("上次本机 · " + skillShortPin(prior))
	click("预览回滚…")
	see("回滚计划")
	if d.skills.preview.After == nil || d.skills.preview.After.Digest != prior.Digest {
		t.Fatal("rollback did not preview the selected pin")
	}
	click("确认回滚")
	if got, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || string(got) != first {
		t.Fatalf("rollback bytes %q: %v", got, err)
	}

	click("产物")
	d.openProjectArtifacts(projectID)
	window.Frame()
	see("运行产物 · 2 项")
	see("synthetic complete")
	if d.artifacts.project != projectID || len(d.artifacts.items) != 2 {
		t.Fatal("Artifacts route did not retain Project ID")
	}
	d.artifacts.run = artifactRunLabel(d.artifacts.items[1].Run)
	d.artifacts.filterArtifacts()
	window.Frame()
	see("运行产物 · 1 项")
	click("校验本机内容")
	see("本机内容已校验")
	if d.artifacts.visible[0].Run.DeviceID != init.Device.ID {
		t.Fatal("artifact used the wrong source device")
	}
}
