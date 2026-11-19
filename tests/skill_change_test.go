package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func skillChange(t *testing.T, result testcli.Result) app.SkillChangePlan {
	t.Helper()
	result.Success(t)
	var envelope struct{ Data app.SkillChangePlan }
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func TestSkillUpdateAndRollback(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		mode := "copy"
		if symlink {
			mode = "symlink"
		}
		t.Run(mode, func(t *testing.T) {
			h := testcli.New(t, binary)
			source := filepath.Join(h.Dir, "local source")
			const v1 = "---\nname: update-fixture\nlicense: MIT\n---\nVersion one.\n"
			const v2 = "---\nname: update-fixture\nlicense: MIT\n---\nVersion two.\n"
			const v3 = "---\nname: update-fixture\nlicense: MIT\n---\nVersion three.\n"
			writeSkillFixture(t, filepath.Join(source, "SKILL.md"), v1, 0600)
			writeSkillFixture(t, filepath.Join(source, "previous.txt"), "previous-only file", 0600)
			if symlink {
				if err := os.Symlink(source, filepath.Join(h.Home, "symlink-probe")); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlink privilege unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			h.Run(t, "init", "--format=json").Success(t)
			h.Run(t, "skill", "add", "--local", source, "--format=json").Success(t)
			apply := []string{"skill", "apply", "update-fixture", "--target", "codex", "--format=json"}
			if symlink {
				apply = append(apply, "--symlink")
			}
			initial := skillPlan(t, h.Run(t, apply...))
			update := []string{"skill", "update", "update-fixture", "--target", "codex", "--format=json"}
			rollback := []string{"skill", "rollback", "update-fixture", "--target", "codex", "--format=json"}
			failure(t, h.Run(t, rollback...), "skill-no-previous-version")
			writeSkillFixture(t, filepath.Join(source, "SKILL.md"), v2, 0600)
			writeSkillFixture(t, filepath.Join(source, "post-install.sh"), "#!/bin/sh\nexit 43\n", 0700)
			if err := os.Remove(filepath.Join(source, "previous.txt")); err != nil {
				t.Fatal(err)
			}

			before := skillPaths(t, h.DataDir)
			preview := skillChange(t, h.Run(t, update...))
			if preview.Confirmation == "" || preview.Status != "ready" || preview.Mode != mode ||
				preview.Before.Digest != initial.Package.Digest || preview.After.Digest == preview.Before.Digest ||
				len(preview.Changes) != 3 || preview.Changes[0].After == nil || preview.Changes[1].Before != nil ||
				preview.Changes[2].Path != "previous.txt" || preview.Changes[2].After != nil {
				t.Fatalf("unexpected update preview: %+v", preview)
			}
			if after := skillPaths(t, h.DataDir); !slices.Equal(before, after) {
				t.Fatalf("preview changed data paths: before %v after %v", before, after)
			}
			installed := filepath.Join(initial.Path, "SKILL.md")
			assertSkillFile(t, installed, v1)
			writeSkillFixture(t, filepath.Join(source, "SKILL.md"), v3, 0600)
			failure(t, h.Run(t, append(update, "--confirm", preview.Confirmation)...), "skill-plan-changed")
			assertSkillFile(t, installed, v1)
			assertSkillFile(t, filepath.Join(initial.Path, "previous.txt"), "previous-only file")

			writeSkillFixture(t, filepath.Join(source, "SKILL.md"), v2, 0600)
			if !symlink {
				writeSkillFixture(t, installed, "external change", 0600)
				unowned := filepath.Join(initial.Path, "manual.txt")
				writeSkillFixture(t, unowned, "unowned", 0600)
				blocked := skillChange(t, h.Run(t, update...))
				if blocked.Status != "blocked" || blocked.Confirmation != "" ||
					len(blocked.Issues) != 1 || !slices.Contains(blocked.Issues[0].Paths, unowned) ||
					!slices.Contains(blocked.Issues[0].Paths, installed) {
					t.Fatalf("expected external paths in preview: %+v", blocked)
				}
				failure(t, h.Run(t, append(update, "--confirm", preview.Confirmation)...), "skill-change-blocked")
				assertSkillFile(t, installed, "external change")
				assertSkillFile(t, unowned, "unowned")
				writeSkillFixture(t, installed, v1, 0600)
				if err := os.Remove(unowned); err != nil {
					t.Fatal(err)
				}
			}
			updated := skillChange(t, h.Run(t, append(update, "--confirm", preview.Confirmation)...))
			if updated.Status != "installed" || updated.After.Digest != preview.After.Digest {
				t.Fatalf("update did not install the approved pin: %+v", updated)
			}
			assertSkillFile(t, installed, v2)
			if _, err := os.Lstat(filepath.Join(initial.Path, "previous.txt")); !os.IsNotExist(err) {
				t.Fatalf("owned obsolete file remains: %v", err)
			}
			if again := skillPlan(t, h.Run(t, apply...)); again.Status != "installed" || again.Package.Digest != updated.After.Digest {
				t.Fatalf("apply changed the local pin: %+v", again)
			}
			if unchanged := skillChange(t, h.Run(t, update...)); unchanged.Status != "unchanged" || unchanged.Confirmation != "" {
				t.Fatalf("unexpected no-op plan: %+v", unchanged)
			}

			// A complete rollback comes from retained data even if the recorded
			// source is no longer a valid package.
			writeSkillFixture(t, filepath.Join(source, "SKILL.md"), "unavailable source", 0600)
			revert := skillChange(t, h.Run(t, rollback...))
			if revert.Confirmation == "" || revert.After.Digest != initial.Package.Digest {
				t.Fatalf("rollback lacks original pin: %+v", revert)
			}
			h.Run(t, append(rollback, "--confirm", revert.Confirmation)...).Success(t)
			assertSkillFile(t, installed, v1)
			assertSkillFile(t, filepath.Join(initial.Path, "previous.txt"), "previous-only file")
			if _, err := os.Lstat(filepath.Join(initial.Path, "post-install.sh")); !os.IsNotExist(err) {
				t.Fatalf("rollback left a file from the newer version: %v", err)
			}
		})
	}
}

func TestSkillGitUpdateRequiresPreviewOfResolvedCommit(t *testing.T) {
	h := gitHarness(t)
	skill := filepath.Join(h.Dir, "SKILL.md")
	writeSkillFixture(t, skill, "---\nname: git-update\n---\nv1\n", 0600)
	git(t, h, "add", "SKILL.md")
	git(t, h, "commit", "-m", "version one")
	v1 := git(t, h, "rev-parse", "HEAD")
	h.Run(t, "skill", "add", "--git", h.Dir, "--ref", v1, "--format=json").Success(t)
	initial := skillPlan(t, h.Run(t, "skill", "apply", "git-update", "--target", "codex", "--format=json"))
	writeSkillFixture(t, skill, "---\nname: git-update\n---\nv2\n", 0600)
	git(t, h, "commit", "-am", "version two")
	v2 := git(t, h, "rev-parse", "HEAD")
	git(t, h, "tag", "reviewed")
	update := []string{"skill", "update", "git-update", "--target", "codex"}
	failure(t, h.Run(t, append(update, "--format=json")...), "usage")
	failure(t, h.Run(t, append(update, "--ref", "HEAD", "--format=json")...), "usage")
	human := h.Run(t, append(update, "--ref", "refs/tags/reviewed")...)
	if human.ExitCode != 0 || !strings.Contains(human.Stdout, v2) || !strings.Contains(human.Stdout, "- SKILL.md") ||
		!strings.Contains(human.Stdout, "+ SKILL.md") || !strings.Contains(human.Stdout, "--confirm") {
		t.Fatalf("missing human pin, diff or confirmation: %+v", human)
	}
	tag := append(update, "--ref", "refs/tags/reviewed", "--format=json")
	plan := skillChange(t, h.Run(t, tag...))
	if plan.After.Commit != v2 || plan.After.Release != "reviewed" {
		t.Fatalf("preview did not resolve tag: %+v", plan.After)
	}
	git(t, h, "tag", "-f", "reviewed", v1)
	failure(t, h.Run(t, append(tag, "--confirm", plan.Confirmation)...), "skill-plan-changed")
	assertSkillFile(t, filepath.Join(initial.Path, "SKILL.md"), "---\nname: git-update\n---\nv1\n")
	git(t, h, "tag", "-f", "reviewed", v2)
	h.Run(t, append(tag, "--confirm", plan.Confirmation)...).Success(t)
	assertSkillFile(t, filepath.Join(initial.Path, "SKILL.md"), "---\nname: git-update\n---\nv2\n")

	rollback := []string{"skill", "rollback", "git-update", "--target", "codex", "--format=json"}
	revert := skillChange(t, h.Run(t, rollback...))
	if revert.After.Commit != v1 || revert.After.Release != "" {
		t.Fatalf("rollback did not recover original commit metadata: %+v", revert.After)
	}
	h.Run(t, append(rollback, "--confirm", revert.Confirmation)...).Success(t)
	assertSkillFile(t, filepath.Join(initial.Path, "SKILL.md"), "---\nname: git-update\n---\nv1\n")
}

func assertSkillFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, want %q: %v", path, got, want, err)
	}
}
