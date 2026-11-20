package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"

	"github.com/whynpc9/CtxFerry/internal/store"
)

func (a App) ListDesktopProjects(ctx context.Context) ([]Project, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT id,slug,created_at FROM projects p
		WHERE NOT EXISTS (SELECT 1 FROM sync_tombstones WHERE id=p.id)
		ORDER BY slug,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Slug, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type DesktopArtifact struct {
	ArtifactManifest
	Content  string // cache, snapshot, metadata-only, missing, invalid, unavailable
	Location string
}

func (a App) ListDesktopArtifacts(ctx context.Context, project string) ([]DesktopArtifact, error) {
	db, p, err := a.desktopArtifactProject(ctx, project)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	list, err := listArtifactManifests(ctx, db, p.ID, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]DesktopArtifact, 0, len(list.Artifacts))
	for _, manifest := range list.Artifacts {
		artifact := DesktopArtifact{ArtifactManifest: manifest}
		payload := manifest.Payload
		switch payload.Storage {
		case "local-only":
			f, err := a.openCachedArtifact(payload)
			if err == nil {
				f.Close()
				artifact.Content = "cache"
			} else {
				artifact.Content = artifactContentError(err)
			}
			if validArtifactHash(payload.SHA256) && payload.Location == artifactLocation(payload.SHA256) {
				artifact.Location = filepath.Join(a.artifactRoot(), filepath.FromSlash(payload.Location))
			}
		case "shared":
			var size int64
			err := db.QueryRowContext(ctx, "SELECT length(data) FROM sync_baselines WHERE path=?", payload.Location).Scan(&size)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				artifact.Content = "missing"
			case err != nil:
				return nil, err
			case size != payload.Size:
				artifact.Content = "invalid"
			default:
				artifact.Content = "snapshot"
			}
			artifact.Location = payload.Location
		default:
			artifact.Content = "metadata-only"
		}
		out = append(out, artifact)
	}
	return out, nil
}

func artifactContentError(err error) string {
	switch Classify(err).Code {
	case "artifact-unavailable":
		return "missing"
	case "artifact-hash-mismatch", "artifact-invalid", "invalid-path", "artifact-changed":
		return "invalid"
	default:
		return "unavailable"
	}
}

func (a App) desktopArtifactProject(ctx context.Context, project string) (*sql.DB, Project, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, Project{}, err
	}
	p, err := selectProject(ctx, db, project)
	if err == nil {
		err = requireSubscription(ctx, db, p.ID)
	}
	if err != nil {
		db.Close()
		return nil, Project{}, err
	}
	return db, p, nil
}

// CheckDesktopArtifact verifies the current local bytes without moving them.
func (a App) CheckDesktopArtifact(ctx context.Context, project, id string) error {
	db, p, err := a.desktopArtifactProject(ctx, project)
	if err != nil {
		return err
	}
	defer db.Close()
	m, err := inspectArtifactManifest(ctx, db, p.ID, id)
	if err != nil {
		return err
	}
	in, err := a.openArtifactPayload(ctx, db, m.Payload)
	if err != nil {
		return err
	}
	defer in.Close()
	return verifyArtifact(in, io.Discard, m.Payload)
}
