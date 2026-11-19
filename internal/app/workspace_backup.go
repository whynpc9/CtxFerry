package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"github.com/whynpc9/CtxFerry/internal/store"
)

const (
	backupCatalog   = "catalog.sqlite"
	backupBundle    = "published.bundle"
	backupArtifacts = "artifacts"
	backupManifest  = "manifest.json"
	backupRef       = "refs/ctxferry/backup"
	backupBoundary  = "Plaintext backup. Protect the destination. Credential stores, provider sessions, Git authentication and execution trust are excluded. Review user-authored content for pasted secrets."
)

type WorkspaceBackup struct {
	Format            int    `json:"format"`
	WorkspaceID       string `json:"workspaceId"`
	Epoch             string `json:"epoch,omitempty"`
	Head              string `json:"head,omitempty"`
	CatalogSHA256     string `json:"catalogSha256"`
	BundleSHA256      string `json:"bundleSha256,omitempty"`
	IncludesLocalOnly bool   `json:"includesLocalOnly"`
}

type WorkspaceBackupResult struct {
	WorkspaceBackup
	Destination string `json:"destination"`
	Boundary    string `json:"boundary"`
}

// The snapshot is built from an allowlist in a new database. Objects are stored
// inline in context_bodies, sync_baselines and sync_objects. Rebuild the derived
// FTS index so shadow tables cannot carry content excluded by the row filter.
func (a App) copyBackupCatalog(ctx context.Context, source *sql.DB, destination string, includeLocal bool) (WorkspaceBackup, error) {
	m := WorkspaceBackup{Format: 1, IncludesLocalOnly: includeLocal}
	snapshot, err := store.OpenCatalog(ctx, destination, true)
	if err != nil {
		return m, err
	}
	if err = snapshot.Close(); err != nil {
		return m, err
	}
	conn, err := source.Conn(ctx)
	if err != nil {
		return m, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "ATTACH DATABASE ? AS backup", destination); err != nil {
		return m, err
	}
	defer conn.ExecContext(context.Background(), "DETACH DATABASE backup")
	err = func() error {
		if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
			return err
		}
		defer conn.ExecContext(context.Background(), "ROLLBACK")
		if err := conn.QueryRowContext(ctx, "SELECT id FROM main.workspace").Scan(&m.WorkspaceID); err != nil {
			return err
		}
		var pending string
		err := conn.QueryRowContext(ctx, "SELECT epoch,confirmed_head,pending FROM main.sync_state").Scan(&m.Epoch, &m.Head, &pending)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if pending != "" {
			return Fail("push-pending", "reconcile pending publication before backing up the workspace")
		}
		var remote string
		err = conn.QueryRowContext(ctx, "SELECT remote FROM main.sync_config").Scan(&remote)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			canonical, _, err := syncRemote(remote)
			if err != nil || canonical != remote {
				return Fail("invalid-remote", "backup requires a canonical credential-free sync remote")
			}
		}
		for _, table := range []string{
			"workspace", "projects", "repositories", "repository_aliases", "memberships",
			"sync_config", "export_identities", "sync_baselines", "sync_outbox",
			"sync_conflicts", "sync_objects", "sync_tombstones", "sync_blocked", "configurations",
		} {
			if _, err := conn.ExecContext(ctx, "INSERT INTO backup."+table+" SELECT * FROM main."+table); err != nil {
				return err
			}
		}
		statements := []string{
			"INSERT INTO backup.sync_state(singleton,epoch,confirmed_head) SELECT singleton,epoch,confirmed_head FROM main.sync_state",
			"INSERT INTO backup.device SELECT singleton,id,'',os,arch,created_at FROM main.device",
			"INSERT INTO backup.records SELECT * FROM main.records WHERE json_extract(metadata,'$.policy.storage')='shared'",
			"INSERT INTO backup.context_bodies SELECT b.* FROM main.context_bodies b JOIN backup.records r ON r.id=b.record_id",
		}
		if includeLocal {
			statements[1] = "INSERT INTO backup.device SELECT * FROM main.device"
			statements[2] = "INSERT INTO backup.records SELECT * FROM main.records"
			for _, table := range []string{"checkout_bindings", "local_configurations", "profile_selections", "project_subscriptions", "resource_routes", "resource_observations", "artifact_runs", "artifacts"} {
				statements = append(statements, "INSERT INTO backup."+table+" SELECT * FROM main."+table)
			}
		}
		for _, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		if includeLocal {
			// The catalog write lock also excludes artifact additions and
			// reclamation until all objects in the snapshot are verified.
			rows, err := conn.QueryContext(ctx, backupArtifactQuery)
			if err != nil {
				return err
			}
			if err = copyBackupArtifacts(rows, a.artifactRoot(), filepath.Join(filepath.Dir(destination), backupArtifacts)); err != nil {
				return err
			}
		}
		_, err = conn.ExecContext(ctx, "COMMIT")
		return err
	}()
	if err != nil {
		return m, err
	}
	if _, err = conn.ExecContext(ctx, "DETACH DATABASE backup"); err != nil {
		return m, err
	}
	snapshot, err = store.OpenCatalog(ctx, destination, false)
	if err != nil {
		return m, err
	}
	defer snapshot.Close()
	if err = store.Write(ctx, snapshot, func(c *sql.Conn) error { return reindex(ctx, c) }); err != nil {
		return m, err
	}
	// Seal the standalone snapshot without requiring a companion WAL or SHM.
	var mode string
	if err = snapshot.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		return m, err
	}
	if mode != "delete" {
		return m, Fail("invalid-backup", "could not seal a standalone SQLite snapshot")
	}
	return m, nil
}

func hashBackupFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", Fail("invalid-backup", "backup components must be regular files")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a App) BackupWorkspace(ctx context.Context, destination string, includeLocal bool) (WorkspaceBackupResult, error) {
	result := WorkspaceBackupResult{Boundary: backupBoundary}
	if destination == "" {
		return result, Fail("usage", "choose a new backup directory with --to")
	}
	destination, err := filepath.Abs(destination)
	if err != nil {
		return result, err
	}
	result.Destination = destination
	release, err := a.syncOwner(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err = os.Mkdir(destination, 0700); err != nil {
		if os.IsExist(err) {
			return result, Fail("destination-exists", "choose a new backup directory")
		}
		return result, err
	}
	// The manifest is the completion marker. Failed copies cannot be restored.
	catalog := filepath.Join(destination, backupCatalog)
	m, err := a.copyBackupCatalog(ctx, db, catalog, includeLocal)
	if err != nil {
		return result, err
	}
	if m.Head != "" {
		dir, err := a.managedGit(ctx)
		if err != nil {
			return result, err
		}
		bundle := filepath.Join(destination, backupBundle)
		if err = createWorkspaceBundle(ctx, dir, bundle, m.Head); err != nil {
			return result, err
		}
		if m.BundleSHA256, err = hashBackupFile(bundle); err != nil {
			return result, err
		}
	}
	if m.CatalogSHA256, err = hashBackupFile(catalog); err != nil {
		return result, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return result, err
	}
	staged := filepath.Join(destination, "manifest.tmp")
	if err = os.WriteFile(staged, append(b, '\n'), 0600); err != nil {
		return result, err
	}
	if err = os.Rename(staged, filepath.Join(destination, backupManifest)); err != nil {
		return result, err
	}
	result.WorkspaceBackup = m
	return result, nil
}

func createWorkspaceBundle(ctx context.Context, dir, bundle, head string) error {
	if !validCommitID(head) {
		return Fail("invalid-backup", "published head is invalid")
	}
	if _, err := syncGit(ctx, dir, nil, "update-ref", backupRef, head); err != nil {
		return err
	}
	defer syncGit(context.Background(), dir, nil, "update-ref", "-d", backupRef, head)
	if _, err := syncGit(ctx, dir, nil, "bundle", "create", bundle, backupRef); err != nil {
		return err
	}
	return os.Chmod(bundle, 0600)
}
