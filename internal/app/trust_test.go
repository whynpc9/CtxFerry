package app

import (
	"slices"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
)

func trustDefinition() ExecutionDefinition {
	return ExecutionDefinition{TaskID: "task", TaskRevision: "r1", MembershipID: "member", RepositoryID: "repo",
		Program: "tools/check", Argv: []string{"a", "b"}, MembershipRoot: "/src/app", WorkingDirectory: "/src/app/work",
		ProfileID: "profile", Profile: "dev", Credentials: []ExecutionCredential{
			{Requirement: "login", Env: "DATABASE_URL", Purpose: "runtime", Type: "connection-string", ResourceSlot: "db",
				ResourceID: "resource", ResourcePurpose: "Integration", BindingID: "binding", BindingRevision: "b1",
				ProviderProfile: provider.Profile{Provider: "fake", Name: "team", Method: "machine"},
				Locator:         provider.Locator{ProjectID: "p", Environment: "dev", Path: "/", Key: "DB"}},
			{Requirement: "api", Env: "API_KEY", Purpose: "runtime", Type: "token", ResourceSlot: "db", ResourceID: "resource",
				BindingID: "other", ProviderProfile: provider.Profile{Provider: "fake", Name: "team", Method: "machine"},
				Locator: provider.Locator{EntryID: "e"}},
		}}
}

func TestReviewedDefinitionCoversExecutionFields(t *testing.T) {
	base := trustDefinition().Reviewed().Digest()
	for name, edit := range map[string]func(*ExecutionDefinition){
		"program":             func(d *ExecutionDefinition) { d.Program = "tools/other" },
		"argv":                func(d *ExecutionDefinition) { d.Argv = []string{"a"} },
		"argv order":          func(d *ExecutionDefinition) { d.Argv = []string{"b", "a"} },
		"allow custom":        func(d *ExecutionDefinition) { d.AllowCustomCommand = true },
		"membership":          func(d *ExecutionDefinition) { d.MembershipID = "member2" },
		"repository":          func(d *ExecutionDefinition) { d.RepositoryID = "repo2" },
		"checkout":            func(d *ExecutionDefinition) { d.MembershipRoot = "/other/app" },
		"working directory":   func(d *ExecutionDefinition) { d.WorkingDirectory = "/src/app" },
		"profile":             func(d *ExecutionDefinition) { d.ProfileID = "profile2" },
		"env":                 func(d *ExecutionDefinition) { d.Credentials[0].Env = "OTHER" },
		"requirement purpose": func(d *ExecutionDefinition) { d.Credentials[0].Purpose = "migration" },
		"type":                func(d *ExecutionDefinition) { d.Credentials[0].Type = "password" },
		"resource":            func(d *ExecutionDefinition) { d.Credentials[0].ResourceID = "resource2" },
		"resource purpose":    func(d *ExecutionDefinition) { d.Credentials[0].ResourcePurpose = "Production" },
		"binding":             func(d *ExecutionDefinition) { d.Credentials[0].BindingID = "binding2" },
		"provider profile":    func(d *ExecutionDefinition) { d.Credentials[0].ProviderProfile.Name = "prod" },
		"auth method":         func(d *ExecutionDefinition) { d.Credentials[0].ProviderProfile.Method = "personal" },
		"locator":             func(d *ExecutionDefinition) { d.Credentials[0].Locator.Key = "PROD_DB" },
		"locator entry":       func(d *ExecutionDefinition) { d.Credentials[1].Locator.EntryID = "f" },
		"removed credential":  func(d *ExecutionDefinition) { d.Credentials = d.Credentials[:1] },
	} {
		d := trustDefinition()
		edit(&d)
		if d.Reviewed().Digest() == base {
			t.Errorf("%s change kept the enabled digest", name)
		}
		if len(executionChanges(trustDefinition().Reviewed(), d.Reviewed())) == 0 {
			t.Errorf("%s change is not listed", name)
		}
	}
	for name, edit := range map[string]func(*ExecutionDefinition){
		"task revision":      func(d *ExecutionDefinition) { d.TaskRevision = "r2" },
		"binding revision":   func(d *ExecutionDefinition) { d.Credentials[0].BindingRevision = "b2" },
		"profile name":       func(d *ExecutionDefinition) { d.Profile = "renamed" },
		"credential order":   func(d *ExecutionDefinition) { slices.Reverse(d.Credentials) },
		"exec command":       func(d *ExecutionDefinition) { d.CustomCommand = &ExecutionCommand{Program: "sh"} },
		"nil versus no args": func(d *ExecutionDefinition) {},
	} {
		d := trustDefinition()
		edit(&d)
		if d.Reviewed().Digest() != base {
			t.Errorf("%s change altered the enabled digest", name)
		}
	}
	empty := trustDefinition()
	empty.Argv = nil
	none := trustDefinition()
	none.Argv = []string{}
	if empty.Reviewed().Digest() != none.Reviewed().Digest() {
		t.Error("nil and empty argument arrays differ")
	}
}
