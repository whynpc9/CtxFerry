package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/provider/providertest"
	"github.com/whynpc9/CtxFerry/internal/store"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

const importValue = "placeholder-only"

type envImportProvider struct {
	*providertest.Fake
	afterCreate error
	calls       int
}

func (p *envImportProvider) Import(ctx context.Context, profile provider.Profile, session []byte, locator provider.Locator, value []byte) error {
	p.calls++
	if err := p.Fake.Import(ctx, profile, session, locator, value); err != nil {
		return err
	}
	return p.afterCreate
}

func TestOnboardingEnvImport(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "Source checkout")
	membership := obj(t, bind(t, h, "--project=checkout"), "binding")["membershipId"].(string)
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	requirement := strings.Replace(dbCredential, "DATABASE_URL", "CTXFERRY_IMPORT_TEST", 1)
	taskID := resourceString(t, addTask(t, h, "check", membership, "unused", "shared", "--resource-slot="+dbSlot,
		"--credential-requirement="+requirement).Success(t), "id")
	localID := resourceString(t, addResource(t, h, "local-db", "Local Resource", "local-only").Success(t), "id")
	resourceID := resourceString(t, addResource(t, h, "shared-db", "Integration service", "shared").Success(t), "id")
	enableSync(t, h)
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)

	selected := filepath.Join(h.Dir, ".env")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const plain = "# Selected file\r\nexport CTXFERRY_IMPORT_TEST=\"" + importValue + "\" # test fixture\r\nOTHER_NAME='single quoted'\r\n"
	write(selected, plain)
	write(filepath.Join(h.Dir, ".env.neighbour"), "NEIGHBOUR_SECRET=neighbour-ignored")
	malicious := filepath.Join(h.Dir, ".env.script")
	write(malicious, "CTXFERRY_IMPORT_TEST="+importValue+"\nSCRIPT_VALUE=$(touch must-not-run)\n")

	p := provider.Profile{Provider: "fake", Name: "team", Method: "machine"}
	locator := provider.Locator{ProjectID: "sandbox", Environment: "dev", Path: "/isolated", Key: "EXACT_ENTRY"}
	ref := providertest.Reference{Profile: p, Locator: locator}
	fake := &envImportProvider{Fake: &providertest.Fake{Methods: []provider.MethodCapabilities{
		{Method: "machine", CreateImport: true}, {Method: "readonly"},
	}}}
	services := provider.Services{Adapters: map[string]provider.Adapter{p.Provider: fake}, Credentials: &providertest.MemoryStore{}}
	run := func(args ...string) testcli.Result {
		t.Helper()
		r := providerCommand(t, h, services, args...)
		for _, hidden := range []string{importValue, "single quoted", "neighbour-ignored", "NEIGHBOUR_SECRET", "must-not-run"} {
			if strings.Contains(r.Stdout+r.Stderr, hidden) {
				t.Fatal("source content outside field names escaped import")
			}
		}
		return r
	}
	scan := run("onboard", "scan-env", selected, "--project=checkout").Success(t)
	if string(scan.Data["fields"]) != `["CTXFERRY_IMPORT_TEST","OTHER_NAME"]` || !bytes.Contains(scan.Data["candidates"], []byte(taskID)) {
		t.Fatal("selected env did not propose the expected fields and Task")
	}
	failure(t, run("onboard", "scan-env", malicious, "--project=checkout"), "invalid-env-file")
	args := func(project, task, resource, key, method, path string) []string {
		return []string{"onboard", "import-env", path, "--project=" + project, "--profile=dev", "--task=" + task,
			"--requirement=login", "--field=CTXFERRY_IMPORT_TEST", "--resource=" + resource, "--alias=db",
			"--provider=fake", "--provider-profile=team", "--method=" + method, "--backend-project=sandbox",
			"--environment=dev", "--path=/isolated", "--key=" + key}
	}
	main := args("checkout", taskID, resourceID, locator.Key, p.Method, selected)
	failure(t, run(args("checkout", taskID, resourceID, locator.Key, p.Method, malicious)...), "invalid-env-file")
	if _, err := os.Stat(filepath.Join(h.Dir, "must-not-run")); !os.IsNotExist(err) {
		t.Fatal("script marker was created or could not be checked")
	}
	failure(t, run(args("checkout", taskID, localID, locator.Key, p.Method, selected)...), "incompatible")
	failure(t, run(args("checkout", taskID, resourceID, locator.Key, "readonly", selected)...), "import-unsupported")
	failure(t, run(append(main, "--provider=missing")...), "provider-unsupported")
	failure(t, run(append(main, "--confirm=stale")...), "env-preview-changed")
	preview := func(in []string) string { return resourceString(t, run(in...).Success(t), "preview") }
	confirm := func(in []string) testcli.Result { return run(append(in, "--confirm="+preview(in))...) }
	failure(t, confirm(main), "authentication-required")
	if fake.calls != 0 || len(fake.Lookups()) != 0 {
		t.Fatal("provider was touched before authenticated confirmation")
	}
	run("provider", "login", p.Provider, "--method="+p.Method, "--provider-profile="+p.Name).Success(t)
	fake.afterCreate = errors.New("redact provider error: " + importValue)
	failure(t, confirm(main), "env-provider-outcome-unknown")
	if fake.Entries[ref] != importValue {
		t.Fatal("fake provider did not receive exactly the selected value")
	}
	fake.afterCreate = nil
	missing := func(project, task string) {
		t.Helper()
		list := run("secret", "list", "--project="+project).Success(t)
		if string(list.Data["bindings"]) != "[]" {
			t.Fatal("an unsuccessful import exposed a catalog binding")
		}
		failure(t, run("secret", "status", task, "login", "--project="+project, "--profile=dev"), "binding-missing")
	}
	missing("checkout", taskID)
	failure(t, confirm(main), "env-provider-collision")
	failure(t, confirm(append(main, "--field=OTHER_NAME", "--recover")), "env-recovery-mismatch")
	missing("checkout", taskID)
	recovered := confirm(append(main, "--recover")).Success(t)
	var binding struct{ ID, ResourceID, Purpose, Type string }
	if err := json.Unmarshal(recovered.Data["binding"], &binding); err != nil || binding.ID == "" ||
		binding.ResourceID != resourceID || binding.Purpose != "runtime" || binding.Type != "connection-string" ||
		string(recovered.Data["recovered"]) != "true" || fake.calls != 2 {
		t.Fatal("recovery did not produce the selected binding without another write")
	}
	failure(t, run(main...), "secret-mapping-exists")

	peerDir := filepath.Join(h.Dir, "peer")
	if err := os.Mkdir(peerDir, 0700); err != nil {
		t.Fatal(err)
	}
	h.Dir = peerDir
	peerMembership := obj(t, bind(t, h, "--slug=peer"), "binding")["membershipId"].(string)
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	peerTask := resourceString(t, addTask(t, h, "check", peerMembership, "unused", "shared", "--resource-slot="+dbSlot,
		"--credential-requirement="+requirement).Success(t), "id")
	missing("peer", peerTask)
	peer := args("peer", peerTask, resourceID, "PEER_ENTRY", p.Method, selected)
	// Inject a failure after the binding insert to exercise rollback of all catalog records.
	db, err := store.Open(t.Context(), h.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TRIGGER env_test_fail BEFORE UPDATE ON configurations BEGIN SELECT RAISE(ABORT, 'fixture failure'); END")
	failure(t, confirm(peer), "env-catalog-commit-failed")
	missing("peer", peerTask)
	exec("DROP TRIGGER env_test_fail")
	peerBinding := confirm(append(peer, "--recover")).Success(t)
	if bytes.Contains(peerBinding.Data["binding"], []byte(binding.ID)) || fake.calls != 3 {
		t.Fatal("same-name field reused another Project's binding or retried a provider write")
	}

	// A fresh target follows the regular create-and-commit path.
	h.Run(t, "profile", "create", "second", "--format=json").Success(t)
	second := confirm(append(args("peer", peerTask, resourceID, "SECOND_ENTRY", p.Method, selected), "--profile=second")).Success(t)
	if string(second.Data["recovered"]) != "false" || fake.calls != 4 {
		t.Fatal("regular import did not commit")
	}
	if contents, err := os.ReadFile(selected); err != nil || string(contents) != plain {
		t.Fatal("selected source file changed")
	}

	h.Run(t, "sync", "--format=json").Success(t)
	paths := strings.Fields(git(t, h, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1", "records"))
	var records strings.Builder
	for _, path := range paths {
		records.WriteString(git(t, h, "--git-dir", remote, "show", "ctxferry-v1:"+path))
	}
	published := records.String()
	for _, forbidden := range []string{importValue, "single quoted", "neighbour-ignored", providertest.SyntheticSession} {
		if strings.Contains(published, forbidden) {
			t.Fatal("a value or session was published")
		}
	}
	if !strings.Contains(published, locator.Key) || !strings.Contains(published, binding.ID) {
		t.Fatal("confirmed binding metadata did not sync")
	}
	err = filepath.WalkDir(h.DataDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(path)
		if bytes.Contains(data, []byte(importValue)) {
			t.Fatal("a credential value was persisted in application data")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
