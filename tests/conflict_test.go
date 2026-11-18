package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func conflictToken(t *testing.T, h *testcli.Harness, id string) string {
	t.Helper()
	v := h.Run(t, "sync", "status", "--format=json").Success(t)
	var cs []map[string]string
	if e := json.Unmarshal(v.Data["conflicts"], &cs); e != nil {
		t.Fatal(e, v)
	}
	for _, c := range cs {
		if c["id"] == id {
			return c["conflictId"]
		}
	}
	t.Fatal("missing conflict", v)
	return ""
}
func getShared(t *testing.T, h *testcli.Harness, id string) map[string]any {
	t.Helper()
	return obj(t, h.Run(t, "context", "get", id, "--format=json").Success(t), "document")
}
func TestConflictChoicesAndIndependentProgress(t *testing.T) {
	for _, choice := range []string{"local", "remote", "file"} {
		t.Run(choice, func(t *testing.T) {
			var metadata atomic.Value
			metadata.Store(privateMetadata)
			a, remote := syncHarness(t, &metadata)
			d := obj(t, putDoc(t, a, "first", "base body", "shared", "full").Success(t), "document")
			id := d["id"].(string)
			other := obj(t, putDoc(t, a, "other", "other base", "shared", "full").Success(t), "document")
			incoming := obj(t, putDoc(t, a, "incoming", "incoming base", "shared", "full").Success(t), "document")
			enableSync(t, a)
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			b := joinDevice(t, a, true)
			b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
			replaceShared(t, a, d, "remote body")
			replaceShared(t, a, incoming, "downloaded independently")
			a.Run(t, "sync", "--format=json").Success(t)
			replaceShared(t, b, d, "local body")
			replaceShared(t, b, other, "uploaded independently")
			b.Run(t, "sync", "--format=json").Success(t)
			if getShared(t, b, incoming["id"].(string))["body"] != "downloaded independently" {
				t.Fatal("download blocked")
			}
			a.Run(t, "sync", "--format=json").Success(t)
			if getShared(t, a, other["id"].(string))["body"] != "uploaded independently" {
				t.Fatal("upload blocked", getShared(t, a, other["id"].(string)))
			}
			token := conflictToken(t, b, id)
			detail := b.Run(t, "sync", "conflicts", id, "--format=json").Success(t)
			for side, want := range map[string]string{"base": "base body", "local": "local body", "remote": "remote body"} {
				v := obj(t, detail, side)
				if v["payload"].(map[string]any)["body"] != want {
					t.Fatal(side, v)
				}
			}
			b.Run(t, "sync", "--format=json").Success(t)
			if conflictToken(t, b, id) != token {
				t.Fatal("repeated sync changed evidence")
			}
			failure(t, b.Run(t, "sync", "resolve", id, "--take", "local", "--if-conflict", "wrong", "--format=json"), "stale-conflict")
			before := getShared(t, a, id)
			args := []string{"sync", "resolve", id, "--if-conflict", token, "--format=json"}
			want := choice + " body"
			if choice == "file" {
				p := filepath.Join(t.TempDir(), "resolution.json")
				os.WriteFile(p, []byte(`{"title":"new","purpose":"notes","source":"explicit","body":"supplied body","policy":{"storage":"shared","agentDisclosure":"full"}}`), 0600)
				args = append(args, "--file", p)
				want = "supplied body"
			} else {
				args = append(args, "--take", choice)
			}
			b.Run(t, args...).Success(t)
			resolved := getShared(t, b, id)
			if resolved["body"] != want || resolved["revision"] == before["revision"] || resolved["previousRevision"] != before["revision"] {
				t.Fatal(resolved, before)
			}
			b.Run(t, "sync", "--format=json").Success(t)
			a.Run(t, "sync", "--format=json").Success(t)
			if getShared(t, a, id)["revision"] != resolved["revision"] {
				t.Fatal("resolution lost")
			}
			if head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); head == "" {
				t.Fatal(head)
			}
		})
	}
}
func TestTombstoneAndRestore(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "first", "original body", "shared", "full").Success(t), "document")
	id := d["id"].(string)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	original := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	a.Run(t, "context", "delete", id, "--if-revision", d["revision"].(string), "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	failure(t, b.Run(t, "context", "get", id, "--format=json"), "document-not-found")
	b.Run(t, "project", "unsubscribe", "checkout", "--format=json").Success(t)
	b.Run(t, "project", "subscribe", "checkout", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	failure(t, b.Run(t, "context", "get", id, "--format=json"), "document-not-found")
	path := "records/" + strings.ReplaceAll(id, "-", "")[:2] + "/" + id + ".json"
	var tomb map[string]any
	json.Unmarshal([]byte(git(t, a, "--git-dir", remote, "show", "ctxferry-v1:"+path)), &tomb)
	if tomb["deleted"] != true {
		t.Fatal(tomb)
	}
	b.Run(t, "context", "restore", id, "--if-revision", tomb["revision"].(string), "--from-commit", original, "--format=json").Success(t)
	restored := getShared(t, b, id)
	if restored["revision"] == d["revision"] || restored["body"] != "original body" {
		t.Fatal(restored)
	}
	b.Run(t, "sync", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	getShared(t, a, id)
	c := joinDevice(t, a, true)
	c.Run(t, "project", "bind", c.Dir, "--project", "checkout", "--format=json").Success(t)
	getShared(t, c, id)
}
func TestEditVersusDelete(t *testing.T) {
	for _, deleteLocal := range []bool{true, false} {
		t.Run(map[bool]string{true: "delete-local", false: "delete-remote"}[deleteLocal], func(t *testing.T) {
			var metadata atomic.Value
			metadata.Store(privateMetadata)
			a, _ := syncHarness(t, &metadata)
			d := obj(t, putDoc(t, a, "first", "base", "shared", "full").Success(t), "document")
			id := d["id"].(string)
			enableSync(t, a)
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			b := joinDevice(t, a, true)
			b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
			deleting, editing := a, b
			if deleteLocal {
				deleting, editing = b, a
			}
			deleting.Run(t, "context", "delete", id, "--if-revision", d["revision"].(string), "--format=json").Success(t)
			replaceShared(t, editing, d, "edited offline")
			a.Run(t, "sync", "--format=json").Success(t)
			b.Run(t, "sync", "--format=json").Success(t)
			token := conflictToken(t, b, id)
			b.Run(t, "sync", "resolve", id, "--if-conflict", token, "--take", "remote", "--format=json").Success(t)
			b.Run(t, "sync", "--format=json").Success(t)
		})
	}
}

func TestConflictEvidenceSurvivesRemoteAdvanceAndOffline(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "base preserved", "shared", "summary").Success(t), "document")
	id := d["id"].(string)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	r := replaceShared(t, a, d, "remote v1")
	replaceShared(t, b, d, "local v1")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	old := conflictToken(t, b, id)
	detail := b.Run(t, "sync", "conflicts", id, "--format=json").Success(t)
	if _, ok := obj(t, detail, "base")["payload"].(map[string]any)["body"]; ok {
		t.Fatal("summary-only base body disclosed")
	}
	replaceShared(t, a, r, "remote v2")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	token := conflictToken(t, b, id)
	if token == old {
		t.Fatal("advanced remote reused stale evidence")
	}
	failure(t, b.Run(t, "sync", "resolve", id, "--if-conflict", old, "--take", "local", "--format=json"), "stale-conflict")
	if e := os.Rename(remote, remote+".offline"); e != nil {
		t.Fatal(e)
	}
	failure(t, b.Run(t, "sync", "--format=json"), "git-error")
	if conflictToken(t, b, id) != token {
		t.Fatal("offline attempt hid conflict")
	}
	b.Run(t, "sync", "resolve", id, "--if-conflict", token, "--take", "remote", "--format=json").Success(t)
	if e := os.Rename(remote+".offline", remote); e != nil {
		t.Fatal(e)
	}
	// An unseen third revision makes the queued offline resolution conflict again.
	current := getShared(t, a, id)
	replaceShared(t, a, current, "remote v3")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	conflictToken(t, b, id)
}

func TestProjectSlugCollisionKeepsUnrelatedSyncFlowing(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "base", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	ap := field(t, a.Run(t, "project", "bind", t.TempDir(), "--slug", "collision", "--format=json").Success(t), "project")
	bp := field(t, b.Run(t, "project", "bind", t.TempDir(), "--slug", "collision", "--format=json").Success(t), "project")
	incoming := obj(t, putDoc(t, a, "remote child", "remote collision child", "shared", "full", "--project", ap).Success(t), "document")
	held := obj(t, putDoc(t, b, "held child", "local collision child", "shared", "full", "--project", bp).Success(t), "document")
	a.Run(t, "sync", "--format=json").Success(t)
	replaceShared(t, b, d, "independent upload")
	for i := 0; i < 2; i++ {
		b.Run(t, "sync", "--format=json").Success(t)
	}
	status := b.Run(t, "sync", "status", "--format=json").Success(t)
	if !strings.Contains(string(status.Data["blocked"]), bp) || !strings.Contains(string(status.Data["blocked"]), held["id"].(string)) {
		t.Fatal("collision not durable", status)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	if getShared(t, a, d["id"].(string))["body"] != "independent upload" {
		t.Fatal("collision blocked independent publish")
	}
	if obj(t, b.Run(t, "context", "get", held["id"].(string), "--project", bp, "--format=json").Success(t), "document")["body"] != "local collision child" {
		t.Fatal("lost held overlay")
	}
	// Only A's collision unit reaches Git; B's blocked objects never do.
	tree := git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1")
	if strings.Contains(tree, bp) || strings.Contains(tree, held["id"].(string)) || !strings.Contains(tree, incoming["id"].(string)) {
		t.Fatal(tree)
	}
}
func TestMalformedDeletionIDsReturnStructuredErrors(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, _ := syncHarness(t, &metadata)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	for _, action := range []string{"delete", "restore"} {
		args := []string{"context", action, "x", "--if-revision", "anything", "--format=json"}
		if action == "restore" {
			args = append(args, "--from-commit", strings.Repeat("0", 40))
		}
		failure(t, a.Run(t, args...), "usage")
	}
}
