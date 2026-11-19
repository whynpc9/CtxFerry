package tests

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func writeSkillFixture(t *testing.T, path, contents string, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func skillPlan(t *testing.T, result testcli.Result) app.SkillPlan {
	t.Helper()
	// Success also verifies the one-object envelope and absence of stderr.
	result.Success(t)
	var envelope struct{ Data app.SkillPlan }
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func skillHome(t *testing.T, h *testcli.Harness) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(h.Home)
	if err != nil {
		t.Fatal(err)
	}
	return home
}

func skillPaths(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		paths = append(paths, path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestSkillLocalProjection(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	source := filepath.Join(h.Dir, "selected")
	writeSkillFixture(t, filepath.Join(source, "SKILL.md"), "---\nname: synthetic\nlicense: MIT\n---\nExample.\n", 0600)
	writeSkillFixture(t, filepath.Join(source, "scripts", "fixture.sh"), "#!/bin/sh\nexit 43\n", 0700)
	writeSkillFixture(t, filepath.Join(source, ".git", "config"), "not included", 0600)
	h.Run(t, "skill", "add", "--local", source, "--platform", runtime.GOOS, "--format=json").Success(t)

	args := []string{"synthetic", "--target", "codex", "--format=json"}
	codex := filepath.Join(skillHome(t, h), ".agents", "skills", "synthetic")
	before := skillPaths(t, h.DataDir)
	plan := skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Status != "ready" || plan.Mode != "copy" || plan.Path != codex || len(plan.Writes) != 2 ||
		plan.Package.License != "MIT" || plan.Package.SourceKind != "local" || !strings.HasPrefix(plan.Package.SourceURL, "file:") {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if _, err := os.Lstat(filepath.Dir(codex)); !os.IsNotExist(err) {
		t.Fatalf("plan touched target root: %v", err)
	}
	if after := skillPaths(t, h.DataDir); !slices.Equal(before, after) {
		t.Fatalf("plan changed data paths: before %v after %v", before, after)
	}

	sentinel := filepath.Join(codex, "manual.txt")
	writeSkillFixture(t, sentinel, "preserve this", 0600)
	plan = skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Status != "blocked" || len(plan.Writes) != 0 || plan.Issues[0].Code != "skill-name-collision" {
		t.Fatalf("collision missing: %+v", plan)
	}
	failure(t, h.Run(t, append([]string{"skill", "apply"}, args...)...), "skill-apply-blocked")
	if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "preserve this" {
		t.Fatalf("collision modified unmanaged data: %q %v", contents, err)
	}
	if err := os.Remove(sentinel); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(codex); err != nil {
		t.Fatal(err)
	}
	// Copy must use the captured pin even after the local source changes.
	writeSkillFixture(t, filepath.Join(source, "SKILL.md"), "source has changed", 0600)
	applied := skillPlan(t, h.Run(t, append([]string{"skill", "apply"}, args...)...))
	if applied.Status != "installed" {
		t.Fatalf("apply: %+v", applied)
	}
	if again := skillPlan(t, h.Run(t, append([]string{"skill", "apply"}, args...)...)); len(again.Writes) != 0 || again.Status != "installed" {
		t.Fatalf("apply must be idempotent: %+v", again)
	}
	if contents, err := os.ReadFile(filepath.Join(codex, "SKILL.md")); err != nil || !strings.HasPrefix(string(contents), "---\n") {
		t.Fatalf("pinned copy: %q %v", contents, err)
	}
	if runtime.GOOS != "windows" {
		executable := filepath.Join(codex, "scripts", "fixture.sh")
		if info, err := os.Stat(executable); err != nil || info.Mode().Perm()&0100 == 0 || !planFileExecutable(applied, executable) {
			t.Fatalf("lost executable bit: %v %v", info, err)
		}
	}
	installed := filepath.Join(codex, "SKILL.md")
	writeSkillFixture(t, installed, "external edit", 0600)
	plan = skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Status != "blocked" || plan.Issues[0].Code != "skill-external-edit" || !slices.Contains(plan.Issues[0].Paths, installed) {
		t.Fatalf("external edit missing: %+v", plan)
	}
	failure(t, h.Run(t, append([]string{"skill", "apply"}, args...)...), "skill-apply-blocked")
	if contents, err := os.ReadFile(installed); err != nil || string(contents) != "external edit" {
		t.Fatalf("apply overwrote an edit: %q %v", contents, err)
	}
}

func planFileExecutable(plan app.SkillPlan, path string) bool {
	for _, write := range plan.Writes {
		if write.Path == path {
			return write.Executable
		}
	}
	return false
}

func TestSkillGitPinAndRequirements(t *testing.T) {
	h := gitHarness(t)
	selected := filepath.Join(h.Dir, "examples", "selected")
	writeSkillFixture(t, filepath.Join(selected, "SKILL.md"), "---\nname: git-fixture\n---\nPinned.\n", 0600)
	writeSkillFixture(t, filepath.Join(selected, "script.sh"), "exit 42\n", 0600)
	writeSkillFixture(t, filepath.Join(h.Dir, "examples", "sibling", "SKILL.md"), "not selected", 0600)
	git(t, h, "add", ".")
	git(t, h, "update-index", "--chmod=+x", "examples/selected/script.sh")
	git(t, h, "commit", "-m", "selected Skill")
	pin := git(t, h, "rev-parse", "HEAD")
	git(t, h, "tag", "-a", "v1", "-m", "release")
	writeSkillFixture(t, filepath.Join(selected, "SKILL.md"), "---\nname: git-fixture\n---\nUnpinned.\n", 0600)
	git(t, h, "commit", "-am", "unselected version")

	add := []string{"skill", "add", "--git", h.Dir, "--subdir", "examples/selected", "--ref", "refs/tags/v1", "--license", "Apache-2.0",
		"--platform", "other-platform", "--depends-on", "ctxferry-test-missing-dependency", "--format=json"}
	added := h.Run(t, add...).Success(t)
	var pkg app.SkillPackage
	wire, err := json.Marshal(added.Data)
	if err != nil || json.Unmarshal(wire, &pkg) != nil {
		t.Fatalf("invalid package result: %s %v", wire, err)
	}
	if pkg.Commit != pin || pkg.Release != "v1" || pkg.Subdir != "examples/selected" || pkg.License != "Apache-2.0" ||
		len(pkg.Files) != 2 || pkg.Files[1].Path != "script.sh" || !pkg.Files[1].Executable {
		t.Fatalf("incorrect pin or payload: %+v", pkg)
	}
	target := filepath.Join(skillHome(t, h), ".claude", "skills", pkg.Name)
	args := []string{pkg.Name, "--target", "claude-code", "--format=json"}
	plan := skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Path != target || plan.Status != "blocked" || len(plan.Issues) != 2 ||
		plan.Issues[0].Code != "platform-unsupported" || plan.Issues[1].Code != "dependency-missing" {
		t.Fatalf("unusable Skill plan: %+v", plan)
	}
	failure(t, h.Run(t, append([]string{"skill", "apply"}, args...)...), "skill-apply-blocked")
	if _, err := os.Lstat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatalf("blocked apply touched target: %v", err)
	}
	failure(t, h.Run(t, "skill", "add", "--git", h.Dir, "--ref", "HEAD", "--format=json"), "usage")
	failure(t, h.Run(t, "skill", "add", "--git", h.Dir, "--ref", pin, "--subdir", "examples/selected", "--format=json"), "skill-name-collision")
}

func TestSkillVerifiedSymlink(t *testing.T) {
	h := testcli.New(t, binary)
	probe := filepath.Join(h.Home, "symlink-probe")
	if err := os.Symlink(h.Dir, probe); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
	h.Run(t, "init", "--format=json").Success(t)
	writeSkillFixture(t, filepath.Join(h.Dir, "SKILL.md"), "---\nname: linked\n---\nSynthetic.\n", 0600)
	h.Run(t, "skill", "add", "--local", ".", "--format=json").Success(t)
	claude := filepath.Join(skillHome(t, h), "custom-claude")
	h.ExtraEnv = append(h.ExtraEnv, "CLAUDE_CONFIG_DIR="+claude)
	args := []string{"linked", "--target", "claude-code", "--symlink", "--format=json"}
	plan := skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Path != filepath.Join(claude, "skills", "linked") || len(plan.Writes) != 1 || plan.Writes[0].Kind != "symlink" {
		t.Fatalf("symlink plan: %+v", plan)
	}
	applied := skillPlan(t, h.Run(t, append([]string{"skill", "apply"}, args...)...))
	actual, err := os.Readlink(applied.Path)
	if err != nil || actual != plan.LinkSource {
		t.Fatalf("link = %q, error = %v", actual, err)
	}
	writeSkillFixture(t, filepath.Join(actual, "SKILL.md"), "tampered package", 0600)
	plan = skillPlan(t, h.Run(t, append([]string{"skill", "plan"}, args...)...))
	if plan.Status != "blocked" || plan.Issues[0].Code != "skill-package-changed" {
		t.Fatalf("tampered symlink source: %+v", plan)
	}
}
