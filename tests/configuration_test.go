package tests

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func setConfig(t *testing.T, h *testcli.Harness, layer, field, value string, flags ...string) testcli.Response {
	t.Helper()
	return h.Run(t, append([]string{"config", "set", field, value, "--layer", layer, "--format=json"}, flags...)...).Success(t)
}

func explain(t *testing.T, h *testcli.Harness, flags ...string) testcli.Response {
	t.Helper()
	return h.Run(t, append([]string{"context", "explain", "--format=json"}, flags...)...).Success(t)
}

func effectiveField(t *testing.T, result testcli.Response, field, layer string, want any) map[string]any {
	t.Helper()
	var values []struct {
		Field  string
		Value  any
		Origin map[string]any
	}
	if err := json.Unmarshal(result.Data["values"], &values); err != nil {
		t.Fatal(err)
	}
	for _, entry := range values {
		if entry.Field != field {
			continue
		}
		storage := "shared"
		if layer == "device" || layer == "checkout" {
			storage = "local-only"
		}
		if entry.Value != want || entry.Origin["layer"] != layer || entry.Origin["storage"] != storage ||
			entry.Origin["source"] != "configuration" || entry.Origin["revision"] == "" ||
			entry.Origin["revision"] == nil || entry.Origin["updatedAt"] == nil {
			t.Fatalf("unexpected effective value for %s: %+v", field, entry)
		}
		return entry.Origin
	}
	t.Fatalf("missing %s: %s", field, result.Data["values"])
	return nil
}

func TestConfigurationPrecedenceAndOrigins(t *testing.T) {
	h := contextHarness(t)
	empty := explain(t, h)
	var keys []string
	for _, key := range []string{"project", "deviceId", "checkout", "profile", "values", "source", "updatedAt", "observedAt", "revision", "freshness"} {
		if _, ok := empty.Data[key]; ok {
			keys = append(keys, key)
		}
	}
	if len(keys) != 10 || len(empty.Data) != len(keys) || string(empty.Data["values"]) != "[]" ||
		string(empty.Data["profile"]) != "null" || string(empty.Data["observedAt"]) != "null" ||
		string(empty.Data["revision"]) != "null" || string(empty.Data["freshness"]) != `"unknown"` {
		t.Fatal(empty)
	}
	setConfig(t, h, "global", "parallelism", "1")
	setConfig(t, h, "global", "timeoutSeconds", "30")
	effectiveField(t, explain(t, h), "parallelism", "global", float64(1))
	setConfig(t, h, "project", "parallelism", "2")
	effectiveField(t, explain(t, h), "parallelism", "project", float64(2))

	created := h.Run(t, "profile", "create", "development", "--format=json").Success(t)
	setConfig(t, h, "profile", "parallelism", "3", "--profile", "development")
	effectiveField(t, explain(t, h), "parallelism", "project", float64(2))
	explicit := explain(t, h, "--profile", "development")
	effectiveField(t, explicit, "parallelism", "profile", float64(3))
	if obj(t, explicit, "profile")["origin"].(map[string]any)["source"] != "explicit" ||
		string(explain(t, h).Data["profile"]) != "null" {
		t.Fatal("invocation-only Profile mutated checkout selection")
	}

	h.Run(t, "profile", "select", "development", "--format=json").Success(t)
	selected := explain(t, h)
	profile := obj(t, selected, "profile")
	checkout := obj(t, selected, "checkout")
	profileOrigin := profile["origin"].(map[string]any)
	checkoutOrigin := checkout["origin"].(map[string]any)
	if profile["name"] != "development" || profile["id"] != fieldID(t, created) || profileOrigin["source"] != "profile-selection" ||
		profileOrigin["storage"] != "local-only" || checkoutOrigin["source"] != "checkout-binding" ||
		checkoutOrigin["storage"] != "local-only" || checkout["path"] == "" {
		t.Fatal(profile, checkout)
	}
	if shown := obj(t, h.Run(t, "context", "show", "--format=json").Success(t), "profile"); !reflect.DeepEqual(profile, shown) {
		t.Fatal(shown)
	}
	effectiveField(t, selected, "parallelism", "profile", float64(3))
	setConfig(t, h, "device", "parallelism", "4")
	effectiveField(t, explain(t, h), "parallelism", "device", float64(4))
	setConfig(t, h, "checkout", "parallelism", "5")
	current := explain(t, h)
	effectiveField(t, current, "parallelism", "checkout", float64(5))
	effectiveField(t, current, "timeoutSeconds", "global", float64(30))

	original := h.Dir
	other := t.TempDir()
	h.Run(t, "project", "bind", other, "--project", "checkout", "--format=json").Success(t)
	h.Dir = other
	sibling := explain(t, h)
	effectiveField(t, sibling, "parallelism", "device", float64(4))
	if string(sibling.Data["profile"]) != "null" {
		t.Fatal("selection followed another checkout", sibling)
	}
	h.Dir = original
	for _, step := range []struct {
		unset, winner string
		value         float64
	}{{"checkout", "device", 4}, {"device", "profile", 3}} {
		h.Run(t, "config", "unset", "parallelism", "--layer", step.unset, "--format=json").Success(t)
		effectiveField(t, explain(t, h), "parallelism", step.winner, step.value)
	}
	h.Run(t, "profile", "clear", "--format=json").Success(t)
	effectiveField(t, explain(t, h), "parallelism", "project", float64(2))
}

func fieldID(t *testing.T, response testcli.Response) string {
	t.Helper()
	var id string
	if err := json.Unmarshal(response.Data["id"], &id); err != nil || id == "" {
		t.Fatal("missing configuration ID", response)
	}
	return id
}

func TestConfigurationErrorsAndPaths(t *testing.T) {
	h := contextHarness(t)
	for _, tc := range []struct {
		field, value, code string
	}{
		{"Parallelism", "1", "unknown-config-field"},
		{"*", "1", "unknown-config-field"},
		{"secretRef", "1", "unknown-config-field"},
		{"executionCommand", "1", "unknown-config-field"},
		{"sensitivity", "1", "unknown-config-field"},
		{"approvedSkillVersion", "1", "unknown-config-field"},
		{"parallelism", `"2"`, "config-type-conflict"},
		{"parallelism", "null", "config-type-conflict"},
		{"parallelism", "2.0", "config-type-conflict"},
		{"parallelism", "1025", "invalid-config"},
		{"timeoutSeconds", "0", "invalid-config"},
		{"workingDirectory", "true", "config-type-conflict"},
		{"workingDirectory", `"../outside"`, "invalid-config-path"},
		{"workingDirectory", `"C:/host-only"`, "invalid-config-path"},
	} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			failure(t, h.Run(t, "config", "set", tc.field, tc.value, "--layer=project", "--format=json"), tc.code)
		})
	}
	failure(t, h.Run(t, "config", "unset", "*", "--layer=device", "--format=json"), "unknown-config-field")
	failure(t, h.Run(t, "config", "set", "parallelism", "2", "--layer=project", "--layer=device", "--format=json"), "usage")
	failure(t, h.Run(t, "profile", "select", "missing", "--format=json"), "profile-not-found")
	failure(t, h.Run(t, "context", "explain", "--profile=missing", "--format=json"), "profile-not-found")
	failure(t, h.Run(t, "context", "explain", "--profile=a", "--profile=b", "--format=json"), "ambiguous-profile")

	setConfig(t, h, "project", "workingDirectory", `"missing/dir"`)
	failure(t, h.Run(t, "context", "explain", "--format=json"), "invalid-config-path")
	if err := os.MkdirAll(filepath.Join(h.Dir, "missing", "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	result := explain(t, h)
	checkout := obj(t, result, "checkout")["path"].(string)
	effectiveField(t, result, "workingDirectory", "project", filepath.Join(checkout, "missing", "dir"))
	h.Dir = t.TempDir()
	failure(t, h.Run(t, "context", "explain", "--project=checkout", "--format=json"), "checkout-required")
	failure(t, h.Run(t, "config", "set", "parallelism", "2", "--layer=checkout", "--project=checkout", "--format=json"), "checkout-required")
}

func TestConfigurationSyncAcrossDevices(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	legacy := obj(t, putDoc(t, a, "legacy", "legacy body", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	db, err := sql.Open("sqlite", filepath.Join(a.DataDir, "workspaces", "default", "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`DROP TABLE profile_selections; DROP TABLE local_configurations; DROP TABLE configurations; PRAGMA user_version=6;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", ".", "--project", "checkout", "--format=json").Success(t)
	if string(explain(t, b).Data["values"]) != "[]" {
		t.Fatal("old catalog has configuration")
	}

	setConfig(t, a, "global", "parallelism", "2")
	projectID := fieldID(t, setConfig(t, a, "project", "timeoutSeconds", "30"))
	a.Run(t, "profile", "create", "development", "--format=json").Success(t)
	setConfig(t, a, "profile", "parallelism", "3", "--profile=development")
	a.Run(t, "profile", "select", "development", "--format=json").Success(t)

	local := filepath.Join(a.Dir, "local-sentinel")
	if err := os.Mkdir(local, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range []string{"device", "checkout"} {
		receipt := setConfig(t, a, layer, "workingDirectory", string(raw))
		if string(receipt.Data["storage"]) != `"local-only"` {
			t.Fatal(receipt)
		}
	}
	setConfig(t, a, "checkout", "parallelism", "9")

	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	remoteHead := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	paths := strings.Fields(git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", remoteHead, "records"))
	configs := 0
	for _, path := range paths {
		record := git(t, a, "--git-dir", remote, "show", remoteHead+":"+path)
		if strings.Contains(record, "local-sentinel") || strings.Contains(record, "workingDirectory") {
			t.Fatal("local override left the device", record)
		}
		var envelope struct{ Kind string }
		if err := json.Unmarshal([]byte(record), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Kind == "configuration" {
			configs++
		}
	}
	if configs != 3 {
		t.Fatal("expected only three ordinary configuration inputs in remote snapshot", configs, paths)
	}

	joined := explain(t, b)
	if string(joined.Data["profile"]) != "null" || strings.Contains(string(joined.Data["values"]), "workingDirectory") {
		t.Fatal("received another device's local settings", joined)
	}
	effectiveField(t, joined, "parallelism", "global", float64(2))
	effectiveField(t, joined, "timeoutSeconds", "project", float64(30))
	effectiveField(t, explain(t, a), "parallelism", "checkout", float64(9))
	b.Run(t, "profile", "select", "development", "--format=json").Success(t)
	effectiveField(t, explain(t, b), "parallelism", "profile", float64(3))
	setConfig(t, b, "device", "parallelism", "5")
	effectiveField(t, explain(t, b), "parallelism", "device", float64(5))
	b.Run(t, "config", "unset", "parallelism", "--layer=device", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	if got := git(t, b, "--git-dir", remote, "rev-parse", "ctxferry-v1"); got != remoteHead {
		t.Fatal("local-only selection or derived view published", got)
	}
	if got := getShared(t, b, legacy["id"].(string)); got["revision"] != legacy["revision"] || got["body"] != "legacy body" {
		t.Fatal("existing record changed", got)
	}

	setConfig(t, a, "project", "timeoutSeconds", "31")
	setConfig(t, b, "project", "timeoutSeconds", "32")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	token := conflictToken(t, b, projectID)
	detail := b.Run(t, "sync", "conflicts", projectID, "--format=json").Success(t)
	for side, want := range map[string]float64{"base": 30, "local": 32, "remote": 31} {
		values := obj(t, detail, side)["payload"].(map[string]any)["values"].(map[string]any)
		if values["timeoutSeconds"] != want {
			t.Fatal(side, values)
		}
	}
	failure(t, b.Run(t, "context", "explain", "--format=json"), "conflict-unresolved")
	failure(t, b.Run(t, "config", "set", "timeoutSeconds", "33", "--layer=project", "--format=json"), "conflict-unresolved")
	b.Run(t, "sync", "resolve", projectID, "--take=local", "--if-conflict", token, "--format=json").Success(t)
	resolved := effectiveField(t, explain(t, b), "timeoutSeconds", "project", float64(32))
	b.Run(t, "sync", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	if got := effectiveField(t, explain(t, a), "timeoutSeconds", "project", float64(32)); !reflect.DeepEqual(resolved, got) {
		t.Fatal("configuration provenance did not converge", resolved, got)
	}

	before := b.Run(t, "sync", "status", "--format=json").Success(t)
	catalog := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, catalog)
	path := "records/" + projectID[:2] + "/" + projectID + ".json"
	original := git(t, a, "-C", catalog, "show", "HEAD:"+path)
	ambiguous := strings.Replace(original, `"timeoutSeconds":`, `"timeoutSeconds": 5, "timeoutSeconds":`, 1)
	if ambiguous == original {
		t.Fatal("missing field in fixture")
	}
	if err := os.WriteFile(filepath.Join(catalog, path), []byte(ambiguous), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, a, "-C", catalog, "add", "--", path)
	git(t, a, "-C", catalog, "commit", "-m", "Introduce ambiguous configuration fixture")
	git(t, a, "-C", catalog, "push", "origin", "ctxferry-v1")
	invalid := b.Run(t, "sync", "--format=json")
	failure(t, invalid, "incomplete")
	if !strings.Contains(invalid.Stderr, "multiple candidates") {
		t.Fatal(invalid)
	}
	after := b.Run(t, "sync", "status", "--format=json").Success(t)
	if string(after.Data["head"]) != string(before.Data["head"]) || string(after.Data["state"]) != `"incomplete"` {
		t.Fatal("invalid configuration advanced the baseline", after)
	}
	effectiveField(t, explain(t, b), "timeoutSeconds", "project", float64(32))
}
