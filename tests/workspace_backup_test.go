package tests

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/store"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func workspaceBody(t *testing.T, h *testcli.Harness, id string) map[string]any {
	t.Helper()
	return obj(t, h.Run(t, "context", "get", id, "--project", "checkout", "--format=json").Success(t), "document")
}

func TestWorkspaceBackupContents(t *testing.T) {
	a, remote, doc := recoveryDevice(t)
	id := doc["id"].(string)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	replaceShared(t, a, doc, "unpublished body")
	putDoc(t, a, "local-only title", "private-local-sentinel", "local-only", "full").Success(t)
	for path, value := range map[string]string{
		filepath.Join(a.Home, ".git-credentials"):                 "git-auth-sentinel",
		filepath.Join(a.DataDir, "synthetic-provider-session"):    "provider-session-sentinel",
		filepath.Join(a.DataDir, "synthetic-credential-delivery"): "resolved-secret-sentinel",
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.Open(t.Context(), a.DataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO execution_trust VALUES ('device','task','profile','checkout','membership','digest','trust-sentinel','stamp')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	failure(t, a.Run(t, "workspace", "backup", "--format=json"), "usage")

	for _, includeLocal := range []bool{false, true} {
		dest := filepath.Join(t.TempDir(), "backup")
		args := []string{"workspace", "backup", "--to", dest, "--format=json"}
		if includeLocal {
			args = append(args, "--include-local-only")
		}
		r := a.Run(t, args...).Success(t)
		if (string(r.Data["includesLocalOnly"]) == "true") != includeLocal {
			t.Fatal(r)
		}
		failure(t, a.Run(t, args...), "destination-exists")
		entries, err := os.ReadDir(dest)
		if err != nil || len(entries) != 3 {
			t.Fatalf("expected exactly manifest, catalog and bundle: %v %v", entries, err)
		}
		for _, entry := range entries {
			b, err := os.ReadFile(filepath.Join(dest, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, sentinel := range []string{"credential-sentinel", "git-auth-sentinel", "provider-session-sentinel", "resolved-secret-sentinel", "trust-sentinel"} {
				if bytes.Contains(b, []byte(sentinel)) {
					t.Fatalf("backup included excluded material in %s", entry.Name())
				}
			}
			if !includeLocal && (bytes.Contains(b, []byte("private-local-sentinel")) || bytes.Contains(b, []byte(a.Dir))) {
				t.Fatalf("default backup included device-local content in %s", entry.Name())
			}
		}
		git(t, a, "--git-dir", remote, "bundle", "verify", filepath.Join(dest, "published.bundle"))
		if refs := git(t, a, "bundle", "list-heads", filepath.Join(dest, "published.bundle")); refs != strings.Trim(string(r.Data["head"]), "\"")+" refs/ctxferry/backup" {
			t.Fatal(refs)
		}
		copyDB, err := store.OpenBackupCatalog(t.Context(), filepath.Join(dest, "catalog.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		var body, base, proposed string
		if err := copyDB.QueryRow("SELECT b.body, json_extract(o.base,'$.revision'), json_extract(o.proposed,'$.revision') FROM sync_outbox o JOIN context_bodies b ON b.record_id=o.id WHERE o.id=?", id).Scan(&body, &base, &proposed); err != nil {
			t.Fatal(err)
		}
		if body != "unpublished body" || base != doc["revision"] || base == proposed {
			t.Fatalf("snapshot lost proposal or base: %s %s %s", body, base, proposed)
		}
		var localCount int
		if err := copyDB.QueryRow("SELECT count(*) FROM context_fts WHERE content LIKE '%private-local-sentinel%'").Scan(&localCount); err != nil || (localCount == 1) != includeLocal {
			t.Fatalf("unexpected local-only index: %d %v", localCount, err)
		}
		copyDB.Close()
	}
}

func TestWorkspaceRestoreEpochWithPendingEdits(t *testing.T) {
	a, remote, first := recoveryDevice(t)
	second := obj(t, putDoc(t, a, "independent", "unchanged base", "shared", "full").Success(t), "document")
	third := obj(t, putDoc(t, a, "recovery", "recoverable base", "shared", "full").Success(t), "document")
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	saved := obj(t, putDoc(t, a, "unpublished", "backed-up queue", "shared", "full").Success(t), "document")
	replaceShared(t, a, third, "backed-up change")
	source := filepath.Join(t.TempDir(), "backup")
	backup := a.Run(t, "workspace", "backup", "--to", source, "--format=json").Success(t)
	backupHead := strings.Trim(string(backup.Data["head"]), "\"")

	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	winner := replaceShared(t, b, first, "newer published")
	lateProject := filepath.Join(t.TempDir(), "late")
	if err := os.Mkdir(lateProject, 0700); err != nil {
		t.Fatal(err)
	}
	b.Run(t, "project", "bind", lateProject, "--slug", "added-after-backup", "--format=json").Success(t)
	added := obj(t, putDoc(t, b, "introduced later", "discarded from live view", "shared", "full").Success(t), "document")
	b.Run(t, "sync", "--format=json").Success(t)
	expected := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	replaceShared(t, b, winner, "peer conflicting")
	replaceShared(t, b, second, "peer independent")
	replaceShared(t, b, added, "peer edit to removed record")

	// Model loss of A's live catalog by recovering only from the selected
	// snapshot, with no copied auth and no checkout from the original device.
	a.DataDir = filepath.Join(t.TempDir(), "recovered")
	a.Run(t, "workspace", "recover-local", "--from", source, "--format=json").Success(t)
	if got := workspaceBody(t, a, saved["id"].(string)); got["body"] != "backed-up queue" {
		t.Fatal(got)
	}
	failure(t, a.Run(t, "workspace", "restore", "--from", source, "--if-head", expected, "--format=json"), "usage")
	failure(t, a.Run(t, "workspace", "restore", "--from", source, "--new-epoch", "--if-head", backupHead, "--format=json"), "stale-head")
	args := []string{"workspace", "restore", "--from", source, "--new-epoch", "--if-head", expected, "--format=json"}
	a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "lost-response"))
	failure(t, a.Run(t, args...), "push-pending")
	after := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	restored := a.Run(t, args...).Success(t)
	if string(restored.Data["head"]) != "\""+after+"\"" || bytes.Equal(backup.Data["epoch"], restored.Data["epoch"]) {
		t.Fatal(restored)
	}
	if got := git(t, a, "--git-dir", remote, "rev-parse", after+"^"); got != expected {
		t.Fatalf("restore lost published history: %s", got)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	peer := b.Run(t, "sync", "--format=json").Success(t)
	if string(peer.Data["state"]) != "\"conflict\"" || !bytes.Equal(peer.Data["epoch"], restored.Data["epoch"]) {
		t.Fatal(peer)
	}
	for _, check := range []struct {
		id                  string
		base, local, remote string
	}{
		{first["id"].(string), "newer published", "peer conflicting", "baseline"},
		{added["id"].(string), "discarded from live view", "peer edit to removed record", "discarded from live view"},
	} {
		conflict := b.Run(t, "sync", "conflicts", check.id, "--format=json").Success(t)
		for side, body := range map[string]string{"base": check.base, "local": check.local, "remote": check.remote} {
			if payload := obj(t, conflict, side)["payload"].(map[string]any); payload["body"] != body {
				t.Fatalf("lost %s for conflict %s: %v", side, check.id, payload)
			}
		}
	}
	a.Run(t, "sync", "--format=json").Success(t)
	for _, check := range []struct {
		h    *testcli.Harness
		doc  map[string]any
		body string
	}{
		{a, first, "baseline"}, {a, second, "peer independent"}, {b, saved, "backed-up queue"}, {b, third, "backed-up change"},
	} {
		if got := workspaceBody(t, check.h, check.doc["id"].(string)); got["body"] != check.body {
			t.Fatal(got)
		}
	}
	if listed := b.Run(t, "project", "list", "--all", "--format=json").Success(t); bytes.Contains(listed.Data["projects"], []byte("added-after-backup")) {
		t.Fatal("post-backup Project remains visible", listed)
	}
	failure(t, a.Run(t, "context", "get", added["id"].(string), "--project", "checkout", "--format=json"), "document-not-found")
}

func TestWorkspaceLocalOnlyRecoveryValidatesAndDoesNotOverwrite(t *testing.T) {
	a := contextHarness(t)
	local := obj(t, putDoc(t, a, "private", "local-only backup", "local-only", "full").Success(t), "document")
	path := filepath.Join(t.TempDir(), "backup")
	r := a.Run(t, "workspace", "backup", "--to", path, "--include-local-only", "--format=json").Success(t)
	if r.Data["head"] != nil {
		t.Fatal(r)
	}
	failure(t, a.Run(t, "workspace", "recover-local", "--from", path, "--format=json"), "destination-exists")
	a.DataDir = filepath.Join(t.TempDir(), "recovered")
	a.Run(t, "workspace", "recover-local", "--from", path, "--format=json").Success(t)
	if got := workspaceBody(t, a, local["id"].(string)); got["body"] != "local-only backup" {
		t.Fatal(got)
	}
	db, err := sql.Open("sqlite", filepath.Join(path, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("DELETE FROM context_bodies"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	failure(t, a.Run(t, "workspace", "recover-local", "--from", path, "--format=json"), "invalid-backup")
}

func TestWorkspaceRestoreWhileDaemonOwnsSync(t *testing.T) {
	a, remote, doc := initializedSync(t)
	source := filepath.Join(t.TempDir(), "backup")
	a.Run(t, "workspace", "backup", "--to", source, "--format=json").Success(t)
	replaceShared(t, a, doc, "newer shared body")
	a.Run(t, "sync", "--format=json").Success(t)
	expected := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")

	log := filepath.Join(t.TempDir(), "fetch")
	daemon := *a
	daemon.ExtraEnv = append(append([]string{}, a.ExtraEnv...), "CTXFERRY_TEST_FETCH_LOG="+log)
	startSyncProcess(t, &daemon, "sync", "serve")
	waitForFile(t, log)
	gate := filepath.Join(t.TempDir(), "restore-push")
	restore := *a
	restore.ExtraEnv = append(append([]string{}, a.ExtraEnv...), "CTXFERRY_TEST_PUSH_GATE="+gate)
	_, interrupt := startSyncProcess(t, &restore, "workspace", "restore", "--from", source, "--new-epoch", "--if-head", expected)
	waitForFile(t, gate+".entered")
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	waiter, _ := startSyncProcess(t, a, "sync")
	select {
	case r := <-waiter:
		t.Fatal("explicit sync completed while restore held the round lock", r)
	case <-time.After(300 * time.Millisecond):
	}
	interrupt()
	select {
	case r := <-waiter:
		if got := r.Success(t); string(got.Data["head"]) != "\""+head+"\"" {
			t.Fatal(got)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not reconcile the restore for the explicit waiter")
	}
	if got := workspaceBody(t, a, doc["id"].(string)); got["body"] != "baseline" {
		t.Fatal(got)
	}
	if got := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1"); got != head {
		t.Fatal("restore was published twice", got)
	}
}

func TestWorkspaceEpochReplansUnacknowledgedOldPush(t *testing.T) {
	a, remote, doc := initializedSync(t)
	source := filepath.Join(t.TempDir(), "backup")
	a.Run(t, "workspace", "backup", "--to", source, "--format=json").Success(t)
	replaceShared(t, a, doc, "newer base")
	a.Run(t, "sync", "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	id := doc["id"].(string)
	replaceShared(t, b, workspaceBody(t, b, id), "won before restore")
	b.ExtraEnv = append(b.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "lost-peer-response"))
	failure(t, b.Run(t, "sync", "--format=json"), "push-pending")
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	a.Run(t, "workspace", "restore", "--from", source, "--new-epoch", "--if-head", head, "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	detail := b.Run(t, "sync", "conflicts", id, "--format=json").Success(t)
	for side, want := range map[string]string{"base": "newer base", "local": "won before restore", "remote": "baseline"} {
		if got := obj(t, detail, side)["payload"].(map[string]any)["body"]; got != want {
			t.Fatalf("lost %s while reconciling old push: %v", side, got)
		}
	}
}
