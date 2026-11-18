package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func recoveryDevice(t *testing.T) (*testcli.Harness, string, map[string]any) {
	t.Helper()
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	d := obj(t, putDoc(t, a, "shared", "baseline", "shared", "full").Success(t), "document")
	enableSync(t, a)
	return a, remote, d
}
func assertSyncState(t *testing.T, h *testcli.Harness, state string) {
	t.Helper()
	r := h.Run(t, "sync", "status", "--format=json").Success(t)
	if string(r.Data["state"]) != `"`+state+`"` {
		t.Fatal(r)
	}
}
func TestLostPushResponseWithDescendant(t *testing.T) {
	for _, initialization := range []bool{true, false} {
		t.Run(map[bool]string{true: "init", false: "routine"}[initialization], func(t *testing.T) {
			a, remote, d := recoveryDevice(t)
			preview := previewSync(t, a)
			if !initialization {
				a.Run(t, "sync", "init", "--preview", preview, "--format=json").Success(t)
				replaceShared(t, a, d, "published once")
			}
			a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "fault"))
			args := []string{"sync", "--format=json"}
			if initialization {
				args = []string{"sync", "init", "--preview", preview, "--format=json"}
			}
			failure(t, a.Run(t, args...), "push-pending")
			candidate := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
			b := joinDevice(t, a, true)
			b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
			got := obj(t, b.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
			replaceShared(t, b, got, "peer descendant")
			b.Run(t, "sync", "--format=json").Success(t)
			head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
			if head == candidate {
				t.Fatal("missing descendant")
			}
			a.Run(t, args...).Success(t)
			a.Run(t, "sync", "--format=json").Success(t)
			if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
				t.Fatal("duplicate publication")
			}
			got = obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
			if got["body"] != "peer descendant" {
				t.Fatal(got)
			}
			assertSyncState(t, a, "synced")
		})
	}
}
func TestLeaseRaceReconcilesBusinessBaselines(t *testing.T) {
	for _, sameRecord := range []bool{false, true} {
		t.Run(map[bool]string{false: "independent", true: "conflict"}[sameRecord], func(t *testing.T) {
			a, remote, first := recoveryDevice(t)
			second := obj(t, putDoc(t, a, "second", "baseline two", "shared", "full").Success(t), "document")
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			b := joinDevice(t, a, true)
			b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
			replaceShared(t, a, first, "A queued")
			target := second
			if sameRecord {
				target = first
			}
			replaceShared(t, b, target, "B winner")
			hook := filepath.Join(remote, "hooks", "pre-receive")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			failure(t, b.Run(t, "sync", "--format=json"), "push-pending")
			if err := os.Remove(hook); err != nil {
				t.Fatal(err)
			}
			a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "fault"), "CTXFERRY_TEST_RACE_GIT="+filepath.Join(b.DataDir, "workspaces/default/sync.git"))
			r := a.Run(t, "sync", "--format=json").Success(t)
			want := `"synced"`
			if sameRecord {
				want = `"conflict"`
			}
			if string(r.Data["state"]) != want {
				t.Fatal(r)
			}
			got := obj(t, a.Run(t, "context", "get", first["id"].(string), "--format=json").Success(t), "document")
			if got["body"] != "A queued" {
				t.Fatal(got)
			}
			if !sameRecord {
				got = obj(t, a.Run(t, "context", "get", second["id"].(string), "--format=json").Success(t), "document")
				if got["body"] != "B winner" {
					t.Fatal(got)
				}
			}
		})
	}
}
func TestHistoryRewriteAndMissingBranchPreserveWork(t *testing.T) {
	a, remote, d := recoveryDevice(t)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	old := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	replaceShared(t, a, d, "local work")
	git(t, a, "--git-dir", remote, "update-ref", "-d", "refs/heads/ctxferry-v1")
	failure(t, a.Run(t, "sync", "--format=json"), "missing-branch")
	if remoteRefs(t, a, remote) != "" {
		t.Fatal("reinitialized missing branch")
	}
	// Re-root the valid tree to distinguish rewrite from catalog damage.
	tree := git(t, a, "--git-dir", remote, "rev-parse", old+"^{tree}")
	root := git(t, a, "--git-dir", remote, "commit-tree", tree, "-m", "replacement root")
	git(t, a, "--git-dir", remote, "update-ref", "refs/heads/ctxferry-v1", root)
	failure(t, a.Run(t, "sync", "--format=json"), "history-rewritten")
	assertSyncState(t, a, "history-rewritten")
	got := obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "local work" || git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != root {
		t.Fatal(got)
	}
}
func TestFutureAndPartialCatalogsRefused(t *testing.T) {
	for _, mode := range []string{"version", "field", "partial", "object"} {
		t.Run(mode, func(t *testing.T) {
			a, remote, d := recoveryDevice(t)
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			before := a.Run(t, "sync", "status", "--format=json").Success(t)
			checkout := filepath.Join(t.TempDir(), "catalog")
			git(t, a, "clone", "--branch", "ctxferry-v1", remote, checkout)
			path := strings.Fields(git(t, a, "-C", checkout, "ls-files", "records/*"))[0]
			if mode == "object" {
				path = strings.Fields(git(t, a, "-C", checkout, "ls-files", "objects/*"))[0]
			}
			full := filepath.Join(checkout, path)
			raw, err := os.ReadFile(full)
			if err != nil {
				t.Fatal(err)
			}
			want := "unsupported-schema"
			switch mode {
			case "version", "field":
				var v map[string]any
				json.Unmarshal(raw, &v)
				if mode == "version" {
					v["schemaVersion"] = 99
				} else {
					v["futureField"] = "must survive"
				}
				raw, _ = json.Marshal(v)
			case "partial":
				raw = raw[:len(raw)/2]
				want = "incomplete"
			case "object":
				raw = []byte("corrupt")
				want = "incomplete"
			}
			if err = os.WriteFile(full, raw, 0600); err != nil {
				t.Fatal(err)
			}
			git(t, a, "-C", checkout, "add", ".")
			git(t, a, "-C", checkout, "commit", "-m", "fault")
			git(t, a, "-C", checkout, "push")
			failure(t, a.Run(t, "sync", "--format=json"), want)
			assertSyncState(t, a, want)
			after := a.Run(t, "sync", "status", "--format=json").Success(t)
			if string(before.Data["head"]) != string(after.Data["head"]) || string(before.Data["lastSyncAt"]) != string(after.Data["lastSyncAt"]) {
				t.Fatal("advanced invalid snapshot")
			}
			got := obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
			if got["body"] != "baseline" {
				t.Fatal(got)
			}
		})
	}
}

func TestInitializationRaceValidatesWinner(t *testing.T) {
	a, remote, d := recoveryDevice(t)
	b := contextHarness(t)
	b.GitPath = a.GitPath
	b.ExtraEnv = a.ExtraEnv
	enableSync(t, b)
	preview := previewSync(t, b)
	hook := filepath.Join(remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	failure(t, b.Run(t, "sync", "init", "--preview", preview, "--format=json"), "push-pending")
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "fault"), "CTXFERRY_TEST_RACE_GIT="+filepath.Join(b.DataDir, "workspaces/default/sync.git"))
	failure(t, a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json"), "branch-exists")
	winner := git(t, b, "--git-dir", filepath.Join(b.DataDir, "workspaces/default/sync.git"), "rev-parse", "refs/ctxferry/pending")
	if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != winner {
		t.Fatal("overwrote winner")
	}
	assertSyncState(t, a, "pending")
	got := obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "baseline" {
		t.Fatal(got)
	}
}
func TestTransportFailuresAndRetryBudget(t *testing.T) {
	for _, code := range []string{"git-error", "auth-required", "unlock-required", "incomplete", "unsupported-schema", "missing-branch"} {
		t.Run(code, func(t *testing.T) {
			a, remote, d := recoveryDevice(t)
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
			replaceShared(t, a, d, "pending local")
			log := filepath.Join(t.TempDir(), "fetches")
			a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_FETCH_LOG="+log, "CTXFERRY_TEST_FETCH_FAILURE="+code)
			start := time.Now()
			failure(t, a.Run(t, "sync", "--format=json"), code)
			elapsed := time.Since(start)
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			attempts := strings.Count(string(raw), "fetch\n")
			want := 1
			state := code
			if code == "git-error" {
				want = 4
				state = "offline"
				if elapsed < 1750*time.Millisecond {
					t.Fatal("missing exponential delay", elapsed)
				}
			}
			if attempts != want {
				t.Fatal(attempts, want)
			}
			assertSyncState(t, a, state)
			if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
				t.Fatal("failure published")
			}
			got := obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
			if got["body"] != "pending local" {
				t.Fatal(got)
			}
			a.ExtraEnv = a.ExtraEnv[:len(a.ExtraEnv)-2]
			a.Run(t, "sync", "--format=json").Success(t)
			assertSyncState(t, a, "synced")
		})
	}
}
func TestPartialGitReadsAndAncestryFailure(t *testing.T) {
	for _, mode := range []string{"tree", "blob", "ancestry"} {
		t.Run(mode, func(t *testing.T) {
			a, remote, d := recoveryDevice(t)
			a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
			replaceShared(t, a, d, "uncertain work")
			a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "fault"))
			failure(t, a.Run(t, "sync", "--format=json"), "push-pending")
			head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
			code := "incomplete"
			if mode == "ancestry" {
				a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_ANCESTRY_FAILURE=1")
				code = "git-error"
			} else {
				a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PARTIAL_READ="+mode)
			}
			failure(t, a.Run(t, "sync", "--format=json"), code)
			if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
				t.Fatal("republished")
			}
			status := a.Run(t, "sync", "status", "--format=json").Success(t)
			if string(status.Data["pending"]) != "1" {
				t.Fatal("lost journal/outbox", status)
			}
			a.ExtraEnv = a.ExtraEnv[:len(a.ExtraEnv)-1]
			a.Run(t, "sync", "--format=json").Success(t)
			assertSyncState(t, a, "synced")
		})
	}
}

func TestServerSideLeaseRace(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX receive hook fixture")
	}
	for _, initialization := range []bool{false, true} {
		t.Run(map[bool]string{false: "routine", true: "init"}[initialization], func(t *testing.T) {
			a, remote, first := recoveryDevice(t)
			second := obj(t, putDoc(t, a, "second", "baseline two", "shared", "full").Success(t), "document")
			var b *testcli.Harness
			expected := strings.Repeat("0", 40)
			if initialization {
				b = contextHarness(t)
				b.GitPath = a.GitPath
				b.ExtraEnv = a.ExtraEnv
				enableSync(t, b)
			} else {
				a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
				expected = git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
				b = joinDevice(t, a, true)
				b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
				replaceShared(t, a, first, "A queued")
				replaceShared(t, b, second, "B server winner")
			}
			hook := filepath.Join(remote, "hooks", "pre-receive")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"sync", "--format=json"}
			if initialization {
				args = []string{"sync", "init", "--preview", previewSync(t, b), "--format=json"}
			}
			failure(t, b.Run(t, args...), "push-pending")
			peer := filepath.Join(b.DataDir, "workspaces/default/sync.git")
			candidate := git(t, b, "--git-dir", peer, "rev-parse", "refs/ctxferry/pending")
			// Seed the winner's objects, then change the branch *inside receive-pack*,
			// after advertisement and before its atomic ref update.
			git(t, a, "--git-dir", remote, "fetch", peer, "refs/ctxferry/pending:refs/ctxferry/racer")
			script := "#!/bin/sh\nunset GIT_QUARANTINE_PATH\ngit update-ref refs/heads/ctxferry-v1 " + candidate + " " + expected + "\nexit 0\n"
			if err := os.WriteFile(hook, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			// Simulate a Git version/helper that supplies no stable rejection
			// text. Recovery must use the remote ref, never diagnostic wording.
			realGit, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			wrapper := t.TempDir()
			quotedGit := "'" + strings.ReplaceAll(realGit, "'", "'\"'\"'") + "'"
			script = "#!/bin/sh\nfor arg do\n if test \"$arg\" = push; then\n  " + quotedGit + " \"$@\" >/dev/null 2>&1\n  exit $?\n fi\ndone\nexec " + quotedGit + " \"$@\"\n"
			if err = os.WriteFile(filepath.Join(wrapper, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			a.GitPath = wrapper + string(os.PathListSeparator) + a.GitPath
			if initialization {
				failure(t, a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json"), "branch-exists")
				if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != candidate {
					t.Fatal("replaced initializer")
				}
			} else {
				a.Run(t, "sync", "--format=json").Success(t)
				got := obj(t, a.Run(t, "context", "get", second["id"].(string), "--format=json").Success(t), "document")
				if got["body"] != "B server winner" {
					t.Fatal(got)
				}
				got = obj(t, a.Run(t, "context", "get", first["id"].(string), "--format=json").Success(t), "document")
				if got["body"] != "A queued" {
					t.Fatal(got)
				}
			}
		})
	}
}
func TestCorruptLooseGitObjectIsIncomplete(t *testing.T) {
	a, remote, d := recoveryDevice(t)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	replaceShared(t, a, d, "pending safe work")
	before := a.Run(t, "sync", "status", "--format=json").Success(t)
	managed := filepath.Join(a.DataDir, "workspaces/default/sync.git")
	paths := strings.Fields(git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1", "objects/"))
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	blob := git(t, a, "--git-dir", managed, "rev-parse", head+":"+paths[0])
	objectPath := filepath.Join(managed, "objects", blob[:2], blob[2:])
	if err := os.Chmod(objectPath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	failure(t, a.Run(t, "sync", "--format=json"), "incomplete")
	assertSyncState(t, a, "incomplete")
	after := a.Run(t, "sync", "status", "--format=json").Success(t)
	if string(before.Data["head"]) != string(after.Data["head"]) || string(before.Data["lastSyncAt"]) != string(after.Data["lastSyncAt"]) || string(after.Data["pending"]) != "1" {
		t.Fatal("corruption changed durable state", after)
	}
	got := obj(t, a.Run(t, "context", "get", d["id"].(string), "--format=json").Success(t), "document")
	if got["body"] != "pending safe work" {
		t.Fatal(got)
	}
}

func TestLostPushPreservesConflictAndDescendantTombstone(t *testing.T) {
	a, remote, d := recoveryDevice(t)
	other := obj(t, putDoc(t, a, "independent", "other base", "shared", "full").Success(t), "document")
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	b.Run(t, "project", "bind", b.Dir, "--project", "checkout", "--format=json").Success(t)
	replaceShared(t, a, d, "remote conflict body")
	a.Run(t, "sync", "--format=json").Success(t)
	replaceShared(t, b, d, "local conflict body")
	replaceShared(t, b, other, "independent accepted")
	b.ExtraEnv = append(b.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "lost-independent"))
	failure(t, b.Run(t, "sync", "--format=json"), "push-pending")
	token := conflictToken(t, b, d["id"].(string))
	candidate := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	a.Run(t, "sync", "--format=json").Success(t)
	accepted := getShared(t, a, other["id"].(string))
	if accepted["body"] != "independent accepted" {
		t.Fatal(accepted)
	}
	a.Run(t, "context", "delete", other["id"].(string), "--if-revision", accepted["revision"].(string), "--format=json").Success(t)
	a.Run(t, "sync", "--format=json").Success(t)
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	if head == candidate {
		t.Fatal("missing descendant tombstone")
	}
	b.Run(t, "sync", "--format=json").Success(t)
	if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
		t.Fatal("acknowledgement republished held work")
	}
	failure(t, b.Run(t, "context", "get", other["id"].(string), "--format=json"), "document-not-found")
	if conflictToken(t, b, d["id"].(string)) != token {
		t.Fatal("lost conflict identity")
	}
	detail := b.Run(t, "sync", "conflicts", d["id"].(string), "--format=json").Success(t)
	for side, want := range map[string]string{"base": "baseline", "local": "local conflict body", "remote": "remote conflict body"} {
		if obj(t, detail, side)["payload"].(map[string]any)["body"] != want {
			t.Fatal(side, detail)
		}
	}
	// Fatal recovery states must be reported alongside retained conflict evidence.
	for _, code := range []string{"incomplete", "unsupported-schema", "unlock-required", "history-rewritten"} {
		b.ExtraEnv = append(b.ExtraEnv, "CTXFERRY_TEST_FETCH_FAILURE="+code)
		failure(t, b.Run(t, "sync", "--format=json"), code)
		assertSyncState(t, b, code)
		if conflictToken(t, b, d["id"].(string)) != token {
			t.Fatal("failure lost conflict")
		}
		b.ExtraEnv = b.ExtraEnv[:len(b.ExtraEnv)-1]
	}
	b.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "resolve", d["id"].(string), "--if-conflict", token, "--take", "local", "--format=json").Success(t)
	resolved := getShared(t, b, d["id"].(string))
	b.ExtraEnv = b.ExtraEnv[:len(b.ExtraEnv)-1]
	b.ExtraEnv = append(b.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "lost-resolution"))
	failure(t, b.Run(t, "sync", "--format=json"), "push-pending")
	published := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	b.Run(t, "sync", "--format=json").Success(t)
	if git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1") != published {
		t.Fatal("resolution duplicated")
	}
	a.Run(t, "sync", "--format=json").Success(t)
	if got := getShared(t, a, d["id"].(string)); got["revision"] != resolved["revision"] || got["body"] != "local conflict body" {
		t.Fatal(got)
	}
	failure(t, a.Run(t, "context", "get", other["id"].(string), "--format=json"), "document-not-found")
	assertSyncState(t, b, "synced")
}
