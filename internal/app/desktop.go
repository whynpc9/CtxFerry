package app

import (
	"context"
	"database/sql"
	"time"

	"github.com/whynpc9/CtxFerry/internal/store"
)

// DesktopSnapshot is a local observation. Reading it never initializes or migrates
// a workspace, contacts the remote, or consults a provider for a credential.
type DesktopSnapshot struct {
	ReadAt     time.Time
	Device     Device
	Bindings   []DesktopBinding
	Sync       SyncResult
	PendingIDs []string
}

type DesktopBinding struct{ Project, Path string }

func (a App) ReadDesktopSnapshot(ctx context.Context) (DesktopSnapshot, error) {
	var result DesktopSnapshot
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return result, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	d := &result.Device
	if err := tx.QueryRowContext(ctx, "SELECT id,name,os,arch,created_at FROM device WHERE singleton=1").Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.CreatedAt); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.slug,b.path FROM checkout_bindings b JOIN projects p ON p.id=b.project_id
		WHERE b.device_id=? AND NOT EXISTS (SELECT 1 FROM sync_tombstones t WHERE t.id=p.id)
		ORDER BY p.slug,b.path`, d.ID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var binding DesktopBinding
		if err := rows.Scan(&binding.Project, &binding.Path); err != nil {
			return result, err
		}
		result.Bindings = append(result.Bindings, binding)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Sync, err = readSyncStatus(ctx, tx)
	if err != nil {
		if Classify(err).Code != "sync-disabled" {
			return result, err
		}
		// The device can have local queue evidence before a remote is configured.
		result.Sync.Conflicts, result.Sync.Blocked, err = conflictStatus(ctx, tx)
		if err != nil {
			return result, err
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id FROM sync_outbox ORDER BY id")
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return result, err
		}
		result.PendingIDs = append(result.PendingIDs, id)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	result.Sync.Pending = len(result.PendingIDs)
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.ReadAt = time.Now().UTC()
	return result, nil
}
