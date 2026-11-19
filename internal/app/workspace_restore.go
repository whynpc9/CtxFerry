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

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type WorkspaceRestoreResult struct {
	WorkspaceID  string `json:"workspaceId"`
	Epoch        string `json:"epoch"`
	Head         string `json:"head"`
	RestoredFrom string `json:"restoredFrom"`
}

func readWorkspaceBackup(ctx context.Context, source string) (string, WorkspaceBackup, error) {
	var m WorkspaceBackup
	if source == "" {
		return "", m, Fail("usage", "choose a backup directory with --from")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return "", m, err
	}
	f, err := os.Open(filepath.Join(source, backupManifest))
	if err != nil {
		return "", m, Fail("invalid-backup", "cannot read backup manifest")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(b) > 4096 || strictJSON(b, &m) != nil || m.Format != 1 || !validUUID(m.WorkspaceID) ||
		m.Epoch != "" && !validUUID(m.Epoch) || m.Head != "" && (!validCommitID(m.Head) || m.Epoch == "") ||
		(m.Head == "") != (m.BundleSHA256 == "") {
		return "", m, Fail("invalid-backup", "invalid backup manifest")
	}
	components := map[string]string{backupCatalog: m.CatalogSHA256}
	if m.Head != "" {
		components[backupBundle] = m.BundleSHA256
	}
	for name, expected := range components {
		got, err := hashBackupFile(filepath.Join(source, name))
		if err != nil || expected != got {
			return "", m, Fail("invalid-backup", "missing or damaged backup component "+name)
		}
	}
	db, err := store.OpenBackupCatalog(ctx, filepath.Join(source, backupCatalog))
	if err != nil {
		return "", m, err
	}
	defer db.Close()
	var id, check, epoch, head, pending string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&check); err != nil || check != "ok" {
		return "", m, Fail("invalid-backup", "backup catalog failed its integrity check")
	}
	if err = db.QueryRowContext(ctx, "SELECT id FROM workspace WHERE singleton=1").Scan(&id); err != nil || id != m.WorkspaceID {
		return "", m, Fail("invalid-backup", "backup catalog workspace does not match its manifest")
	}
	err = db.QueryRowContext(ctx, "SELECT epoch,confirmed_head,pending FROM sync_state WHERE singleton=1").Scan(&epoch, &head, &pending)
	if err != nil && err != sql.ErrNoRows || head != m.Head || epoch != m.Epoch || pending != "" {
		return "", m, Fail("invalid-backup", "backup catalog sync state does not match its manifest")
	}
	return source, m, nil
}

func importWorkspaceBundle(ctx context.Context, dir, source string, m WorkspaceBackup) (workspaceFormat, map[string][]byte, error) {
	path := filepath.Join(source, backupBundle)
	if _, err := syncGit(ctx, dir, nil, "bundle", "verify", path); err != nil {
		return workspaceFormat{}, nil, Fail("invalid-backup", "cannot verify published Git bundle")
	}
	refs, err := syncGit(ctx, dir, nil, "bundle", "unbundle", path)
	if err != nil || refs != m.Head+" "+backupRef {
		return workspaceFormat{}, nil, Fail("invalid-backup", "published bundle head does not match its manifest")
	}
	files, err := readSnapshot(ctx, dir, m.Head)
	if err != nil {
		return workspaceFormat{}, nil, err
	}
	w, _, err := validateSnapshot(files, m.WorkspaceID, m.Epoch)
	return w, files, err
}

// RecoverLocalWorkspace restores the original device's offline state into a
// new data root. Keeping its identity also keeps pending proposals and any
// explicitly saved device-scoped bindings associated with their original device.
func (a App) RecoverLocalWorkspace(ctx context.Context, source string) (WorkspaceBackupResult, error) {
	result := WorkspaceBackupResult{Destination: a.DataDir, Boundary: backupBoundary}
	source, m, err := readWorkspaceBackup(ctx, source)
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(filepath.Dir(a.DataDir), 0700); err != nil {
		return result, err
	}
	if err = os.Mkdir(a.DataDir, 0700); err != nil {
		if os.IsExist(err) {
			return result, Fail("destination-exists", "local recovery requires a new application data directory")
		}
		return result, err
	}
	dest := store.Path(a.DataDir)
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return result, err
	}
	if m.Head != "" {
		dir, err := a.managedGit(ctx)
		if err != nil {
			return result, err
		}
		if _, _, err = importWorkspaceBundle(ctx, dir, source, m); err != nil {
			return result, err
		}
		if _, err = syncGit(ctx, dir, nil, "update-ref", "refs/ctxferry/recovered", m.Head); err != nil {
			return result, err
		}
	}
	input, err := os.Open(filepath.Join(source, backupCatalog))
	if err != nil {
		return result, err
	}
	defer input.Close()
	temp := dest + ".recovery"
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	h, err := copyRecoveryCatalog(f, input)
	if err != nil {
		return result, err
	}
	if h != m.CatalogSHA256 {
		return result, Fail("invalid-backup", "backup catalog changed during recovery")
	}
	if m.IncludesLocalOnly {
		if err = a.recoverBackupArtifacts(ctx, temp, source); err != nil {
			return result, err
		}
	}
	// Create the live catalog only after its history, snapshot and objects are complete.
	if err = os.Link(temp, dest); err != nil {
		return result, err
	}
	os.Remove(temp)
	result.WorkspaceBackup = m
	return result, nil
}

func copyRecoveryCatalog(f *os.File, input io.Reader) (string, error) {
	h := sha256.New()
	_, err := io.Copy(io.MultiWriter(f, h), input)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), closeErr
}

// RestoreWorkspace publishes an explicit epoch change to the configured private
// repository. Local recovery is separate so an existing device's queue is
// never replaced with a different device's snapshot.
func (a App) RestoreWorkspace(ctx context.Context, source, expected string, newEpoch bool) (WorkspaceRestoreResult, error) {
	var result WorkspaceRestoreResult
	if !newEpoch || !validCommitID(expected) {
		return result, Fail("usage", "restore requires --new-epoch and --if-head <current published commit>")
	}
	source, m, err := readWorkspaceBackup(ctx, source)
	if err != nil {
		return result, err
	}
	if m.Head == "" {
		return result, Fail("invalid-backup", "backup has no published history; use workspace recover-local")
	}
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
	conf, err := loadSync(ctx, db)
	if err != nil {
		return result, err
	}
	if conf.WorkspaceID != m.WorkspaceID {
		return result, Fail("workspace-mismatch", "backup belongs to a different workspace")
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return result, err
	}
	if check := privacyCheck(ctx, conf); check.Privacy != "private" {
		return result, Fail(check.Failure, "authenticated private repository identity could not be confirmed")
	}
	backend := newSyncBackend()
	head, current, err := backend.Fetch(ctx, dir, conf)
	if err != nil {
		return result, err
	}
	_, currentRecords, err := validateSnapshot(current, conf.WorkspaceID, "")
	if err != nil {
		return result, err
	}
	var confirmed, pending string
	if err = db.QueryRowContext(ctx, "SELECT confirmed_head,pending FROM sync_state").Scan(&confirmed, &pending); err != nil {
		return result, err
	}
	if confirmed == "" {
		return result, Fail("initialization-required", "restore requires a previously published workspace")
	}
	if pending != "" {
		var prior pendingPush
		if json.Unmarshal([]byte(pending), &prior) != nil || prior.RestoredFrom == "" {
			return result, Fail("push-pending", "run sync to reconcile the pending publication before restoring")
		}
		if prior.ExpectedHead == expected && prior.RestoredFrom == m.Head {
			accepted, err := backend.Ancestor(ctx, dir, prior.Candidate, head)
			if err != nil {
				return result, err
			}
			result = WorkspaceRestoreResult{conf.WorkspaceID, prior.Epoch, prior.Candidate, prior.RestoredFrom}
			if accepted {
				return result, nil
			}
			if head == expected {
				return result, backend.Publish(ctx, dir, conf, prior)
			}
		}
	}
	if head != expected {
		return result, Fail("stale-head", "published head changed; inspect it before choosing a new --if-head")
	}
	extends, err := backend.Ancestor(ctx, dir, confirmed, head)
	if err != nil {
		return result, err
	}
	if !extends {
		return result, Fail("history-rewritten", "remote head no longer extends confirmed history")
	}
	w, files, err := importWorkspaceBundle(ctx, dir, source, m)
	if err != nil {
		return result, err
	}
	extends, err = backend.Ancestor(ctx, dir, m.Head, head)
	if err != nil {
		return result, err
	}
	if !extends {
		return result, Fail("invalid-history", "backup commit is outside the published workspace history")
	}
	w.Epoch = uuid.NewString()
	w.RestoredFrom = m.Head
	files["workspace.json"], err = json.Marshal(w)
	if err != nil {
		return result, err
	}
	// Keep identities introduced after the backup as tombstones. Peers can
	// compare their pending revisions and explicitly resolve a deletion.
	for id, r := range currentRecords {
		path := recordPath(id)
		if files[path] != nil {
			continue
		}
		if !r.Deleted {
			r.Deleted = true
			r.PreviousRevision = r.Revision
			r.Revision = uuid.NewString()
			r.OperationID = uuid.NewString()
			r.Provenance.UpdatedAt = now()
		}
		files[path], err = json.Marshal(r.publishedEnvelope)
		if err != nil {
			return result, err
		}
		if r.Value.Body != nil {
			path := "objects/" + r.Value.Body.SHA256
			files[path] = current[path]
		}
	}
	if _, _, err = validateSnapshot(files, conf.WorkspaceID, w.Epoch); err != nil {
		return result, err
	}
	candidate, err := candidateCommit(ctx, dir, files, head)
	if err != nil {
		return result, err
	}
	attempt := pendingPush{ExpectedHead: head, Candidate: candidate, Epoch: w.Epoch, RestoredFrom: m.Head}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		return result, err
	}
	if _, err = syncGit(ctx, dir, nil, "update-ref", "refs/ctxferry/pending", candidate); err != nil {
		return result, err
	}
	if err = store.Write(ctx, db, func(c *sql.Conn) error {
		if _, err := c.ExecContext(ctx, "UPDATE sync_state SET pending=?", string(encoded)); err != nil {
			return err
		}
		// A running service reconciles this journal even if it had paused.
		_, err := c.ExecContext(ctx, "UPDATE sync_progress SET requested=requested+1")
		return err
	}); err != nil {
		return result, err
	}
	result = WorkspaceRestoreResult{conf.WorkspaceID, w.Epoch, candidate, m.Head}
	return result, backend.Publish(ctx, dir, conf, attempt)
}
