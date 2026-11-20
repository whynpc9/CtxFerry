package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopSkillPreviewApplyUpdateAndRollback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := App{DataDir: filepath.Join(root, "catalog")}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	project, err := a.BindProject(ctx, t.TempDir(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	const versionOne = "---\nname: example\n---\nFirst version\n"
	const versionTwo = "---\nname: example\n---\nSecond version\n"
	path := filepath.Join(source, "SKILL.md")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, versionOne)
	if _, err := a.AddSkill(ctx, SkillImport{Local: source, Share: "private"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetSkillGroup(ctx, "project:"+project.Project.ID, []string{"example"}); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListDesktopSkills(ctx, project.Project.ID)
	if err != nil || len(list) != 1 || list[0].Shared == nil || list[0].Name != "example" {
		t.Fatalf("project Skill selection: %+v, %v", list, err)
	}
	p, err := a.PreviewDesktopSkillInstall(ctx, "example", "codex", false)
	if err != nil || p.Confirmation == "" || len(p.Files) != 1 || p.Files[0].After.Contents != versionOne {
		t.Fatalf("install preview: %+v, %v", p, err)
	}
	if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
		t.Fatalf("preview wrote target: %v", err)
	}

	// A different pin arrives before confirmation. The old approval cannot install it.
	write(path, versionTwo)
	if _, err := a.AddSkill(ctx, SkillImport{Local: source, Share: "private"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyDesktopSkill(ctx, "example", "codex", false, p.Confirmation); err == nil || Classify(err).Code != "skill-plan-changed" {
		t.Fatalf("stale install accepted: %v", err)
	}
	if _, err := os.Stat(p.Path); !os.IsNotExist(err) {
		t.Fatalf("stale approval wrote target: %v", err)
	}
	fresh, err := a.PreviewDesktopSkillInstall(ctx, "example", "codex", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ApplyDesktopSkill(ctx, "example", "codex", false, fresh.Confirmation); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(fresh.Path, "SKILL.md")
	write(path, versionOne)
	if _, err := a.AddSkill(ctx, SkillImport{Local: source, Share: "private"}); err != nil {
		t.Fatal(err)
	}
	input := SkillChangeInput{Name: "example", Target: "codex"}
	write(installed, versionTwo+"local edit\n")
	blocked, err := a.PreviewDesktopSkillChange(ctx, input, nil)
	if err != nil || blocked.Status != "blocked" || blocked.Confirmation != "" || len(blocked.Files) != 1 ||
		!blocked.Files[0].LocalChanged || !strings.Contains(blocked.Files[0].Local.Contents, "local edit") {
		t.Fatalf("blocked local edit: %+v, %v", blocked, err)
	}
	write(installed, versionTwo)
	stale, err := a.PreviewDesktopSkillChange(ctx, input, fresh.After)
	if err == nil || Classify(err).Code != "skill-candidate-changed" || stale.Confirmation != "" {
		t.Fatalf("stale candidate preview: %+v, %v", stale, err)
	}
	update, err := a.PreviewDesktopSkillChange(ctx, input, nil)
	if err != nil || update.Confirmation == "" || update.Files[0].Before.Contents != versionTwo || update.Files[0].After.Contents != versionOne {
		t.Fatalf("update preview: %+v, %v", update, err)
	}
	input.Confirm = update.Confirmation
	if _, err := a.ChangeSkill(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Rollback = true
	installedState, err := a.ReadDesktopSkillTarget(ctx, "example", "codex", false)
	if err != nil || installedState.Previous == nil || installedState.Previous.Digest != fresh.After.Digest {
		t.Fatalf("previous candidate: %+v, %v", installedState, err)
	}
	rollback, err := a.PreviewDesktopSkillChange(ctx, input, installedState.Previous)
	if err != nil || rollback.Confirmation == "" || rollback.Files[0].After.Contents != versionTwo {
		t.Fatalf("rollback preview: %+v, %v", rollback, err)
	}
	input.Confirm = rollback.Confirmation
	if _, err := a.ChangeSkill(ctx, input); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(installed)
	if err != nil || string(got) != versionTwo {
		t.Fatalf("rolled back bytes: %q, %v", got, err)
	}
	state, err := a.ReadDesktopSkillTarget(ctx, "example", "codex", false)
	if err != nil || state.Installed == nil || state.Installed.Digest != fresh.After.Digest {
		t.Fatalf("local receipt: %+v, %v", state, err)
	}
	list, err = a.ListDesktopSkills(ctx, project.Project.ID)
	if err != nil || len(list) != 1 || list[0].Shared.Digest != update.After.Digest {
		t.Fatalf("rollback changed shared intent: %+v, %v", list, err)
	}
}

func TestDesktopArtifactAvailabilityAndIntegrity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a := App{DataDir: filepath.Join(root, "catalog")}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	project, err := a.BindProject(ctx, t.TempDir(), "sample")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "summary.txt")
	if err := os.WriteFile(path, []byte("report"), 0600); err != nil {
		t.Fatal(err)
	}
	p := project.Project.ID
	item, err := a.AddArtifact(ctx, p, root, path, ArtifactRegistration{
		Type: "text/plain", Sensitivity: "internal", CacheLimit: DefaultArtifactCacheLimit,
		Provenance: &RunProvenance{SourceRevision: "test-revision", Profile: "none", Command: []string{"report"},
			Summary: "Synthetic run", Completed: []string{"report generated"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, err := a.ListDesktopArtifacts(ctx, p)
	if err != nil || len(list) != 1 || list[0].Content != "cache" || list[0].Run.Completed[0] != "report generated" {
		t.Fatalf("artifact list: %+v, %v", list, err)
	}
	if err := a.CheckDesktopArtifact(ctx, p, item.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(list[0].Location, []byte("damage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckDesktopArtifact(ctx, p, item.ID); err == nil || Classify(err).Code != "artifact-hash-mismatch" {
		t.Fatalf("check should fail: %v", err)
	}
	output := filepath.Join(root, "export.txt")
	if _, err := a.FetchArtifact(ctx, p, root, item.ID, output); err == nil || Classify(err).Code != "artifact-hash-mismatch" {
		t.Fatalf("export should fail: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed export exposed output: %v", err)
	}
	if err := os.Remove(list[0].Location); err != nil {
		t.Fatal(err)
	}
	list, err = a.ListDesktopArtifacts(ctx, p)
	if err != nil || len(list) != 1 || list[0].Content != "missing" {
		t.Fatalf("missing artifact state: %+v, %v", list, err)
	}
}
