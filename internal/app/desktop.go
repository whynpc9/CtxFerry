package app

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/whynpc9/CtxFerry/internal/provider"
	"github.com/whynpc9/CtxFerry/internal/store"
)

// DesktopSnapshot contains local catalog evidence and metadata-only connection
// observations. A failed section must not be interpreted as an empty result.
type DesktopSnapshot struct {
	ReadAt         time.Time
	Workspace      Workspace
	Device         Device
	Bindings       []DesktopBinding
	BindingsErr    error
	Sync           SyncResult
	SyncErr        error
	LastOutcome    string
	RepositoryURL  string
	PendingIDs     []string
	QueuePending   int
	Queue          []DesktopQueueRecord
	Providers      []DoctorProvider
	ProvidersErr   error
	BackgroundSync DoctorCheck
}

type DesktopBinding struct {
	ProjectID, Project, Path string
}

// DesktopQueueRecord is a display projection. It carries stable navigation IDs
// and local labels, without record bodies, provider locators or conflict tokens.
type DesktopQueueRecord struct {
	ID, ProjectID, Project, Kind, Title string
	Pending, Conflict, Blocked          bool
}

// Waiting reports a queued record that has no known local blocker. An
// explicit network check or sync round can still discover new blockers.
func (r DesktopQueueRecord) Waiting() bool { return r.Pending && !r.Conflict && !r.Blocked }

func (a App) ReadDesktopSnapshot(ctx context.Context) (DesktopSnapshot, error) {
	var out DesktopSnapshot
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()

	w, d := &out.Workspace, &out.Device
	if err := tx.QueryRowContext(ctx, "SELECT id,created_at FROM workspace WHERE singleton=1").Scan(&w.ID, &w.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = store.ErrNotInitialized
		}
		return out, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT id,name,os,arch,created_at FROM device WHERE singleton=1").Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.CreatedAt); err != nil {
		return out, err
	}

	out.Bindings, out.BindingsErr = desktopBindings(ctx, tx, d.ID)
	out.SyncErr = out.readSync(ctx, tx)
	var keys []provider.SessionKey
	if out.BindingsErr == nil {
		keys, out.ProvidersErr = desktopProviderKeys(ctx, tx, out)
	} else {
		out.ProvidersErr = out.BindingsErr
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out.ReadAt = time.Now().UTC()

	if len(keys) > 0 && out.ProvidersErr == nil {
		storage, sessions := a.doctorCredentialStorage(ctx, keys)
		for _, key := range keys {
			out.Providers = append(out.Providers, a.doctorProviderProfile(key, storage, sessions[key]))
		}
	}
	out.BackgroundSync = a.backgroundSyncStatus(ctx)
	return out, nil
}

func desktopBindings(ctx context.Context, tx *sql.Tx, device string) ([]DesktopBinding, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.slug,b.path FROM checkout_bindings b JOIN projects p ON p.id=b.project_id
		WHERE b.device_id=? AND NOT EXISTS (SELECT 1 FROM sync_tombstones t WHERE t.id=p.id)
		ORDER BY p.slug,b.path`, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bindings []DesktopBinding
	for rows.Next() {
		var binding DesktopBinding
		if err := rows.Scan(&binding.ProjectID, &binding.Project, &binding.Path); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func desktopProviderKeys(ctx context.Context, tx *sql.Tx, s DesktopSnapshot) ([]provider.SessionKey, error) {
	projects := map[string]bool{}
	profiles := map[provider.Profile]bool{}
	for _, b := range s.Bindings {
		if projects[b.ProjectID] {
			continue
		}
		projects[b.ProjectID] = true
		list, err := listProjectSecrets(ctx, tx, Project{ID: b.ProjectID})
		if err != nil {
			return nil, err
		}
		for _, binding := range list.Bindings {
			profiles[binding.ProviderProfile] = true
		}
	}
	ordered := slices.Collect(maps.Keys(profiles))
	sortProviderProfiles(ordered)
	keys := make([]provider.SessionKey, 0, len(ordered))
	for _, profile := range ordered {
		keys = append(keys, provider.SessionKey{WorkspaceID: s.Workspace.ID, DeviceID: s.Device.ID, Profile: profile})
	}
	return keys, nil
}

func (s *DesktopSnapshot) readSync(ctx context.Context, tx *sql.Tx) error {
	var err error
	s.Sync, err = readSyncStatus(ctx, tx)
	switch {
	case err == nil:
		canonical, repo, err := syncRemote(s.Sync.Config.Remote)
		if err != nil || canonical != s.Sync.Config.Remote {
			s.Sync.Config = nil
			return Fail("invalid-remote", "saved sync repository is invalid")
		}
		s.RepositoryURL = "https://github.com/" + repo
		if err := tx.QueryRowContext(ctx, "SELECT outcome FROM sync_state WHERE singleton=1").Scan(&s.LastOutcome); err != nil {
			return err
		}
	case Classify(err).Code == "sync-disabled":
		// A local queue and conflict evidence can exist without a remote.
		s.Sync.Conflicts, s.Sync.Blocked, err = conflictStatus(ctx, tx)
		if err != nil {
			return err
		}
	default:
		return err
	}

	// Label queue rows with locally readable catalog metadata. Some records
	// (such as deletions or unsubscribed objects) only have an ID locally.
	rows, err := tx.QueryContext(ctx, `
WITH events AS (
	SELECT id,1 pending,0 conflict,0 blocked FROM sync_outbox
	UNION ALL SELECT id,0,1,0 FROM sync_conflicts WHERE active=1
	UNION ALL SELECT id,0,0,1 FROM sync_blocked
), queue AS (
	SELECT id,max(pending) pending,max(conflict) conflict,max(blocked) blocked FROM events GROUP BY id
)
SELECT q.id,q.pending,q.conflict,q.blocked,coalesce(p.id,''),coalesce(p.slug,''),
       coalesce(r.kind,CASE WHEN q.id=p.id THEN 'project'
                            WHEN cfg.id IS NOT NULL THEN 'configuration'
                            WHEN m.id IS NOT NULL THEN 'membership' END,''),
       coalesce(CASE WHEN json_valid(r.metadata) THEN json_extract(r.metadata,'$.title') END,'')
FROM queue q LEFT JOIN records r ON r.id=q.id
LEFT JOIN configurations cfg ON cfg.id=q.id
LEFT JOIN memberships m ON m.id=q.id
LEFT JOIN projects p ON p.id=coalesce(r.project_id,cfg.project_id,m.project_id,q.id)
ORDER BY q.conflict DESC,q.blocked DESC,p.slug,q.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r DesktopQueueRecord
		if err := rows.Scan(&r.ID, &r.Pending, &r.Conflict, &r.Blocked, &r.ProjectID, &r.Project, &r.Kind, &r.Title); err != nil {
			return err
		}
		if r.Title == "" && r.Kind == "project" {
			r.Title = r.Project
		}
		if r.Pending {
			s.PendingIDs = append(s.PendingIDs, r.ID)
		}
		if r.Waiting() {
			s.QueuePending++
		}
		s.Queue = append(s.Queue, r)
	}
	s.Sync.Pending = len(s.PendingIDs)
	return rows.Err()
}

// CheckDesktopRemote reads the current target before an explicit network
// check. It never publishes, joins or applies remote records to the catalog.
func (a App) CheckDesktopRemote(ctx context.Context) (SyncConfig, SyncCheck, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return SyncConfig{}, SyncCheck{}, err
	}
	conf, err := loadSync(ctx, db)
	db.Close()
	if err != nil {
		return conf, SyncCheck{}, err
	}
	canonical, _, err := syncRemote(conf.Remote)
	if err != nil || canonical != conf.Remote {
		return conf, SyncCheck{}, Fail("invalid-remote", "saved sync repository is invalid")
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return conf, SyncCheck{}, err
	}
	return conf, checkSync(ctx, dir, conf), nil
}
