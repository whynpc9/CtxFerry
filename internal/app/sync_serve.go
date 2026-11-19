package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/whynpc9/CtxFerry/internal/store"
)

const (
	syncPoll     = 200 * time.Millisecond
	syncQuiet    = time.Second
	syncMaxDelay = 5 * time.Second
	syncCatchup  = 30 * time.Second
)

type syncReceipt struct {
	Result SyncResult
	Error  *Error
}

// The driver lock elects one foreground service or one-shot sync. The existing
// round lock still excludes catalog mutations and other sync operations during
// each network attempt. Idle services never hold the round lock.
func (a App) syncDriver(ctx context.Context) (func(), error) {
	return a.syncLock(ctx, "sync-driver.sqlite", "0")
}

func (a App) Sync(ctx context.Context) (SyncResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return SyncResult{}, err
	}
	defer db.Close()

	var ticket int64
	if err := db.QueryRowContext(ctx, "UPDATE sync_progress SET requested=requested+1 RETURNING requested").Scan(&ticket); err != nil {
		return SyncResult{}, err
	}
	poll := time.NewTicker(syncPoll)
	defer poll.Stop()
	for {
		if result, done, err := completedSync(ctx, db, ticket); done || err != nil {
			return result, err
		}
		release, err := a.syncDriver(ctx)
		if err == nil {
			defer release()
			// The previous driver may have finished just before we took over.
			if result, done, err := completedSync(ctx, db, ticket); done || err != nil {
				return result, err
			}
			return a.runSync(ctx, db, true)
		}
		if Classify(err).Code != "sync-busy" {
			return SyncResult{}, err
		}
		select {
		case <-ctx.Done():
			return SyncResult{}, Fail("sync-busy", "stopped waiting for the local sync owner")
		case <-poll.C:
		}
	}
}

func completedSync(ctx context.Context, db *sql.DB, ticket int64) (SyncResult, bool, error) {
	var encoded string
	err := db.QueryRowContext(ctx, "SELECT receipt FROM sync_progress WHERE completed>=?", ticket).Scan(&encoded)
	if err == sql.ErrNoRows {
		return SyncResult{}, false, nil
	}
	if err != nil {
		return SyncResult{}, false, err
	}
	var receipt syncReceipt
	if err := json.Unmarshal([]byte(encoded), &receipt); err != nil {
		return SyncResult{}, false, err
	}
	if receipt.Error != nil {
		return receipt.Result, true, receipt.Error
	}
	return receipt.Result, true, nil
}

func (a App) runSync(ctx context.Context, db *sql.DB, waitForWriter bool) (SyncResult, error) {
	var covered int64
	round := func(ctx context.Context) (SyncResult, error) {
		covered = 0
		release, err := a.syncOwner(ctx)
		for waitForWriter && err != nil && Classify(err).Code == "sync-busy" && ctx.Err() == nil {
			release, err = a.syncOwner(ctx)
		}
		if err != nil {
			return SyncResult{}, err
		}
		defer release()
		result, err := a.syncRound(ctx, db)
		// Every request visible here was submitted before another local
		// mutation could commit. Later requests remain eligible for a round.
		if e := db.QueryRowContext(ctx, "SELECT requested FROM sync_progress").Scan(&covered); e != nil {
			return result, e
		}
		return result, err
	}
	result, err := retrySync(ctx, round)
	if ctx.Err() != nil || covered == 0 {
		return result, err
	}
	receipt := syncReceipt{Result: result}
	if err != nil {
		receipt.Error = Classify(err)
	}
	encoded, encodeErr := json.Marshal(receipt)
	if encodeErr != nil {
		return result, encodeErr
	}
	if _, writeErr := db.ExecContext(ctx, "UPDATE sync_progress SET completed=?, receipt=? WHERE completed<?", covered, string(encoded), covered); writeErr != nil {
		return result, writeErr
	}
	return result, err
}

type syncWork struct {
	requested bool
	ready     bool
	outbox    string
}

func pendingSyncWork(ctx context.Context, db *sql.DB) (syncWork, error) {
	var work syncWork
	err := db.QueryRowContext(ctx, `
SELECT requested>completed,
       EXISTS (SELECT 1 FROM sync_state WHERE confirmed_head<>''),
       coalesce((SELECT group_concat(operation_id, '' ORDER BY id) FROM sync_outbox), '')
FROM sync_progress`).Scan(&work.requested, &work.ready, &work.outbox)
	return work, err
}

// ServeSync watches committed outbox changes and periodically catches up with
// remote edits. Startup also catches work queued while the service was stopped.
func (a App) ServeSync(ctx context.Context) error {
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return err
	}
	defer db.Close()
	release, err := a.syncDriver(ctx)
	if err != nil {
		return err
	}
	defer release()

	poll := time.NewTicker(syncPoll)
	defer poll.Stop()
	var nextCatchup, changed, firstChange time.Time
	var previous string
	var backoff time.Duration
	paused := false
	for ctx.Err() == nil {
		work, err := pendingSyncWork(ctx, db)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			return err
		}
		now := time.Now()
		if work.outbox == "" {
			changed, firstChange = time.Time{}, time.Time{}
		} else if previous != work.outbox {
			if firstChange.IsZero() {
				firstChange = now
			}
			changed = now
		}
		previous = work.outbox
		coalesced := !changed.IsZero() && (now.Sub(changed) >= syncQuiet || now.Sub(firstChange) >= syncMaxDelay)
		// A scheduled catch-up yields to edits currently being coalesced.
		due := changed.IsZero() && !now.Before(nextCatchup)
		if backoff != 0 && now.Before(nextCatchup) {
			coalesced = false
		}
		if work.requested || (work.ready && !paused && (coalesced || due)) {
			_, err := a.runSync(ctx, db, work.requested)
			changed, firstChange = time.Time{}, time.Time{}
			delay := syncCatchup
			var code string
			if err != nil {
				code = Classify(err).Code
			}
			switch code {
			case "":
				backoff, paused = 0, false
			case "offline", "git-error", "lease-rejected", "sync-busy", "store-busy", "push-pending":
				backoff = min(max(2*time.Second, 2*backoff), time.Minute)
				delay, paused = backoff, false
			case "sync-disabled", "initialization-required":
				backoff, paused = 0, false
			default:
				// Definitive failures need user action. Explicit sync can
				// resume the loop after credentials or data are repaired.
				paused = true
			}
			nextCatchup = time.Now().Add(delay)
		}
		select {
		case <-ctx.Done():
		case <-poll.C:
		}
	}
	return nil
}
