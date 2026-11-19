package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/cli"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

type executionReview struct {
	State          string
	Preview        string
	CustomCommands bool
	Changes        []struct{ Field string }
}

func reviewTask(t *testing.T, h *testcli.Harness, task string, flags ...string) executionReview {
	t.Helper()
	r := h.Run(t, append([]string{"task", "trust", "preview", task, "--format=json"}, flags...)...).Success(t)
	raw, err := json.Marshal(r.Data)
	if err != nil {
		t.Fatal(err)
	}
	var review executionReview
	if err := json.Unmarshal(raw, &review); err != nil {
		t.Fatal(err)
	}
	return review
}

func enableTask(t *testing.T, h *testcli.Harness, task string, flags ...string) executionReview {
	t.Helper()
	review := reviewTask(t, h, task, flags...)
	h.Run(t, append([]string{"task", "trust", "enable", task, "--preview", review.Preview, "--format=json"}, flags...)...).Success(t)
	return review
}

func changedFields(review executionReview) []string {
	fields := []string{}
	for _, change := range review.Changes {
		fields = append(fields, change.Field)
	}
	return fields
}

func taskRevision(t *testing.T, h *testcli.Harness, task string) string {
	t.Helper()
	return resourceString(t, h.Run(t, "task", "inspect", task, "--format=json").Success(t), "revision")
}

// refusedChange asserts a control-plane execution-definition-changed refusal
// with the given state and changed fields.
func refusedChange(t *testing.T, r testcli.Result, state string, fields ...string) {
	t.Helper()
	refusedTask(t, r, "execution-definition-changed")
	var event struct {
		Error struct {
			Details struct {
				State         string
				ChangedFields []string
			}
		}
	}
	if err := json.Unmarshal([]byte(r.Stdout), &event); err != nil {
		t.Fatal(err)
	}
	if d := event.Error.Details; d.State != state || len(fields) != 0 && !slices.Equal(d.ChangedFields, fields) {
		t.Fatalf("unexpected trust refusal: %+v", d)
	}
}

func execTask(t *testing.T, h *testcli.Harness, services cli.Services, args ...string) testcli.Result {
	t.Helper()
	t.Chdir(h.Dir)
	var stdout, stderr bytes.Buffer
	code := cli.RunWithServices(t.Context(), append([]string{"--data-dir", h.DataDir}, args...), &stdout, &stderr, services)
	r := testcli.Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
	assertNoSecretValues(t, r.Stdout+r.Stderr)
	return r
}

func TestExecutionTrustAcrossDevices(t *testing.T) {
	fixture := taskFixture(t)
	program := "tools/" + filepath.Base(fixture)
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	git(t, a, "init")
	git(t, a, "commit", "--allow-empty", "-m", "Source checkout")
	installTaskFixture(t, a.Dir, fixture, "device A")
	a.Dir = filepath.Join(a.Dir, "app")
	binding := obj(t, bind(t, a, "--project=checkout"), "binding")
	mid, repo := binding["membershipId"].(string), binding["repositoryId"].(string)
	a.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	git(t, b, "init")
	installTaskFixture(t, b.Dir, fixture, "device B")
	b.Dir = filepath.Join(b.Dir, "app")
	bind(t, b, "--project=checkout", "--repository="+repo)

	task := func(name string, flags ...string) {
		t.Helper()
		flags = append([]string{"--working-directory=working", "--resource-slot=" + dbSlot, "--credential-requirement=" + dbCredential,
			"--input", "the database under test"}, flags...)
		addTask(t, a, name, mid, program, "shared", flags...).Success(t)
	}
	task("integration", "--arg=credentials", "--arg=DATABASE_URL")
	task("adhoc", "--arg=credentials", "--arg=DATABASE_URL", "--allow-custom-command")
	resourceID := resourceString(t, addResource(t, a, "shared-db", "Integration service", "shared").Success(t), "id")
	for _, name := range []string{"integration", "adhoc"} {
		a.Run(t, "profile", "map-resource", "dev", name, "db", resourceID, "--format=json").Success(t)
	}
	a.Run(t, "profile", "select", "dev", "--format=json").Success(t)
	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	ref := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "BACKEND_DB"}
	other := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "NEIGHBOUR_DB"}
	bindTask := func(task, alias string, loc provider.Locator) string {
		t.Helper()
		return field(t, a.Run(t, "secret", "bind", task, "login", "--profile=dev", "--alias", alias,
			"--provider=fake", "--provider-profile=team", "--method=machine", "--backend-project", loc.ProjectID,
			"--environment", loc.Environment, "--path", loc.Path, "--key", loc.Key, "--format=json").Success(t), "binding")
	}
	bindingID := bindTask("integration", "db", ref)
	a.Run(t, "secret", "bind", "adhoc", "login", "--profile=dev", "--binding", bindingID, "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "profile", "select", "dev", "--format=json").Success(t)

	fake := &providertest.Fake{
		Entries: map[providertest.Reference]string{{Profile: p, Locator: ref}: secretValue, {Profile: p, Locator: other}: otherSecretValue},
		Methods: []provider.MethodCapabilities{{Method: "machine", MachineAuth: true}},
	}
	providers := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	// The production trust decision: device-local records in each data directory.
	local := cli.Services{Providers: providers}
	for _, h := range []*testcli.Harness{a, b} {
		providerCommand(t, h, providers, "provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)
	}
	run := func(h *testcli.Harness, task string) testcli.Result {
		t.Helper()
		return credentialRun(t, h, local, task, "--format=jsonl")
	}
	lookups := func() int { return len(fake.Lookups()) }

	// Nothing is enabled yet: credentialed runs are refused before any provider call.
	refusedChange(t, run(a, "integration"), "not-enabled")
	if lookups() != 0 {
		t.Fatal("provider called before the device trust decision")
	}
	review := reviewTask(t, a, "integration")
	if review.State != "not-enabled" || review.Preview == "" || review.CustomCommands {
		t.Fatalf("unexpected first preview: %+v", review)
	}
	failure(t, a.Run(t, "task", "trust", "enable", "integration", "--preview", strings.Repeat("0", 64), "--format=json"), "preview-stale")
	a.Run(t, "task", "trust", "enable", "integration", "--preview", review.Preview, "--format=json").Success(t)
	if again := reviewTask(t, a, "integration"); again.State != "enabled" || again.Preview != review.Preview {
		t.Fatalf("enabled definition is not reported: %+v", again)
	}
	// Once enabled, runs do not ask again.
	for range 2 {
		assertChildEnvironment(t, reportedEnvironment(t, run(a, "integration")), "DATABASE_URL", secretValue)
	}

	// Device A's decision is not device B's, and trust records are never synchronized.
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before := lookups()
	refusedChange(t, run(b, "integration"), "not-enabled")
	if lookups() != before {
		t.Fatal("device B used device A's trust decision")
	}
	published := git(t, a, "--git-dir", remote, "grep", "-h", "-e", ".", "ctxferry-v1")
	if strings.Contains(published, review.Preview) || strings.Contains(published, "execution_trust") ||
		strings.Contains(published, "ctxferry-execution-trust") {
		t.Fatal("a trust record reached the sync repository")
	}
	enableTask(t, b, "integration")
	assertChildEnvironment(t, reportedEnvironment(t, run(b, "integration")), "DATABASE_URL", secretValue)

	// A description edit on device A keeps the enabled state on device B.
	revision := taskRevision(t, a, "integration")
	a.Run(t, "task", "update", "integration", "--if-revision", revision, "--purpose", "Check the integration database",
		"--input", "a migrated schema", "--format=json").Success(t)
	failure(t, a.Run(t, "task", "update", "integration", "--if-revision", revision, "--purpose", "stale", "--format=json"), "revision-conflict")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	if taskRevision(t, b, "integration") == revision {
		t.Fatal("description edit did not reach device B")
	}
	assertChildEnvironment(t, reportedEnvironment(t, run(b, "integration")), "DATABASE_URL", secretValue)
	assertChildEnvironment(t, reportedEnvironment(t, run(a, "integration")), "DATABASE_URL", secretValue)

	// An execution-field change on device A blocks device B until it is reviewed there.
	a.Run(t, "task", "update", "integration", "--if-revision", taskRevision(t, a, "integration"),
		"--arg=credentials", "--arg=DATABASE_URL", "--arg=OTHER_URL", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before = lookups()
	refusedChange(t, run(b, "integration"), "execution-definition-changed", "argv")
	refusedChange(t, run(a, "integration"), "execution-definition-changed", "argv")
	if lookups() != before {
		t.Fatal("a changed execution definition reached the provider")
	}
	changed := reviewTask(t, b, "integration")
	if changed.State != "execution-definition-changed" || !slices.Equal(changedFields(changed), []string{"argv"}) {
		t.Fatalf("preview does not show the change: %+v", changed)
	}

	// A change synchronized between preview and enable is never enabled unseen.
	bindTask("integration", "neighbour", other)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	failure(t, b.Run(t, "task", "trust", "enable", "integration", "--preview", changed.Preview, "--format=json"), "preview-stale")
	refusedChange(t, run(b, "integration"), "execution-definition-changed",
		"argv", "credentials.login.bindingId", "credentials.login.locator")
	rebound := enableTask(t, b, "integration")
	if !slices.Equal(changedFields(rebound), []string{"argv", "credentials.login.bindingId", "credentials.login.locator"}) {
		t.Fatalf("preview does not show the remapped binding: %+v", rebound)
	}
	assertChildEnvironment(t, reportedEnvironment(t, run(b, "integration")), "DATABASE_URL", otherSecretValue)

	// A Resource purpose change blocks too; restoring the reviewed value runs again.
	restore := editBinding(t, b, resourceID, "$.payload.purpose", "Production service")
	refusedChange(t, run(b, "integration"), "execution-definition-changed", "credentials.login.resourcePurpose")
	restore()
	assertChildEnvironment(t, reportedEnvironment(t, run(b, "integration")), "DATABASE_URL", otherSecretValue)

	// exec is refused where the Task does not allow custom commands, before
	// any trust decision or provider call.
	before = lookups()
	refusedTask(t, execTask(t, b, local, "exec", "--task", "integration", "--format=jsonl", "--", program, "credentials", "DATABASE_URL"),
		"custom-command-not-allowed")
	text := execTask(t, b, local, "exec", "--task", "integration", "--", program, "credentials", "DATABASE_URL")
	if text.ExitCode == 0 || !strings.Contains(text.Stderr, "custom-command-not-allowed (control-plane)") {
		t.Fatalf("text exec refusal: %+v", text)
	}
	if lookups() != before {
		t.Fatal("a refused exec reached the provider")
	}
	for _, args := range [][]string{
		{"exec", "--task", "adhoc", "--format=json", "--", program},
		{"exec", "--task", "adhoc", program},
		{"exec", "--", program},
		{"exec", "--task", "adhoc", "--"},
	} {
		r := execTask(t, b, local, args...)
		if r.ExitCode != 2 || !strings.Contains(r.Stdout+r.Stderr, "ctxferry exec --task") {
			t.Fatalf("exec usage error for %q: %+v", args, r)
		}
	}

	// exec where the Task allows it: the same trust, credentials and contract as run.
	refusedChange(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", program, "credentials", "DATABASE_URL"), "not-enabled")
	if adhoc := enableTask(t, b, "adhoc"); !adhoc.CustomCommands {
		t.Fatalf("preview does not disclose custom commands: %+v", adhoc)
	}
	custom := reportedEnvironment(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", program, "credentials", "DATABASE_URL"))
	assertChildEnvironment(t, custom, "DATABASE_URL", secretValue)
	absolute := filepath.Join(b.Dir, "working", program)
	leak := execTask(t, b, local, "exec", "--task", "adhoc", "--", absolute, "leak", "DATABASE_URL")
	if leak.ExitCode != 5 || leak.Stdout != "before [REDACTED] after [REDACTED][REDACTED]\n" || leak.Stderr != "diagnostic [REDACTED]\n" {
		t.Fatalf("exec did not follow the output and exit contract: %+v", leak)
	}
	inspect := execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", program, "inspect", "--format", "json", "--")
	assertTaskOutput(t, inspect, "device B", "--format", "json", "--")

	// The caller's program path reaches the OS unchanged: ".." after a
	// symlinked directory is resolved by the filesystem, not lexically.
	if runtime.GOOS != "windows" {
		elsewhere := t.TempDir()
		working := filepath.Join(b.Dir, "working")
		for dir, label := range map[string]string{elsewhere: "elsewhere", working: "working"} {
			if err := os.WriteFile(filepath.Join(dir, "check"), []byte("#!/bin/sh\necho "+label+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(elsewhere, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(elsewhere, "bin"), filepath.Join(working, "links")); err != nil {
			t.Fatal(err)
		}
		events := runEvents(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", "links/../check"))
		if len(events) != 3 || events[1].Data != "elsewhere\n" || *events[2].ExitCode != 0 {
			t.Fatalf("exec did not run the caller's path: %+v", events)
		}
	} else {
		before = lookups()
		refusedTask(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", `\tools\check.exe`), "invalid-command")
		refusedTask(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", `C:check.exe`), "invalid-command")
		if lookups() != before {
			t.Fatal("a refused exec path reached the provider")
		}
	}

	// Allowing custom commands is itself an execution change: a synchronized
	// opt-in never lets exec run a caller-chosen command unreviewed.
	a.Run(t, "task", "update", "integration", "--if-revision", taskRevision(t, a, "integration"), "--allow-custom-command", "--format=json").Success(t)
	a.Run(t, "task", "update", "adhoc", "--if-revision", taskRevision(t, a, "adhoc"), "--allow-custom-command=false", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before = lookups()
	refusedChange(t, execTask(t, b, local, "exec", "--task", "integration", "--format=jsonl", "--", program, "credentials", "DATABASE_URL"),
		"execution-definition-changed", "allowCustomCommand")
	refusedTask(t, execTask(t, b, local, "exec", "--task", "adhoc", "--format=jsonl", "--", program, "credentials", "DATABASE_URL"),
		"custom-command-not-allowed")
	refusedChange(t, run(b, "adhoc"), "execution-definition-changed", "allowCustomCommand")
	if lookups() != before {
		t.Fatal("custom-command changes reached the provider")
	}
	if review := enableTask(t, b, "integration"); !review.CustomCommands {
		t.Fatalf("custom commands not disclosed: %+v", review)
	}
	assertChildEnvironment(t, reportedEnvironment(t, execTask(t, b, local, "exec", "--task", "integration", "--format=jsonl", "--",
		program, "credentials", "DATABASE_URL")), "DATABASE_URL", otherSecretValue)

	// Concurrent edits of one Task stay in conflict and block credentialed
	// execution and its review until resolved; the resolution is then reviewed.
	integrationID := resourceString(t, a.Run(t, "task", "inspect", "integration", "--format=json").Success(t), "id")
	a.Run(t, "task", "update", "integration", "--if-revision", taskRevision(t, a, "integration"),
		"--arg=credentials", "--arg=DATABASE_URL", "--format=json").Success(t)
	b.Run(t, "task", "update", "integration", "--if-revision", taskRevision(t, b, "integration"), "--purpose", "Device B note", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before = lookups()
	refusedTask(t, run(b, "integration"), "conflict-unresolved")
	refusedTask(t, execTask(t, b, local, "exec", "--task", "integration", "--format=jsonl", "--", program, "credentials"), "conflict-unresolved")
	failure(t, b.Run(t, "task", "trust", "preview", "integration", "--format=json"), "conflict-unresolved")
	if lookups() != before {
		t.Fatal("a conflicted Task reached the provider")
	}
	b.Run(t, "sync", "resolve", integrationID, "--if-conflict", conflictToken(t, b, integrationID), "--take", "remote", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	refusedChange(t, run(b, "integration"), "execution-definition-changed", "argv")
	enableTask(t, b, "integration")
	assertChildEnvironment(t, reportedEnvironment(t, run(b, "integration")), "DATABASE_URL", otherSecretValue)

	// A Task edit that would strand a Profile mapping is refused, so sync can
	// still publish; the Task keeps its revision.
	current := taskRevision(t, a, "integration")
	renamed := strings.Replace(dbCredential, `"name":"login"`, `"name":"renamed"`, 1)
	failure(t, a.Run(t, "task", "update", "integration", "--if-revision", current, "--credential-requirement", renamed, "--format=json"),
		"invalid-task-dependency")
	if taskRevision(t, a, "integration") != current {
		t.Fatal("a refused update changed the Task")
	}
	a.Run(t, "sync", "--format=json").Success(t)

	// A synchronized move to another membership is previewed against the
	// earlier approval and blocks until enabled for the new root.
	root := filepath.Dir(a.Dir)
	moved := obj(t, a.Run(t, "project", "bind", root, "--project=checkout", "--repository="+repo, "--format=json").Success(t), "binding")
	a.Run(t, "task", "update", "integration", "--if-revision", current, "--membership", moved["membershipId"].(string),
		"--working-directory", "app/working", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	// Profile selection is per checkout, so the new root names its Profile.
	movedRun := func() testcli.Result {
		return credentialRun(t, b, local, "integration", "--profile=dev", "--format=jsonl")
	}
	refusedChange(t, movedRun(), "execution-definition-changed", "membershipId", "membershipRoot")
	if review := reviewTask(t, b, "integration", "--profile=dev"); review.State != "execution-definition-changed" ||
		!slices.Equal(changedFields(review), []string{"membershipId", "membershipRoot"}) {
		t.Fatalf("membership move is not previewed as a change: %+v", review)
	}
	enableTask(t, b, "integration", "--profile=dev")
	assertChildEnvironment(t, reportedEnvironment(t, movedRun()), "DATABASE_URL", otherSecretValue)

	// Approval is per checkout: another worktree of the same Repository needs
	// its own review, and enabling it leaves the first checkout's approval intact.
	bRoot, err := filepath.EvalSymlinks(filepath.Dir(b.Dir))
	if err != nil {
		t.Fatal(err)
	}
	git(t, b, "add", "-A")
	git(t, b, "commit", "-q", "-m", "fixture")
	worktree := filepath.Join(t.TempDir(), "worktree")
	git(t, b, "worktree", "add", "-q", worktree)
	if worktree, err = filepath.EvalSymlinks(worktree); err != nil {
		t.Fatal(err)
	}
	checkoutRun := func(root string) testcli.Result {
		return credentialRun(t, b, local, "integration", "--profile=dev", "--checkout", root, "--format=jsonl")
	}
	refusedTask(t, credentialRun(t, b, local, "integration", "--profile=dev", "--format=jsonl"), "ambiguous-checkout")
	refusedChange(t, checkoutRun(worktree), "not-enabled")
	if review := reviewTask(t, b, "integration", "--profile=dev", "--checkout", worktree); review.State != "not-enabled" {
		t.Fatalf("another checkout was previewed as a change: %+v", review)
	}
	enableTask(t, b, "integration", "--profile=dev", "--checkout", worktree)
	for _, root := range []string{worktree, bRoot} {
		assertChildEnvironment(t, reportedEnvironment(t, checkoutRun(root)), "DATABASE_URL", otherSecretValue)
	}

	// A credential-free Task needs no trust decision.
	addTask(t, a, "plain", mid, program, "shared", "--working-directory=working", "--arg=marker").Success(t)
	if plain := reviewTask(t, a, "plain"); plain.State != "not-required" || plain.Preview != "" {
		t.Fatalf("credential-free preview: %+v", plain)
	}
	failure(t, a.Run(t, "task", "trust", "enable", "plain", "--preview", review.Preview, "--format=json"), "trust-not-required")

	assertNoPersistedValues(t, a.DataDir, b.DataDir)
	assertNoSecretValues(t, git(t, a, "--git-dir", remote, "grep", "-h", ".", "ctxferry-v1"))
	help := a.Run(t, "exec", "--help")
	if !strings.Contains(help.Stdout, "exfiltrate") || !strings.Contains(help.Stdout, "not OS isolation") {
		t.Fatal("exec help does not state the delivery limits", help.Stdout)
	}
}
