package tests

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTaskShortcutsInferMembershipAndResolveNames(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, _ := syncHarness(t, &metadata)
	h.Run(t, "init", "--format=json").Success(t)
	git(t, h, "init")
	mid := obj(t, bind(t, h, "--project=checkout"), "binding")["membershipId"].(string)

	task := h.Run(t, "task", "add", "check", "--purpose=Run the check", "--storage=shared",
		"--slot=db:database:dev", "--credential=login:db:runtime:connection-string:DATABASE_URL",
		"--format=json", "--", "tools/check", "--verbose", "two words").Success(t)
	if resourceString(t, task, "membershipId") != mid || resourceString(t, task, "program") != "tools/check" ||
		string(task.Data["argv"]) != `["--verbose","two words"]` ||
		string(task.Data["resourceSlots"]) != "["+dbSlot+"]" || string(task.Data["credentialRequirements"]) != "["+dbCredential+"]" {
		t.Fatal(task)
	}
	failure(t, h.Run(t, "task", "add", "both", "--purpose=x", "--storage=local-only", "--program=a", "--format=json", "--", "b"), "usage")
	failure(t, h.Run(t, "task", "add", "bad", "--purpose=x", "--storage=local-only", "--slot=db:database", "--format=json", "--", "b"), "usage")

	updated := h.Run(t, "task", "update", "check", "--if-revision", resourceString(t, task, "revision"), "--format=json", "--", "tools/other").Success(t)
	if resourceString(t, updated, "program") != "tools/other" || string(updated.Data["argv"]) != "[]" {
		t.Fatal(updated)
	}

	resourceID := resourceString(t, addResource(t, h, "primary-db", "Integration service", "shared").Success(t), "id")
	detail := h.Run(t, "resource", "inspect", "primary-db", "--format=json").Success(t)
	if obj(t, detail, "resource")["id"] != resourceID {
		t.Fatal(detail)
	}
	h.Run(t, "resource", "route", "primary-db", "--endpoint=127.0.0.1:5432", "--format=json").Success(t)
	failure(t, h.Run(t, "resource", "inspect", "absent-db", "--format=json"), "resource-not-found")
	h.Run(t, "profile", "create", "dev", "--format=json").Success(t)
	mapping := h.Run(t, "profile", "map-resource", "dev", "check", "db", "primary-db", "--format=json").Success(t)
	if resourceString(t, mapping, "resourceId") != resourceID {
		t.Fatal(mapping)
	}
}

func TestTaskAddRefusesAmbiguousMembership(t *testing.T) {
	h := gitHarness(t)
	mid := obj(t, bind(t, h, "--slug=demo"), "binding")["membershipId"].(string)
	// A second Repository membership leaves nothing to infer from first its checkouts.
	other := t.TempDir()
	first := h.Dir
	h.Dir = other
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "initial")
	bind(t, h, "--project=demo")
	h.Dir = t.TempDir()
	r := h.Run(t, "task", "add", "check", "--project=demo", "--purpose=x", "--storage=local-only", "--format=json", "--", "make")
	failure(t, r, "usage")
	if !strings.Contains(r.Stdout, "--membership") {
		t.Fatal(r)
	}
	h.Dir = first
	task := h.Run(t, "task", "add", "check", "--purpose=x", "--storage=local-only", "--format=json", "--", "make").Success(t)
	if resourceString(t, task, "membershipId") != mid {
		t.Fatal(task)
	}
}

func TestContextPutDefaultsAndGetByTitle(t *testing.T) {
	h := gitHarness(t)
	bind(t, h, "--slug=demo")
	put := h.Run(t, "context", "put", "--title", "Release notes", "--body", "Ship on Fridays.", "--format=json").Success(t)
	doc := obj(t, put, "document")
	policy := doc["policy"].(map[string]any)
	if doc["purpose"] != "note" || doc["source"] != "ctxferry CLI" || policy["storage"] != "local-only" || policy["agentDisclosure"] != "full" {
		t.Fatal(put)
	}
	got := h.Run(t, "context", "get", "Release notes", "--format=json").Success(t)
	if body := obj(t, got, "document")["body"]; body != "Ship on Fridays." {
		t.Fatal(got)
	}
	failure(t, h.Run(t, "context", "get", "Absent", "--format=json"), "document-not-found")
	h.Run(t, "context", "put", "--title", "Release notes", "--body", "Second copy", "--format=json").Success(t)
	failure(t, h.Run(t, "context", "get", "Release notes", "--format=json"), "ambiguous-document")
	failure(t, h.Run(t, "context", "put", "--title", "No body", "--format=json"), "usage")
	failure(t, h.Run(t, "context", "put", "--title", "Two bodies", "--body", "a", "--body-file", "-", "--format=json"), "usage")

	// Replacement replaces every field and takes no defaults.
	var receipt struct{ ID, Revision string }
	raw, _ := json.Marshal(doc)
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	failure(t, h.Run(t, "context", "put", receipt.ID, "--if-revision", receipt.Revision, "--title", "Release notes", "--body", "x", "--format=json"), "invalid-document")
}
