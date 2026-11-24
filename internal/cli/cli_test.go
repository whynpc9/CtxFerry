package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestValidateChecksGrammarWithoutRunning(t *testing.T) {
	for _, args := range [][]string{
		{"sync", "init", "--preview", "abc", "--data-dir", "/nonexistent/data"},
		{"doctor", "--format", "json"},
		{"project", "list", "--all"},
	} {
		if err := Validate(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"sync", "init", "--no-such-flag"},
		{"project", "bind"},
		{"no-such-command"},
		{"doctor", "--format", "yaml"},
	} {
		if err := Validate(args); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
}

// The embedded agent Skill names commands for agents and humans to run; each
// must still parse.
func TestAgentSkillCommandsParse(t *testing.T) {
	raw, err := os.ReadFile("../app/skill_entry.md")
	if err != nil {
		t.Fatal(err)
	}
	commands := regexp.MustCompile("`ctxferry ([^`]+)`").FindAllStringSubmatch(string(raw), -1)
	if len(commands) < 10 {
		t.Fatalf("found only %d commands", len(commands))
	}
	for _, m := range commands {
		line := strings.TrimSuffix(m[1], " ...")
		args := regexp.MustCompile(`"[^"]*"|\S+`).FindAllString(line, -1)
		for i, arg := range args {
			args[i] = strings.Trim(arg, `"`)
		}
		if err := Validate(args); err != nil {
			t.Errorf("%s: %v", m[0], err)
		}
	}
}
