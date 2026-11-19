package app

import (
	"context"
	"database/sql"
)

func inspectArtifactManifest(ctx context.Context, db *sql.DB, projectID, id string) (ArtifactManifest, error) {
	if !validUUID(id) {
		return ArtifactManifest{}, Fail("artifact-not-found", "artifact was not found in this Project")
	}
	list, err := listArtifactManifests(ctx, db, projectID, "", id)
	if err != nil {
		return ArtifactManifest{}, err
	}
	if len(list.Artifacts) == 0 {
		return ArtifactManifest{}, Fail("artifact-not-found", "artifact was not found in this Project")
	}
	return list.Artifacts[0], nil
}

func listArtifactManifests(ctx context.Context, db *sql.DB, projectID, runID, id string) (ArtifactList, error) {
	out := ArtifactList{Artifacts: []ArtifactManifest{}}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, artifactQuery+" AND (?='' OR a.id=?) AND (?='' OR r.id=?) ORDER BY a.rowid",
		projectID, id, id, runID, runID)
	if err != nil {
		return out, err
	}
	byID := map[string]int{}
	for rows.Next() {
		m, err := scanArtifact(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		byID[m.ID] = len(out.Artifacts)
		out.Artifacts = append(out.Artifacts, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT r.metadata, o.id IS NOT NULL
		FROM records r LEFT JOIN sync_outbox o ON o.id=r.id
		WHERE r.kind=? AND r.project_id=? AND (?='' OR r.id=?)
		AND (?='' OR json_extract(r.metadata,'$.payload.run.id')=?)
		ORDER BY json_extract(r.metadata,'$.provenance.updatedAt'), r.id`, artifactKind, projectID, id, id, runID, runID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		var pending bool
		if err := rows.Scan(&raw, &pending); err != nil {
			rows.Close()
			return out, err
		}
		r, err := decodeRecord(raw)
		if err != nil {
			rows.Close()
			return out, err
		}
		p := r.Artifact
		state := "shared"
		switch {
		case pending:
			state = "pending"
		case r.Deleted:
			state = "withdrawn"
		case p.Omitted != "":
			state = "metadata-only"
		}
		status := &ArtifactSyncStatus{State: state, Mode: p.Mode, Reason: p.Omitted}
		if pos, local := byID[r.ID]; local {
			out.Artifacts[pos].Sync = status
		} else if !r.Deleted {
			payload := ArtifactPayload{Name: p.Name, Type: p.Type, Size: p.Size, SHA256: p.SHA256, Sensitivity: p.Sensitivity, Storage: state}
			if state == "shared" {
				payload.Location = p.objectPath()
			}
			run := ArtifactRun{
				ID: p.Run.ID, ProjectID: r.Scope.ID, DeviceID: p.Run.DeviceID,
				CreatedAt: p.Run.CreatedAt, OS: p.Run.OS, Arch: p.Run.Arch, RunProvenance: RunProvenance{Summary: p.Run.Summary},
			}
			byID[r.ID] = len(out.Artifacts)
			out.Artifacts = append(out.Artifacts, ArtifactManifest{ID: r.ID, CreatedAt: r.Provenance.UpdatedAt, Run: run, Payload: payload, Sync: status})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT o.artifact_id,o.device_id,o.sha256,o.observed_at
		FROM artifact_observations o JOIN records r ON r.id=o.artifact_id
		WHERE r.project_id=? AND (?='' OR r.id=?) AND o.sha256=json_extract(r.metadata,'$.payload.sha256')
		ORDER BY o.observed_at,o.device_id`, projectID, id, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var artifactID string
		observation := ArtifactObservation{Scope: "payload-integrity", Tool: "ctxferry"}
		if err := rows.Scan(&artifactID, &observation.DeviceID, &observation.SHA256, &observation.ObservedAt); err != nil {
			return out, err
		}
		if pos, ok := byID[artifactID]; ok && out.Artifacts[pos].Sync.State == "shared" {
			out.Artifacts[pos].Observations = append(out.Artifacts[pos].Observations, observation)
		}
	}
	return out, rows.Err()
}
