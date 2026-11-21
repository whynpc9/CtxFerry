package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"maps"
	"path/filepath"
	"sync"
	"testing"

	"github.com/whynpc9/CtxFerry/internal/store"
)

// memoryRemote is the in-memory adapter at the sync backend seam. Every device
// in a test shares one instance, as devices share one private repository.
type memoryRemote struct {
	mu      sync.Mutex
	commits map[string]memoryCommit
	heads   map[string]string
	// beforePublish runs once inside the next Publish, before the lease check.
	beforePublish func()
	rejected      int
}

type memoryCommit struct {
	parent string
	files  map[string][]byte
}

func newMemoryRemote() *memoryRemote {
	return &memoryRemote{commits: map[string]memoryCommit{}, heads: map[string]string{}}
}

func (m *memoryRemote) Privacy(context.Context, SyncConfig) SyncCheck {
	return SyncCheck{Privacy: "private", Identity: "1:owner/catalog"}
}

func (m *memoryRemote) Check(ctx context.Context, conf SyncConfig) SyncCheck {
	c := m.Privacy(ctx, conf)
	c.GitReachable = true
	return c
}

func (m *memoryRemote) Head(_ context.Context, conf SyncConfig) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.heads[conf.Branch], nil
}

func (m *memoryRemote) Fetch(_ context.Context, conf SyncConfig) (string, map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	head := m.heads[conf.Branch]
	if head == "" {
		return "", nil, Fail("missing-branch", "configured sync branch is missing")
	}
	return head, maps.Clone(m.commits[head].files), nil
}

func (m *memoryRemote) Ancestor(_ context.Context, base, head string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := head; id != ""; id = m.commits[id].parent {
		if id == base {
			return true, nil
		}
	}
	return false, nil
}

func (m *memoryRemote) Snapshot(_ context.Context, commit string) (map[string][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.commits[commit]
	if !ok {
		return nil, Fail("incomplete", "unknown commit")
	}
	return maps.Clone(c.files), nil
}

func (m *memoryRemote) History(_ context.Context, commit string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var total int64
	for id := commit; id != ""; id = m.commits[id].parent {
		for _, b := range m.commits[id].files {
			total += int64(len(b))
		}
	}
	return total, nil
}

func (m *memoryRemote) Stage(_ context.Context, files map[string][]byte, parent string) (string, error) {
	var b [20]byte
	rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits[id] = memoryCommit{parent, maps.Clone(files)}
	return id, nil
}

func (m *memoryRemote) Verify(_ context.Context, candidate, parent string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.commits[candidate]; !ok || c.parent != parent {
		return Fail("invalid-candidate", "candidate must have the fetched head as its only parent")
	}
	return nil
}

func (m *memoryRemote) Publish(_ context.Context, conf SyncConfig, p pendingPush) error {
	m.mu.Lock()
	hook := m.beforePublish
	m.beforePublish = nil
	m.mu.Unlock()
	if hook != nil {
		hook()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.heads[conf.Branch] != p.ExpectedHead {
		m.rejected++
		return Fail("lease-rejected", "remote changed; fetch and reconcile before publication")
	}
	m.heads[conf.Branch] = p.Candidate
	return nil
}

const memoryRemoteURL = "https://github.com/owner/catalog"

// publishWorkspace initializes a first device with one shared Project and
// publishes it to remote.
func publishWorkspace(t *testing.T, remote *memoryRemote) App {
	t.Helper()
	ctx := t.Context()
	a := App{DataDir: filepath.Join(t.TempDir(), "first"), backend: remote}
	if _, err := a.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.BindProject(ctx, t.TempDir(), "shared"); err != nil {
		t.Fatal(err)
	}
	if r, err := a.EnableSync(ctx, memoryRemoteURL, "main"); err != nil || r.State != "enabled" {
		t.Fatalf("enable sync: %+v, %v", r, err)
	}
	preview, err := a.PreviewSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := a.InitSync(ctx, preview.PreviewID); err != nil || r.State != "synced" {
		t.Fatalf("initial publication: %+v, %v", r, err)
	}
	return a
}

func joinWorkspace(t *testing.T, remote *memoryRemote) App {
	t.Helper()
	b := App{DataDir: filepath.Join(t.TempDir(), "second"), backend: remote}
	if r, err := b.Join(t.Context(), memoryRemoteURL, "main", nil, true); err != nil || r.State != "synced" {
		t.Fatalf("join: %+v, %v", r, err)
	}
	return b
}

func putShared(t *testing.T, a App, id, revision, body string) ContextDoc {
	t.Helper()
	r, err := a.PutContext(t.Context(), "shared", "", PutContext{
		ID: id, IfRevision: revision, Title: "Runbook", Purpose: "note", Source: "test",
		Body: body, Storage: "shared", Disclosure: "full",
	})
	if err != nil {
		t.Fatal(err)
	}
	return *r.Document
}

func contextBody(t *testing.T, a App, id string) string {
	t.Helper()
	db, err := store.OpenReadOnly(t.Context(), a.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var body string
	if err := db.QueryRowContext(t.Context(), "SELECT body FROM context_bodies WHERE record_id=?", id).Scan(&body); err != nil {
		t.Fatalf("context %s on %s: %v", id, a.DataDir, err)
	}
	return body
}

func TestSyncRoundCarriesSharedContextBetweenDevices(t *testing.T) {
	remote := newMemoryRemote()
	first := publishWorkspace(t, remote)
	second := joinWorkspace(t, remote)

	doc := putShared(t, first, "", "", "v1")
	r, err := first.Sync(t.Context())
	if err != nil || r.State != "synced" || r.Head != remote.heads["main"] {
		t.Fatalf("publishing round: %+v, %v", r, err)
	}
	if r, err = second.Sync(t.Context()); err != nil || r.State != "synced" {
		t.Fatalf("receiving round: %+v, %v", r, err)
	}
	if got := contextBody(t, second, doc.ID); got != "v1" {
		t.Fatalf("second device body = %q", got)
	}
}

func TestSyncRoundRecordsConcurrentEditsAsConflict(t *testing.T) {
	remote := newMemoryRemote()
	first := publishWorkspace(t, remote)
	doc := putShared(t, first, "", "", "base")
	if _, err := first.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	second := joinWorkspace(t, remote)

	putShared(t, first, doc.ID, doc.Revision, "first edit")
	putShared(t, second, doc.ID, doc.Revision, "second edit")
	if _, err := first.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	r, err := second.Sync(t.Context())
	if err != nil || r.State != "conflict" {
		t.Fatalf("concurrent edit round: %+v, %v", r, err)
	}
	conflict, err := second.Conflict(t.Context(), doc.ID)
	if err != nil || conflict.Local == nil || conflict.Remote == nil || conflict.Head != remote.heads["main"] {
		t.Fatalf("conflict detail: %+v, %v", conflict, err)
	}
	if got := contextBody(t, second, doc.ID); got != "second edit" {
		t.Fatalf("conflict replaced the local working version with %q", got)
	}
}

// A lost lease starts a fresh round; the retried round publishes on top of
// the competing head instead of overwriting it.
func TestSyncRoundRetriesAfterLostLease(t *testing.T) {
	remote := newMemoryRemote()
	first := publishWorkspace(t, remote)
	second := joinWorkspace(t, remote)

	other := putShared(t, second, "", "", "from second")
	mine := putShared(t, first, "", "", "from first")
	remote.beforePublish = func() {
		if _, err := second.Sync(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("competing round: %v", err)
		}
	}
	r, err := first.Sync(t.Context())
	if err != nil || r.State != "synced" || remote.rejected != 1 {
		t.Fatalf("round after lost lease: %+v, %v, %d rejections", r, err, remote.rejected)
	}
	if got := contextBody(t, first, other.ID); got != "from second" {
		t.Fatalf("competing publication was not merged: %q", got)
	}
	if _, err := second.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := contextBody(t, second, mine.ID); got != "from first" {
		t.Fatalf("retried publication missing on second device: %q", got)
	}
}
