package app

import (
	"context"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"
)

// syncBackend is the transport boundary. Candidate creation and the durable
// publication journal belong to the application, including on transport failure.
type syncBackend interface {
	Fetch(context.Context, string, SyncConfig) (string, map[string][]byte, error)
	Ancestor(context.Context, string, string, string) (bool, error)
	Publish(context.Context, string, SyncConfig, pendingPush) error
}
type gitSyncBackend struct{}

func (gitSyncBackend) Fetch(ctx context.Context, dir string, conf SyncConfig) (string, map[string][]byte, error) {
	return fetchSnapshot(ctx, dir, conf)
}

func (gitSyncBackend) Ancestor(ctx context.Context, dir, base, head string) (bool, error) {
	_, err := syncGit(ctx, dir, nil, "merge-base", "--is-ancestor", base, head)
	if err != nil && Classify(err).Code == "not-ancestor" {
		return false, nil
	}
	return err == nil, err
}
func (gitSyncBackend) Publish(ctx context.Context, dir string, conf SyncConfig, p pendingPush) error {
	_, remote := syncEndpoints(conf.Remote)
	ref := "refs/heads/" + conf.Branch
	_, err := syncGit(ctx, dir, nil, "push", "--porcelain", "--force-with-lease="+ref+":"+p.ExpectedHead, "--", remote, p.Candidate+":"+ref)
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
	head, readErr := readRemoteRef(ctx, dir, conf)
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
func inspectInitialization(ctx context.Context, dir string, conf SyncConfig, p pendingPush) (bool, error) {
	remoteHead, err := readRemoteRef(ctx, dir, conf)
	if err != nil {
		return false, err
	}
	if remoteHead == "" {
		return false, nil
	}
	head, files, err := newSyncBackend().Fetch(ctx, dir, conf)
	if err != nil {
		return false, err
	}
	if _, _, err = validateSnapshot(files, "", ""); err != nil {
		return false, err
	}
	accepted, err := newSyncBackend().Ancestor(ctx, dir, p.Candidate, head)
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
