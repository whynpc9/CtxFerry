package cli

import (
	"strings"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
)

func TestTextRenderingIsReadable(t *testing.T) {
	task := app.Task{ID: "t-1", TaskSpec: app.TaskSpec{Name: "check", Purpose: "Run checks", Program: "go",
		Argv: []string{"test", "two words", ""}, WorkingDirectory: ".",
		CredentialRequirements: []app.CredentialRequirement{{Name: "login", ResourceSlot: "db", Purpose: "runtime", Type: "dsn", Env: "DATABASE_URL"}}}}
	for _, result := range []any{task, app.TaskList{Tasks: []app.Task{task}}, app.SecretStatus{Task: "check", Requirement: "login", Profile: "dev"}} {
		var out strings.Builder
		if err := render(&out, result); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if strings.HasPrefix(text, "{") || text == "" {
			t.Fatalf("%T rendered as JSON or nothing: %q", result, text)
		}
	}
	var out strings.Builder
	if err := render(&out, task); err != nil || !strings.Contains(out.String(), `Command: go test "two words" ""`) ||
		!strings.Contains(out.String(), "DATABASE_URL") {
		t.Fatal(out.String(), err)
	}
}
