package tests

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func startSyncProcess(t *testing.T, h *testcli.Harness, args ...string) (<-chan testcli.Result, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := h.Command(ctx, append(args, "--format=json")...)
	var out, diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostics
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	result := make(chan testcli.Result, 1)
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		result <- testcli.Result{Stdout: out.String(), Stderr: diagnostics.String(), ExitCode: cmd.ProcessState.ExitCode()}
		close(done)
	}()
	stop := func() {
		cancel()
		<-done
	}
	t.Cleanup(stop)
	return result, stop
}

func waitForSync(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	waitForSync(t, path, func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
}

func initializedSync(t *testing.T) (*testcli.Harness, string, map[string]any) {
	t.Helper()
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	h, remote := syncHarness(t, &metadata)
	doc := obj(t, putDoc(t, h, "shared", "baseline", "shared", "full").Success(t), "document")
	enableSync(t, h)
	h.Run(t, "sync", "init", "--preview", previewSync(t, h), "--format=json").Success(t)
	return h, remote, doc
}

func TestSyncServeIsSingleOwnerAndExplicitSyncWaits(t *testing.T) {
	h, _, _ := initializedSync(t)
	gate := filepath.Join(t.TempDir(), "fetch")
	log := filepath.Join(t.TempDir(), "attempts")
	owner := *h
	owner.ExtraEnv = append(append([]string{}, h.ExtraEnv...), "CTXFERRY_TEST_FETCH_GATE="+gate, "CTXFERRY_TEST_FETCH_LOG="+log)
	_, stop := startSyncProcess(t, &owner, "sync", "serve")
	waitForFile(t, gate+".entered")
	failure(t, h.Run(t, "sync", "serve", "--format=json"), "sync-busy")

	waiter := *h
	waiter.ExtraEnv = append(append([]string{}, h.ExtraEnv...), "CTXFERRY_TEST_FETCH_LOG="+log)
	result, _ := startSyncProcess(t, &waiter, "sync")
	select {
	case early := <-result:
		t.Fatal("explicit sync returned before the owner made progress", early)
	case <-time.After(500 * time.Millisecond):
	}
	if err := os.WriteFile(gate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-result:
		if v := r.Success(t); string(v.Data["state"]) != `"synced"` {
			t.Fatal(v)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("explicit sync did not receive the owner's result")
	}
	if contents, err := os.ReadFile(log); err != nil || string(contents) != "fetch\n" {
		t.Fatalf("expected one fetch, got %q (%v)", contents, err)
	}
	stop()
	h.Run(t, "sync", "--format=json").Success(t)
}

func TestSyncServeCoalescesLocalEdits(t *testing.T) {
	h, remote, doc := initializedSync(t)
	base := git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	log := filepath.Join(t.TempDir(), "attempts")
	owner := *h
	owner.ExtraEnv = append(append([]string{}, h.ExtraEnv...), "CTXFERRY_TEST_FETCH_LOG="+log)
	startSyncProcess(t, &owner, "sync", "serve")
	waitForFile(t, log)

	for _, contents := range []string{"first change", "second change", "third change"} {
		doc = replaceShared(t, h, doc, contents)
	}
	waitForSync(t, "automatic publication", func() bool {
		return git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1") != base
	})
	peer := joinDevice(t, h, true)
	peer.Run(t, "project", "bind", peer.Dir, "--project", "checkout", "--format=json").Success(t)
	got := obj(t, peer.Run(t, "context", "get", doc["id"].(string), "--format=json").Success(t), "document")
	if got["revision"] != doc["revision"] || got["body"] != "third change" {
		t.Fatal("automatic sync published an intermediate edit", got)
	}
	if count := git(t, h, "--git-dir", remote, "rev-list", "--count", base+"..ctxferry-v1"); count != "1" {
		t.Fatal("expected one publication for the burst, got", count)
	}
}

func TestSyncServeRecoversInterruptedPublication(t *testing.T) {
	h, remote, doc := initializedSync(t)
	queued := replaceShared(t, h, doc, "publication survived interruption")
	gate := filepath.Join(t.TempDir(), "push")
	owner := *h
	owner.ExtraEnv = append(append([]string{}, h.ExtraEnv...), "CTXFERRY_TEST_PUSH_GATE="+gate)
	_, stop := startSyncProcess(t, &owner, "sync", "serve")
	waitForFile(t, gate+".entered")
	head := git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	stop()
	pending := h.Run(t, "sync", "status", "--format=json").Success(t)
	if string(pending.Data["state"]) != `"pending"` || string(pending.Data["pending"]) != "1" {
		t.Fatal("publication journal was lost", pending)
	}
	startSyncProcess(t, h, "sync", "serve")
	waitForSync(t, "automatic publication reconciliation after restart", func() bool {
		status := h.Run(t, "sync", "status", "--format=json").Success(t)
		return string(status.Data["state"]) == `"synced"` && string(status.Data["pending"]) == "0"
	})
	if git(t, h, "--git-dir", remote, "rev-parse", "ctxferry-v1") != head {
		t.Fatal("restart duplicated the accepted publication")
	}
	peer := joinDevice(t, h, true)
	peer.Run(t, "project", "bind", peer.Dir, "--project", "checkout", "--format=json").Success(t)
	peerDoc := obj(t, peer.Run(t, "context", "get", doc["id"].(string), "--format=json").Success(t), "document")
	if peerDoc["revision"] != queued["revision"] {
		t.Fatal("peer did not see recovered publication", peerDoc)
	}
}

func TestSyncInstallServiceWithoutManager(t *testing.T) {
	h := testcli.New(t, binary)
	h.Run(t, "init", "--format=json").Success(t)
	// The other platforms use system tools that may be available even when PATH
	// is isolated. Never register a real service from this scenario.
	if runtime.GOOS != "linux" {
		t.Skip("missing-systemctl scenario is Linux-specific")
	}
	r := h.Run(t, "sync", "install-service", "--format=json").Success(t)
	if string(r.Data["available"]) != "false" || string(r.Data["installed"]) != "false" ||
		!strings.Contains(string(r.Data["message"]), "foreground") {
		t.Fatal(r)
	}
}
