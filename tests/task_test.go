package tests

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

const dbSlot = `{"name":"db","kind":"database","stage":"dev"}`
const dbCredential = `{"name":"login","resourceSlot":"db","purpose":"runtime","type":"connection-string","env":"DATABASE_URL"}`

func taskFixture(t *testing.T) string {
	t.Helper()
	name := "task-fixture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("go", "build", "-o", path, "./testdata/task").CombinedOutput(); err != nil {
		t.Fatalf("build child fixture: %v\n%s", err, out)
	}
	return path
}

func installTaskFixture(t *testing.T, root, fixture, label string) {
	t.Helper()
	dir := filepath.Join(root, "app", "working")
	if err := os.MkdirAll(filepath.Join(dir, "tools"), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tools", filepath.Base(fixture)), raw, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture-location"), []byte(label), 0600); err != nil {
		t.Fatal(err)
	}
}

func addTask(t *testing.T, h *testcli.Harness, name, membership, program, storage string, flags ...string) testcli.Result {
	t.Helper()
	args := []string{"task", "add", name, "--membership", membership, "--program", program, "--storage", storage,
		"--purpose", "Run the fixture", "--format=json"}
	return h.Run(t, append(args, flags...)...)
}

type runEvent struct {
	Type, Data, Encoding, Origin string
	ExitCode                     *int
	Truncated                    *bool
	Error                        *struct{ Code, Origin string }
}

func runEvents(t *testing.T, r testcli.Result) []runEvent {
	t.Helper()
	if r.Stderr != "" || len(r.Stdout) > 64*1024 || !strings.HasSuffix(r.Stdout, "\n") {
		t.Fatalf("invalid JSONL transport: exit %d, %d stdout bytes, stderr %q", r.ExitCode, len(r.Stdout), r.Stderr)
	}
	var events []runEvent
	for _, line := range strings.Split(strings.TrimSuffix(r.Stdout, "\n"), "\n") {
		var event runEvent
		if len(line)+1 > 8*1024 || json.Unmarshal([]byte(line), &event) != nil {
			t.Fatal("invalid or oversized JSONL event")
		}
		switch event.Type {
		case "stdout", "stderr":
			if len(events) == 0 || events[0].Type != "start" {
				t.Fatal("output arrived before start")
			}
			if event.Encoding == "base64" {
				raw, err := base64.StdEncoding.DecodeString(event.Data)
				if err != nil {
					t.Fatal(err)
				}
				event.Data = string(raw)
			}
		case "start":
			if len(events) != 0 {
				t.Fatal("late start event")
			}
		case "result":
			if event.Truncated == nil {
				t.Fatal("result omitted truncated")
			}
		default:
			t.Fatal("unexpected event type", event.Type)
		}
		if len(events) > 0 && events[len(events)-1].Type == "result" {
			t.Fatal("event after result")
		}
		events = append(events, event)
	}
	if events[len(events)-1].Type != "result" {
		t.Fatal("missing result event")
	}
	return events
}

func refusedTask(t *testing.T, r testcli.Result, code string) {
	t.Helper()
	events := runEvents(t, r)
	if len(events) != 1 || r.ExitCode == 0 || events[0].Error == nil || events[0].Error.Code != code ||
		events[0].Error.Origin != "control-plane" || events[0].Origin != "control-plane" || events[0].ExitCode != nil || *events[0].Truncated {
		t.Fatalf("expected control-plane %s: %+v", code, r)
	}
}

func assertTaskOutput(t *testing.T, r testcli.Result, expected ...string) {
	t.Helper()
	events := runEvents(t, r)
	var out, stderr strings.Builder
	for _, event := range events {
		switch event.Type {
		case "stdout":
			out.WriteString(event.Data)
		case "stderr":
			stderr.WriteString(event.Data)
		}
	}
	var got []string
	result := events[len(events)-1]
	if r.ExitCode != 17 || result.ExitCode == nil || *result.ExitCode != 17 || result.Origin != "child" ||
		result.Error != nil || *result.Truncated || json.Unmarshal([]byte(out.String()), &got) != nil ||
		!slices.Equal(got, expected) || stderr.String() != "child diagnostic\n" {
		t.Fatalf("unexpected Task result: args %q, result %+v, raw %+v", got, result, r)
	}
}

func TestTaskExecution(t *testing.T) {
	fixture := taskFixture(t)
	program := "tools/" + filepath.Base(fixture)
	h := gitHarness(t)
	h.ExtraEnv = []string{"GH_TOKEN=synthetic", "BW_SESSION=synthetic", "GIT_CONFIG_COUNT=0",
		"INFISICAL_TOKEN=synthetic", "INFISICAL_CLIENT_ID=synthetic", "INFISICAL_CLIENT_SECRET=synthetic",
		"CTXFERRY_INFISICAL_ACCESS_TOKEN=synthetic"}
	root := h.Dir
	installTaskFixture(t, root, fixture, "primary")
	h.Dir = filepath.Join(root, "app")
	binding := obj(t, bind(t, h, "--slug=demo"), "binding")
	mid, repo := binding["membershipId"].(string), binding["repositoryId"].(string)
	register := func(name string, extra ...string) testcli.Response {
		t.Helper()
		return addTask(t, h, name, mid, program, "local-only", append([]string{"--working-directory=working"}, extra...)...).Success(t)
	}
	args := []string{"two words", "", `"quoted"`, `$(write-marker); & | *`, "語😀", "--literal"}
	flags := []string{"--arg=inspect", "--input=fixture-location", "--platform=" + runtime.GOOS}
	for _, arg := range args {
		flags = append(flags, "--arg="+arg)
	}
	task := register("check-fixture", flags...)
	id := resourceString(t, task, "id")
	if inspected := h.Run(t, "task", "inspect", id, "--format=json").Success(t); string(inspected.Data["inputs"]) != `["fixture-location"]` {
		t.Fatal(inspected)
	}
	if list := h.Run(t, "task", "list", "--format=json").Success(t); !strings.Contains(string(list.Data["tasks"]), "Run the fixture") {
		t.Fatal(list)
	}
	show := h.Run(t, "context", "show", "--format=json").Success(t)
	search := h.Run(t, "context", "search", "CHECK fixture", "--format=json").Success(t)
	for _, result := range []testcli.Response{show, search} {
		if !strings.Contains(string(result.Data["tasks"]), id) || strings.Contains(string(result.Data["tasks"]), "argv") {
			t.Fatal(result)
		}
	}
	failure(t, h.Run(t, "context", "get", id, "--format=json"), "document-not-found")
	caller := filepath.Join(h.Dir, "caller")
	if err := os.MkdirAll(caller, 0700); err != nil {
		t.Fatal(err)
	}
	h.Dir = caller
	run := func(name string, flags ...string) testcli.Result {
		t.Helper()
		return h.Run(t, append([]string{"run", name, "--format=jsonl"}, flags...)...)
	}
	assertTaskOutput(t, run("check-fixture"), append([]string{"primary"}, args...)...)
	text := h.Run(t, "run", "check-fixture")
	if text.ExitCode != 17 || !strings.Contains(text.Stdout, "primary") || text.Stderr != "child diagnostic\n" {
		t.Fatal(text)
	}
	register("credential-task", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential, "--arg=marker")
	refusedTask(t, run("credential-task"), "credentials-required")
	if _, err := os.Stat(filepath.Join(root, "app", "working", "task-ran")); !os.IsNotExist(err) {
		t.Fatal("credential-dependent child launched", err)
	}
	inspect := h.Run(t, "task", "inspect", "credential-task", "--format=json").Success(t)
	if string(inspect.Data["resourceSlots"]) != "["+dbSlot+"]" || string(inspect.Data["credentialRequirements"]) != "["+dbCredential+"]" {
		t.Fatal(inspect)
	}
	register("other-platform", "--platform=unavailable", "--arg=marker")
	refusedTask(t, run("other-platform"), "platform-unsupported")
	if _, err := os.Stat(filepath.Join(root, "app", "working", "task-ran")); !os.IsNotExist(err) {
		t.Fatal("unsupported child launched", err)
	}
	register("flood", "--arg=flood")
	noisy := run("flood")
	events := runEvents(t, noisy)
	if result := events[len(events)-1]; noisy.ExitCode != 23 || len(events) < 3 || !*result.Truncated || result.ExitCode == nil || *result.ExitCode != 23 {
		t.Fatalf("wrong flood result: %d, %+v", noisy.ExitCode, result)
	}
	usage := h.Run(t, "run", "flood", "--format=json")
	failure(t, usage, "usage")
	if usage.ExitCode != 2 || !strings.Contains(usage.Stderr, "jsonl") || !strings.Contains(usage.Stdout, "control-plane") {
		t.Fatal(usage)
	}
	refusedTask(t, h.Run(t, "run", "--unknown-flag", "--format=jsonl"), "usage")
	refusedTask(t, run("missing-task"), "task-not-found")
	addTask(t, h, "bad-program", mid, "ctxferry-fixture-does-not-exist", "local-only").Success(t)
	refusedTask(t, run("bad-program"), "launch-failed")
	failure(t, addTask(t, h, "bad-path", mid, program, "local-only", "--working-directory=../outside"), "invalid-task")
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Dir(root), filepath.Join(root, "app", "escape")); err != nil {
			t.Fatal(err)
		}
		addTask(t, h, "escape", mid, program, "local-only", "--working-directory=escape").Success(t)
		refusedTask(t, run("escape"), "invalid-working-directory")
	}

	wt := filepath.Join(t.TempDir(), "linked worktree")
	h.Dir = root
	git(t, h, "worktree", "add", "-b", "issue13-test", wt)
	installTaskFixture(t, wt, fixture, "worktree")
	h.Dir = caller
	refusedTask(t, run("check-fixture"), "ambiguous-checkout")
	assertTaskOutput(t, run("check-fixture", "--checkout", wt), append([]string{"worktree"}, args...)...)
	refusedTask(t, run("check-fixture", "--checkout", caller), "checkout-not-found")
	h.Dir = filepath.Join(wt, "app")
	assertTaskOutput(t, run("check-fixture", "--checkout", root), append([]string{"primary"}, args...)...)
	clone := filepath.Join(t.TempDir(), "another clone")
	git(t, h, "clone", root, clone)
	installTaskFixture(t, clone, fixture, "clone")
	refusedTask(t, run("check-fixture", "--checkout", clone), "checkout-not-found")
	h.Dir = filepath.Join(clone, "app")
	bind(t, h, "--project=demo", "--repository="+repo)
	assertTaskOutput(t, run("check-fixture", "--checkout", clone), append([]string{"clone"}, args...)...)
	other := filepath.Join(t.TempDir(), "different repository")
	git(t, h, "init", other)
	installTaskFixture(t, other, fixture, "wrong repository")
	h.Dir = filepath.Join(other, "app")
	bind(t, h, "--project=demo")
	refusedTask(t, run("check-fixture", "--checkout", other), "checkout-not-found")
	assertTaskOutput(t, run("check-fixture", "--checkout", root), append([]string{"primary"}, args...)...)
}

func TestTaskProfileSyncAndValidation(t *testing.T) {
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
	cfg := a.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	setConfig(t, a, "profile", "parallelism", "3", "--profile=dev")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	git(t, b, "init")
	installTaskFixture(t, b.Dir, fixture, "device B")
	b.Dir = filepath.Join(b.Dir, "app")
	if local := obj(t, bind(t, b, "--project=checkout", "--repository="+repo), "binding"); local["membershipId"] != mid {
		t.Fatal("Device B did not use the synced membership", local)
	}
	b.Run(t, "task", "list", "--format=json").Success(t)

	task := addTask(t, a, "shared-task", mid, program, "shared", "--resource-slot="+dbSlot,
		"--working-directory=working", "--arg=inspect").Success(t)
	taskID := resourceString(t, task, "id")
	localTask := addTask(t, a, "local-task", mid, program, "local-only", "--resource-slot="+dbSlot).Success(t)
	local := addResource(t, a, "local-db", "A local dependency", "local-only").Success(t)
	shared := addResource(t, a, "shared-db", "A shared dependency", "shared").Success(t)
	resourceID := resourceString(t, shared, "id")
	wrong := a.Run(t, "resource", "add", "--identity=wrong-stage", "--purpose=Other stage", "--kind=database",
		"--stage=production", "--storage=shared", "--format=json").Success(t)
	mapResource := func(name, slot, resource string) testcli.Result {
		t.Helper()
		return a.Run(t, "profile", "map-resource", "dev", name, slot, resource, "--format=json")
	}
	failure(t, mapResource("shared-task", "db", resourceString(t, local, "id")), "invalid-reference")
	failure(t, mapResource("local-task", "db", resourceID), "invalid-reference")
	failure(t, mapResource("shared-task", "other", resourceID), "incompatible")
	failure(t, mapResource("shared-task", "db", resourceString(t, wrong, "id")), "incompatible")
	a.Run(t, "profile", "select", "dev", "--format=json").Success(t)
	refusedTask(t, a.Run(t, "run", taskID, "--format=jsonl"), "binding-missing")
	mapResource("shared-task", "db", resourceID).Success(t)
	assertTaskOutput(t, a.Run(t, "run", taskID, "--format=jsonl"), "device A")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	failure(t, b.Run(t, "task", "inspect", resourceString(t, localTask, "id"), "--format=json"), "task-not-found")
	refusedTask(t, b.Run(t, "run", taskID, "--format=jsonl"), "profile-required")
	assertTaskOutput(t, b.Run(t, "run", taskID, "--profile=dev", "--format=jsonl"), "device B")
	b.Run(t, "profile", "select", "dev", "--format=json").Success(t)
	effectiveField(t, explain(t, b), "parallelism", "profile", float64(3))
	assertTaskOutput(t, b.Run(t, "run", taskID, "--format=jsonl"), "device B")

	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if after := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); after != head {
		t.Fatal("no-op sync rewrote Task or Profile records", after, head)
	}
	catalog := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, catalog)
	profileID := resourceString(t, cfg, "id")
	for _, tc := range []struct {
		name, id string
		corrupt  func(map[string]any)
	}{
		{"orphan membership", taskID, func(r map[string]any) {
			r["payload"].(map[string]any)["membershipId"] = "00000000-0000-4000-8000-000000000001"
		}},
		{"duplicate mapping", profileID, func(r map[string]any) {
			p := r["payload"].(map[string]any)
			mapping := p["resourceMappings"].([]any)[0]
			p["resourceMappings"] = []any{mapping, mapping}
		}},
		{"unknown task slot", profileID, func(r map[string]any) {
			r["payload"].(map[string]any)["resourceMappings"].([]any)[0].(map[string]any)["slot"] = "missing"
		}},
		{"incompatible resource", resourceID, func(r map[string]any) {
			r["payload"].(map[string]any)["stage"] = "production"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("records/%s/%s.json", tc.id[:2], tc.id)
			original := git(t, a, "-C", catalog, "show", head+":"+path)
			var record map[string]any
			if err := json.Unmarshal([]byte(original), &record); err != nil {
				t.Fatal(err)
			}
			tc.corrupt(record)
			wire, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			git(t, a, "-C", catalog, "checkout", head, "--", "records")
			if err := os.WriteFile(filepath.Join(catalog, filepath.FromSlash(path)), wire, 0600); err != nil {
				t.Fatal(err)
			}
			git(t, a, "-C", catalog, "add", "records")
			git(t, a, "-C", catalog, "commit", "-m", tc.name)
			git(t, a, "-C", catalog, "push", "origin", "ctxferry-v1")
			failure(t, b.Run(t, "sync", "--format=json"), "incomplete")
			assertTaskOutput(t, b.Run(t, "run", taskID, "--format=jsonl"), "device B")
		})
	}
}
