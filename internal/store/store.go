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

func Path(root string) string { return filepath.Join(root, "workspaces", "default", "catalog.sqlite") }

func Open(ctx context.Context, root string, create bool) (*sql.DB, error) {
	path := Path(root)
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
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	q := u.Query()
	q.Set("mode", "rw")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version > 4 {
		return fail(ErrUnsupportedSchema)
	}
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
	if version < 4 {
		err = Write(ctx, db, func(c *sql.Conn) error {
			var v int
			if err := c.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
				return err
			}
			if v > 4 {
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
				_, err := c.ExecContext(ctx, syncSchema)
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
