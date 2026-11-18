package tests

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func syncHarness(t *testing.T, response *atomic.Value) (*testcli.Harness, string) {
	t.Helper()
	h := contextHarness(t)
	path, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	h.GitPath = filepath.Dir(path)
	remote := filepath.Join(t.TempDir(), "remote.git")
	git(t, h, "init", "--bare", remote)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/example/context" || r.Header.Get("Authorization") != "Bearer credential-sentinel" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, response.Load().(string))
	}))
	t.Cleanup(server.Close)
	h.ExtraEnv = []string{"CTXFERRY_TEST_API=" + server.URL, "CTXFERRY_TEST_REMOTE=" + remote, "GH_TOKEN=credential-sentinel", "GIT_DIR=/must-not-be-used", "GIT_WORK_TREE=/must-not-be-used", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=url.https://evil.invalid/.insteadOf", "GIT_CONFIG_VALUE_0=https://github.com/"}
	return h, remote
}

const privateMetadata = `{"id":42,"full_name":"example/context","private":true}`

func enableSync(t *testing.T, h *testcli.Harness) testcli.Response {
	t.Helper()
	return h.Run(t, "sync", "enable", "--remote=https://github.com/example/context.git", "--format=json").Success(t)
}
func previewSync(t *testing.T, h *testcli.Harness) string {
	t.Helper()
	v := h.Run(t, "sync", "preview", "--format=json").Success(t)
	var id string
	if err := json.Unmarshal(v.Data["previewId"], &id); err != nil {
		t.Fatal(err)
	}
	return id
}
func remoteRefs(t *testing.T, h *testcli.Harness, remote string) string {
	return git(t, h, "--git-dir", remote, "for-each-ref", "--format=%(refname)")
}
func TestFirstPrivatePublish(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	shared := obj(t, putDoc(t, h, "shared notes", "approvedbody", "shared", "summary").Success(t), "document")
	local := obj(t, putDoc(t, h, "localsentinel", "localbodysecret", "local-only", "full").Success(t), "document")
	v := enableSync(t, h)
	check := obj(t, v, "check")
	if check["gitReachable"] != true || check["privacy"] != "private" {
		t.Fatal(check)
	}
	preview := previewSync(t, h)
	if refs := remoteRefs(t, h, remote); refs != "" {
		t.Fatal("preview uploaded", refs)
	}
	before, err := os.ReadDir(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	published := h.Run(t, "sync", "init", "--preview", preview, "--format=json").Success(t)
	if string(published.Data["state"]) != `"synced"` {
		t.Fatal(published)
	}
	after, _ := os.ReadDir(h.Dir)
	if len(before) != len(after) {
		t.Fatal("source directory modified")
	}
	parents := git(t, h, "--git-dir", remote, "rev-list", "--parents", "ctxferry-v1")
	if len(strings.Fields(parents)) != 1 {
		t.Fatal("not root", parents)
	}
	paths := strings.Fields(git(t, h, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1"))
	hasDoc, hasObject := false, false
	for _, path := range paths {
		raw := git(t, h, "--git-dir", remote, "show", "ctxferry-v1:"+path)
		for _, sentinel := range []string{"credential-sentinel", "localsentinel", "localbodysecret", local["id"].(string), h.Dir, h.Home, "catalog.sqlite"} {
			if strings.Contains(raw, sentinel) {
				t.Fatal("private value published", path, sentinel)
			}
		}
		if strings.HasPrefix(path, "records/") {
			var env map[string]any
			if err = json.Unmarshal([]byte(raw), &env); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"schemaVersion", "id", "kind", "workspaceId", "scope", "revision", "previousRevision", "operationId", "deleted", "provenance", "policy", "payload"} {
				if _, ok := env[key]; !ok {
					t.Fatal("envelope missing", key)
				}
			}
			if env["id"] == shared["id"] {
				hasDoc = true
				manifest := env["payload"].(map[string]any)["body"].(map[string]any)
				body := git(t, h, "--git-dir", remote, "show", "ctxferry-v1:objects/"+manifest["sha256"].(string))
				if body != "approvedbody" {
					t.Fatal("disclosure incorrectly removed shared body")
				}
			}
		}
		if strings.HasPrefix(path, "objects/") {
			hasObject = true
			sum := sha256.Sum256([]byte(raw))
			if strings.TrimPrefix(path, "objects/") != hex.EncodeToString(sum[:]) {
				t.Fatal("bad object hash")
			}
		}
	}
	if !hasDoc || !hasObject {
		t.Fatal("missing record/object")
	}
	failure(t, h.Run(t, "sync", "init", "--preview", preview, "--format=json"), "already-published")
	// Access config and operational state never contain auth tokens.
	db, err := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces/default/catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored string
	if err = db.QueryRow("SELECT remote || workspace_id || branch || identity FROM sync_config").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "credential-sentinel") {
		t.Fatal("credential persisted")
	}
	for _, args := range [][]string{{"sync", "status"}, {"sync", "check"}} {
		r := h.Run(t, append(args, "--format=json")...)
		r.Success(t)
		if strings.Contains(r.Stdout+r.Stderr, "credential-sentinel") {
			t.Fatal("credential leaked")
		}
	}
}
func TestPrivacyFailsClosed(t *testing.T) {
	for name, body := range map[string]string{"public": `{"id":42,"full_name":"example/context","private":false}`, "missing-private": `{"id":42,"full_name":"example/context"}`, "wrong-identity": `{"id":42,"full_name":"other/repo","private":true}`, "malformed": "{", "null-private": `{"id":42,"full_name":"example/context","private":null}`} {
		t.Run(name, func(t *testing.T) {
			var metadata atomic.Value
			metadata.Store(body)
			h, remote := syncHarness(t, &metadata)
			v := enableSync(t, h)
			if obj(t, v, "check")["gitReachable"] != true || string(v.Data["state"]) != `"unverified"` {
				t.Fatal(v)
			}
			failure(t, h.Run(t, "sync", "preview", "--format=json"), "sync-disabled")
			if remoteRefs(t, h, remote) != "" {
				t.Fatal("upload before privacy proof")
			}
			putDoc(t, h, "offline", "local still works", "local-only", "full").Success(t)
		})
	}
}
func TestPrivacyRecheckedAndPreviewStale(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, h, "shared", "sharedbody", "shared", "full").Success(t), "document")
	enableSync(t, h)
	preview := previewSync(t, h)
	metadata.Store(`{"id":42,"full_name":"example/context","private":false}`)
	failure(t, h.Run(t, "sync", "init", "--preview", preview, "--format=json"), "privacy-unconfirmed")
	metadata.Store(`{"id":43,"full_name":"example/context","private":true}`)
	failure(t, h.Run(t, "sync", "init", "--preview", preview, "--format=json"), "privacy-unconfirmed")
	metadata.Store(privateMetadata)
	putDoc(t, h, "shared", "withdrawn", "local-only", "full", d["id"].(string), "--if-revision", d["revision"].(string)).Success(t)
	failure(t, h.Run(t, "sync", "init", "--preview", preview, "--format=json"), "stale-preview")
	if remoteRefs(t, h, remote) != "" {
		t.Fatal("stale preview published")
	}
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	tree := git(t, h, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1")
	if strings.Contains(tree, d["id"].(string)) || strings.Contains(tree, "objects/") {
		t.Fatal("withdrawn record or body published")
	}
}
func TestExistingBranchAndPending(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	enableSync(t, h)
	preview := previewSync(t, h)
	// Another initializer wins before this client's expected-absent publication.
	git(t, h, "init")
	git(t, h, "commit", "--allow-empty", "-m", "other root")
	winner := git(t, h, "rev-parse", "HEAD")
	git(t, h, "push", remote, "HEAD:refs/heads/ctxferry-v1")
	failure(t, h.Run(t, "sync", "init", "--preview", preview, "--format=json"), "incomplete")
	if git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1") != winner {
		t.Fatal("overwrote winner")
	}
	status := h.Run(t, "sync", "status", "--format=json").Success(t)
	if string(status.Data["state"]) != `"incomplete"` {
		t.Fatal("invalid competing catalog not reported")
	}
}
func TestExportRejectsUnsupportedReferences(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, h, "shared", "body", "shared", "full").Success(t), "document")
	enableSync(t, h)
	db, err := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces/default/catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE records SET metadata=json_set(metadata,'$.scope.id','local-only-record') WHERE id=?`, d["id"])
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	failure(t, h.Run(t, "sync", "preview", "--format=json"), "invalid-reference")
	if remoteRefs(t, h, remote) != "" {
		t.Fatal("broken dependency uploaded")
	}
}

func TestIndependentReachabilityAndAuthentication(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, _ := syncHarness(t, &metadata)
	// Metadata can be confirmed while Git is unavailable.
	h.ExtraEnv = append(h.ExtraEnv, "CTXFERRY_TEST_REMOTE="+filepath.Join(t.TempDir(), "missing.git"))
	v := enableSync(t, h)
	check := obj(t, v, "check")
	if check["gitReachable"] != false || check["privacy"] != "private" || string(v.Data["state"]) != `"unverified"` {
		t.Fatal(v)
	}
	var other atomic.Value
	other.Store(privateMetadata)
	h, remote := syncHarness(t, &other)
	h.ExtraEnv = append(h.ExtraEnv, "GH_TOKEN=wrong-authentication")
	v = enableSync(t, h)
	check = obj(t, v, "check")
	if check["gitReachable"] != true || check["privacy"] != "unknown" {
		t.Fatal(v)
	}
	if remoteRefs(t, h, remote) != "" {
		t.Fatal("auth failure uploaded")
	}
}
func TestSyncRejectsURLRewrite(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	path := filepath.Join(h.Home, ".gitconfig")
	if err := os.WriteFile(path, []byte("[url \"https://evil.invalid/\"]\n\tinsteadOf = https://github.com/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	failure(t, h.Run(t, "sync", "enable", "--remote=https://github.com/example/context", "--format=json"), "unsafe-git-config")
	if remoteRefs(t, h, remote) != "" {
		t.Fatal("redirected upload")
	}
	for _, url := range []string{"https://token@github.com/example/context.git", "https://github.com/example/context?token=credential-sentinel", "ext::command", "-option"} {
		r := h.Run(t, "sync", "enable", "--remote", url, "--format=json")
		failure(t, r, "invalid-remote")
		if strings.Contains(r.Stdout+r.Stderr, "credential-sentinel") {
			t.Fatal("credential URL echoed")
		}
	}
}
func TestPendingBeforeRejectedPushAndRestart(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fixture hook")
	}
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	putDoc(t, h, "shared", "body", "shared", "full").Success(t)
	enableSync(t, h)
	preview := previewSync(t, h)
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho credential-sentinel >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := h.Run(t, "sync", "init", "--preview", preview, "--format=json")
	failure(t, r, "push-pending")
	if strings.Contains(r.Stdout+r.Stderr, "credential-sentinel") {
		t.Fatal("Git diagnostic leaked")
	}
	status := h.Run(t, "sync", "status", "--format=json").Success(t)
	if string(status.Data["state"]) != `"pending"` {
		t.Fatal("lost pending")
	}
	// The candidate exists durably even though the remote rejected it.
	db, err := sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces/default/catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	err = db.QueryRow("SELECT pending FROM sync_state").Scan(&raw)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	var pending map[string]any
	if err = json.Unmarshal([]byte(raw), &pending); err != nil {
		t.Fatal(err)
	}
	candidate := pending["candidateCommit"].(string)
	managed := filepath.Join(h.DataDir, "workspaces/default/sync.git")
	if git(t, h, "--git-dir", managed, "rev-parse", "refs/ctxferry/pending") != candidate || pending["expectedHead"] != "" {
		t.Fatal("pending precondition/candidate")
	}
	if err = os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	// A separate CLI process retries precisely the recorded root.
	success := h.Run(t, "sync", "init", "--preview", preview, "--format=json").Success(t)
	if string(success.Data["head"]) != fmt.Sprintf("%q", candidate) {
		t.Fatal("candidate regenerated")
	}
	// Simulate a lost response/local acknowledgement after acceptance. No second push
	// is needed when the branch points at the durable candidate.
	db, err = sql.Open("sqlite", filepath.Join(h.DataDir, "workspaces/default/catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE sync_state SET confirmed_head='',pending=?", raw)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	h.Run(t, "sync", "init", "--preview", preview, "--format=json").Success(t)
	if git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1") != candidate {
		t.Fatal("remote changed during reconciliation")
	}
}

func TestSyncResolvesIsolationSymlinks(t *testing.T) {
	for _, kind := range []string{"data-root", "managed-repository"} {
		t.Run(kind, func(t *testing.T) {
			var metadata atomic.Value
			metadata.Store(privateMetadata)
			h, _ := syncHarness(t, &metadata)
			git(t, h, "init")
			var target, link string
			if kind == "data-root" {
				target = filepath.Join(h.Dir, "state")
				link = h.DataDir
				if err := os.Rename(h.DataDir, target); err != nil {
					t.Fatal(err)
				}
			} else {
				target = h.Dir
				link = filepath.Join(h.DataDir, "workspaces/default/sync.git")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("directory symlinks unavailable: %v", err)
			}
			failure(t, h.Run(t, "sync", "enable", "--remote=https://github.com/example/context", "--format=json"), "invalid-path")
			if kind == "data-root" {
				if _, err := os.Stat(filepath.Join(target, "workspaces/default/sync.git")); !os.IsNotExist(err) {
					t.Fatal("created managed Git in checkout", err)
				}
			}
		})
	}
}
