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
	if _, err := core.BindProject(ctx, t.TempDir(), "alternate"); err != nil {
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
	d := desktop{core: core, route: desktopRoute{Page: pageOverview}, data: snapshot}
	window := ui.NewTester(d.view, 1280, 1400)
	click := func(label string) {
		t.Helper()
		if err := window.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	see := func(label string) {
		t.Helper()
		if _, ok := window.Access(label); !ok {
			t.Fatalf("missing %q · route=%+v catalog=%s skills=%+v target=%+v", label, d.route, d.catalogProject, d.skills, d.skills.targetState)
		}
	}
	click("Catalog")
	click("Open device-wide Skills")
	see("Preview install…")
	d.navigate(desktopRoute{Page: pageSkills, ProjectID: projectID})
	window.Frame()
	if d.skills.project != projectID || d.skills.name != "sample-skill" {
		t.Fatalf("Skills route did not retain Project ID: %+v", d.skills.entries)
	}
	click("Preview install…")
	see("install plan")
	see("Confirm install")
	if d.skills.preview.Confirmation == "" || len(d.skills.preview.Files) != 1 || d.skills.preview.Files[0].Path != "SKILL.md" {
		t.Fatal("view did not store the core preview")
	}
	target := d.skills.preview.Path
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("preview touched target: %v", err)
	}
	click("Confirm install")
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if d.skills.targetState.Installed == nil {
		t.Fatal("view did not reload the installation receipt")
	}
	see("Candidate pin")
	if err := os.WriteFile(skillFile, []byte(first+"Approved change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	approved, err := core.AddSkill(ctx, app.SkillImport{Local: source, Share: "private"})
	if err != nil {
		t.Fatal(err)
	}
	d.loadSkills()
	window.Frame()
	click("Preview update…")
	click("Confirm update")
	prior := d.skills.targetState.Previous
	if prior == nil {
		t.Fatal("update did not retain the previous pin")
	}
	click("Shared · " + skillShortPin(&approved))
	click("Previous local · " + skillShortPin(prior))
	click("Preview rollback…")
	see("rollback plan")
	if d.skills.preview.After == nil || d.skills.preview.After.Digest != prior.Digest {
		t.Fatal("rollback did not preview the selected pin")
	}
	click("Confirm rollback")
	if got, err := os.ReadFile(filepath.Join(target, "SKILL.md")); err != nil || string(got) != first {
		t.Fatalf("rollback bytes %q: %v", got, err)
	}

	d.navigate(desktopRoute{Page: pageArtifacts, ProjectID: projectID})
	window.Frame()
	see("2 artifacts · newest first")
	see("synthetic complete")
	if d.artifacts.project != projectID || len(d.artifacts.items) != 2 {
		t.Fatal("Artifacts route did not retain Project ID")
	}
	d.artifacts.run = artifactRunLabel(d.artifacts.items[1].Run)
	d.artifacts.filterArtifacts()
	window.Frame()
	see("1 artifact · newest first")
	click("Verify local content")
	see("Local content verified")
	if d.artifacts.visible[0].Run.DeviceID != init.Device.ID {
		t.Fatal("artifact used the wrong source device")
	}
	linked := desktop{core: core, route: desktopRoute{Page: pageOverview}, data: snapshot}
	linked.navigate(desktopRoute{Page: pageArtifacts, ProjectID: projectID})
	if linked.artifacts.project != projectID || len(linked.artifacts.items) != 2 {
		t.Fatal("initial route lost its explicit Project ID to the default")
	}
	for _, control := range window.AccessAll() {
		if control.Name == "Project scope" && control.Bounds.H > 20 {
			box := control.Bounds
			window.ClickAt(box.X+box.W/2, box.Y+box.H/2)
			break
		}
	}
	click("alternate")
	see("This Project has no registered run artifacts.")
}

func TestActualProjectLinksToSkillsAndArtifacts(t *testing.T) {
	ctx := t.Context()
	core := app.App{DataDir: t.TempDir()}
	if _, err := core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	bound, err := core.BindProject(ctx, t.TempDir(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := core.BindProject(ctx, t.TempDir(), "alternate"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := core.ReadDesktopSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct{ page, tab, link, empty string }{
		{pageSkills, "Skills", "Open Skills", "This Project has no shared Skills selected."},
		{pageArtifacts, "Artifacts", "Open Run artifacts", "This Project has no registered run artifacts."},
	} {
		t.Run(task.page, func(t *testing.T) {
			d := desktop{
				core: core, data: snapshot,
				route:   desktopRoute{Page: pageProjects, ProjectID: bound.Project.ID},
				project: projectPage{core: core, ctx: ctx, run: func(work func() func()) { work()() }},
			}
			window := ui.NewTester(d.view, 1180, 750)
			for _, control := range window.AccessAll() {
				if control.Segment && control.Name == task.tab {
					box := control.Bounds
					window.ClickAt(box.X+box.W/2, box.Y+box.H/2)
					break
				}
			}
			if err := window.Click(task.link); err != nil {
				t.Fatal(err)
			}
			if d.route.ProjectID != bound.Project.ID || d.route.Page != task.page {
				t.Fatalf("route=%+v", d.route)
			}
			if _, ok := window.Access(task.empty); !ok {
				t.Fatalf("missing scoped state %q", task.empty)
			}
			if err := window.Click("Open Project"); err != nil {
				t.Fatal(err)
			}
			if d.route.Page != pageProjects || d.route.ProjectID != bound.Project.ID || d.project.target.CheckoutID != bound.Binding.ID {
				t.Fatalf("return route=%+v, checkout=%s", d.route, d.project.target.CheckoutID)
			}
			if _, ok := window.Access(task.link); !ok {
				t.Fatal("return did not retain the Project tab")
			}
		})
	}
}
