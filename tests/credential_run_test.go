package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/cli"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/store"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

const otherSecretValue = "ctxferry-synthetic-neighbour-value"

// recordingTrust stands in for the device trust decision owned by #16.
type recordingTrust struct {
	definitions []app.ExecutionDefinition
	deny        error
}

func (r *recordingTrust) Enabled(_ context.Context, d app.ExecutionDefinition) error {
	r.definitions = append(r.definitions, d)
	return r.deny
}

func assertNoSecretValues(t *testing.T, output string) {
	t.Helper()
	assertNoProviderData(t, output)
	if strings.Contains(output, otherSecretValue) {
		t.Fatal("a delivered value escaped into captured output")
	}
}

func credentialRun(t *testing.T, h *testcli.Harness, services cli.Services, args ...string) testcli.Result {
	t.Helper()
	return credentialRunContext(t, t.Context(), h, services, args...)
}

func credentialRunContext(t *testing.T, ctx context.Context, h *testcli.Harness, services cli.Services, args ...string) testcli.Result {
	t.Helper()
	t.Chdir(h.Dir)
	var stdout, stderr bytes.Buffer
	code := cli.RunWithServices(ctx, append([]string{"--data-dir", h.DataDir, "run"}, args...), &stdout, &stderr, services)
	r := testcli.Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
	assertNoSecretValues(t, r.Stdout+r.Stderr)
	return r
}

// blockingAdapter waits in Resolve until the invocation is interrupted.
type blockingAdapter struct {
	*providertest.Fake
	started chan struct{}
}

func (b blockingAdapter) Resolve(ctx context.Context, _ provider.Profile, _ []byte, _ provider.Locator) ([]byte, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func observationCount(t *testing.T, h *testcli.Harness, id string) int {
	t.Helper()
	var observations []any
	if err := json.Unmarshal(h.Run(t, "resource", "inspect", id, "--format=json").Success(t).Data["observations"], &observations); err != nil {
		t.Fatal(err)
	}
	return len(observations)
}

type credentialReport struct {
	Names   []string
	Digests map[string]string
}

func reportedEnvironment(t *testing.T, r testcli.Result) credentialReport {
	t.Helper()
	events := runEvents(t, r)
	var out strings.Builder
	for _, event := range events {
		if event.Type == "stdout" {
			out.WriteString(event.Data)
		}
	}
	result := events[len(events)-1]
	var report credentialReport
	if r.ExitCode != 0 || result.Origin != "child" || result.ExitCode == nil || *result.ExitCode != 0 ||
		json.Unmarshal([]byte(out.String()), &report) != nil {
		t.Fatalf("credentialed child did not report: %+v", r)
	}
	return report
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// The child sees the neutral base and its own delivered variables only.
func assertChildEnvironment(t *testing.T, report credentialReport, delivered, value string) {
	t.Helper()
	base := []string{"PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "HOME", "USERPROFILE", "TMP", "TMPDIR", "TEMP",
		"LANG", "LC_ALL", "TERM", "COLORTERM"}
	for _, name := range report.Names {
		if name != delivered && !slices.Contains(base, strings.ToUpper(name)) {
			t.Fatalf("child received %s", name)
		}
	}
	if !slices.Contains(report.Names, delivered) || report.Digests[delivered] != digest(value) || len(report.Digests) != 1 {
		t.Fatalf("child did not receive exactly its delivered value: %+v", report)
	}
}

func assertNoPersistedValues(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			assertNoSecretValues(t, string(data))
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func editBinding(t *testing.T, h *testcli.Harness, id, path string, value any) func() {
	t.Helper()
	db, err := store.Open(t.Context(), h.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var original string
	if err := db.QueryRow("SELECT metadata FROM records WHERE id=?", id).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE records SET metadata=json_set(metadata,?,?) WHERE id=?", path, value, id); err != nil {
		t.Fatal(err)
	}
	return func() {
		db, err := store.Open(t.Context(), h.DataDir, false)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec("UPDATE records SET metadata=? WHERE id=?", original, id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCredentialedRun(t *testing.T) {
	// Caller environment carrying Git, provider and unrelated Task material.
	for name, value := range map[string]string{
		"GH_TOKEN": "synthetic", "BW_SESSION": "synthetic", "GIT_CONFIG_COUNT": "0", "CTXFERRY_TEST_API": "synthetic",
		"INFISICAL_TOKEN": "synthetic", "GIT_ASKPASS": "synthetic", "SSH_AUTH_SOCK": "synthetic",
		"DATABASE_URL": "caller-value", "OTHER_URL": "caller-value",
	} {
		t.Setenv(name, value)
	}
	fixture := taskFixture(t)
	program := "tools/" + filepath.Base(fixture)
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	git(t, a, "init")
	git(t, a, "commit", "--allow-empty", "-m", "Source checkout")
	installTaskFixture(t, a.Dir, fixture, "device A")
	root := a.Dir
	a.Dir = filepath.Join(root, "app")
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

	neighbourCredential := strings.Replace(dbCredential, "DATABASE_URL", "OTHER_URL", 1)
	task := func(name, credential string, args ...string) string {
		t.Helper()
		flags := []string{"--working-directory=working", "--resource-slot=" + dbSlot, "--credential-requirement=" + credential}
		for _, arg := range args {
			flags = append(flags, "--arg="+arg)
		}
		return resourceString(t, addTask(t, a, name, mid, program, "shared", flags...).Success(t), "id")
	}
	integration := task("integration", dbCredential, "credentials", "DATABASE_URL", "OTHER_URL")
	task("leaky", dbCredential, "leak", "DATABASE_URL")
	task("neighbour", neighbourCredential, "credentials", "DATABASE_URL", "OTHER_URL")
	resourceID := resourceString(t, addResource(t, a, "shared-db", "Integration service", "shared").Success(t), "id")
	otherResource := resourceString(t, addResource(t, a, "other-db", "Other service", "shared").Success(t), "id")
	for _, name := range []string{"integration", "leaky", "neighbour"} {
		a.Run(t, "profile", "map-resource", "dev", name, "db", resourceID, "--format=json").Success(t)
	}
	a.Run(t, "profile", "select", "dev", "--format=json").Success(t)

	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	ref := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "BACKEND_DB"}
	other := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "NEIGHBOUR_DB"}
	bindOn := func(h *testcli.Harness, task, alias string, loc provider.Locator) string {
		t.Helper()
		return field(t, h.Run(t, "secret", "bind", task, "login", "--profile=dev", "--alias", alias,
			"--provider=fake", "--provider-profile=team", "--method=machine", "--backend-project", loc.ProjectID,
			"--environment", loc.Environment, "--path", loc.Path, "--key", loc.Key, "--format=json").Success(t), "binding")
	}
	newBinding := func(task, alias string, loc provider.Locator) string {
		t.Helper()
		return bindOn(a, task, alias, loc)
	}
	bindingID := newBinding("integration", "db", ref)
	a.Run(t, "secret", "bind", "leaky", "login", "--profile=dev", "--binding", bindingID, "--format=json").Success(t)
	newBinding("neighbour", "neighbour", other)

	fake := &providertest.Fake{
		Entries: map[providertest.Reference]string{{Profile: p, Locator: ref}: secretValue, {Profile: p, Locator: other}: otherSecretValue},
		// A native execution capability does not hand the child to the provider.
		Methods: []provider.MethodCapabilities{{Method: "machine", MachineAuth: true, NativeExec: true}},
	}
	providers := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	trust := &recordingTrust{}
	trusted := cli.Services{Providers: providers, Trust: trust}
	jsonl := func(h *testcli.Harness, services cli.Services, name string, flags ...string) testcli.Result {
		t.Helper()
		return credentialRun(t, h, services, append([]string{name, "--format=jsonl"}, flags...)...)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a.Run(t, "resource", "route", resourceID, "--endpoint", listener.Addr().String(), "--format=json").Success(t)

	// Without the device trust decision credentialed runs fail closed before any provider call.
	refusedTask(t, a.Run(t, "run", "integration", "--format=jsonl"), "execution-definition-changed")
	refusedTask(t, jsonl(a, cli.Services{Providers: providers}, "integration"), "execution-definition-changed")
	trust.deny = errors.New("definition changed")
	refusedTask(t, jsonl(a, trusted, "integration"), "execution-definition-changed")
	trust.deny = nil
	refusedTask(t, jsonl(a, trusted, "integration"), "authentication-required")
	if fake.LoginCount() != 0 || len(fake.Lookups()) != 0 {
		t.Fatal("provider called before trust and device login")
	}
	providerCommand(t, a, providers, "provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)

	report := reportedEnvironment(t, jsonl(a, trusted, "integration"))
	assertChildEnvironment(t, report, "DATABASE_URL", secretValue)
	if got := fake.Lookups(); !slices.Equal(got, []providertest.Reference{{Profile: p, Locator: ref}}) {
		t.Fatal("run resolved references beyond the Task's own requirement", got)
	}
	definition := trust.definitions[len(trust.definitions)-1]
	membershipRoot, err := filepath.EvalSymlinks(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if definition.TaskID != integration || definition.MembershipID != mid || definition.Program != program ||
		!slices.Equal(definition.Argv, []string{"credentials", "DATABASE_URL", "OTHER_URL"}) ||
		definition.MembershipRoot != membershipRoot || definition.WorkingDirectory != filepath.Join(membershipRoot, "working") ||
		definition.Profile != "dev" || len(definition.Credentials) != 1 {
		t.Fatalf("trust decision received the wrong definition: %+v", definition)
	}
	if c := definition.Credentials[0]; c.BindingID != bindingID || c.Locator != ref || c.ProviderProfile != p ||
		c.ResourceID != resourceID || c.ResourcePurpose != "Integration service" || c.Env != "DATABASE_URL" || c.BindingRevision == "" {
		t.Fatalf("trust decision received the wrong credential chain: %+v", c)
	}
	neighbour := reportedEnvironment(t, jsonl(a, trusted, "neighbour"))
	assertChildEnvironment(t, neighbour, "OTHER_URL", otherSecretValue)
	if got := fake.Lookups(); got[len(got)-1].Locator != other || len(got) != 2 {
		t.Fatal("neighbour Task resolved another Task's reference", got)
	}

	// Delivered values are replaced in forwarded output, including across writes.
	leak := jsonl(a, trusted, "leaky")
	events := runEvents(t, leak)
	var stdout, stderr strings.Builder
	for _, event := range events {
		switch event.Type {
		case "stdout":
			stdout.WriteString(event.Data)
		case "stderr":
			stderr.WriteString(event.Data)
		}
	}
	if leak.ExitCode != 5 || stdout.String() != "before [REDACTED] after [REDACTED][REDACTED]\n" || stderr.String() != "diagnostic [REDACTED]\n" {
		t.Fatalf("output was not protected: %q %q", stdout.String(), stderr.String())
	}
	text := credentialRun(t, a, trusted, "leaky")
	if text.ExitCode != 5 || text.Stdout != "before [REDACTED] after [REDACTED][REDACTED]\n" || text.Stderr != "diagnostic [REDACTED]\n" {
		t.Fatalf("text output was not protected: %+v", text)
	}

	// Authorization and launch use one private snapshot: changing the returned
	// plan cannot redirect delivery. One shared writer stays one stream.
	application := app.App{DataDir: a.DataDir, Providers: providers, Trust: trust}
	t.Chdir(a.Dir)
	plan, err := application.PrepareTask(t.Context(), "", a.Dir, "leaky", "", "")
	if err != nil {
		t.Fatal(err)
	}
	plan.Task.Program, plan.Task.Argv, plan.Dir = "ctxferry-redirected", []string{"credentials", "DATABASE_URL"}, t.TempDir()
	plan.Task.CredentialRequirements = nil
	var combined bytes.Buffer
	code, err := application.ExecuteTask(t.Context(), plan, nil, &combined, &combined, func() {})
	if err != nil || code != 5 || !strings.Contains(combined.String(), "before [REDACTED] after [REDACTED][REDACTED]\n") ||
		!strings.Contains(combined.String(), "diagnostic [REDACTED]\n") {
		t.Fatalf("snapshot or shared output was not honoured: %d %v %q", code, err, combined.String())
	}
	assertNoSecretValues(t, combined.String())
	if d := trust.definitions[len(trust.definitions)-1]; d.Program != program || !slices.Equal(d.Argv, []string{"leak", "DATABASE_URL"}) {
		t.Fatalf("trust decision saw a changed plan: %+v", d)
	}
	if _, err := application.ExecuteTask(t.Context(), app.TaskExecution{Task: plan.Task}, nil, &combined, &combined, func() {}); app.Classify(err).Code != "credentials-required" {
		t.Fatal("an unprepared plan was executed", err)
	}

	// Interrupting provider resolution reports an interruption, not a credential fault.
	blocking := blockingAdapter{fake, make(chan struct{})}
	interruptible := cli.Services{Trust: trust, Providers: provider.Services{
		Adapters: map[string]provider.Adapter{"fake": blocking}, Credentials: providers.Credentials}}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { <-blocking.started; cancel() }()
	refusedTask(t, credentialRunContext(t, ctx, a, interruptible, "integration", "--format=jsonl"), "interrupted")

	// The requirement, selected Resource and binding are compared before each run.
	lookups := len(fake.Lookups())
	for _, edit := range []struct{ path, value string }{
		{"$.payload.purpose", "migration"}, {"$.payload.type", "password"}, {"$.payload.resourceId", otherResource},
	} {
		restore := editBinding(t, a, bindingID, edit.path, edit.value)
		refusedTask(t, jsonl(a, trusted, "integration"), "incompatible")
		restore()
	}
	if len(fake.Lookups()) != lookups {
		t.Fatal("incompatible binding reached the provider")
	}

	// A recorded unreachable observation blocks the run; a reachable local
	// namesake is never used in its place, and run itself opens no connection.
	namesake, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer namesake.Close()
	namesakeID := resourceString(t, addResource(t, a, "shared-db", "Local namesake", "local-only", "--endpoint", namesake.Addr().String()).Success(t), "id")
	a.Run(t, "resource", "check", namesakeID, "--format=json").Success(t)
	closed := listener.Addr().String()
	listener.Close()
	a.Run(t, "resource", "check", resourceID, "--format=json").Success(t)
	observed := observationCount(t, a, resourceID)
	unreachable := jsonl(a, trusted, "integration")
	refusedTask(t, unreachable, "resource-unreachable")
	if !strings.Contains(unreachable.Stdout, closed) || strings.Contains(unreachable.Stdout, namesake.Addr().String()) ||
		!strings.Contains(unreachable.Stdout, resourceID) || len(fake.Lookups()) != lookups || observationCount(t, a, resourceID) != observed {
		t.Fatalf("unreachable diagnosis is wrong: %s", unreachable.Stdout)
	}

	// Authentication is device-local: device A's login does not serve device B.
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	refusedTask(t, jsonl(b, trusted, "integration", "--profile=dev"), "authentication-required")
	if len(fake.Lookups()) != lookups {
		t.Fatal("device B used another device's session")
	}
	providerCommand(t, b, providers, "provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)
	assertChildEnvironment(t, reportedEnvironment(t, jsonl(b, trusted, "integration", "--profile=dev")), "DATABASE_URL", secretValue)
	fake.SetState(p, provider.Locked)
	refusedTask(t, jsonl(b, trusted, "integration", "--profile=dev"), "authentication-required")
	fake.SetState(p, provider.Authenticated)
	delete(fake.Entries, providertest.Reference{Profile: p, Locator: ref})
	refusedTask(t, jsonl(b, trusted, "integration", "--profile=dev"), "secret-unavailable")

	// Concurrent Profile mapping edits block credentialed runs as binding-conflict.
	newBinding("integration", "device-a-edit", other)
	bindOn(b, "integration", "device-b-edit", ref)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before := len(fake.Lookups())
	refusedTask(t, jsonl(b, trusted, "integration", "--profile=dev"), "binding-conflict")
	if len(fake.Lookups()) != before {
		t.Fatal("conflicted mapping reached the provider")
	}

	// Values are never persisted: no general-purpose cache, store entry or published record.
	assertNoPersistedValues(t, a.DataDir, b.DataDir)
	assertNoSecretValues(t, git(t, a, "--git-dir", remote, "grep", "-h", ".", "ctxferry-v1"))

	help := a.Run(t, "run", "--help")
	if !strings.Contains(help.Stdout, "exfiltrate") || !strings.Contains(help.Stdout, "not OS isolation") {
		t.Fatal("run help does not state the delivery limits", help.Stdout)
	}
}
