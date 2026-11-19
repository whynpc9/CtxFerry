package app

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
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
		"checkout root":       func(d *ExecutionDefinition) { d.CheckoutRoot = "/worktree" },
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

func TestCustomProgramPathIsPassedUnchanged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "work")
	for program, want := range map[string]string{
		"tools/../check": dir + string(filepath.Separator) + "tools/../check",
		"check":          "check",
	} {
		got, err := launchProgram(taskLaunch{program: program, dir: dir, custom: true})
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", program, got, err)
		}
	}
	if runtime.GOOS != "windows" {
		// A trailing backslash is part of a POSIX directory name.
		odd := dir + `\`
		if got, _ := launchProgram(taskLaunch{program: "./check", dir: odd, custom: true}); got != odd+"/./check" {
			t.Errorf("working directory was rewritten: %q", got)
		}
	}
	abs := filepath.Join(dir, "a") + string(filepath.Separator) + ".." + string(filepath.Separator) + "b"
	if got, err := launchProgram(taskLaunch{program: abs, dir: dir, custom: true}); err != nil || got != abs {
		t.Errorf("absolute path was rewritten: %q", got)
	}
	command := ExecutionCommand{Program: `\tools\check.exe`}
	if runtime.GOOS == "windows" {
		for _, program := range []string{`\tools\check.exe`, `/tools/check.exe`, `C:check.exe`} {
			command.Program = program
			if _, err := (App{}).PrepareCustomTask(t.Context(), "", "", "task", "", "", command); Classify(err).Code != "invalid-command" {
				t.Errorf("%s was accepted: %v", program, err)
			}
		}
	}
}

func TestTrustLookupSeparatesCheckoutsFromChanges(t *testing.T) {
	a := App{DataDir: t.TempDir()}
	if _, err := a.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), a.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	enabled := trustDefinition()
	enabled.CheckoutRoot = "/src"
	r := enabled.Reviewed()
	raw, _ := json.Marshal(r)
	if _, err := db.Exec(`INSERT INTO execution_trust SELECT id,?,?,?,?,?,?,? FROM device`, r.TaskID, r.ProfileID,
		r.CheckoutRoot, r.MembershipRoot, r.Digest(), string(raw), "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	lookup := func(edit func(*ExecutionDefinition)) *trustRecord {
		d := enabled
		edit(&d)
		record, err := loadTrust(t.Context(), db, d.Reviewed())
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	if lookup(func(*ExecutionDefinition) {}) == nil {
		t.Error("exact approval not found")
	}
	if lookup(func(d *ExecutionDefinition) { d.MembershipRoot, d.WorkingDirectory = "/src/moved", "/src/moved/work" }) == nil {
		t.Error("in-place membership change lost its prior approval")
	}
	if lookup(func(d *ExecutionDefinition) { d.MembershipID, d.MembershipRoot = "member2", "/src" }) == nil {
		t.Error("membership move lost its prior approval")
	}
	if lookup(func(d *ExecutionDefinition) {
		d.RepositoryID, d.MembershipID, d.CheckoutRoot, d.MembershipRoot = "repo2", "member3", "/other", "/other"
	}) == nil {
		t.Error("a move to another Repository lost its prior approval")
	}
	if lookup(func(d *ExecutionDefinition) {
		d.MembershipID, d.CheckoutRoot, d.MembershipRoot = "member2", "/worktree", "/worktree"
	}) != nil {
		t.Error("another checkout of the Repository was treated as a change")
	}
	if lookup(func(d *ExecutionDefinition) { d.CheckoutRoot, d.MembershipRoot = "/worktree", "/worktree/app" }) != nil {
		t.Error("another checkout was treated as a change of this one")
	}
	if lookup(func(d *ExecutionDefinition) { d.CheckoutRoot = "/worktree" }) != nil {
		t.Error("a checkout sharing the membership directory reused another checkout's approval")
	}
}
