package tests

import (
	"encoding/json"
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
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

type standaloneBinding struct {
	ID, Alias, ResourceID, Purpose, Type string
	Locator                              provider.Locator
}

func bindingOf(t *testing.T, r testcli.Response) standaloneBinding {
	t.Helper()
	var b standaloneBinding
	if err := json.Unmarshal(r.Data["binding"], &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// An API key needs no Resource: a standalone requirement, one secret bind
// that creates the default Profile, provider login and the trust decision.
func TestStandaloneCredentials(t *testing.T) {
	fixture := taskFixture(t)
	program := "tools/" + filepath.Base(fixture)
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, _ := syncHarness(t, &metadata)
	git(t, a, "init")
	git(t, a, "commit", "--allow-empty", "-m", "Source checkout")
	installTaskFixture(t, a.Dir, fixture, "device A")
	a.Dir = filepath.Join(a.Dir, "app")
	membership := obj(t, bind(t, a, "--project=checkout"), "binding")
	mid, repo := membership["membershipId"].(string), membership["repositoryId"].(string)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)

	addTask(t, a, "agent", mid, program, "shared", "--working-directory=working",
		"--credential=openai::model-api:api-key:OPENAI_API_KEY", "--credential=anthropic::model-api:api-key:ANTHROPIC_API_KEY",
		"--arg=credentials", "--arg=OPENAI_API_KEY", "--arg=ANTHROPIC_API_KEY").Success(t)
	// A Task that allows custom commands may omit its own command.
	a.Run(t, "task", "add", "adhoc", "--membership", mid, "--storage", "shared", "--purpose", "Ad-hoc agent commands",
		"--working-directory=working", "--credential=openai::model-api:api-key:OPENAI_API_KEY", "--allow-custom-command", "--format=json").Success(t)
	failure(t, a.Run(t, "task", "add", "bare", "--membership", mid, "--storage", "shared", "--purpose", "No command", "--format=json"), "invalid-task")
	failure(t, addTask(t, a, "dangling", mid, program, "shared", "--credential=key:missing:model-api:api-key:KEY"), "invalid-task")
	if text := a.Run(t, "task", "inspect", "agent").Stdout; !strings.Contains(text, "standalone") {
		t.Fatalf("text output does not mark standalone requirements:\n%s", text)
	}

	// Nothing selects a Profile and the Project has none: bind creates default.
	created := a.Run(t, "secret", "bind", "agent", "openai", "--provider=fake", "--provider-profile=team", "--method=machine",
		"--backend-project=backend", "--environment=sandbox", "--format=json").Success(t)
	openai := bindingOf(t, created)
	want := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/", Key: "OPENAI_API_KEY"}
	if string(created.Data["profile"]) != `"default"` || openai.Alias != "openai" || openai.ResourceID != "" || openai.Locator != want {
		t.Fatalf("bind did not apply the standalone defaults: %s %+v", created.Data["profile"], openai)
	}
	// --like copies the namespace and keys the entry by the requirement's variable.
	anthropic := bindingOf(t, a.Run(t, "secret", "bind", "agent", "anthropic", "--like", "openai", "--format=json").Success(t))
	if want.Key = "ANTHROPIC_API_KEY"; anthropic.Locator != want || anthropic.Alias != "anthropic" || anthropic.ID == openai.ID {
		t.Fatalf("--like did not copy the namespace: %+v", anthropic)
	}
	failure(t, a.Run(t, "secret", "bind", "agent", "anthropic", "--like", "missing", "--format=json"), "binding-not-found")
	failure(t, a.Run(t, "secret", "bind", "adhoc", "openai", "--binding", openai.ID, "--like", "openai", "--format=json"), "usage")
	a.Run(t, "secret", "bind", "adhoc", "openai", "--binding", openai.ID, "--format=json").Success(t)

	// A Resource's binding never serves a standalone requirement.
	addTask(t, a, "migrate", mid, program, "shared", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential).Success(t)
	resourceID := resourceString(t, addResource(t, a, "shared-db", "Integration service", "shared").Success(t), "id")
	a.Run(t, "profile", "map-resource", "default", "migrate", "db", resourceID, "--format=json").Success(t)
	slotBinding := bindingOf(t, a.Run(t, "secret", "bind", "migrate", "login", "--like", "openai", "--key=DB", "--format=json").Success(t))
	if slotBinding.ResourceID != resourceID {
		t.Fatalf("slot-attached binding lost its Resource: %+v", slotBinding)
	}
	failure(t, a.Run(t, "secret", "bind", "agent", "openai", "--binding", slotBinding.ID, "--format=json"), "incompatible")
	failure(t, a.Run(t, "secret", "bind", "migrate", "login", "--binding", openai.ID, "--format=json"), "incompatible")

	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	openaiRef := providertest.Reference{Profile: p, Locator: openai.Locator}
	fake := &providertest.Fake{
		Entries: map[providertest.Reference]string{openaiRef: secretValue, {Profile: p, Locator: anthropic.Locator}: otherSecretValue},
		Methods: []provider.MethodCapabilities{{Method: "machine", MachineAuth: true}},
	}
	providers := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	local := cli.Services{Providers: providers}
	providerCommand(t, a, providers, "provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)

	// Status checks the entry but has no Resource to reach.
	status := providerCommand(t, a, providers, "secret", "status", "agent", "openai").Success(t)
	if string(status.Data["profile"]) != `"default"` || string(status.Data["resourceReachable"]) != "null" ||
		string(status.Data["entryRetrievable"]) != "true" {
		t.Fatalf("standalone status: %+v", status.Data)
	}

	// The reviewed definition carries no Resource fields for standalone credentials.
	preview := a.Run(t, "task", "trust", "preview", "agent", "--format=json").Success(t)
	if raw := string(preview.Data["definition"]); strings.Contains(raw, "resourceId") || !strings.Contains(raw, openai.ID) {
		t.Fatalf("standalone definition: %s", raw)
	}
	refusedChange(t, credentialRun(t, a, local, "agent", "--format=jsonl"), "not-enabled")
	enableTask(t, a, "agent")
	report := reportedEnvironment(t, credentialRun(t, a, local, "agent", "--format=jsonl"))
	if report.Digests["OPENAI_API_KEY"] != digest(secretValue) || report.Digests["ANTHROPIC_API_KEY"] != digest(otherSecretValue) {
		t.Fatalf("standalone credentials were not delivered: %+v", report)
	}

	// An exec-only Task refuses run and lends its credentials to exec.
	refusedTask(t, credentialRun(t, a, local, "adhoc", "--format=jsonl"), "command-required")
	enableTask(t, a, "adhoc")
	execReport := reportedEnvironment(t, execTask(t, a, local, "exec", "--task", "adhoc", "--format=jsonl", "--", program, "credentials", "OPENAI_API_KEY"))
	assertChildEnvironment(t, execReport, "OPENAI_API_KEY", secretValue)

	// The Desktop offers standalone bindings for a standalone requirement.
	core := app.App{DataDir: a.DataDir, Providers: providers}
	slot, err := core.DesktopSecretSlot(t.Context(), "checkout", app.SecretTarget{Profile: "default", Task: "adhoc", Requirement: "openai"})
	if err != nil || slot.Issue != "" || !slot.Standalone || slot.Resource.ID != "" || !slot.Status.Bound ||
		!slices.ContainsFunc(slot.Reusable, func(b app.SecretBinding) bool { return b.ID == openai.ID }) ||
		slices.ContainsFunc(slot.Reusable, func(b app.SecretBinding) bool { return b.ResourceID != "" }) ||
		slot.SourceProjects[slot.Status.Project.ID] != "checkout" {
		t.Fatalf("Desktop standalone slot: %v %+v", err, slot)
	}

	// Standalone records and the created default Profile synchronize.
	a.Run(t, "sync", "--format=json").Success(t)
	b := joinDevice(t, a, true)
	git(t, b, "init")
	installTaskFixture(t, b.Dir, fixture, "device B")
	b.Dir = filepath.Join(b.Dir, "app")
	bind(t, b, "--project=checkout", "--repository="+repo)
	var listed []standaloneBinding
	if err := json.Unmarshal(b.Run(t, "secret", "list", "--format=json").Success(t).Data["bindings"], &listed); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(listed, func(x standaloneBinding) bool { return x.ID == openai.ID && x.ResourceID == "" }) {
		t.Fatalf("device B did not receive the standalone binding: %+v", listed)
	}
	providerCommand(t, b, providers, "provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)
	if s := providerCommand(t, b, providers, "secret", "status", "agent", "openai").Success(t); string(s.Data["profile"]) != `"default"` {
		t.Fatalf("device B did not fall back to the default Profile: %+v", s.Data)
	}
}

// Several env fields import as one reviewed batch into standalone requirements.
func TestBatchEnvImport(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, _ := syncHarness(t, &metadata)
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "Source checkout")
	mid := obj(t, bind(t, h, "--project=checkout"), "binding")["membershipId"].(string)
	addTask(t, h, "agent", mid, "unused", "shared", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential,
		"--credential=openai::model-api:api-key:OPENAI_API_KEY", "--credential=anthropic::model-api:api-key:ANTHROPIC_API_KEY").Success(t)
	resourceID := resourceString(t, addResource(t, h, "shared-db", "Integration service", "shared").Success(t), "id")
	enableSync(t, h)
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	env := filepath.Join(h.Dir, ".env")
	if err := os.WriteFile(env, []byte("OPENAI_API_KEY="+secretValue+"\nANTHROPIC_API_KEY="+otherSecretValue+"\nDATABASE_URL="+importValue+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	at := func(key string) providertest.Reference {
		return providertest.Reference{Profile: p, Locator: provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/", Key: key}}
	}
	// The second entry already exists, so the first confirmation stops after the first.
	fake := &providertest.Fake{Entries: map[providertest.Reference]string{at("ANTHROPIC_API_KEY"): otherSecretValue},
		Methods: []provider.MethodCapabilities{{Method: "machine", MachineAuth: true, CreateImport: true}}}
	services := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	run := func(args ...string) testcli.Result {
		t.Helper()
		r := providerCommand(t, h, services, args...)
		for _, value := range []string{secretValue, otherSecretValue, importValue} {
			if strings.Contains(r.Stdout+r.Stderr, value) {
				t.Fatal("a source value escaped import")
			}
		}
		return r
	}
	batch := []string{"onboard", "import-env", env, "--project=checkout", "--task=agent", "--field=OPENAI_API_KEY", "--field=ANTHROPIC_API_KEY",
		"--provider=fake", "--provider-profile=team", "--method=machine", "--backend-project=backend", "--environment=sandbox"}
	previewOf := func(args []string) testcli.Response {
		t.Helper()
		return run(args...).Success(t)
	}
	confirm := func(args []string) testcli.Result {
		t.Helper()
		return run(append(args, "--confirm="+resourceString(t, previewOf(args), "preview"))...)
	}

	plan := previewOf(batch)
	var imports []struct {
		Field       string
		Requirement struct{ Name string }
		Binding     standaloneBinding
	}
	if err := json.Unmarshal(plan.Data["imports"], &imports); err != nil {
		t.Fatal(err)
	}
	if string(plan.Data["profile"]) != `"default"` || string(plan.Data["createProfile"]) != "true" || len(imports) != 2 ||
		imports[1].Requirement.Name != "anthropic" || imports[1].Binding.Alias != "anthropic" ||
		imports[1].Binding.Locator.Key != "ANTHROPIC_API_KEY" || imports[1].Binding.ResourceID != "" {
		t.Fatalf("batch preview: %+v %s", imports, plan.Data["profile"])
	}
	failure(t, run(append(batch, "--requirement=openai")...), "usage")
	failure(t, run(append(batch, "--resource="+resourceID)...), "usage")
	failure(t, run(append(batch, "--field=DATABASE_URL")...), "usage")
	failure(t, run(append(batch, "--recover=OTHER")...), "usage")
	failure(t, run(append(batch, "--field=MISSING_KEY")...), "env-field-missing")

	run("provider", "login", "fake", "--provider-profile=team", "--method=machine").Success(t)
	partial := confirm(batch)
	failure(t, partial, "env-provider-collision")
	var refused struct {
		Error struct {
			Details struct {
				Field   string
				Created []string
			}
		}
	}
	if err := json.Unmarshal([]byte(partial.Stdout), &refused); err != nil || refused.Error.Details.Field != "ANTHROPIC_API_KEY" ||
		!slices.Equal(refused.Error.Details.Created, []string{"OPENAI_API_KEY"}) {
		t.Fatalf("partial failure does not name the created entries: %s", partial.Stdout)
	}
	if fake.Entries[at("OPENAI_API_KEY")] != secretValue {
		t.Fatal("first entry was not created")
	}
	// Both entries exist now; recovery verifies them and commits the batch.
	receipt := confirm(append(batch, "--recover")).Success(t)
	var results []envImportResult
	if err := json.Unmarshal(receipt.Data["imports"], &results); err != nil || len(results) != 2 ||
		!results[0].Recovered || !results[1].Recovered || string(receipt.Data["profile"]) != `"default"` {
		t.Fatalf("batch receipt: %s", receipt.Data["imports"])
	}
	failure(t, run(batch...), "secret-mapping-exists")

	// A slot-attached field takes --resource; the default Profile now exists.
	single := []string{"onboard", "import-env", env, "--project=checkout", "--task=agent", "--field=DATABASE_URL", "--like=openai", "--key=DB"}
	failure(t, run(single...), "usage")
	db := importResult(t, confirm(append(single, "--resource="+resourceID)).Success(t))
	if db.Binding.ResourceID != resourceID || db.Binding.Alias != "login" || db.Recovered {
		t.Fatalf("slot-attached import: %+v", db)
	}
	if fake.Entries[at("DB")] != importValue {
		t.Fatal("slot-attached entry was not created")
	}

	// Without --profile the checkout's selection wins over default, and a slot
	// the Profile already maps needs no --resource.
	h.Run(t, "profile", "create", "staging", "--format=json").Success(t)
	h.Run(t, "profile", "select", "staging", "--format=json").Success(t)
	h.Run(t, "profile", "map-resource", "staging", "agent", "db", resourceID, "--format=json").Success(t)
	mapped := []string{"onboard", "import-env", env, "--project=checkout", "--task=agent", "--field=DATABASE_URL", "--like=openai", "--key=STAGING_DB"}
	staged := confirm(mapped).Success(t)
	if string(staged.Data["profile"]) != `"staging"` || importResult(t, staged).Binding.ResourceID != resourceID {
		t.Fatalf("import ignored the selected Profile or its Resource mapping: %s %s", staged.Data["profile"], staged.Data["imports"])
	}
}
