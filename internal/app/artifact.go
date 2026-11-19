package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"slices"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type RunProvenance struct {
	SourceRevision string   `json:"sourceRevision"`
	Dirty          bool     `json:"dirty"`
	DiffID         string   `json:"diffId,omitempty"`
	Profile        string   `json:"profile"`
	ToolVersions   []string `json:"toolVersions"`
	Inputs         []string `json:"inputs"`
	Command        []string `json:"command"`
	ExitCode       int      `json:"exitCode"`
	Summary        string   `json:"summary"`
	Completed      []string `json:"completed"`
	Uncompleted    []string `json:"uncompleted"`
}

type ArtifactRun struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	DeviceID  string `json:"deviceId"`
	CreatedAt string `json:"createdAt"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	RunProvenance
}

type ArtifactPayload struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Sensitivity string `json:"sensitivity"`
	Storage     string `json:"storage"`
	Location    string `json:"location"`
}

type ArtifactManifest struct {
	ID        string          `json:"id"`
	CreatedAt string          `json:"createdAt"`
	Run       ArtifactRun     `json:"run"`
	Payload   ArtifactPayload `json:"payload"`
}

type ArtifactList struct {
	Artifacts []ArtifactManifest `json:"artifacts"`
}

type ArtifactFetch struct {
	ID     string `json:"id"`
	Output string `json:"output"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ArtifactRegistration struct {
	RunID       string
	Provenance  *RunProvenance
	Type        string
	Sensitivity string
	CacheLimit  int64
}

func (a App) artifactProject(ctx context.Context, selector, cwd string) (*sql.DB, Resolution, error) {
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return nil, Resolution{}, err
	}
	resolution, err := resolveProjectLocation(ctx, db, selector, cwd, false)
	if err == nil {
		err = requireSubscription(ctx, db, resolution.Project.ID)
	}
	if err != nil {
		db.Close()
		return nil, Resolution{}, err
	}
	return db, resolution, nil
}

func (a App) AddArtifact(ctx context.Context, selector, cwd, path string, spec ArtifactRegistration) (ArtifactManifest, error) {
	if (spec.RunID == "") == (spec.Provenance == nil) {
		return ArtifactManifest{}, Fail("usage", "provide provenance for a new run or select an existing run")
	}
	if spec.CacheLimit <= 0 {
		return ArtifactManifest{}, Fail("usage", "artifact cache limit must be positive")
	}
	mediaType, _, err := mime.ParseMediaType(spec.Type)
	if err != nil || !strings.Contains(mediaType, "/") {
		return ArtifactManifest{}, Fail("usage", "artifact type must be a media type such as application/json")
	}
	switch spec.Sensitivity {
	case "public", "internal", "sensitive":
	default:
		return ArtifactManifest{}, Fail("usage", "sensitivity must be public, internal or sensitive")
	}

	db, resolution, err := a.artifactProject(ctx, selector, cwd)
	if err != nil {
		return ArtifactManifest{}, err
	}
	defer db.Close()
	id, err := newID()
	if err != nil {
		return ArtifactManifest{}, err
	}
	manifest := ArtifactManifest{ID: id, CreatedAt: now()}
	var runData []byte
	if spec.Provenance != nil {
		run, err := makeArtifactRun(resolution, manifest.CreatedAt, *spec.Provenance)
		if err != nil {
			return ArtifactManifest{}, err
		}
		manifest.Run = run
		runData, err = json.Marshal(run)
		if err != nil {
			return ArtifactManifest{}, err
		}
		if len(runData) > 256<<10 {
			return ArtifactManifest{}, Fail("size-limit", "run provenance exceeds 256 KiB")
		}
	} else {
		err := db.QueryRowContext(ctx, "SELECT provenance FROM artifact_runs WHERE project_id=? AND id=?", resolution.Project.ID, spec.RunID).Scan(&runData)
		if errors.Is(err, sql.ErrNoRows) {
			return ArtifactManifest{}, Fail("run-not-found", "run was not found in this Project")
		}
		if err != nil {
			return ArtifactManifest{}, err
		}
		if err := json.Unmarshal(runData, &manifest.Run); err != nil {
			return ArtifactManifest{}, err
		}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	payload, source, err := readArtifactSource(path, spec.CacheLimit)
	if err != nil {
		return ArtifactManifest{}, err
	}
	defer source.Close()
	payload.Type, payload.Sensitivity = spec.Type, spec.Sensitivity
	manifest.Payload = payload
	payloadData, err := json.Marshal(payload)
	if err != nil {
		return ArtifactManifest{}, err
	}

	err = store.Write(ctx, db, func(c *sql.Conn) error {
		// Only these local writes publish objects. Serialize capacity checks and
		// publication alongside manifest inserts so concurrent adds share the cap.
		if err := a.cacheArtifact(ctx, c, source, payload, spec.CacheLimit); err != nil {
			return err
		}
		if spec.Provenance != nil {
			if _, err := c.ExecContext(ctx, "INSERT INTO artifact_runs(id,project_id,provenance) VALUES(?,?,?)", manifest.Run.ID, manifest.Run.ProjectID, runData); err != nil {
				return err
			}
		}
		_, err := c.ExecContext(ctx, "INSERT INTO artifacts(id,run_id,created_at,sha256,payload) VALUES(?,?,?,?,?)", manifest.ID, manifest.Run.ID, manifest.CreatedAt, payload.SHA256, payloadData)
		return err
	})
	return manifest, err
}

func makeArtifactRun(resolution Resolution, createdAt string, p RunProvenance) (ArtifactRun, error) {
	required := []string{p.SourceRevision, p.Profile, p.Summary}
	if len(p.Command) > 0 {
		required = append(required, p.Command[0])
	}
	if len(p.Command) == 0 || slices.ContainsFunc(required, func(value string) bool { return strings.TrimSpace(value) == "" }) {
		return ArtifactRun{}, Fail("usage", "a new run requires a source revision, Profile, command and summary")
	}
	if p.DiffID != "" && !p.Dirty {
		return ArtifactRun{}, Fail("usage", "a diff identity requires dirty source")
	}
	for _, values := range []*[]string{&p.ToolVersions, &p.Inputs, &p.Completed, &p.Uncompleted} {
		if *values == nil {
			*values = []string{}
		}
	}
	id, err := newID()
	if err != nil {
		return ArtifactRun{}, err
	}
	return ArtifactRun{
		ID: id, ProjectID: resolution.Project.ID, DeviceID: resolution.Device.ID,
		CreatedAt: createdAt, OS: resolution.Device.OS, Arch: resolution.Device.Arch,
		RunProvenance: p,
	}, nil
}

const artifactQuery = `SELECT a.id,a.created_at,a.payload,r.provenance
	FROM artifacts a JOIN artifact_runs r ON r.id=a.run_id WHERE r.project_id=?`

func scanArtifact(row interface{ Scan(...any) error }) (ArtifactManifest, error) {
	var m ArtifactManifest
	var payload, run []byte
	if err := row.Scan(&m.ID, &m.CreatedAt, &payload, &run); err != nil {
		return m, err
	}
	if err := json.Unmarshal(payload, &m.Payload); err != nil {
		return m, err
	}
	return m, json.Unmarshal(run, &m.Run)
}

func (a App) ListArtifacts(ctx context.Context, selector, cwd, runID string) (ArtifactList, error) {
	out := ArtifactList{Artifacts: []ArtifactManifest{}}
	db, resolution, err := a.artifactProject(ctx, selector, cwd)
	if err != nil {
		return out, err
	}
	defer db.Close()
	query := artifactQuery
	args := []any{resolution.Project.ID}
	if runID != "" {
		query += " AND r.id=?"
		args = append(args, runID)
	}
	rows, err := db.QueryContext(ctx, query+" ORDER BY a.rowid", args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanArtifact(rows)
		if err != nil {
			return out, err
		}
		out.Artifacts = append(out.Artifacts, m)
	}
	return out, rows.Err()
}

func (a App) InspectArtifact(ctx context.Context, selector, cwd, id string) (ArtifactManifest, error) {
	db, resolution, err := a.artifactProject(ctx, selector, cwd)
	if err != nil {
		return ArtifactManifest{}, err
	}
	defer db.Close()
	m, err := scanArtifact(db.QueryRowContext(ctx, artifactQuery+" AND a.id=?", resolution.Project.ID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, Fail("artifact-not-found", "artifact was not found in this Project")
	}
	return m, err
}

func (a App) FetchArtifact(ctx context.Context, selector, cwd, id, output string) (ArtifactFetch, error) {
	if output == "" {
		return ArtifactFetch{}, Fail("usage", "artifact fetch requires an output file")
	}
	m, err := a.InspectArtifact(ctx, selector, cwd, id)
	if err != nil {
		return ArtifactFetch{}, err
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(cwd, output)
	}
	if err := a.fetchArtifactObject(m.Payload, output); err != nil {
		return ArtifactFetch{}, err
	}
	return ArtifactFetch{ID: id, Output: output, Size: m.Payload.Size, SHA256: m.Payload.SHA256}, nil
}
