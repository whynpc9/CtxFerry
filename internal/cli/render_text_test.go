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

func TestTrustPreviewCommandCannotRedrawTheTerminal(t *testing.T) {
	review := app.ExecutionReview{Task: "check", State: "not-enabled", Definition: &app.ReviewedDefinition{
		Program: "tools/my tool", Argv: []string{"\rCommand: go test ./...", "\x1b[2K", "plain"}, CheckoutRoot: "/repo\nCommand: x"}}
	var out strings.Builder
	if err := render(&out, review); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.ContainsAny(text, "\r\x1b") || strings.Count(text, "Command:") != 3 ||
		!strings.Contains(text, `Command: "tools/my tool" "\rCommand: go test ./..." "\x1b[2K" plain`) {
		t.Fatalf("%q", text)
	}
	var doc strings.Builder
	body := "line\x1b]0;title\x07"
	if err := render(&doc, app.ContextResult{Document: &app.ContextDoc{Title: "t", Body: &body}}); err != nil || strings.ContainsAny(doc.String(), "\x1b\x07") {
		t.Fatalf("%q %v", doc.String(), err)
	}
}

func TestEnabledTrustReviewOffersNoEnablePrompt(t *testing.T) {
	var out strings.Builder
	if err := render(&out, app.ExecutionReview{Task: "check", State: "enabled", Preview: "token", Definition: &app.ReviewedDefinition{Program: "make"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Preview token") || strings.Contains(out.String(), "Enable with") {
		t.Fatal(out.String())
	}
}

func TestContextIndexKeepsRowsOnOneLine(t *testing.T) {
	summary := "two\nlines"
	var out strings.Builder
	err := render(&out, app.ContextResult{Summary: &summary, Documents: []app.ContextDoc{{ID: "d-1", Title: "Notes",
		Purpose: "a\tb\nFake: row", Policy: app.Policy{Storage: "shared", AgentDisclosure: "summary"}}}})
	if err != nil || strings.Contains(out.String(), "\nFake: row") || strings.Contains(out.String(), "\nlines") ||
		!strings.Contains(out.String(), "shared/summary") {
		t.Fatalf("%q %v", out.String(), err)
	}
}
