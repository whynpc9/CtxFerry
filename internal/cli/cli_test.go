package cli

import "testing"

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
