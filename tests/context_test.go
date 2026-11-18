package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func contextHarness(t *testing.T) *testcli.Harness {
	t.Helper()
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	h.Run(t, "project", "bind", ".", "--format=json").Success(t)
	return h
}
func putDoc(t *testing.T, h *testcli.Harness, title, body, storage, mode string, extra ...string) testcli.Result {
	t.Helper()
	path := filepath.Join(h.Home, "body.md")
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"context", "put", "--title", title, "--purpose", "notes", "--source", "maintainer", "--summary", "approved overview", "--body-file", path, "--storage", storage, "--disclosure", mode, "--format=json"}
	return h.Run(t, append(args, extra...)...)
}
func documents(t *testing.T, r testcli.Response) []map[string]any {
	t.Helper()
	var d []map[string]any
	if e := json.Unmarshal(r.Data["documents"], &d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestContextDisclosure(t *testing.T) {
	h := contextHarness(t)
	for _, mode := range []string{"full", "summary", "reference", "local-tool"} {
		t.Run(mode, func(t *testing.T) {
			hidden := "bodyonlysentinel" + strings.ReplaceAll(mode, "-", "")
			receipt := putDoc(t, h, "document "+mode, hidden, "local-only", mode)
			v := receipt.Success(t)
			d := obj(t, v, "document")
			id := d["id"].(string)
			if strings.Contains(receipt.Stdout, hidden) || strings.Contains(receipt.Stdout, "approved overview") {
				t.Fatal("receipt leaks content")
			}
			for _, format := range []string{"json", "text"} {
				r := h.Run(t, "context", "get", id, "--format", format)
				if r.ExitCode != 0 {
					t.Fatal(r)
				}
				if strings.Contains(r.Stdout, hidden) != (mode == "full") {
					t.Fatal("body disclosure", r.Stdout)
				}
				if strings.Contains(r.Stdout, "approved overview") != (mode == "full" || mode == "summary") {
					t.Fatal("summary disclosure", r.Stdout)
				}
				show := h.Run(t, "context", "show", "--format", format)
				if strings.Contains(show.Stdout, hidden) {
					t.Fatal("show leaks body")
				}
			}
			got := obj(t, h.Run(t, "context", "get", id, "--format=json").Success(t), "document")
			for _, key := range []string{"source", "updatedAt", "observedAt", "revision", "freshness", "scope", "policy"} {
				if _, ok := got[key]; !ok {
					t.Fatal("missing provenance", key)
				}
			}
			if got["freshness"] != "unknown" || got["observedAt"] != nil {
				t.Fatal(got)
			}
			matches := documents(t, h.Run(t, "context", "search", hidden, "--format=json").Success(t))
			if (len(matches) == 1) != (mode == "full") {
				t.Fatal("hidden text influenced search", matches)
			}
			if mode == "full" {
				next := putDoc(t, h, "document full", "replacementhidden", "shared", "summary", id, "--if-revision", d["revision"].(string)).Success(t)
				updated := obj(t, next, "document")
				if updated["revision"] == d["revision"] || updated["previousRevision"] != d["revision"] {
					t.Fatal("revision chain")
				}
				for _, term := range []string{hidden, "replacementhidden"} {
					if len(documents(t, h.Run(t, "context", "search", term, "--format=json").Success(t))) != 0 {
						t.Fatal("old/hidden index entry survives")
					}
				}
				failure(t, putDoc(t, h, "lost", "lostupdate", "shared", "full", id, "--if-revision", d["revision"].(string)), "revision-conflict")
			}
		})
	}
	for _, query := range []string{`" OR *`, `x'); DROP TABLE records;--`, "AND", "maintainer"} {
		h.Run(t, "context", "search", query, "--format=json").Success(t)
	}
	// Storage is independent of disclosure.
	d := obj(t, putDoc(t, h, "shared tool", "privatetoolbody", "shared", "local-tool").Success(t), "document")
	if strings.Contains(h.Run(t, "context", "get", d["id"].(string), "--format=json").Stdout, "privatetoolbody") {
		t.Fatal("shared broadened disclosure")
	}
}
func TestContextProjectAndSummary(t *testing.T) {
	h := contextHarness(t)
	d := obj(t, putDoc(t, h, "overview", "hiddenbody", "local-only", "summary", "--purpose", "project-summary").Success(t), "document")
	show := h.Run(t, "context", "show", "--format=json").Success(t)
	// OS working-directory APIs can preserve Windows 8.3 names or expand
	// macOS /var. Assert directory identity rather than path spelling.
	expectedDir, e := os.Stat(h.Dir)
	if e != nil {
		t.Fatal(e)
	}
	actualDir, e := os.Stat(obj(t, show, "environment")["cwd"].(string))
	if e != nil {
		t.Fatal(e)
	}
	if string(show.Data["summary"]) != `"approved overview"` || !os.SameFile(expectedDir, actualDir) {
		t.Fatal("orientation", show)
	}
	putDoc(t, h, "overview", "hiddenbody", "local-only", "reference", d["id"].(string), "--if-revision", d["revision"].(string), "--purpose", "project-summary").Success(t)
	if string(h.Run(t, "context", "show", "--format=json").Success(t).Data["summary"]) != "null" {
		t.Fatal("restricted project summary")
	}
	original := h.Dir
	h.Dir = t.TempDir()
	for _, args := range [][]string{{"show"}, {"get", d["id"].(string)}, {"search", "overview"}} {
		r := h.Run(t, append([]string{"context", "--format=json"}, args...)...)
		failure(t, r, "project-unbound")
		if !strings.Contains(r.Stdout, `"candidates":[]`) {
			t.Fatal("candidates missing")
		}
	}
	projects := h.Run(t, "project", "list", "--format=json").Success(t)
	var ps []map[string]any
	json.Unmarshal(projects.Data["projects"], &ps)
	h.Run(t, "context", "get", d["id"].(string), "--project", ps[0]["id"].(string), "--format=json").Success(t)
	h.Run(t, "project", "bind", ".", "--slug=other", "--format=json").Success(t)
	failure(t, h.Run(t, "context", "get", d["id"].(string), "--format=json"), "document-not-found")
	h.Dir = filepath.Join(original, "nested")
	os.Mkdir(h.Dir, 0700)
	h.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t)
}
func TestContextLimitsAndPagination(t *testing.T) {
	h := contextHarness(t)
	body := strings.Repeat("界", (1024*1024)/3) + "x" // exactly 1 MiB UTF-8
	d := obj(t, putDoc(t, h, "large", body, "local-only", "full").Success(t), "document")
	got := obj(t, h.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != body {
		t.Fatal("body altered")
	}
	failure(t, putDoc(t, h, "too large", body+"x", "local-only", "full", d["id"].(string), "--if-revision", d["revision"].(string)), "size-limit")
	summaryPath := filepath.Join(h.Home, "summary.txt")
	if e := os.WriteFile(summaryPath, []byte(strings.Repeat("x", 262144)), 0600); e != nil {
		t.Fatal(e)
	}
	bodyPath := filepath.Join(h.Home, "small.md")
	os.WriteFile(bodyPath, []byte("small"), 0600)
	failure(t, h.Run(t, "context", "put", "--title=large metadata", "--purpose=notes", "--source=maintainer", "--body-file", bodyPath, "--summary-file", summaryPath, "--storage=local-only", "--disclosure=summary", "--format=json"), "size-limit")
	failure(t, putDoc(t, h, "invalid", string([]byte{255}), "local-only", "full"), "invalid-document")
	failure(t, putDoc(t, h, "invalid", "small", "local-only", "typo"), "invalid-policy")
	h.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t)
	for i := 0; i < 18; i++ {
		putDoc(t, h, fmt.Sprint(i)+strings.Repeat("\"<&界", 30), "notindexedbody", "local-only", "summary").Success(t)
	}
	ids := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 30; pages++ {
		r := h.Run(t, "context", "show", "--cursor", cursor, "--format=json")
		v := r.Success(t)
		if len(r.Stdout) > 8192 {
			t.Fatal("JSON budget", len(r.Stdout))
		}
		for _, d := range documents(t, v) {
			id := d["id"].(string)
			if ids[id] {
				t.Fatal("duplicate continuation")
			}
			ids[id] = true
		}
		if string(v.Data["truncated"]) == "false" {
			break
		}
		if e := json.Unmarshal(v.Data["nextCursor"], &cursor); e != nil || cursor == "" {
			t.Fatal("missing continuation")
		}
	}
	if len(ids) != 19 {
		t.Fatal("pagination lost documents", len(ids))
	}
	text := h.Run(t, "context", "show")
	if text.ExitCode != 0 || len(text.Stdout) > 8192 {
		t.Fatal("text budget", text)
	}
}
func TestContextConcurrentReplaceAndStale(t *testing.T) {
	h := contextHarness(t)
	d := obj(t, putDoc(t, h, "concurrent", "oldword", "local-only", "full").Success(t), "document")
	// Separate body paths avoid a fixture race.
	path := filepath.Join(h.Home, "concurrent.md")
	os.WriteFile(path, []byte("newword"), 0600)
	var wg sync.WaitGroup
	results := make(chan testcli.Result, 2)
	for range 2 {
		wg.Go(func() {
			results <- h.Run(t, "context", "put", d["id"].(string), "--if-revision", d["revision"].(string), "--title=concurrent", "--purpose=notes", "--source=maintainer", "--body-file", path, "--storage=local-only", "--disclosure=full", "--valid-until=2000-01-01T00:00:00Z", "--format=json")
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for r := range results {
		if r.ExitCode == 0 {
			r.Success(t)
			successes++
		} else {
			failure(t, r, "revision-conflict")
		}
	}
	if successes != 1 {
		t.Fatal("lost update", successes)
	}
	got := obj(t, h.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["freshness"] != "stale" {
		t.Fatal("expiry", got)
	}
	if len(documents(t, h.Run(t, "context", "search", "oldword", "--format=json").Success(t))) != 0 {
		t.Fatal("outdated search")
	}
	if len(documents(t, h.Run(t, "context", "search", "newword", "--format=json").Success(t))) != 1 {
		t.Fatal("new search")
	}
}
func TestContextMigratesV2(t *testing.T) {
	h := contextHarness(t)
	before := h.Run(t, "project", "list", "--format=json").Success(t)
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`DROP TABLE context_fts; DROP TABLE context_bodies; DROP TABLE records; PRAGMA user_version=2;`)
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	h.Run(t, "context", "show", "--format=json").Success(t)
	after := h.Run(t, "project", "list", "--format=json").Success(t)
	if string(before.Data["projects"]) != string(after.Data["projects"]) {
		t.Fatal("migration changed identities")
	}
	putDoc(t, h, "after migration", "persisted", "local-only", "full").Success(t)
}

func TestContextUnknownPolicyFailsClosed(t *testing.T) {
	h := contextHarness(t)
	d := obj(t, putDoc(t, h, "policy", "neverdisclose", "local-only", "full").Success(t), "document")
	db, e := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces", "default", "catalog.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`UPDATE records SET metadata=json_set(metadata,'$.policy.agentDisclosure','future-policy') WHERE id=?`, d["id"])
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"get", d["id"].(string)}, {"show"}, {"search", "neverdisclose"}} {
		r := h.Run(t, append([]string{"context", "--format=json"}, args...)...)
		failure(t, r, "invalid-policy")
		if strings.Contains(r.Stdout+r.Stderr, "neverdisclose") {
			t.Fatal("fail-open")
		}
	}
}

func TestContextEscapedSummaryPagination(t *testing.T) {
	h := contextHarness(t)
	path := filepath.Join(h.Home, "escaped.md")
	if e := os.WriteFile(path, []byte("body"), 0600); e != nil {
		t.Fatal(e)
	}
	first := h.Run(t, "context", "put", "--title", strings.Repeat("<", 160), "--purpose=project-summary", "--source", strings.Repeat("<", 160), "--summary", strings.Repeat("<", 512), "--body-file", path, "--storage=local-only", "--disclosure=summary", "--format=json").Success(t)
	firstID := field(t, first, "document")
	for _, format := range []string{"json", "text"} {
		r := h.Run(t, "context", "show", "--format", format)
		if r.ExitCode != 0 || len(r.Stdout) > 8192 || !strings.Contains(r.Stdout, firstID) {
			t.Fatal("single escaped entry lost", r)
		}
	}
	second := putDoc(t, h, "another", "body", "local-only", "full").Success(t)
	ids := map[string]bool{}
	cursor := ""
	for range 3 {
		r := h.Run(t, "context", "show", "--cursor", cursor, "--format=json")
		v := r.Success(t)
		if len(r.Stdout) > 8192 {
			t.Fatal("budget")
		}
		ds := documents(t, v)
		if len(ds) == 0 {
			t.Fatal("empty page cannot progress")
		}
		for _, d := range ds {
			id := d["id"].(string)
			if ids[id] {
				t.Fatal("duplicate page")
			}
			ids[id] = true
		}
		if string(v.Data["truncated"]) == "false" {
			break
		}
		if e := json.Unmarshal(v.Data["nextCursor"], &cursor); e != nil || cursor != ds[len(ds)-1]["id"] {
			t.Fatal("cursor skips emitted boundary")
		}
	}
	if !ids[firstID] || !ids[field(t, second, "document")] {
		t.Fatal("lost entry", ids)
	}
}
