package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/store"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func onboarding(t *testing.T, h *testcli.Harness, args ...string) testcli.Response {
	t.Helper()
	result := h.Run(t, append([]string{"onboard", "--format=json"}, args...)...)
	for _, forbidden := range []string{"onboarding-fixture-private-value", "supplied by the test harness", "unrelated-discovery-sentinel"} {
		if strings.Contains(result.Stdout+result.Stderr, forbidden) {
			t.Fatal("onboarding printed note content or unrelated discovery data")
		}
	}
	return result.Success(t)
}

func fixtureNote(t *testing.T, dir, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "onboarding", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertOnboardingStep(t *testing.T, r testcli.Response, step string) {
	t.Helper()
	if string(r.Data["step"]) != `"`+step+`"` {
		t.Fatalf("expected onboarding step %q, got %s", step, r.Data["step"])
	}
}

func TestOnboardingResumesBeforeBindingAndFinishesWithDoctor(t *testing.T) {
	h := testcli.New(t, binary)
	h.ExtraEnv = []string{"DONT_READ=unrelated-discovery-sentinel"}
	unselected := fixtureNote(t, h.Dir, "private.md")
	assertOnboardingStep(t, onboarding(t, h, "status"), "not-started")
	if _, err := os.Stat(h.DataDir); !os.IsNotExist(err) {
		t.Fatal("status initialized the catalog")
	}
	started := onboarding(t, h, "start", ".")
	assertOnboardingStep(t, started, "preview")
	catalog := store.Path(h.DataDir)
	before, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	// Every invocation runs a fresh CLI process, including from another directory.
	checkout := h.Dir
	h.Dir = t.TempDir()
	resumed := onboarding(t, h, "status")
	previewID := field(t, resumed, "preview")
	if previewID != field(t, started, "preview") || obj(t, resumed, "preview")["newSlug"] != "checkout" {
		t.Fatal("preview was not resumed for the selected checkout")
	}
	after, err := os.ReadFile(catalog)
	if err != nil || string(before) != string(after) {
		t.Fatal("preview changed the catalog", err)
	}
	if r := h.Run(t, "project", "list", "--format=json").Success(t); string(r.Data["projects"]) != "[]" {
		t.Fatal("preview created a Project")
	}
	failure(t, h.Run(t, "onboard", "finish", "--format=json"), "onboarding-step")
	failure(t, h.Run(t, "onboard", "bind", "--preview=wrong", "--format=json"), "preview-changed")
	bound := onboarding(t, h, "bind", "--preview", previewID)
	assertOnboardingStep(t, bound, "notes")
	if len(string(bound.Data["projectId"])) < 3 {
		t.Fatal("no Project bound")
	}
	assertOnboardingStep(t, onboarding(t, h, "status"), "notes")

	completed := onboarding(t, h, "finish")
	assertOnboardingStep(t, completed, "complete")
	var report app.DoctorReport
	if err := json.Unmarshal(completed.Data["doctor"], &report); err != nil {
		t.Fatal(err)
	}
	if report.Catalog.Status != "available" || report.DirectoryBinding.Status != "available" ||
		report.DirectoryBinding.Source != "path-binding" || report.LastSync.State != "disabled" ||
		report.DirectoryBinding.ProjectID == "" || report.ObservedAt.IsZero() {
		t.Fatalf("finish did not diagnose the bound checkout: %+v", report)
	}
	assertOnboardingStep(t, onboarding(t, h, "status"), "complete")
	h.Dir = checkout
	if r := doctor(t, h); r.DirectoryBinding != report.DirectoryBinding || r.PathResolution.Directory != report.PathResolution.Directory {
		t.Fatal("standalone doctor disagrees about the bound checkout")
	}
	if docs := h.Run(t, "context", "show", "--format=json").Success(t).Data["documents"]; string(docs) != "[]" {
		t.Fatal("an unselected note was imported")
	}
	if files, err := os.ReadDir(h.Dir); err != nil || len(files) != 1 || files[0].Name() != filepath.Base(unselected) {
		t.Fatal("onboarding modified the checkout", files, err)
	}
}

func TestOnboardingRemoteIsOnlyACandidateAndPreviewCanGoStale(t *testing.T) {
	h := gitHarness(t)
	existing := bind(t, h, "--slug=existing", "--remote=https://github.com/example/project.git")
	h.Dir = t.TempDir()
	git(t, h, "init")
	git(t, h, "remote", "add", "origin", "git@github.com:Example/Project.git")
	git(t, h, "remote", "add", "other", "https://unrelated-discovery-sentinel@github.com/example/other")
	preview := obj(t, onboarding(t, h, "start", "."), "preview")
	if preview["source"] != "choose-project" || preview["id"] != nil || len(preview["candidates"].([]any)) != 1 {
		t.Fatalf("remote should require explicit selection: %+v", preview)
	}
	failure(t, h.Run(t, "project", "inspect", "--format=json"), "project-unbound")
	selected := onboarding(t, h, "status", "--project=existing")
	id := field(t, selected, "preview")
	git(t, h, "remote", "set-url", "origin", "https://github.com/example/changed")
	failure(t, h.Run(t, "onboard", "bind", "--project=existing", "--preview", id, "--format=json"), "preview-changed")
	git(t, h, "remote", "set-url", "origin", "https://github.com/example/project")
	bound := onboarding(t, h, "bind", "--project=existing", "--preview", id)
	if string(bound.Data["projectId"]) != `"`+field(t, existing, "project")+`"` {
		t.Fatal("previously previewed Project was not bound")
	}
	var bindings []app.Binding
	if err := json.Unmarshal(inspect(t, h).Data["bindings"], &bindings); err != nil || len(bindings) != 2 {
		t.Fatal("expected both checkout bindings", err)
	}
	if bindings[0].RepositoryID == "" || bindings[0].RepositoryID != bindings[1].RepositoryID {
		t.Fatal("confirmed remote candidate did not reuse its Repository")
	}
}

func TestOnboardingNotesRespectBothPoliciesThroughSync(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	private := fixtureNote(t, t.TempDir(), "private.md")
	shared := fixtureNote(t, t.TempDir(), "shared.md")
	scanned := onboarding(t, h, "scan", private)
	if string(scanned.Data["findings"]) != `[{"type":"credential-like-field","field":"API_TOKEN"}]` {
		t.Fatal(scanned)
	}
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("API_TOKEN=onboarding-fixture-private-value"), 0600); err != nil {
		t.Fatal(err)
	}
	failure(t, h.Run(t, "onboard", "scan", envFile, "--format=json"), "invalid-note")
	started := onboarding(t, h, "start", ".")
	onboarding(t, h, "bind", "--preview", field(t, started, "preview"))
	failure(t, h.Run(t, "onboard", "import", private, "--title=private", "--format=json"), "invalid-policy")
	localArgs := []string{"import", private, "--title=local-only-title", "--storage=local-only", "--disclosure=reference"}
	local := onboarding(t, h, localArgs...)
	localID := local.Data["documentId"]
	if string(local.Data["findings"]) != string(scanned.Data["findings"]) ||
		obj(t, local, "policy")["storage"] != "local-only" {
		t.Fatal("scan changed or hid the selected policy")
	}
	if importAgain := onboarding(t, h, localArgs...); string(importAgain.Data["documentId"]) != string(localID) {
		t.Fatal("a retry created a duplicate document")
	}

	enableSync(t, h)
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	firstTree := git(t, h, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1")
	if strings.Contains(firstTree, "objects/") {
		t.Fatal("initial publish uploaded a local-only note object")
	}

	published := onboarding(t, h, "import", shared, "--title=Shared fixture", "--storage=shared", "--disclosure=summary", "--summary=Safe summary")
	if string(published.Data["findings"]) != string(scanned.Data["findings"]) ||
		obj(t, published, "policy")["storage"] != "shared" {
		t.Fatal("advisory finding overrode a shared classification")
	}
	var sharedID string
	if err := json.Unmarshal(published.Data["documentId"], &sharedID); err != nil {
		t.Fatal(err)
	}
	retrieved := h.Run(t, "context", "get", sharedID, "--format=json").Success(t)
	doc := obj(t, retrieved, "document")
	if doc["summary"] != "Safe summary" || doc["body"] != nil {
		t.Fatal("context query did not apply summary-only disclosure")
	}

	h.Run(t, "sync", "--format=json").Success(t)
	tree := strings.Fields(git(t, h, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1"))
	var objectCount int
	for _, path := range tree {
		raw := git(t, h, "--git-dir", remote, "show", "ctxferry-v1:"+path)
		for _, forbidden := range []string{"onboarding-fixture-private-value", "local-only-title", strings.Trim(string(localID), `"`), private, shared, "onboarding_notes"} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("published tree contains private onboarding data at %s", path)
			}
		}
		if strings.HasPrefix(path, "objects/") {
			objectCount++
			if !strings.Contains(raw, "supplied by the test harness") {
				t.Fatal("wrong published note body")
			}
		}
	}
	if objectCount != 1 {
		t.Fatalf("expected one shared note object, got %d", objectCount)
	}

	state := onboarding(t, h, "status")
	var ids []string
	if err := json.Unmarshal(state.Data["documentIds"], &ids); err != nil || len(ids) != 2 || ids[1] != sharedID {
		t.Fatal("saved receipts do not match the imported notes", err)
	}
	assertOnboardingStep(t, onboarding(t, h, "finish"), "complete")
	failure(t, h.Run(t, "onboard", "import", shared, "--title=Shared", "--storage=shared", "--disclosure=reference", "--format=json"), "onboarding-step")
}
