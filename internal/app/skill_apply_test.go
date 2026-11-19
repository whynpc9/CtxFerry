package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillApplyRecovery(t *testing.T) {
	for _, point := range []string{"staged", "switched", "collision"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			a := App{DataDir: t.TempDir()}
			init, err := a.Init(ctx)
			if err != nil {
				t.Fatal(err)
			}
			source := t.TempDir()
			if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: resume-test\n---\nSynthetic.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			pkg, err := a.AddSkill(ctx, SkillImport{Local: source})
			if err != nil {
				t.Fatal(err)
			}
			project, err := a.planSkill(pkg.Name, "codex", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := project.stage(init.Device.ID); err != nil {
				t.Fatal(err)
			}
			if point == "switched" {
				if err := project.publish(); err != nil {
					t.Fatal(err)
				}
			}
			if point == "collision" {
				if err := os.Mkdir(project.Path, 0700); err != nil {
					t.Fatal(err)
				}
				// The final move independently refuses replacement even if a
				// target appears after the preceding identity check.
				if err := moveSkillExclusive(project.receipt.stagePath(&project), project.Path); err == nil {
					t.Fatal("exclusive move replaced an unmanaged directory")
				}
				// Simulate an empty target appearing between planning and switching.
				if err := project.publish(); err == nil {
					t.Fatal("publication replaced an unmanaged directory")
				}
				blocked, err := a.PlanSkill(pkg.Name, "codex", false)
				if err != nil || blocked.Status != "blocked" || blocked.Issues[0].Code != "skill-name-collision" {
					t.Fatalf("unexpected collision plan: %+v %v", blocked, err)
				}
				if err := os.Remove(project.Path); err != nil {
					t.Fatal(err)
				}
			}
			// Reopen via App's public entry points as a new invocation would.
			plan, err := a.PlanSkill(pkg.Name, "codex", false)
			if err != nil || plan.Status != "recovery-pending" {
				t.Fatalf("unexpected recovery plan: %+v %v", plan, err)
			}
			if (point == "switched") != (len(plan.Writes) == 0) {
				t.Fatalf("recovery writes do not match the interruption point: %+v", plan)
			}
			if result, err := a.ApplySkill(ctx, pkg.Name, "codex", false); err != nil || result.Status != "installed" {
				t.Fatalf("resume failed: %+v %v", result, err)
			}
			var receipt skillReceipt
			if err := readSkillJSON(project.receiptPath, &receipt); err != nil || receipt.State != "installed" ||
				receipt.DeviceID != init.Device.ID || receipt.Identity != project.receipt.Identity || len(receipt.Files) != 1 ||
				receipt.Files[0].Digest != pkg.Files[0].Digest {
				t.Fatalf("missing ownership receipt: %+v %v", receipt, err)
			}
		})
	}
}
