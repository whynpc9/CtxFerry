//go:build ctxferry_test

package app

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"time"
)

// The executable scenario seam is absent from production builds.
func syncEndpoints(remote string) (string, string) {
	api := os.Getenv("CTXFERRY_TEST_API")
	u, err := url.Parse(api)
	if err == nil && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
		return api, os.Getenv("CTXFERRY_TEST_REMOTE")
	}
	return "https://api.github.com", remote
}

// Faults exist only in the scenario binary and wrap the real Git backend.
type faultSyncBackend struct{ syncBackend }

func newSyncBackend() syncBackend { return faultSyncBackend{gitSyncBackend{}} }
func (b faultSyncBackend) Ancestor(ctx context.Context, dir, base, head string) (bool, error) {
	if os.Getenv("CTXFERRY_TEST_ANCESTRY_FAILURE") == "1" {
		return false, Fail("git-error", "injected ancestry failure")
	}
	return b.syncBackend.Ancestor(ctx, dir, base, head)
}
func (b faultSyncBackend) Publish(ctx context.Context, dir string, conf SyncConfig, p pendingPush) error {
	if marker := os.Getenv("CTXFERRY_TEST_PUSH_FAULT"); marker != "" {
		// Exclusive creation makes a fault happen once across command restarts.
		f, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			f.Close()
			if peer := os.Getenv("CTXFERRY_TEST_RACE_GIT"); peer != "" {
				candidate, err := syncGit(ctx, peer, nil, "rev-parse", "refs/ctxferry/pending")
				if err != nil {
					return err
				}
				race := p
				race.Candidate = candidate
				if err = b.syncBackend.Publish(ctx, peer, conf, race); err != nil {
					return err
				}
			} else {
				if err := b.syncBackend.Publish(ctx, dir, conf, p); err != nil {
					return err
				}
				return Fail("push-pending", "injected lost push response")
			}
		}
	}
	if err := b.syncBackend.Publish(ctx, dir, conf, p); err != nil {
		return err
	}
	return waitForTestGate(ctx, "CTXFERRY_TEST_PUSH_GATE")
}

func (b faultSyncBackend) Fetch(ctx context.Context, dir string, conf SyncConfig) (string, map[string][]byte, error) {
	if path := os.Getenv("CTXFERRY_TEST_FETCH_LOG"); path != "" {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err == nil {
			f.WriteString("fetch\n")
			f.Close()
		}
	}
	if code := os.Getenv("CTXFERRY_TEST_FETCH_FAILURE"); code != "" {
		return "", nil, Fail(code, "injected fetch failure")
	}
	if err := waitForTestGate(ctx, "CTXFERRY_TEST_FETCH_GATE"); err != nil {
		return "", nil, err
	}
	return b.syncBackend.Fetch(ctx, dir, conf)
}

func waitForTestGate(ctx context.Context, key string) error {
	gate := os.Getenv(key)
	if gate == "" {
		return nil
	}
	if err := os.WriteFile(gate+".entered", nil, 0600); err != nil {
		return err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(gate); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func syncReadResult(args []string, out string) string {
	mode := os.Getenv("CTXFERRY_TEST_PARTIAL_READ")
	if len(out) > 0 && len(args) > 0 && (mode == "tree" && args[0] == "ls-tree" || mode == "blob" && args[0] == "cat-file" && len(args) > 1 && args[1] == "--batch") {
		return out[:len(out)-1]
	}
	return out
}

func syncHistorySize(measured int64) int64 {
	if n, err := strconv.ParseInt(os.Getenv("CTXFERRY_TEST_HISTORY_BYTES"), 10, 64); err == nil && n >= 0 {
		return n
	}
	return measured
}
