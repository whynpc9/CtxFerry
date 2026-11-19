package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillBootstrapRecovery(t *testing.T) {
	for _, point := range []string{"staged", "published", "instructions-written"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			a := App{DataDir: t.TempDir()}
			if _, err := a.Init(ctx); err != nil {
				t.Fatal(err)
			}
			p, err := a.planAgentEntry("codex", "install")
			if err != nil {
				t.Fatal(err)
			}
			if err := p.stageSkill(); err != nil {
				t.Fatal(err)
			}
			if point != "staged" {
				if err := moveSkillExclusive(p.skillDir, p.path); err != nil {
					t.Fatal(err)
				}
			}
			if point == "instructions-written" {
				if err := p.writeInstructions(); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := a.BootstrapAgentEntry(ctx, "codex", "install", "")
			if err != nil || plan.Status != "ready" || plan.Preview == "" {
				t.Fatalf("recovery plan: %+v, %v", plan, err)
			}
			if result, err := a.BootstrapAgentEntry(ctx, "codex", "install", plan.Preview); err != nil || result.Status != "installed" {
				t.Fatalf("recovery: %+v, %v", result, err)
			}
			if result, err := a.BootstrapAgentEntry(ctx, "codex", "install", ""); err != nil || result.Status != "unchanged" {
				t.Fatalf("repeat: %+v, %v", result, err)
			}
			if got, err := os.ReadFile(p.instructions); err != nil || string(got) != entryBlock {
				t.Fatalf("recovered instructions: %q, %v", got, err)
			}
			if got, err := os.ReadFile(filepath.Join(p.path, "SKILL.md")); err != nil || string(got) != entrySkill {
				t.Fatalf("recovered Skill: %q, %v", got, err)
			}
		})
	}
}
