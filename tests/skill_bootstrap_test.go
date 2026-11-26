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

const bootstrapMarker = "<!-- ctxferry:begin -->"

func bootstrapRun(t *testing.T, h *testcli.Harness, target, action string, args ...string) testcli.Result {
	t.Helper()
	return h.Run(t, append([]string{"skill", "bootstrap", action, "--target", target, "--format", "json"}, args...)...)
}

func bootstrapPlan(t *testing.T, result testcli.Result) app.AgentEntryPlan {
	t.Helper()
	result.Success(t)
	var envelope struct{ Data app.AgentEntryPlan }
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

func assertBootstrapFile(t *testing.T, path, expected string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != expected {
		t.Fatalf("%s = %q, error %v, want %q", path, data, err, expected)
	}
}

func assertBootstrapAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, got %v", path, err)
	}
}

func TestSkillBootstrapPreservesInstructions(t *testing.T) {
	for _, target := range []struct{ name, instructions, skills, original, newline string }{
		{"codex", ".codex/AGENTS.md", ".agents/skills", "# Settings\nprivate codex text", "\n"},
		{"claude-code", ".claude/CLAUDE.md", ".claude/skills", "# Settings\r\nprivate claude text", "\r\n"},
	} {
		t.Run(target.name, func(t *testing.T) {
			h := testcli.New(t, binary)
			h.Run(t, "init", "--format=json").Success(t)
			home := skillHome(t, h)
			instructions := filepath.Join(home, target.instructions)
			skill := filepath.Join(home, target.skills, "ctxferry", "SKILL.md")
			writeSkillFixture(t, instructions, target.original, 0640)
			before := skillPaths(t, h.DataDir)

			result := bootstrapRun(t, h, target.name, "install")
			plan := bootstrapPlan(t, result)
			if plan.Status != "ready" || plan.Preview == "" || len(plan.Writes) != 2 ||
				plan.Writes[0].Path != skill || plan.Writes[1].Path != instructions ||
				!strings.Contains(plan.Writes[0].Content, "ctxferry context show") ||
				!strings.HasPrefix(plan.Writes[1].Content, target.newline+bootstrapMarker) ||
				strings.Contains(result.Stdout, "private ") {
				t.Fatalf("unexpected preview: %+v", result)
			}
			assertBootstrapFile(t, instructions, target.original)
			assertBootstrapAbsent(t, filepath.Dir(skill))
			if after := skillPaths(t, h.DataDir); !slices.Equal(before, after) {
				t.Fatalf("preview wrote local state: %v -> %v", before, after)
			}

			changed := target.original + " with a user edit"
			writeSkillFixture(t, instructions, changed, 0640)
			failure(t, bootstrapRun(t, h, target.name, "install", "--preview", plan.Preview), "skill-entry-preview-stale")
			assertBootstrapFile(t, instructions, changed)
			assertBootstrapAbsent(t, filepath.Dir(skill))

			plan = bootstrapPlan(t, bootstrapRun(t, h, target.name, "install"))
			if applied := bootstrapPlan(t, bootstrapRun(t, h, target.name, "install", "--preview", plan.Preview)); applied.Status != "installed" {
				t.Fatalf("install: %+v", applied)
			}
			assertBootstrapFile(t, skill, plan.Writes[0].Content)
			installed := changed + plan.Writes[1].Content
			assertBootstrapFile(t, instructions, installed)
			if runtime.GOOS != "windows" {
				if info, err := os.Stat(instructions); err != nil || info.Mode().Perm() != 0640 {
					t.Fatalf("instruction mode: %v %v", info, err)
				}
			}
			again := bootstrapPlan(t, bootstrapRun(t, h, target.name, "install", "--preview", plan.Preview))
			if again.Status != "unchanged" || len(again.Writes) != 0 || again.Preview != "" {
				t.Fatalf("repeat install: %+v", again)
			}
			assertBootstrapFile(t, instructions, installed)

			removal := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall"))
			assertBootstrapFile(t, instructions, installed)
			assertBootstrapFile(t, skill, plan.Writes[0].Content)
			if removal.Preview == "" || len(removal.Writes) != 3 {
				t.Fatalf("uninstall preview: %+v", removal)
			}
			if applied := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall", "--preview", removal.Preview)); applied.Status != "uninstalled" {
				t.Fatalf("uninstall: %+v", applied)
			}
			assertBootstrapAbsent(t, filepath.Dir(skill))
			assertBootstrapFile(t, instructions, changed)
			if again := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall")); again.Status != "unchanged" || len(again.Writes) != 0 {
				t.Fatalf("repeat uninstall: %+v", again)
			}
		})
	}
}

func TestSkillBootstrapCustomHomesAndOwnedFiles(t *testing.T) {
	for _, tool := range []struct{ name, env, instructions string }{
		{"codex", "CODEX_HOME", "AGENTS.md"},
		{"claude-code", "CLAUDE_CONFIG_DIR", "CLAUDE.md"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			h := testcli.New(t, binary)
			h.Run(t, "init", "--format=json").Success(t)
			home := skillHome(t, h)
			config := filepath.Join(home, "custom-agent")
			h.ExtraEnv = append(h.ExtraEnv, tool.env+"="+config)
			instructions := filepath.Join(config, tool.instructions)
			skillRoot := filepath.Join(config, "skills")
			if tool.name == "codex" {
				skillRoot = filepath.Join(home, ".agents", "skills")
			}
			skill := filepath.Join(skillRoot, "ctxferry", "SKILL.md")

			plan := bootstrapPlan(t, bootstrapRun(t, h, tool.name, "install"))
			if len(plan.Writes) != 2 || plan.Writes[0].Path != skill ||
				plan.Writes[1].Path != instructions || plan.Writes[1].Action != "create-file" {
				t.Fatalf("custom path preview: %+v", plan)
			}
			assertBootstrapAbsent(t, config)
			bootstrapPlan(t, bootstrapRun(t, h, tool.name, "install", "--preview", plan.Preview))

			sibling := filepath.Join(filepath.Dir(skill), "user-notes.md")
			writeSkillFixture(t, sibling, "user file", 0600)
			remove := bootstrapPlan(t, bootstrapRun(t, h, tool.name, "uninstall"))
			bootstrapPlan(t, bootstrapRun(t, h, tool.name, "uninstall", "--preview", remove.Preview))
			assertBootstrapAbsent(t, instructions)
			assertBootstrapAbsent(t, skill)
			assertBootstrapFile(t, sibling, "user file")

			failure(t, bootstrapRun(t, h, tool.name, "install"), "skill-name-collision")
			bootstrapPlan(t, bootstrapRun(t, h, tool.name, "uninstall"))
			assertBootstrapFile(t, sibling, "user file")
		})
	}
}

func TestSkillBootstrapNativeDiscoveryTargets(t *testing.T) {
	for _, target := range []struct{ name, config, override string }{
		{"cursor", ".cursor", ""},
		{"pi", ".pi/agent", ""},
		{"pi", "custom-pi", "PI_CODING_AGENT_DIR"},
	} {
		t.Run(target.name+"/"+target.config, func(t *testing.T) {
			h := testcli.New(t, binary)
			h.Run(t, "init", "--format=json").Success(t)
			config := filepath.Join(skillHome(t, h), target.config)
			if target.override != "" {
				h.ExtraEnv = append(h.ExtraEnv, target.override+"="+config)
			}
			skill := filepath.Join(config, "skills", "ctxferry", "SKILL.md")
			instructions := filepath.Join(config, "AGENTS.md")
			writeSkillFixture(t, instructions, "user instructions\n", 0600)
			before := skillPaths(t, h.DataDir)

			plan := bootstrapPlan(t, bootstrapRun(t, h, target.name, "install"))
			if plan.Status != "ready" || plan.Preview == "" || len(plan.Writes) != 1 ||
				plan.Writes[0].Path != skill || plan.Writes[0].Action != "create" ||
				!strings.Contains(plan.Writes[0].Content, "ctxferry context search") {
				t.Fatalf("install preview: %+v", plan)
			}
			assertBootstrapAbsent(t, filepath.Dir(skill))
			if after := skillPaths(t, h.DataDir); !slices.Equal(before, after) {
				t.Fatalf("preview wrote local state: %v -> %v", before, after)
			}

			writeSkillFixture(t, skill, "unowned Skill", 0600)
			failure(t, bootstrapRun(t, h, target.name, "install", "--preview", plan.Preview), "skill-name-collision")
			if got := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall")); got.Status != "unchanged" {
				t.Fatalf("unowned uninstall: %+v", got)
			}
			assertBootstrapFile(t, skill, "unowned Skill")
			if err := os.RemoveAll(filepath.Dir(skill)); err != nil {
				t.Fatal(err)
			}

			if got := bootstrapPlan(t, bootstrapRun(t, h, target.name, "install", "--preview", plan.Preview)); got.Status != "installed" {
				t.Fatalf("install: %+v", got)
			}
			assertBootstrapFile(t, skill, plan.Writes[0].Content)
			if got := bootstrapPlan(t, bootstrapRun(t, h, target.name, "install")); got.Status != "unchanged" || len(got.Writes) != 0 {
				t.Fatalf("repeat install: %+v", got)
			}
			writeSkillFixture(t, skill, "edited Skill", 0600)
			failure(t, bootstrapRun(t, h, target.name, "uninstall"), "skill-external-edit")
			assertBootstrapFile(t, skill, "edited Skill")
			writeSkillFixture(t, skill, plan.Writes[0].Content, 0600)

			removal := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall"))
			if removal.Preview == "" || len(removal.Writes) != 2 || removal.Writes[0].Path != skill || removal.Writes[0].Action != "remove" {
				t.Fatalf("uninstall preview: %+v", removal)
			}
			assertBootstrapFile(t, skill, plan.Writes[0].Content)
			if got := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall", "--preview", removal.Preview)); got.Status != "uninstalled" {
				t.Fatalf("uninstall: %+v", got)
			}
			assertBootstrapAbsent(t, filepath.Dir(skill))
			assertBootstrapFile(t, instructions, "user instructions\n")
			if got := bootstrapPlan(t, bootstrapRun(t, h, target.name, "uninstall")); got.Status != "unchanged" {
				t.Fatalf("repeat uninstall: %+v", got)
			}
		})
	}
}

func TestSkillBootstrapKeepsLaterUserEdits(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	home := skillHome(t, h)
	instructions := filepath.Join(home, ".codex", "AGENTS.md")
	skill := filepath.Join(home, ".agents", "skills", "ctxferry", "SKILL.md")
	const original = "original user line"
	const addition = "later user line\n"
	writeSkillFixture(t, instructions, original, 0600)
	plan := bootstrapPlan(t, bootstrapRun(t, h, "codex", "install"))
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "install", "--preview", plan.Preview))
	block := plan.Writes[1].Content
	editedBlock := strings.Replace(block, "For project context", "Changed project context", 1)
	writeSkillFixture(t, instructions, original+editedBlock, 0600)
	failure(t, bootstrapRun(t, h, "codex", "uninstall"), "skill-entry-conflict")
	assertBootstrapFile(t, instructions, original+editedBlock)
	assertBootstrapFile(t, skill, plan.Writes[0].Content)

	writeSkillFixture(t, instructions, original+block+addition, 0600)
	writeSkillFixture(t, skill, "user edited Skill", 0600)
	failure(t, bootstrapRun(t, h, "codex", "uninstall"), "skill-external-edit")
	assertBootstrapFile(t, instructions, original+block+addition)
	assertBootstrapFile(t, skill, "user edited Skill")

	writeSkillFixture(t, skill, plan.Writes[0].Content, 0600)
	remove := bootstrapPlan(t, bootstrapRun(t, h, "codex", "uninstall"))
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "uninstall", "--preview", remove.Preview))
	assertBootstrapFile(t, instructions, original+"\n"+addition)
	assertBootstrapAbsent(t, skill)
}

func TestSkillBootstrapNeverAdoptsAnUnownedBlock(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	home := skillHome(t, h)
	instructions := filepath.Join(home, ".codex", "AGENTS.md")
	plan := bootstrapPlan(t, bootstrapRun(t, h, "codex", "install"))
	block := plan.Writes[1].Content
	writeSkillFixture(t, instructions, block, 0600)
	failure(t, bootstrapRun(t, h, "codex", "install"), "skill-entry-conflict")
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "uninstall"))
	assertBootstrapFile(t, instructions, block)
	assertBootstrapAbsent(t, filepath.Join(home, ".agents"))
}

func TestSkillBootstrapTargetsActiveCodexInstructions(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	config := filepath.Join(skillHome(t, h), ".codex")
	base := filepath.Join(config, "AGENTS.md")
	override := filepath.Join(config, "AGENTS.override.md")
	writeSkillFixture(t, base, "base instructions\n", 0600)
	writeSkillFixture(t, override, "override instructions\n", 0600)

	plan := bootstrapPlan(t, bootstrapRun(t, h, "codex", "install"))
	if len(plan.Writes) != 2 || plan.Writes[1].Path != override {
		t.Fatalf("expected active instructions in preview: %+v", plan)
	}
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "install", "--preview", plan.Preview))
	assertBootstrapFile(t, base, "base instructions\n")
	assertBootstrapFile(t, override, "override instructions\n"+plan.Writes[1].Content)

	// A changed precedence does not redirect uninstall to an unowned file.
	moved := filepath.Join(config, "saved.md")
	if err := os.Rename(override, moved); err != nil {
		t.Fatal(err)
	}
	removal := bootstrapPlan(t, bootstrapRun(t, h, "codex", "uninstall"))
	bootstrapPlan(t, bootstrapRun(t, h, "codex", "uninstall", "--preview", removal.Preview))
	assertBootstrapFile(t, base, "base instructions\n")
	assertBootstrapFile(t, moved, "override instructions\n"+plan.Writes[1].Content)
}
