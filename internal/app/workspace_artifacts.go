package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/whynpc9/CtxFerry/internal/store"
)

const backupArtifactQuery = "SELECT sha256,payload FROM artifacts"

func copyBackupArtifacts(rows *sql.Rows, source, destination string) error {
	defer rows.Close()
	copied := map[string]int64{}
	for rows.Next() {
		var hash string
		var raw []byte
		if err := rows.Scan(&hash, &raw); err != nil {
			return err
		}
		var p ArtifactPayload
		if json.Unmarshal(raw, &p) != nil || !validArtifactHash(hash) || hash != p.SHA256 ||
			p.Size < 0 || p.Location != artifactLocation(hash) || p.Storage != "local-only" {
			return Fail("artifact-invalid", "backup artifact manifest has an invalid payload location or identity")
		}
		if size, ok := copied[hash]; ok {
			if size != p.Size {
				return Fail("artifact-hash-mismatch", "artifact manifests disagree about the payload size")
			}
			continue
		}
		relative := filepath.FromSlash(p.Location)
		if err := copyBackupArtifact(filepath.Join(source, relative), filepath.Join(destination, relative), p); err != nil {
			return err
		}
		copied[hash] = p.Size
	}
	return rows.Err()
}

func copyBackupArtifact(source, destination string, p ArtifactPayload) error {
	in, size, err := openArtifactFile(source)
	if os.IsNotExist(err) {
		return Fail("artifact-unavailable", "a referenced artifact payload is missing")
	}
	if err != nil {
		return err
	}
	defer in.Close()
	if size != p.Size {
		return Fail("artifact-hash-mismatch", "referenced artifact payload size does not match")
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return finishArtifactCopy(in, out, p)
}

func (a App) recoverBackupArtifacts(ctx context.Context, catalog, source string) error {
	db, err := store.OpenBackupCatalog(ctx, catalog)
	if err != nil {
		return err
	}
	defer db.Close()
	// Format-1 catalogs saved before the artifact schema have no payloads.
	var hasArtifacts bool
	if err = db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM sqlite_schema WHERE type='table' AND name='artifacts')").Scan(&hasArtifacts); err != nil || !hasArtifacts {
		return err
	}
	rows, err := db.QueryContext(ctx, backupArtifactQuery)
	if err != nil {
		return err
	}
	return copyBackupArtifacts(rows, filepath.Join(source, backupArtifacts), a.artifactRoot())
}
