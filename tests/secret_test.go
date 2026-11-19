package tests

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/cli"
	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

const secretValue = "ctxferry-synthetic-entry-value"

func providerCommand(t *testing.T, h *testcli.Harness, services provider.Services, args ...string) testcli.Result {
	t.Helper()
	t.Chdir(h.Dir)
	var stdout, stderr bytes.Buffer
	code := cli.RunWithProviders(t.Context(), append([]string{"--data-dir", h.DataDir, "--format=json"}, args...), &stdout, &stderr, services)
	r := testcli.Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
	assertNoProviderData(t, r.Stdout+r.Stderr)
	return r
}

func assertNoProviderData(t *testing.T, output string) {
	t.Helper()
	if strings.Contains(output, secretValue) || strings.Contains(output, providertest.SyntheticSession) {
		t.Fatal("provider value or session escaped the provider boundary")
	}
}

func secretFacts(t *testing.T, r testcli.Result, expectedCode string) map[string]any {
	t.Helper()
	var facts map[string]any
	if expectedCode == "" {
		r.Success(t)
		var wire struct{ Data map[string]any }
		if err := json.Unmarshal([]byte(r.Stdout), &wire); err != nil {
			t.Fatal(err)
		}
		facts = wire.Data
	} else {
		failure(t, r, expectedCode)
		var wire struct {
			Error struct{ Details map[string]any }
		}
		if err := json.Unmarshal([]byte(r.Stdout), &wire); err != nil {
			t.Fatal(err)
		}
		facts = wire.Error.Details
	}
	return facts
}

func TestSecretBindingsAcrossDevices(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	git(t, a, "init")
	git(t, a, "commit", "--allow-empty", "-m", "Source checkout")
	mid := obj(t, bind(t, a, "--project=checkout"), "binding")["membershipId"].(string)
	cfg := a.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	cfgID := resourceString(t, cfg, "id")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)

	task := addTask(t, a, "check", mid, "unused", "shared", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential).Success(t)
	taskID := resourceString(t, task, "id")
	resource := addResource(t, a, "shared-db", "Integration service", "shared").Success(t)
	resourceID := resourceString(t, resource, "id")
	localID := resourceString(t, addResource(t, a, "local-db", "Local resource", "local-only").Success(t), "id")
	mapResource := func(h *testcli.Harness, task, id string) testcli.Result {
		return h.Run(t, "profile", "map-resource", "dev", task, "db", id, "--format=json")
	}
	failure(t, mapResource(a, taskID, localID), "invalid-reference")
	mapResource(a, taskID, resourceID).Success(t)

	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	ref := provider.Locator{ProjectID: "backend", Environment: "sandbox", Path: "/apps", Key: "BACKEND_DB"}
	near := ref
	near.Path = "/other"
	fake := &providertest.Fake{
		Entries: map[providertest.Reference]string{{Profile: p, Locator: ref}: secretValue},
		Methods: []provider.MethodCapabilities{
			{Method: "machine", MachineAuth: true, SessionExpiry: true},
			{Method: "interactive", OfflineRead: true},
		},
	}
	services := provider.Services{Adapters: map[string]provider.Adapter{"fake": fake}, Credentials: &providertest.MemoryStore{}}
	status := func(h *testcli.Harness, project, task string, flags ...string) testcli.Result {
		t.Helper()
		return providerCommand(t, h, services, append([]string{"secret", "status", task, "login", "--project", project}, flags...)...)
	}
	secretFacts(t, status(a, "checkout", taskID), "profile-required")
	failure(t, status(a, "checkout", taskID, "--profile=dev", "--profile=other"), "ambiguous-profile")
	missing := secretFacts(t, status(a, "checkout", taskID, "--profile=dev"), "binding-missing")
	if missing["task"] != "check" || missing["requirement"] != "login" || missing["resourceSlot"] != "db" {
		t.Fatal(missing)
	}
	a.Run(t, "profile", "select", "dev", "--format=json").Success(t)
	newBinding := func(h *testcli.Harness, task, alias string, loc provider.Locator, extra ...string) testcli.Result {
		return h.Run(t, append([]string{"secret", "bind", task, "login", "--profile=dev", "--alias", alias,
			"--provider=fake", "--provider-profile=team", "--method=machine", "--backend-project", loc.ProjectID,
			"--environment", loc.Environment, "--path", loc.Path, "--key", loc.Key, "--format=json"}, extra...)...)
	}
	failure(t, newBinding(a, taskID, "db", ref, "--entry-id=another-entry"), "invalid-secret-binding")
	first := newBinding(a, taskID, "db", ref).Success(t)
	bindingID := field(t, first, "binding")
	if retry := newBinding(a, taskID, "db", ref).Success(t); field(t, retry, "binding") != bindingID {
		t.Fatal("identical bind retried as a new binding")
	}
	if !strings.Contains(string(first.Data["binding"]), ref.Key) || strings.Contains(string(first.Data["binding"]), "DATABASE_URL") {
		t.Fatal(first)
	}

	login := []string{"provider", "login", "fake", "--provider-profile=team", "--method=machine"}
	unavailable := provider.Services{Adapters: services.Adapters, Credentials: &providertest.MemoryStore{Unavailable: true}}
	failure(t, providerCommand(t, a, unavailable, login...), "credential-storage-unavailable")
	failure(t, a.Run(t, append(login, "--format=json")...), "credential-storage-unavailable")
	if fake.LoginCount() != 0 || len(fake.Lookups()) != 0 {
		t.Fatal("provider called before secure storage and explicit status")
	}
	loginResult := providerCommand(t, a, services, login...).Success(t)
	var capabilities []provider.MethodCapabilities
	if err := json.Unmarshal(loginResult.Data["capabilities"], &capabilities); err != nil || !slices.Equal(capabilities, fake.Methods) {
		t.Fatal(loginResult)
	}

	// Reachability comes from a separate device-local Resource observation.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a.Run(t, "resource", "route", resourceID, "--endpoint", listener.Addr().String(), "--format=json").Success(t)
	a.Run(t, "resource", "check", resourceID, "--format=json").Success(t)
	facts := secretFacts(t, status(a, "checkout", taskID), "")
	if facts["bound"] != true || facts["authenticated"] != true || facts["entryRetrievable"] != true || facts["resourceReachable"] != true {
		t.Fatal(facts)
	}
	if got := fake.Lookups(); !slices.Equal(got, []providertest.Reference{{Profile: p, Locator: ref}}) {
		t.Fatal("status did not use exactly the selected locator", got)
	}

	root := a.Dir
	a.Dir = filepath.Join(root, "peer")
	if err := os.Mkdir(a.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	peerMembership := obj(t, bind(t, a, "--slug=peer"), "binding")["membershipId"].(string)
	a.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	peerTask := resourceString(t, addTask(t, a, "check", peerMembership, "unused", "shared", "--resource-slot="+dbSlot, "--credential-requirement="+dbCredential).Success(t), "id")
	mapResource(a, peerTask, resourceID).Success(t)
	missing = secretFacts(t, status(a, "peer", peerTask, "--profile=dev"), "binding-missing")
	if missing["resourceReachable"] != true || missing["entryRetrievable"] != nil || len(fake.Lookups()) != 1 {
		t.Fatal("same task name, delivery variable and Resource must not choose a binding", missing)
	}
	if r := providerCommand(t, a, services, "secret", "list", "--project=peer").Success(t); string(r.Data["bindings"]) != "[]" {
		t.Fatal("unmapped cross-Project binding listed", r)
	}
	reuse := func(h *testcli.Harness, task, binding string) testcli.Result {
		return h.Run(t, "secret", "bind", task, "login", "--profile=dev", "--binding", binding, "--format=json")
	}
	for _, requirement := range []string{
		strings.Replace(dbCredential, "runtime", "migration", 1),
		strings.Replace(dbCredential, "connection-string", "password", 1),
	} {
		bad := resourceString(t, addTask(t, a, "mismatch", peerMembership, "unused", "shared", "--resource-slot="+dbSlot, "--credential-requirement="+requirement).Success(t), "id")
		mapResource(a, bad, resourceID).Success(t)
		failure(t, reuse(a, bad, bindingID), "incompatible")
	}
	failure(t, status(a, "peer", "mismatch", "--profile=dev"), "ambiguous-task")
	otherResource := resourceString(t, addResource(t, a, "other-db", "Other service", "shared").Success(t), "id")
	mapResource(a, peerTask, otherResource).Success(t)
	failure(t, reuse(a, peerTask, bindingID), "incompatible")
	mapResource(a, peerTask, resourceID).Success(t)
	if r := reuse(a, peerTask, bindingID).Success(t); field(t, r, "binding") != bindingID {
		t.Fatal("reuse created a different SecretBinding", r)
	}
	failure(t, mapResource(a, peerTask, otherResource), "incompatible")
	a.Dir = root
	otherBinding := field(t, newBinding(a, taskID, "near-match", near).Success(t), "binding")
	facts = secretFacts(t, status(a, "checkout", taskID), "secret-unavailable")
	if facts["bound"] != true || facts["authenticated"] != true || facts["entryRetrievable"] != false || facts["resourceReachable"] != true {
		t.Fatal(facts)
	}
	if got := fake.Lookups(); got[len(got)-1].Locator != near {
		t.Fatal("near-match lookup used a different reference", got)
	}
	reuse(a, taskID, bindingID).Success(t)
	fake.SetState(p, provider.Locked)
	facts = secretFacts(t, status(a, "checkout", taskID), "authentication-required")
	if facts["authenticated"] != false || facts["entryRetrievable"] != nil || facts["resourceReachable"] != true {
		t.Fatal(facts)
	}
	fake.SetState(p, provider.Authenticated)

	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	before := len(fake.Lookups())
	if r := providerCommand(t, b, services, "secret", "list", "--project=peer").Success(t); !strings.Contains(string(r.Data["bindings"]), bindingID) || strings.Contains(string(r.Data["bindings"]), otherBinding) || len(fake.Lookups()) != before {
		t.Fatal("list should show only peer mappings and must not call Resolve", r)
	}
	facts = secretFacts(t, status(b, "peer", peerTask, "--profile=dev"), "authentication-required")
	if facts["bound"] != true || facts["authenticated"] != false || facts["entryRetrievable"] != nil || facts["resourceReachable"] != nil || len(fake.Lookups()) != before {
		t.Fatal("provider session or resource observation crossed devices", facts)
	}
	refusedTask(t, b.Run(t, "run", peerTask, "--project=peer", "--profile=dev", "--format=jsonl"), "credentials-required")

	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if after := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); after != head {
		t.Fatal("no-op sync rewrote credential records", head, after)
	}
	raw := git(t, a, "--git-dir", remote, "grep", "-h", ".", "ctxferry-v1")
	assertNoProviderData(t, raw)
	if strings.Contains(raw, localID) || strings.Contains(raw, listener.Addr().String()) {
		t.Fatal("local Resource or route published through a shared binding")
	}
	if err := filepath.WalkDir(a.DataDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		assertNoProviderData(t, string(data))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Two explicit edits to the same Profile produce a conflict before lookup.
	a.Run(t, "secret", "bind", taskID, "login", "--profile=dev", "--binding="+otherBinding, "--format=json").Success(t)
	newBinding(b, taskID, "peer-edit", near, "--project=checkout").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	secretFacts(t, status(b, "checkout", taskID, "--profile=dev"), "binding-conflict")
	if len(fake.Lookups()) != before {
		t.Fatal("conflicted binding must never call Resolve")
	}

	// An additional device with a valid baseline must reject remote records
	// carrying values, ambiguous mappings or broken references.
	testInvalidSecretSnapshots(t, a, remote, cfgID, bindingID, head)
}

func testInvalidSecretSnapshots(t *testing.T, a *testcli.Harness, remote, cfgID, bindingID, head string) {
	t.Helper()
	c := joinDevice(t, a, true)
	catalog := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, catalog)
	for _, tc := range []struct {
		name, id, code string
		edit           func(map[string]any)
	}{
		{"unknown auth field", bindingID, "unsupported-schema", func(p map[string]any) {
			p["providerProfile"].(map[string]any)["session"] = providertest.SyntheticSession
		}},
		{"duplicate credential mapping", cfgID, "incomplete", func(p map[string]any) {
			m := p["secretMappings"].([]any)[0]
			p["secretMappings"] = []any{m, m}
		}},
		{"missing Resource", bindingID, "incomplete", func(p map[string]any) {
			p["resourceId"] = "00000000-0000-4000-8000-000000000001"
		}},
		{"different credential purpose", bindingID, "incomplete", func(p map[string]any) {
			p["purpose"] = "migration"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "records/" + tc.id[:2] + "/" + tc.id + ".json"
			var record map[string]any
			if err := json.Unmarshal([]byte(git(t, a, "-C", catalog, "show", head+":"+path)), &record); err != nil {
				t.Fatal(err)
			}
			tc.edit(record["payload"].(map[string]any))
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			git(t, a, "-C", catalog, "checkout", head, "--", "records")
			if err := os.WriteFile(filepath.Join(catalog, filepath.FromSlash(path)), data, 0600); err != nil {
				t.Fatal(err)
			}
			git(t, a, "-C", catalog, "add", "records")
			git(t, a, "-C", catalog, "commit", "-m", tc.name)
			git(t, a, "-C", catalog, "push", "origin", "ctxferry-v1")
			r := c.Run(t, "sync", "--format=json")
			failure(t, r, tc.code)
			assertNoProviderData(t, r.Stdout+r.Stderr)
			list := c.Run(t, "secret", "list", "--project=peer", "--format=json").Success(t)
			if !strings.Contains(string(list.Data["bindings"]), bindingID) {
				t.Fatal("invalid snapshot replaced the visible catalog", list)
			}
			assertNoProviderData(t, string(list.Data["bindings"]))
		})
	}
}
