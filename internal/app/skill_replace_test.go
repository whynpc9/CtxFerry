package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillReplacementRecovery(t *testing.T) {
	for _, point := range []string{"staged", "parked", "published", "failed-publication", "external-edit-after-staging", "collision"} {
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
			const before = "---\nname: replacement-test\n---\nPrevious.\n"
			const after = "---\nname: replacement-test\n---\nCandidate.\n"
			write := func(path, data string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(source, "SKILL.md"), before)
			write(filepath.Join(source, "previous.txt"), "complete previous version")
			pkg, err := a.AddSkill(ctx, SkillImport{Local: source})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.ApplySkill(ctx, pkg.Name, "codex", false); err != nil {
				t.Fatal(err)
			}
			old, err := a.planSkill(pkg.Name, "codex", false)
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(source, "SKILL.md"), after)
			next := old
			var payload skillPayload
			next.Package, payload, err = a.prepareSkillUpdate(ctx, pkg, SkillChangeInput{})
			if err != nil {
				t.Fatal(err)
			}
			version := skillVersion(next.Package)
			if err := saveSkillSnapshot(next.packageDir(pkg.Name, version), next.Package, payload.data); err != nil {
				t.Fatal(err)
			}
			if err := next.loadPackage(pkg.Name, version, false); err != nil {
				t.Fatal(err)
			}
			if err := next.stageReplacing(init.Device.ID, &old.receipt); err != nil {
				t.Fatal(err)
			}

			expected := before
			switch point {
			case "failed-publication":
				if err := os.Remove(filepath.Join(next.receipt.stagePath(&next), "previous.txt")); err != nil {
					t.Fatal(err)
				}
				if err := next.completeReplacement(&old); err == nil {
					t.Fatal("damaged staging payload was published")
				}
			case "external-edit-after-staging":
				expected = "external edit during update"
				write(filepath.Join(old.Path, "SKILL.md"), expected)
				if err := next.completeReplacement(&old); err == nil {
					t.Fatal("update succeeded over an external edit")
				}
			case "parked", "published", "collision":
				if err := moveSkillExclusive(next.Path, next.previousPath()); err != nil {
					t.Fatal(err)
				}
				if point == "published" {
					if err := next.publish(); err != nil {
						t.Fatal(err)
					}
				}
				if point == "collision" {
					if err := os.Mkdir(next.Path, 0700); err != nil {
						t.Fatal(err)
					}
					unowned := filepath.Join(next.Path, "unowned.txt")
					write(unowned, "must survive")
					if _, err := a.ApplySkill(ctx, pkg.Name, "codex", false); err == nil || Classify(err).Code != "skill-recovery-conflict" {
						t.Fatalf("recovery should block on the unowned target: %v", err)
					}
					assertSkillReplacementFile(t, unowned, "must survive")
					assertSkillReplacementFile(t, filepath.Join(next.previousPath(), "SKILL.md"), before)
					if err := os.Remove(unowned); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(next.Path); err != nil {
						t.Fatal(err)
					}
				}
			}

			if point == "staged" || point == "parked" || point == "published" || point == "collision" {
				plan, err := a.PlanSkill(pkg.Name, "codex", false)
				if err != nil || plan.Status != "recovery-pending" {
					t.Fatalf("unexpected recovery plan: %+v %v", plan, err)
				}
				if _, err := a.ApplySkill(ctx, pkg.Name, "codex", false); err != nil {
					t.Fatal(err)
				}
			}
			assertSkillReplacementFile(t, filepath.Join(old.Path, "SKILL.md"), expected)
			assertSkillReplacementFile(t, filepath.Join(old.Path, "previous.txt"), "complete previous version")
			var receipt skillReceipt
			if err := readSkillJSON(old.receiptPath, &receipt); err != nil || receipt.Identity != old.receipt.Identity ||
				receipt.State != "installed" || receipt.Previous != nil || receipt.Replaces != nil {
				t.Fatalf("failed replacement changed the installed receipt: %+v %v", receipt, err)
			}
		})
	}
}

func assertSkillReplacementFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s = %q, want %q: %v", path, got, want, err)
	}
}
