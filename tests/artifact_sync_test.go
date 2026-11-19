package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/testcli"
)

func addSyncArtifact(t *testing.T, h *testcli.Harness, name, content, sensitivity string) app.ArtifactManifest {
	t.Helper()
	writeArtifactFile(t, filepath.Join(h.Dir, name), content)
	args := artifactAddArgs(name)
	args[5] = "--sensitivity=" + sensitivity
	return artifactData[app.ArtifactManifest](t, h.Run(t, args...).Success(t))
}

func artifactStringField(t *testing.T, r testcli.Response, key string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(r.Data[key], &value); err != nil || value == "" {
		t.Fatalf("invalid %s in %+v", key, r)
	}
	return value
}

func queueArtifact(t *testing.T, h *testcli.Harness, id, mode string) testcli.Response {
	t.Helper()
	args := []string{"artifact", "share", id, "--mode", mode, "--format=json"}
	preview := h.Run(t, args...).Success(t)
	token := artifactStringField(t, preview, "previewId")
	h.Run(t, append(args, "--preview", token)...).Success(t)
	return preview
}

func inspectSyncedArtifact(t *testing.T, h *testcli.Harness, id string) app.ArtifactManifest {
	t.Helper()
	return artifactData[app.ArtifactManifest](t, h.Run(t, "artifact", "inspect", id, "--project", "checkout", "--format=json").Success(t))
}

func fetchSyncedArtifact(t *testing.T, h *testcli.Harness, id, want string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "fetched")
	h.Run(t, "artifact", "fetch", id, "--project", "checkout", "--output", target, "--format=json").Success(t)
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, []byte(want)) {
		t.Fatalf("fetched %d bytes, expected %d: %v", len(got), len(want), err)
	}
}

func artifactSyncPair(t *testing.T) (*testcli.Harness, *testcli.Harness, string) {
	t.Helper()
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	enableSync(t, a)
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	return a, joinDevice(t, a, true), remote
}

func TestArtifactSyncTwoDevices(t *testing.T) {
	a, b, remote := artifactSyncPair(t)
	// Raw byte limits apply even when the payload compresses extremely well.
	atLimit := strings.Repeat("a", 1<<20)
	m := addSyncArtifact(t, a, "report.json", atLimit, "internal")
	over := addSyncArtifact(t, a, "large.json", atLimit+"x", "internal")
	sensitive := addSyncArtifact(t, a, "restricted.json", "sensitive-body-sentinel", "sensitive")
	weights := addSyncArtifact(t, a, "weights.safetensors", "weights-body-sentinel", "public")
	dump := addSyncArtifact(t, a, "mislabeled.json", "PGDMP dump-body-sentinel", "internal")
	private := addSyncArtifact(t, a, "private.json", "unapproved-local-body-sentinel", "internal")

	preview := a.Run(t, "artifact", "share", m.ID, "--format=json").Success(t)
	if string(preview.Data["newPayloadBytes"]) != "1048576" || bytes.Contains(preview.Data["manifest"], []byte("command")) {
		t.Fatal(preview)
	}
	failure(t, a.Run(t, "artifact", "share", m.ID, "--mode", "explicit", "--preview", artifactStringField(t, preview, "previewId"), "--format=json"), "stale-preview")
	a.Run(t, "sync", "--format=json").Success(t)
	failure(t, b.Run(t, "artifact", "inspect", m.ID, "--project", "checkout", "--format=json"), "artifact-not-found")

	for _, item := range []app.ArtifactManifest{m, over, sensitive, weights, dump} {
		queueArtifact(t, a, item.ID, "automatic")
	}
	if got := inspectSyncedArtifact(t, a, m.ID); got.Sync.State != "pending" || got.Payload.Storage != "local-only" {
		t.Fatal(got)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	origin := inspectSyncedArtifact(t, a, m.ID)
	peer := inspectSyncedArtifact(t, b, m.ID)
	if origin.Sync.State != "shared" || origin.Payload != m.Payload || origin.Run.ID != m.Run.ID ||
		len(origin.Observations) != 0 || peer.Sync.State != "shared" || peer.Payload.Storage != "shared" ||
		peer.Run.ID != m.Run.ID || peer.Run.DeviceID != m.Run.DeviceID || peer.Run.Summary != m.Run.Summary ||
		peer.Run.Command != nil || len(peer.Observations) != 1 || peer.Observations[0].DeviceID == m.Run.DeviceID ||
		peer.Observations[0].SHA256 != m.Payload.SHA256 || peer.Observations[0].Scope != "payload-integrity" {
		t.Fatalf("sender %+v, receiver %+v", origin, peer)
	}
	fetchSyncedArtifact(t, b, m.ID, atLimit)
	for id, reason := range map[string]string{over.ID: "payload-size", sensitive.ID: "sensitive", weights.ID: "excluded-format", dump.ID: "excluded-format"} {
		local, got := inspectSyncedArtifact(t, a, id), inspectSyncedArtifact(t, b, id)
		if local.Payload.Storage != "local-only" || local.Sync.State != "metadata-only" ||
			got.Payload.Storage != "metadata-only" || got.Payload.Location != "" || got.Sync.Reason != reason || len(got.Observations) != 0 {
			t.Fatalf("sender %+v, receiver %+v", local, got)
		}
		failure(t, b.Run(t, "artifact", "fetch", id, "--project", "checkout", "--output", filepath.Join(t.TempDir(), "unavailable"), "--format=json"), "artifact-unavailable")
	}
	fetchSyncedArtifact(t, a, over.ID, atLimit+"x")
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")
	tree := git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", head)
	if strings.Count(tree, "objects/") != 1 || strings.Contains(tree, private.ID) {
		t.Fatal(tree)
	}
	for _, h := range []*testcli.Harness{a, b} {
		status := h.Run(t, "sync", "status", "--format=json").Success(t)
		if artifactStringField(t, status, "head") != head || string(status.Data["pending"]) != "0" {
			t.Fatal(status)
		}
	}
	// Reuse the same raw object from a distinct registration and publish only its record.
	duplicate := addSyncArtifact(t, a, "duplicate.json", atLimit, "public")
	reused := queueArtifact(t, a, duplicate.ID, "automatic")
	if string(reused.Data["newPayloadBytes"]) != "0" {
		t.Fatal(reused)
	}
	a.Run(t, "sync", "--format=json").Success(t)
	diff := git(t, a, "--git-dir", remote, "diff", "--name-only", head, "ctxferry-v1")
	if !strings.Contains(diff, duplicate.ID) || strings.Contains(diff, "objects/") {
		t.Fatal(diff)
	}
	b.Run(t, "sync", "--format=json").Success(t)
	if got := inspectSyncedArtifact(t, b, m.ID); len(got.Observations) != 1 || got.Observations[0] != peer.Observations[0] {
		t.Fatal("the original receiving observation changed", got)
	}
	unsubscribed := joinDevice(t, a, false)
	failure(t, unsubscribed.Run(t, "artifact", "inspect", m.ID, "--project", "checkout", "--format=json"), "not-subscribed")
}

func TestArtifactSyncHistoryPauseAndExplicitLimit(t *testing.T) {
	a, b, remote := artifactSyncPair(t)
	a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_HISTORY_BYTES="+strconv.Itoa(100<<20))
	warn := a.Run(t, "sync", "--format=json").Success(t)
	history := obj(t, warn, "history")
	if history["automaticPayloadPause"] != false || history["warning"] == nil {
		t.Fatal(history)
	}
	paused := addSyncArtifact(t, a, "paused.json", "pause-body-sentinel", "internal")
	queueArtifact(t, a, paused.ID, "automatic")
	ordinary := obj(t, putDoc(t, a, "unrelated", "ordinary catalog still travels", "shared", "full").Success(t), "document")
	a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_HISTORY_BYTES="+strconv.Itoa(250<<20))
	b.ExtraEnv = append(b.ExtraEnv, "CTXFERRY_TEST_HISTORY_BYTES="+strconv.Itoa(250<<20))
	for _, h := range []*testcli.Harness{a, b} {
		status := h.Run(t, "sync", "--format=json").Success(t)
		if history := obj(t, status, "history"); history["automaticPayloadPause"] != true || history["warning"] == nil {
			t.Fatal(status)
		}
	}
	if got := inspectSyncedArtifact(t, b, paused.ID); got.Sync.Reason != "history-pause" || got.Payload.Storage != "metadata-only" {
		t.Fatal(got)
	}
	if got := workspaceBody(t, b, ordinary["id"].(string)); got["body"] != "ordinary catalog still travels" {
		t.Fatal(got)
	}

	sample := strings.Repeat("x", 5<<20)
	explicit := addSyncArtifact(t, a, "sample.json", sample, "internal")
	tooLarge := addSyncArtifact(t, a, "too-large.json", sample+"!", "public")
	approved := queueArtifact(t, a, explicit.ID, "explicit")
	if string(approved.Data["newPayloadBytes"]) != "5242880" || obj(t, approved, "history")["automaticPayloadPause"] != true {
		t.Fatal(approved)
	}
	queueArtifact(t, a, tooLarge.ID, "explicit")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	fetchSyncedArtifact(t, b, explicit.ID, sample)
	if got := inspectSyncedArtifact(t, b, tooLarge.ID); got.Sync.Reason != "payload-size" {
		t.Fatal(got)
	}
	tree := git(t, a, "--git-dir", remote, "ls-tree", "-r", "--name-only", "ctxferry-v1")
	if strings.Contains(tree, paused.Payload.SHA256) || strings.Contains(tree, tooLarge.Payload.SHA256) || !strings.Contains(tree, explicit.Payload.SHA256) {
		t.Fatal(tree)
	}
}

func TestArtifactSyncRoutineBudgetAndLostReply(t *testing.T) {
	a, b, _ := artifactSyncPair(t)
	// Cross the raw budget with five objects that are each within 1 MiB.
	var manifests []app.ArtifactManifest
	for i := range 5 {
		m := addSyncArtifact(t, a, fmt.Sprintf("report-%d.json", i), strings.Repeat(string(rune('a'+i)), 1<<20), "public")
		queueArtifact(t, a, m.ID, "automatic")
		manifests = append(manifests, m)
	}
	a.ExtraEnv = append(a.ExtraEnv, "CTXFERRY_TEST_PUSH_FAULT="+filepath.Join(t.TempDir(), "lost-reply"))
	failure(t, a.Run(t, "sync", "--format=json"), "push-pending")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	var shared, omitted int
	for _, m := range manifests {
		sender, receiver := inspectSyncedArtifact(t, a, m.ID), inspectSyncedArtifact(t, b, m.ID)
		if sender.Sync == nil || receiver.Sync == nil || *sender.Sync != *receiver.Sync {
			t.Fatalf("sender %+v, receiver %+v", sender, receiver)
		}
		switch {
		case receiver.Sync.State == "shared":
			shared++
		case receiver.Sync.Reason == "publication-budget":
			omitted++
		}
	}
	if shared != 4 || omitted != 1 {
		t.Fatalf("got %d shared and %d omitted", shared, omitted)
	}
}

func TestArtifactSyncInvalidSnapshotPreservesVerifiedCatalog(t *testing.T) {
	a, b, remote := artifactSyncPair(t)
	m := addSyncArtifact(t, a, "report.json", "verified-before-corruption", "internal")
	queueArtifact(t, a, m.ID, "automatic")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	head := artifactStringField(t, b.Run(t, "sync", "status", "--format=json").Success(t), "head")

	checkout := filepath.Join(t.TempDir(), "catalog")
	git(t, a, "clone", "--branch", "ctxferry-v1", remote, checkout)
	// Put a syntactically valid artifact under a different, nonexistent Project.
	record := "records/" + m.ID[:2] + "/" + m.ID + ".json"
	artifactPath := filepath.Join(checkout, filepath.FromSlash(record))
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	wrongScope := bytes.Replace(data, []byte(m.Run.ProjectID), []byte("12345678-abcd-4000-8000-123456789abc"), 1)
	writeArtifactFile(t, artifactPath, string(wrongScope))
	git(t, a, "-C", checkout, "add", "--", record)
	git(t, a, "-C", checkout, "commit", "-m", "invalid artifact scope")
	git(t, a, "-C", checkout, "push", "origin", "ctxferry-v1")
	failure(t, b.Run(t, "sync", "--format=json"), "incomplete")

	writeArtifactFile(t, artifactPath, string(data))
	object := "objects/" + m.Payload.SHA256
	writeArtifactFile(t, filepath.Join(checkout, filepath.FromSlash(object)), "tampered-before-corruption")
	git(t, a, "-C", checkout, "add", "--", record, object)
	git(t, a, "-C", checkout, "commit", "-m", "corrupt artifact object")
	git(t, a, "-C", checkout, "push", "origin", "ctxferry-v1")
	failure(t, b.Run(t, "sync", "--format=json"), "incomplete")
	if got := artifactStringField(t, b.Run(t, "sync", "status", "--format=json").Success(t), "head"); got != head {
		t.Fatal("unverified artifact advanced the confirmed head", got)
	}
	fetchSyncedArtifact(t, b, m.ID, "verified-before-corruption")
}

func TestArtifactSyncInitialPreviewBackupAndEpochRestore(t *testing.T) {
	var metadata atomic.Value
	metadata.Store(privateMetadata)
	a, remote := syncHarness(t, &metadata)
	enableSync(t, a)
	const firstBytes = "original payload"
	first := addSyncArtifact(t, a, "original.json", firstBytes, "internal")
	queueArtifact(t, a, first.ID, "automatic")
	a.Run(t, "sync", "init", "--preview", previewSync(t, a), "--format=json").Success(t)
	b := joinDevice(t, a, true)
	fetchSyncedArtifact(t, b, first.ID, firstBytes)
	backup := filepath.Join(t.TempDir(), "saved")
	b.Run(t, "workspace", "backup", "--to", backup, "--include-local-only", "--format=json").Success(t)
	privateBackup := filepath.Join(t.TempDir(), "sender-saved")
	a.Run(t, "workspace", "backup", "--to", privateBackup, "--include-local-only", "--format=json").Success(t)

	const lateBytes = "registered after snapshot"
	late := addSyncArtifact(t, a, "late.json", lateBytes, "internal")
	queueArtifact(t, a, late.ID, "automatic")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	fetchSyncedArtifact(t, b, late.ID, lateBytes)
	head := git(t, a, "--git-dir", remote, "rev-parse", "ctxferry-v1")

	recovered := testcli.New(t, a.Binary)
	recovered.GitPath, recovered.ExtraEnv = a.GitPath, a.ExtraEnv
	recovered.DataDir = filepath.Join(t.TempDir(), "recovered")
	recovered.Run(t, "workspace", "recover-local", "--from", backup, "--format=json").Success(t)
	fetchSyncedArtifact(t, recovered, first.ID, firstBytes)
	if got := inspectSyncedArtifact(t, recovered, first.ID); len(got.Observations) != 1 {
		t.Fatal("the backed-up receiving observation is missing", got)
	}
	recovered.Run(t, "workspace", "restore", "--from", backup, "--if-head", head, "--new-epoch", "--format=json").Success(t)
	for _, h := range []*testcli.Harness{a, b} {
		h.Run(t, "sync", "--format=json").Success(t)
		fetchSyncedArtifact(t, h, first.ID, firstBytes)
	}
	if got := inspectSyncedArtifact(t, a, late.ID); got.Sync.State != "withdrawn" || got.Payload.Storage != "local-only" {
		t.Fatal(got)
	}
	fetchSyncedArtifact(t, a, late.ID, lateBytes)
	failure(t, b.Run(t, "artifact", "inspect", late.ID, "--project", "checkout", "--format=json"), "artifact-not-found")

	recovered.DataDir = filepath.Join(t.TempDir(), "recovered-sender")
	recovered.Run(t, "workspace", "recover-local", "--from", privateBackup, "--format=json").Success(t)
	fetchSyncedArtifact(t, recovered, first.ID, firstBytes)
	if got := inspectSyncedArtifact(t, recovered, first.ID); got.Payload.Storage != "local-only" || got.Sync.State != "shared" {
		t.Fatal(got)
	}
}

func TestArtifactSyncMigratesExistingLocalRegistration(t *testing.T) {
	a, b, _ := artifactSyncPair(t)
	const content = "legacy local object"
	legacy := addSyncArtifact(t, a, "legacy.json", content, "internal")
	db, err := sql.Open("sqlite", filepath.Join(a.DataDir, "workspaces", "default", "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`DROP TABLE artifact_observations; ALTER TABLE sync_state DROP COLUMN history_bytes; DROP TABLE onboarding_notes; DROP TABLE onboarding; PRAGMA user_version=11;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	fetchSyncedArtifact(t, a, legacy.ID, content)
	if got := inspectSyncedArtifact(t, a, legacy.ID); got.Sync != nil || got.Payload != legacy.Payload {
		t.Fatal(got)
	}
	queueArtifact(t, a, legacy.ID, "automatic")
	a.Run(t, "sync", "--format=json").Success(t)
	b.Run(t, "sync", "--format=json").Success(t)
	fetchSyncedArtifact(t, b, legacy.ID, content)
}
