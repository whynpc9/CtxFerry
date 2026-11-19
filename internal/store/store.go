// Package store owns local persistence. It does not perform network operations.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var ErrNotInitialized = errors.New("workspace is not initialized; run ctxferry init")
var ErrUnsupportedSchema = errors.New("unsupported local schema")
var ErrMigrationRequired = errors.New("local schema requires migration")

const schemaVersion = 13

func Path(root string) string { return filepath.Join(root, "workspaces", "default", "catalog.sqlite") }

func Open(ctx context.Context, root string, create bool) (*sql.DB, error) {
	return OpenCatalog(ctx, Path(root), create)
}

// OpenCatalog opens a catalog at an explicit path, including staged snapshots.
func OpenCatalog(ctx context.Context, path string, create bool) (*sql.DB, error) {
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotInitialized
		}
		return nil, err
	}
	db, version, err := openCatalog(ctx, path, "rw")
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	if version == 0 && !create {
		return fail(ErrNotInitialized)
	}
	var mode string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return fail(err)
	}
	if mode != "wal" {
		return fail(fmt.Errorf("WAL unavailable: %s", mode))
	}
	if version < schemaVersion {
		err = Write(ctx, db, func(c *sql.Conn) error {
			var v int
			if err := c.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
				return err
			}
			if v > schemaVersion {
				return ErrUnsupportedSchema
			}
			if v == 0 {
				if _, err := c.ExecContext(ctx, schema); err != nil {
					return err
				}
				v = 1
			}
			if v == 1 {
				if _, err := c.ExecContext(ctx, identificationSchema); err != nil {
					return err
				}
				v = 2
			}
			if v == 2 {
				if _, err := c.ExecContext(ctx, contextSchema); err != nil {
					return err
				}
				v = 3
			}
			if v == 3 {
				if _, err := c.ExecContext(ctx, syncSchema); err != nil {
					return err
				}
				v = 4
			}
			if v == 4 {
				if _, err := c.ExecContext(ctx, roundSchema); err != nil {
					return err
				}
				v = 5
			}
			if v == 5 {
				if _, err := c.ExecContext(ctx, conflictSchema); err != nil {
					return err
				}
				v = 6
			}
			if v == 6 {
				if _, err := c.ExecContext(ctx, configurationSchema); err != nil {
					return err
				}
				v = 7
			}
			if v == 7 {
				if _, err := c.ExecContext(ctx, resourceSchema); err != nil {
					return err
				}
				v = 8
			}
			if v == 8 {
				if _, err := c.ExecContext(ctx, trustSchema); err != nil {
					return err
				}
				v = 9
			}
			if v == 9 {
				if _, err := c.ExecContext(ctx, syncProgressSchema); err != nil {
					return err
				}
				v = 10
			}
			if v == 10 {
				if _, err := c.ExecContext(ctx, artifactSchema); err != nil {
					return err
				}
				v = 11
			}
			if v == 11 {
				if _, err := c.ExecContext(ctx, onboardingSchema); err != nil {
					return err
				}
				v = 12
			}
			if v == 12 {
				_, err := c.ExecContext(ctx, artifactSyncSchema)
				return err
			}
			return nil
		})
		if err != nil {
			return fail(err)
		}
	}
	return db, nil
}

// Write serializes short read/modify/write operations before reading their baseline.
func Write(ctx context.Context, db *sql.DB, fn func(*sql.Conn) error) error {
	c, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	if _, err = c.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer c.ExecContext(context.Background(), "ROLLBACK")
	if err = fn(c); err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "COMMIT")
	return err
}

// OpenReadOnly observes the current catalog without creating or migrating it.
func OpenReadOnly(ctx context.Context, root string) (*sql.DB, error) {
	return openCatalogReadOnly(ctx, Path(root), schemaVersion)
}

// OpenBackupCatalog accepts earlier catalogs written by the workspace backup format.
func OpenBackupCatalog(ctx context.Context, path string) (*sql.DB, error) {
	return openCatalogReadOnly(ctx, path, 10)
}

func openCatalogReadOnly(ctx context.Context, path string, minimum int) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotInitialized
		}
		return nil, err
	}
	db, version, err := openCatalog(ctx, path, "ro")
	if err != nil {
		return nil, err
	}
	if version < minimum {
		db.Close()
		if version == 0 {
			return nil, ErrNotInitialized
		}
		return nil, ErrMigrationRequired
	}
	return db, nil
}

func openCatalog(ctx context.Context, path, mode string) (*sql.DB, int, error) {
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := u.Query()
	q.Set("mode", mode)
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, 0, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, int, error) { db.Close(); return nil, 0, err }
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > schemaVersion {
		return fail(ErrUnsupportedSchema)
	}
	return db, version, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS workspace (singleton INTEGER PRIMARY KEY CHECK(singleton=1), id TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS device (singleton INTEGER PRIMARY KEY CHECK(singleton=1), id TEXT NOT NULL, name TEXT NOT NULL, os TEXT NOT NULL, arch TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects (id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS checkout_bindings (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), device_id TEXT NOT NULL, path TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(device_id,path));
PRAGMA user_version=1;`

const identificationSchema = `
CREATE TABLE repositories (id TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE repository_aliases (repository_id TEXT NOT NULL REFERENCES repositories(id), remote TEXT NOT NULL, PRIMARY KEY(repository_id,remote));
CREATE TABLE memberships (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), repository_id TEXT NOT NULL REFERENCES repositories(id), subdirectory TEXT NOT NULL, UNIQUE(project_id,repository_id,subdirectory));
ALTER TABLE checkout_bindings ADD COLUMN membership_id TEXT REFERENCES memberships(id);
ALTER TABLE checkout_bindings ADD COLUMN checkout_root TEXT;
ALTER TABLE checkout_bindings ADD COLUMN git_dir TEXT;
ALTER TABLE checkout_bindings ADD COLUMN git_common_dir TEXT;
PRAGMA user_version=2;`

const contextSchema = `
CREATE TABLE records (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), kind TEXT NOT NULL, metadata TEXT NOT NULL);
CREATE INDEX records_project ON records(project_id,id);
CREATE TABLE context_bodies (record_id TEXT PRIMARY KEY REFERENCES records(id), body TEXT NOT NULL);
CREATE VIRTUAL TABLE context_fts USING fts5(id UNINDEXED, project_id UNINDEXED, content);
PRAGMA user_version=3;`

const syncSchema = `
CREATE TABLE sync_config (singleton INTEGER PRIMARY KEY CHECK(singleton=1), remote TEXT NOT NULL, workspace_id TEXT NOT NULL, branch TEXT NOT NULL, identity TEXT NOT NULL);
CREATE TABLE sync_state (singleton INTEGER PRIMARY KEY CHECK(singleton=1), epoch TEXT NOT NULL, confirmed_head TEXT NOT NULL DEFAULT '', pending TEXT NOT NULL DEFAULT '');
CREATE TABLE sync_previews (id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, files TEXT NOT NULL);
CREATE TABLE export_identities (id TEXT PRIMARY KEY, revision TEXT NOT NULL, operation_id TEXT NOT NULL, fingerprint TEXT NOT NULL, previous_revision TEXT NOT NULL);
PRAGMA user_version=4;`

const roundSchema = `
CREATE TABLE sync_baselines (path TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE sync_outbox (id TEXT PRIMARY KEY, base BLOB, proposed BLOB NOT NULL, operation_id TEXT NOT NULL, epoch TEXT NOT NULL);
CREATE TABLE project_subscriptions (project_id TEXT PRIMARY KEY REFERENCES projects(id), subscribed INTEGER NOT NULL);
ALTER TABLE sync_state ADD COLUMN last_sync_at TEXT NOT NULL DEFAULT '';
ALTER TABLE sync_state ADD COLUMN outcome TEXT NOT NULL DEFAULT '';
PRAGMA user_version=5;`

const conflictSchema = `
CREATE TABLE IF NOT EXISTS sync_conflicts (token TEXT PRIMARY KEY, id TEXT NOT NULL, base BLOB, local BLOB NOT NULL, remote BLOB NOT NULL, head TEXT NOT NULL, active INTEGER NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS active_conflict ON sync_conflicts(id) WHERE active=1;
CREATE TABLE IF NOT EXISTS sync_objects (path TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS sync_tombstones (id TEXT PRIMARY KEY, data BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS sync_blocked (id TEXT PRIMARY KEY, reason TEXT NOT NULL);
PRAGMA user_version=6;`

const configurationSchema = `
CREATE TABLE IF NOT EXISTS configurations (id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects(id), payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS local_configurations (
    device_id TEXT NOT NULL, project_id TEXT NOT NULL REFERENCES projects(id), checkout TEXT NOT NULL,
    payload TEXT NOT NULL, PRIMARY KEY(device_id,project_id,checkout)
);
CREATE TABLE IF NOT EXISTS profile_selections (
    device_id TEXT NOT NULL, project_id TEXT NOT NULL REFERENCES projects(id), checkout TEXT NOT NULL,
    profile TEXT NOT NULL, selected_at TEXT NOT NULL, PRIMARY KEY(device_id,project_id,checkout)
);
PRAGMA user_version=7;`

const resourceSchema = `
CREATE TABLE resource_routes (
  resource_id TEXT NOT NULL REFERENCES records(id),
  device_id TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  proxy TEXT NOT NULL,
  network TEXT NOT NULL,
  PRIMARY KEY(resource_id,device_id)
);
CREATE TABLE resource_observations (
  seq INTEGER PRIMARY KEY,
  resource_id TEXT NOT NULL REFERENCES records(id),
  device_id TEXT NOT NULL,
  data TEXT NOT NULL
);
CREATE INDEX resource_observation_history ON resource_observations(resource_id,device_id,seq);
PRAGMA user_version=8;`

// Execution trust is device-local: it is never exported to the sync repository.
const trustSchema = `
CREATE TABLE IF NOT EXISTS execution_trust (
  device_id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  profile_id TEXT NOT NULL,
  checkout_root TEXT NOT NULL,
  membership_root TEXT NOT NULL,
  digest TEXT NOT NULL,
  definition TEXT NOT NULL,
  enabled_at TEXT NOT NULL,
  PRIMARY KEY(device_id,task_id,profile_id,checkout_root,membership_root)
);
PRAGMA user_version=9;`

const syncProgressSchema = `
CREATE TABLE IF NOT EXISTS sync_progress (
    singleton INTEGER PRIMARY KEY CHECK(singleton=1),
    requested INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    receipt TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO sync_progress(singleton) VALUES (1);
PRAGMA user_version=10;`

// Artifact manifests are local catalog data. Payload bytes live in the object cache.
const artifactSchema = `
CREATE TABLE IF NOT EXISTS artifact_runs (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    provenance TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS artifact_run_project ON artifact_runs(project_id,id);
CREATE TABLE IF NOT EXISTS artifacts (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES artifact_runs(id),
    created_at TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS artifacts_run ON artifacts(run_id);
CREATE INDEX IF NOT EXISTS artifacts_hash ON artifacts(sha256);
PRAGMA user_version=11;`

const onboardingSchema = `
CREATE TABLE IF NOT EXISTS onboarding (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 directory TEXT NOT NULL,
 project_id TEXT NOT NULL DEFAULT '',
 completed INTEGER NOT NULL DEFAULT 0 CHECK(completed IN (0,1))
);
CREATE TABLE IF NOT EXISTS onboarding_notes (
 path TEXT PRIMARY KEY,
 fingerprint TEXT NOT NULL,
 document_id TEXT NOT NULL UNIQUE
);
PRAGMA user_version=12;`

const artifactSyncSchema = `
ALTER TABLE sync_state ADD COLUMN history_bytes INTEGER;
CREATE TABLE artifact_observations (
    artifact_id TEXT NOT NULL REFERENCES records(id),
    device_id TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    PRIMARY KEY(artifact_id,device_id,sha256)
);
PRAGMA user_version=13;`
