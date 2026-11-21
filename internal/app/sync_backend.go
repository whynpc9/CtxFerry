package app

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

// syncBackend is the seam between the sync protocol and the private remote.
// It owns every Git and GitHub operation a sync round performs: identity
// checks, snapshot reads, history measurement, candidate staging and the
// leased publish. Candidate selection and the durable publication journal stay
// with the application, including on transport failure.
//
// Production uses gitSyncBackend over the managed bare repository; tests
// inject an in-memory backend through App.backend.
type syncBackend interface {
	// Privacy confirms the authenticated private repository identity.
	Privacy(ctx context.Context, conf SyncConfig) SyncCheck
	// Check adds Git reachability to the identity check.
	Check(ctx context.Context, conf SyncConfig) SyncCheck
	// Head lists the remote branch head; "" only when the branch is absent.
	Head(ctx context.Context, conf SyncConfig) (string, error)
	// Fetch reads the remote branch head and its snapshot.
	Fetch(ctx context.Context, conf SyncConfig) (string, map[string][]byte, error)
	// Ancestor reports whether base is in head's history.
	Ancestor(ctx context.Context, base, head string) (bool, error)
	// Snapshot reads a retained commit, such as a confirmed head or candidate.
	Snapshot(ctx context.Context, commit string) (map[string][]byte, error)
	// History measures the raw bytes reachable from commit.
	History(ctx context.Context, commit string) (int64, error)
	// Stage commits files with parent as the only parent ("" for a root) and
	// retains the candidate for recovery.
	Stage(ctx context.Context, files map[string][]byte, parent string) (string, error)
	// Verify checks that a retained candidate has exactly the expected parent.
	Verify(ctx context.Context, candidate, parent string) error
	// Publish leases the branch from p.ExpectedHead to p.Candidate.
	Publish(ctx context.Context, conf SyncConfig, p pendingPush) error
}

// openSyncBackend returns the injected backend, or the Git backend over the
// managed repository.
func (a App) openSyncBackend(ctx context.Context) (syncBackend, error) {
	if a.backend != nil {
		return a.backend, nil
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return nil, err
	}
	return newSyncBackend(dir), nil
}

type gitSyncBackend struct{ dir string }

func (b gitSyncBackend) Privacy(ctx context.Context, conf SyncConfig) SyncCheck {
	return privacyCheck(ctx, conf)
}

func (b gitSyncBackend) Check(ctx context.Context, conf SyncConfig) SyncCheck {
	return checkSync(ctx, b.dir, conf)
}

func (b gitSyncBackend) Head(ctx context.Context, conf SyncConfig) (string, error) {
	return readRemoteRef(ctx, b.dir, conf)
}

func (b gitSyncBackend) Fetch(ctx context.Context, conf SyncConfig) (string, map[string][]byte, error) {
	return fetchSnapshot(ctx, b.dir, conf)
}

func (b gitSyncBackend) Ancestor(ctx context.Context, base, head string) (bool, error) {
	_, err := syncGit(ctx, b.dir, nil, "merge-base", "--is-ancestor", base, head)
	if err != nil && Classify(err).Code == "not-ancestor" {
		return false, nil
	}
	return err == nil, err
}

func (b gitSyncBackend) Snapshot(ctx context.Context, commit string) (map[string][]byte, error) {
	return readSnapshot(ctx, b.dir, commit)
}

func (b gitSyncBackend) History(ctx context.Context, commit string) (int64, error) {
	return measureSyncHistory(ctx, b.dir, commit)
}

func (b gitSyncBackend) Stage(ctx context.Context, files map[string][]byte, parent string) (string, error) {
	var parents []string
	if parent != "" {
		parents = append(parents, parent)
	}
	candidate, err := candidateCommit(ctx, b.dir, files, parents...)
	if err != nil {
		return "", err
	}
	if err = b.Verify(ctx, candidate, parent); err != nil {
		return "", err
	}
	if _, err = syncGit(ctx, b.dir, nil, "update-ref", "refs/ctxferry/pending", candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func (b gitSyncBackend) Verify(ctx context.Context, candidate, parent string) error {
	parents, err := syncGit(ctx, b.dir, nil, "rev-list", "--parents", "-n", "1", candidate)
	if err != nil {
		return err
	}
	if parent == "" {
		if len(strings.Fields(parents)) != 1 || strings.Fields(parents)[0] != candidate {
			return Fail("invalid-pending", "initial candidate must be a root commit")
		}
		return nil
	}
	if parents != candidate+" "+parent {
		return Fail("invalid-candidate", "candidate must have the fetched head as its only parent")
	}
	return nil
}

func (b gitSyncBackend) Publish(ctx context.Context, conf SyncConfig, p pendingPush) error {
	_, remote := syncEndpoints(conf.Remote)
	ref := "refs/heads/" + conf.Branch
	_, err := syncGit(ctx, b.dir, nil, "push", "--porcelain", "--force-with-lease="+ref+":"+p.ExpectedHead, "--", remote, p.Candidate+":"+ref)
	if err == nil {
		return nil
	}
	switch Classify(err).Code {
	case "lease-rejected", "auth-required", "unlock-required", "incomplete":
		return err
	}
	// Git versions and remote helpers use different ref-update diagnostics.
	// A changed remote head is enough to require history reconciliation,
	// including when a successful push lost its response. Never retry blindly.
	head, readErr := readRemoteRef(ctx, b.dir, conf)
	if readErr != nil {
		return readErr
	}
	if head != p.ExpectedHead {
		return Fail("lease-rejected", "remote head changed; fetch and reconcile pending publication")
	}
	return Fail("push-pending", "publication result requires reconciliation; candidate and operations retained")
}

// A command has a bounded retry budget. A rejected lease starts a whole fresh
// round, so history and business baselines are checked before another publish.
func (a App) InitSync(ctx context.Context, preview string) (SyncResult, error) {
	return retrySync(ctx, func(ctx context.Context) (SyncResult, error) { return a.initSync(ctx, preview) })
}
func retrySync(ctx context.Context, round func(context.Context) (SyncResult, error)) (SyncResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var r SyncResult
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		r, err = round(ctx)
		if err == nil {
			return r, nil
		}
		switch Classify(err).Code {
		case "git-error", "offline", "lease-rejected":
		default:
			return r, err
		}
		if attempt == 3 {
			break
		}
		timer := time.NewTimer(250 * time.Millisecond << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return r, Fail("offline", "sync cancelled; local work retained")
		case <-timer.C:
		}
	}
	return r, err
}

// Initialization alone may interpret a successful, exact ref listing as absent.
// Existing workspaces are validated without silently adopting a competing root.
func inspectInitialization(ctx context.Context, backend syncBackend, conf SyncConfig, p pendingPush) (bool, error) {
	remoteHead, err := backend.Head(ctx, conf)
	if err != nil {
		return false, err
	}
	if remoteHead == "" {
		return false, nil
	}
	head, files, err := backend.Fetch(ctx, conf)
	if err != nil {
		return false, err
	}
	if _, _, err = validateSnapshot(files, "", ""); err != nil {
		return false, err
	}
	accepted, err := backend.Ancestor(ctx, p.Candidate, head)
	if err != nil {
		return false, err
	}
	if !accepted {
		return false, Fail("branch-exists", "another initializer created a validated workspace; local work retained, explicit join required")
	}
	if _, _, err = validateSnapshot(files, conf.WorkspaceID, p.Epoch); err != nil {
		return false, err
	}
	return true, nil
}
func validCommitID(id string) bool {
	if len(id) != 40 && len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func persistSyncFailure(db *sql.DB, err error) {
	code := Classify(err).Code
	switch code {
	case "git-error":
		code = "offline"
	case "invalid-catalog", "size-limit":
		code = "incomplete"
	case "push-pending", "branch-exists", "lease-rejected":
		code = "pending"
	}
	db.ExecContext(context.Background(), "UPDATE sync_state SET outcome=?", code)
}

// Only a successful exact ref listing can establish absence. All malformed
// listings and transport failures stay errors, including during initialization.
func readRemoteRef(ctx context.Context, dir string, conf SyncConfig) (string, error) {
	_, remote := syncEndpoints(conf.Remote)
	ref := "refs/heads/" + conf.Branch
	listing, err := syncGit(ctx, dir, nil, "ls-remote", "--refs", "--", remote, ref)
	if err != nil {
		return "", err
	}
	if listing == "" {
		return "", nil
	}
	fields := strings.Fields(listing)
	if len(fields) != 2 || fields[1] != ref || !validCommitID(fields[0]) {
		return "", Fail("incomplete", "invalid remote ref listing")
	}
	return fields[0], nil
}
