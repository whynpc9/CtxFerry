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

func TestTrustPreviewShowsEveryReviewedCredentialField(t *testing.T) {
	review := app.ExecutionReview{Task: "check", State: "not-enabled", Preview: "token", Definition: &app.ReviewedDefinition{
		Program: "make", Credentials: []app.ReviewedCredential{{Requirement: "login", Env: "DATABASE_URL", Purpose: "runtime",
			Type: "dsn", ResourceSlot: "db", ResourceID: "res-1", ResourcePurpose: "Dev Postgres", BindingID: "bind-1"}}}}
	var out strings.Builder
	if err := render(&out, review); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"runtime", "dsn", "db", "res-1", "Dev Postgres", "bind-1", "DATABASE_URL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("preview omits %q:\n%s", want, out.String())
		}
	}
}
