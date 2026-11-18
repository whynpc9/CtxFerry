package tests

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func joinDevice(t *testing.T, h *testcli.Harness, all bool) *testcli.Harness {
	t.Helper()
	b := testcli.New(t, h.Binary)
	b.GitPath = h.GitPath
	b.ExtraEnv = h.ExtraEnv
	args := []string{"join", "https://github.com/example/context.git", "--format=json"}
	if all {
		args = append(args, "--all-projects")
	}
	b.Run(t, args...).Success(t)
	return b
}
func replaceShared(t *testing.T, h *testcli.Harness, d map[string]any, body string) map[string]any {
	t.Helper()
	file := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(file, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return obj(t, h.Run(t, "context", "put", d["id"].(string), "--if-revision", d["revision"].(string), "--title", "revised", "--purpose", "notes", "--source", "explicit", "--storage", "shared", "--disclosure", "full", "--body-file", file, "--format=json").Success(t), "document")
}
func TestTwoDeviceSync(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", " \noriginal\n\t", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	projects := b.Run(t, "project", "list", "--format=json").Success(t)
	var ps []map[string]any
	json.Unmarshal(projects.Data["projects"], &ps)
	if len(ps) != 1 {
		t.Fatal(projects)
	}
	slug := ps[0]["slug"].(string)
	b.Run(t, "project", "bind", b.Dir, "--project", slug, "--format=json").Success(t)
	got := obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != " \noriginal\n\t" {
		t.Fatal(got)
	}
	originalHead := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); head != originalHead {
		t.Fatal("no-op sync wrote a commit")
	}
	// Queue while transport is unavailable. Reads remain local across CLI processes.
	if err := os.Rename(remote, remote+".offline"); err != nil {
		t.Fatal(err)
	}
	changed := replaceShared(t, b, got, "offline revision\n")
	status := b.Run(t, "sync", "status", "--format=json").Success(t)
	if string(status.Data["state"]) != `"pending"` {
		t.Fatal(status)
	}
	failure(t, b.Run(t, "sync", "--format=json"), "git-error")
	offline := b.Run(t, "sync", "status", "--format=json").Success(t)
	if string(offline.Data["state"]) != `"offline"` || string(offline.Data["pending"]) != "1" {
		t.Fatal(offline)
	}
	b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t)
	if err := os.Rename(remote+".offline", remote); err != nil {
		t.Fatal(err)
	}
	b.Run(t, "sync", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	got = obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "offline revision\n" || got["revision"] != changed["revision"] {
		t.Fatal(got)
	}
	changed = replaceShared(t, a, got, "back to B")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "context", "get", d["id"].(string), "--sync", "--format=json").Success(t)
	got = obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["revision"] != changed["revision"] {
		t.Fatal(got)
	}
	// Subscriptions affect local queries and keep the entire published tree intact.
	b.Run(t, "project", "unsubscribe", slug, "--format=json").Success(t)
	failure(t, b.Run(t, "context", "get", d["id"].(string), "--format=json"), "not-subscribed")
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
		t.Fatal("subscription wrote remote")
	}
	b.Run(t, "project", "subscribe", slug, "--format=json").Success(t)
	got = obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	replaceShared(t, a, changed, "A pending")
	replaceShared(t, b, got, "B pending")
	a.Run(t, "sync", "--format=json").Success(t)
	conflict := b.Run(t, "sync", "--format=json").Success(t)
	if !strings.Contains(string(conflict.Data["state"]), "conflict") {
		t.Fatal(conflict)
	}
	got = obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "B pending" {
		t.Fatal("lost local conflict", got)
	}
}

func TestSyncRejectsInvalidSnapshotAtomically(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "original body", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	before := b.Run(t, "sync", "status", "--format=json").Success(t)
	checkout := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, checkout)
	paths := strings.Fields(git(t, a, "-C", checkout, "ls-files", "objects/*"))
	if len(paths) != 1 {
		t.Fatal(paths)
	}
	if err := os.WriteFile(filepath.Join(checkout, paths[0]), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, a, "-C", checkout, "add", "--", paths[0])
	git(t, a, "-C", checkout, "commit", "-m", "corrupt object")
	git(t, a, "-C", checkout, "push", "origin", "ctxferry-v1")
	failure(t, b.Run(t, "sync", "--format=json"), "incomplete")
	after := b.Run(t, "sync", "status", "--format=json").Success(t)
	if string(before.Data["head"]) != string(after.Data["head"]) || string(before.Data["lastSyncAt"]) != string(after.Data["lastSyncAt"]) {
		t.Fatal("invalid snapshot advanced baseline")
	}
	got := obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "original body" {
		t.Fatal(got)
	}
	c := testcli.New(t, a.Binary)
	c.GitPath = a.GitPath
	c.ExtraEnv = a.ExtraEnv
	failure(t, c.Run(t, "join", "https://github.com/example/context.git", "--format=json"), "incomplete")
	c.Run(t, "init", "--format=json").Success(t)
}

func TestIndependentChangesAndObjectReuse(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	first := obj(t, putDoc(t, a, "first", "same body", "shared", "full").Success(t), "document")
	second := obj(t, putDoc(t, a, "second", "second body", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	old := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	replaceShared(t, a, first, "same body")
	a.Run(t, "sync", "--format=json").Success(t)
	delta := strings.Fields(git(t, a, "--git-dir", remote, "diff", "--name-only", old, "ctxferry-v1"))
	if len(delta) != 1 || !strings.Contains(delta[0], first["id"].(string)) {
		t.Fatal("metadata edit rewrote unrelated files", delta)
	}
	replaceShared(t, b, second, "independent B")
	b.Run(t, "sync", "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	got := obj(t, a.Run(t, "context", "get", second["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "independent B" {
		t.Fatal(got)
	}
}

func TestOfflineSchema4PublisherUpgrade(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "baseline", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	db, err := sql.Open("sqlite", filepath.Join(a.DataDir, "workspaces/default/catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`DROP TABLE sync_baselines; DROP TABLE sync_outbox; DROP TABLE project_subscriptions; ALTER TABLE sync_state DROP COLUMN last_sync_at; ALTER TABLE sync_state DROP COLUMN outcome; PRAGMA user_version=4;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(remote, remote+".offline"); err != nil {
		t.Fatal(err)
	}
	replaceShared(t, a, d, "upgraded offline")
	a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t)
	if err = os.Rename(remote+".offline", remote); err != nil {
		t.Fatal(err)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	got := obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "upgraded offline" {
		t.Fatal(got)
	}
}
func TestRejectedPushCanResume(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote shell hook fault injection requires POSIX shell")
	}
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "baseline", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	replaceShared(t, a, d, "queued")
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	failure(t, a.Run(t, "sync", "--format=json"), "push-pending")
	a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t)
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	got := obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "queued" {
		t.Fatal(got)
	}
}

func TestAcceptedPendingPushAcknowledged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("remote shell hook fault injection requires POSIX shell")
	}
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "baseline", "shared", "full").Success(t), "document")
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	replaceShared(t, a, d, "accepted pending")
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	failure(t, a.Run(t, "sync", "--format=json"), "push-pending")
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	// Deliver the pinned candidate outside the interrupted client, preserving its journal.
	managed := filepath.Join(a.DataDir, "workspaces/default/sync.git")
	candidate := git(t, a, "--git-dir", managed, "rev-parse", "refs/ctxferry/pending")
	git(t, a, "--git-dir", managed, "push", remote, candidate+":refs/heads/ctxferry-v1")
	a.Run(t, "sync", "--format=json").Success(t)
	if head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); head != candidate {
		t.Fatal("acknowledgement produced another commit")
	}
	status := a.Run(t, "sync", "status", "--format=json").Success(t)
	if string(status.Data["pending"]) != "0" || string(status.Data["state"]) != `"synced"` {
		t.Fatal(status)
	}
}
