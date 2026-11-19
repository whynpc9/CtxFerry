package app

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

// Enable prepares and stores the definition under the store's write lock, so
// no catalog write can commit between the digest check and the approval.
func TestEnableHoldsWriteLock(t *testing.T) {
	a := App{DataDir: t.TempDir()}
	if _, err := a.Init(t.Context()); err != nil {
		t.Fatal(err)
	}
	var concurrent error
	ran := false
	enableLocked = func() {
		ran = true
		path := filepath.ToSlash(store.Path(a.DataDir))
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		db, err := sql.Open("sqlite", "file://"+path+"?_pragma=busy_timeout(50)")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		_, concurrent = db.Exec("BEGIN IMMEDIATE")
		if concurrent == nil {
			db.Exec("ROLLBACK")
		}
	}
	defer func() { enableLocked = func() {} }()
	if _, err := a.EnableExecution(t.Context(), "", t.TempDir(), "task", "", "", "preview"); err == nil {
		t.Fatal("enable succeeded without a Task")
	}
	if !ran || concurrent == nil {
		t.Fatal("a concurrent writer could commit while enable prepared the definition", ran)
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
