package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

const (
	artifactKind          = "artifact"
	artifactAutoLimit     = 1 << 20
	artifactExplicitLimit = 5 << 20
)

type artifactRunSummary struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId"`
	CreatedAt string `json:"createdAt"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Summary   string `json:"summary"`
}

type artifactPublication struct {
	Run         artifactRunSummary `json:"run"`
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Size        int64              `json:"size"`
	SHA256      string             `json:"sha256"`
	Sensitivity string             `json:"sensitivity"`
	Mode        string             `json:"mode"`
	Omitted     string             `json:"omitted,omitempty"`
}

type ArtifactSyncStatus struct {
	State  string `json:"state"`
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
}

type ArtifactObservation struct {
	DeviceID   string `json:"deviceId"`
	ObservedAt string `json:"observedAt"`
	SHA256     string `json:"sha256"`
	Scope      string `json:"scope"`
	Tool       string `json:"tool"`
}

type ArtifactSharePreview struct {
	PreviewID        string              `json:"previewId"`
	ID               string              `json:"id"`
	ProjectID        string              `json:"projectId"`
	Manifest         artifactPublication `json:"manifest"`
	Boundary         string              `json:"boundary"`
	History          *SyncHistory        `json:"history,omitempty"`
	NewPayloadBytes  int64               `json:"newPayloadBytes"`
	ExpectedOmission string              `json:"expectedOmission,omitempty"`
	State            string              `json:"state"`
}

func (p artifactPublication) objectPath() string {
	return "objects/" + p.SHA256
}

func sameArtifactIdentity(a, b artifactPublication) bool {
	a.Omitted, b.Omitted = "", ""
	return a == b
}

func (p artifactPublication) payloadReason() string {
	if p.Mode == "metadata" {
		return "metadata-selected"
	}
	if p.Sensitivity == "sensitive" {
		return "sensitive"
	}
	mediaType, _, _ := mime.ParseMediaType(p.Type)
	// An explicit selection attests that a supported file is a small,
	// shareable report or sample. Known raw formats stay out even if mislabeled.
	switch strings.ToLower(filepath.Ext(p.Name)) {
	case ".log", ".dump", ".sql", ".db", ".sqlite", ".sqlite3", ".bak", ".pcap", ".pcapng", ".env", ".pem", ".key", ".pt", ".pth", ".onnx", ".gguf", ".safetensors", ".bin", ".zip", ".gz", ".tar", ".7z", ".parquet", ".jsonl", ".ndjson":
		return "excluded-format"
	}
	switch mediaType {
	case "application/json", "application/xml", "application/pdf", "text/xml", "text/plain", "text/markdown", "text/csv", "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "excluded-format"
	}
	limit := int64(artifactAutoLimit)
	if p.Mode == "explicit" {
		limit = artifactExplicitLimit
	}
	if p.Size > limit {
		return "payload-size"
	}
	return ""
}

func validArtifactPublication(p artifactPublication) bool {
	mediaType, _, err := mime.ParseMediaType(p.Type)
	if err != nil || !strings.Contains(mediaType, "/") || p.Size < 0 || !validArtifactHash(p.SHA256) ||
		strings.TrimSpace(p.Name) == "" || p.Name == "." || p.Name == ".." || strings.ContainsAny(p.Name, "/\\\r\n\x00") ||
		!validUUID(p.Run.ID) || !validUUID(p.Run.DeviceID) || p.Run.OS == "" || p.Run.Arch == "" ||
		strings.TrimSpace(p.Run.Summary) == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, p.Run.CreatedAt); err != nil {
		return false
	}
	switch p.Sensitivity {
	case "public", "internal", "sensitive":
	default:
		return false
	}
	switch p.Mode {
	case "automatic", "explicit", "metadata":
	default:
		return false
	}
	if reason := p.payloadReason(); reason != "" {
		return p.Omitted == reason
	}
	switch p.Omitted {
	case "", "history-pause", "publication-budget", "payload-unavailable", "excluded-format", "staging-budget":
		return p.Omitted != "history-pause" || p.Mode == "automatic"
	default:
		return false
	}
}

func excludedArtifactBytes(data []byte) bool {
	data = bytes.TrimSpace(data)
	return bytes.HasPrefix(data, []byte("version https://git-lfs.github.com/spec/v1")) ||
		bytes.HasPrefix(data, []byte("SQLite format 3\x00")) ||
		bytes.HasPrefix(data, []byte("PGDMP")) ||
		bytes.HasPrefix(data, []byte("GGUF"))
}

func (a App) readLocalArtifactBytes(p artifactPublication) ([]byte, error) {
	f, size, err := openArtifactFile(filepath.Join(a.artifactRoot(), filepath.FromSlash(artifactLocation(p.SHA256))))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if size != p.Size {
		return nil, Fail("artifact-hash-mismatch", "cached artifact payload size does not match")
	}
	var b bytes.Buffer
	if err := verifyArtifact(f, &b, ArtifactPayload{Size: p.Size, SHA256: p.SHA256}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// ShareArtifact previews one registered artifact, then queues exactly the
// reviewed metadata and payload mode. The sync planner applies current quotas.
func (a App) ShareArtifact(ctx context.Context, selector, cwd, id, mode, approval string) (ArtifactSharePreview, error) {
	var result ArtifactSharePreview
	switch mode {
	case "automatic", "explicit", "metadata":
	default:
		return result, Fail("usage", "artifact share mode must be automatic, explicit or metadata")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return result, err
	}
	defer release()
	db, resolution, err := a.artifactProject(ctx, selector, cwd)
	if err != nil {
		return result, err
	}
	defer db.Close()
	if err := a.prepareBaseline(ctx, db); err != nil {
		return result, err
	}
	conf, err := loadSync(ctx, db)
	if err != nil {
		return result, err
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		m, err := scanArtifact(c.QueryRowContext(ctx, artifactQuery+" AND a.id=?", resolution.Project.ID, id))
		if errors.Is(err, sql.ErrNoRows) {
			return Fail("artifact-not-found", "only an artifact registered locally in this Project can be shared")
		}
		if err != nil {
			return err
		}
		if m.Run.ProjectID != resolution.Project.ID || m.Run.DeviceID != resolution.Device.ID {
			return Fail("invalid-reference", "artifact run does not belong to this Project and device")
		}
		var exists, reused bool
		if err := c.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM records WHERE id=?), EXISTS(SELECT 1 FROM sync_baselines WHERE path=?)",
			id, "objects/"+m.Payload.SHA256).Scan(&exists, &reused); err != nil {
			return err
		}
		if exists {
			return Fail("artifact-already-shared", "this immutable artifact already has a publication record")
		}
		var head string
		var history sql.NullInt64
		if err := c.QueryRowContext(ctx, "SELECT confirmed_head,history_bytes FROM sync_state").Scan(&head, &history); err != nil {
			return err
		}
		p := artifactPublication{
			Run:  artifactRunSummary{m.Run.ID, m.Run.DeviceID, m.Run.CreatedAt, m.Run.OS, m.Run.Arch, m.Run.Summary},
			Name: m.Payload.Name, Type: m.Payload.Type, Size: m.Payload.Size, SHA256: m.Payload.SHA256,
			Sensitivity: m.Payload.Sensitivity, Mode: mode,
		}
		p.Omitted = p.payloadReason()
		if p.Omitted == "" {
			data, err := a.readLocalArtifactBytes(p)
			if err != nil {
				return err
			}
			if excludedArtifactBytes(data) {
				p.Omitted = "excluded-format"
			}
		}
		if !validArtifactPublication(p) || !validUUID(id) {
			return Fail("artifact-invalid", "artifact metadata cannot be shared")
		}
		const boundary = "The previewed manifest and eligible payload can enter private Git history in plaintext. Review the name, summary and payload for private paths, raw data and credentials. History retains published content."
		result = ArtifactSharePreview{ID: id, ProjectID: m.Run.ProjectID, Manifest: p, Boundary: boundary, State: "preview"}
		if history.Valid || head == "" {
			result.History = syncHistory(history.Int64)
		}
		if p.Omitted == "" && !reused {
			if p.Mode == "automatic" && history.Valid && (history.Int64 >= historyPauseBytes || history.Int64+p.Size > historyPauseBytes) {
				result.ExpectedOmission = "history-pause"
			} else {
				result.NewPayloadBytes = p.Size
			}
		}
		env := publishedEnvelope{
			SchemaVersion: 1, ID: id, Kind: artifactKind, WorkspaceID: conf.WorkspaceID, Scope: Scope{"project", m.Run.ProjectID},
			Policy: Policy{"shared", "reference"}, Payload: p,
		}
		env.Provenance.DeviceID, env.Provenance.UpdatedAt = m.Run.DeviceID, m.CreatedAt
		preview, err := json.Marshal(struct {
			Record          publishedEnvelope
			Head            string
			NewPayloadBytes int64
		}{env, head, result.NewPayloadBytes})
		if err != nil {
			return err
		}
		// Leave room for the assigned revision and operation IDs.
		if len(preview) > MetadataLimit-256 {
			return Fail("size-limit", "artifact publication metadata exceeds 256 KiB")
		}
		result.PreviewID = digest(preview)
		if approval == "" {
			return nil
		}
		if approval != result.PreviewID {
			return Fail("stale-preview", "inspect a fresh artifact share preview")
		}
		env.Revision, env.OperationID = uuid.NewString(), uuid.NewString()
		data, err := json.Marshal(env)
		if err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?)", id, m.Run.ProjectID, artifactKind, string(data)); err != nil {
			return err
		}
		if err := queueChanges(ctx, c); err != nil {
			return err
		}
		result.State = "pending"
		return nil
	})
	return result, err
}

func installArtifactPublication(ctx context.Context, c *sql.Conn, r remoteRecord) error {
	data, err := json.Marshal(r.publishedEnvelope)
	if err != nil {
		return err
	}
	if _, err := c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata",
		r.ID, r.Scope.ID, artifactKind, string(data)); err != nil {
		return err
	}
	if r.Deleted || r.Artifact.Omitted != "" {
		return nil
	}
	_, err = c.ExecContext(ctx, `INSERT OR IGNORE INTO artifact_observations(artifact_id,device_id,sha256,observed_at)
		SELECT ?,id,?,? FROM device WHERE id<>?`, r.ID, r.Artifact.SHA256, now(), r.Artifact.Run.DeviceID)
	return err
}
