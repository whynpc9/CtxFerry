package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// recordKind is the seam each published Record kind plugs into. Snapshot
// decoding, validation, installation, export, dependency planning and
// deferred-record retention iterate recordKinds instead of switching on a
// kind name, so a new kind is one entry in this table.
type recordKind struct {
	name string
	// table holds the installed local projection. A baseline record that is
	// absent from it was deferred, not deleted.
	table string
	// decode fills the kind's typed payload. A nil decode reads recordPayload.
	decode func(r *remoteRecord, payload json.RawMessage, fields map[string]json.RawMessage) error
	// detailedDecodeErrors reports decode failures as an incomplete record of this kind.
	detailedDecodeErrors bool
	// validate checks one record within a snapshot.
	validate func(r remoteRecord, s *snapshotCheck) error
	// references reports whether r's typed references resolve to live records.
	references func(r remoteRecord, records map[string]remoteRecord) bool
	// dependencies are the record IDs, besides a Project scope, that r needs.
	dependencies func(r remoteRecord) []string
	// uniqueKey names a workspace-wide uniqueness constraint r participates in.
	uniqueKey func(r remoteRecord) string
	// install writes the local projection of r.
	install func(ctx context.Context, c *sql.Conn, files map[string][]byte, r remoteRecord) error
	// exportIdentity tracks the exported revision in export_identities.
	exportIdentity bool
	// export adds every shared record of this kind to the snapshot.
	export func(x *snapshotExport) error
}

// recordKinds is in installation order: a kind only depends on earlier kinds.
// It is assigned in init because exporters consult the registry themselves.
var recordKinds []recordKind

func init() {
	recordKinds = []recordKind{
		projectKind, repositoryKind, membershipKind, contextKind, configurationKind,
		resourceKind, taskKind, secretBindingRecordKind, artifactRecordKind, skillPinRecordKind, skillGroupRecordKind,
	}
}

func lookupKind(name string) (recordKind, bool) {
	for _, k := range recordKinds {
		if k.name == name {
			return k, true
		}
	}
	return recordKind{}, false
}

func registeredKinds(table string) []string {
	var names []string
	for _, k := range recordKinds {
		if k.table == table {
			names = append(names, k.name)
		}
	}
	return names
}

func uniquenessKey(r remoteRecord) string {
	if k, ok := lookupKind(r.Kind); ok && k.uniqueKey != nil {
		return k.uniqueKey(r)
	}
	return ""
}

// snapshotCheck carries cross-record state while a snapshot is validated.
type snapshotCheck struct {
	workspace       workspaceFormat
	files           map[string][]byte
	runs            map[string]remoteRecord
	artifactObjects map[string]bool
}

func invalidCatalog() error {
	return Fail("incomplete", "catalog hash or reference validation failed")
}

func workspaceScope(r remoteRecord, s *snapshotCheck) bool {
	return r.Scope == (Scope{"workspace", s.workspace.WorkspaceID})
}

func liveRecord(records map[string]remoteRecord, id, kind string) bool {
	r, ok := records[id]
	return ok && r.Kind == kind && !r.Deleted
}

func strictPayload(r remoteRecord, v any) error { return strictJSON(r.Data, v) }

func installEnvelope(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
	b, err := json.Marshal(r.publishedEnvelope)
	if err != nil {
		return err
	}
	_, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", r.ID, r.Scope.ID, r.Kind, string(b))
	return err
}

func noTaskDependencyErrors(r remoteRecord, records map[string]remoteRecord) bool {
	return len(invalidTaskDependencies(r, records)) == 0
}

var projectKind = recordKind{
	name:  "project",
	table: "projects",
	validate: func(r remoteRecord, s *snapshotCheck) error {
		var p Project
		if err := strictPayload(r, &p); err != nil {
			return err
		}
		if p.ID != r.ID || !validSlug.MatchString(p.Slug) || len(p.Slug) > 63 || !workspaceScope(r, s) {
			return invalidCatalog()
		}
		if _, err := time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
			return invalidCatalog()
		}
		return nil
	},
	uniqueKey: func(r remoteRecord) string { return "slug:" + r.Value.Slug },
	install: func(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
		_, err := c.ExecContext(ctx, "INSERT INTO projects VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET slug=excluded.slug", r.ID, r.Value.Slug, r.Value.CreatedAt)
		return err
	},
	exportIdentity: true,
	export: func(x *snapshotExport) error {
		rows, err := x.conn.QueryContext(x.ctx, "SELECT id,slug,created_at FROM projects ORDER BY id")
		if err != nil {
			return err
		}
		var projects []Project
		for rows.Next() {
			var p Project
			if err = rows.Scan(&p.ID, &p.Slug, &p.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			projects = append(projects, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, p := range projects {
			x.projects[p.ID] = true
			if err = x.add(p.ID, "project", Scope{"workspace", x.conf.WorkspaceID}, p, nil); err != nil {
				return err
			}
		}
		return nil
	},
}

var repositoryKind = recordKind{
	name:  "repository",
	table: "repositories",
	validate: func(r remoteRecord, s *snapshotCheck) error {
		var p struct {
			RemoteAliases []string `json:"remoteAliases"`
		}
		if err := strictPayload(r, &p); err != nil {
			return err
		}
		if !workspaceScope(r, s) {
			return invalidCatalog()
		}
		for _, alias := range r.Value.RemoteAliases {
			n, e := normalizeRemote("https://" + alias)
			if e != nil || n != alias {
				return invalidCatalog()
			}
		}
		return nil
	},
	install: func(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
		if _, err := c.ExecContext(ctx, "INSERT OR IGNORE INTO repositories VALUES(?,?)", r.ID, r.Provenance.UpdatedAt); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "DELETE FROM repository_aliases WHERE repository_id=?", r.ID); err != nil {
			return err
		}
		for _, alias := range r.Value.RemoteAliases {
			if _, err := c.ExecContext(ctx, "INSERT INTO repository_aliases VALUES(?,?)", r.ID, alias); err != nil {
				return err
			}
		}
		return nil
	},
	exportIdentity: true,
	export: func(x *snapshotExport) error {
		ids, err := queryStrings(x.ctx, x.conn, "SELECT id FROM repositories ORDER BY id")
		if err != nil {
			return err
		}
		for _, id := range ids {
			x.repos[id] = true
		}
		for _, id := range ids {
			aliases, err := queryStrings(x.ctx, x.conn, "SELECT remote FROM repository_aliases WHERE repository_id=? ORDER BY remote", id)
			if err != nil {
				return err
			}
			for _, alias := range aliases {
				if normalized, e := normalizeRemote("https://" + alias); e != nil || normalized != alias {
					return Fail("invalid-record", "invalid shared repository alias")
				}
			}
			if err = x.add(id, "repository", Scope{"workspace", x.conf.WorkspaceID}, map[string]any{"remoteAliases": aliases}, nil); err != nil {
				return err
			}
		}
		return nil
	},
}

var membershipKind = recordKind{
	name:  "membership",
	table: "memberships",
	validate: func(r remoteRecord, _ *snapshotCheck) error {
		var p struct {
			RepositoryID string `json:"repositoryId"`
			Subdirectory string `json:"subdirectory"`
		}
		if err := strictPayload(r, &p); err != nil {
			return err
		}
		v := r.Value
		if r.Scope.Kind != "project" || !validUUID(v.RepositoryID) || v.Subdirectory == "" || filepath.IsAbs(v.Subdirectory) || strings.Contains(v.Subdirectory, "..") || strings.Contains(v.Subdirectory, "\\") {
			return invalidCatalog()
		}
		return nil
	},
	references: func(r remoteRecord, records map[string]remoteRecord) bool {
		return liveRecord(records, r.Value.RepositoryID, "repository")
	},
	dependencies: func(r remoteRecord) []string { return []string{r.Value.RepositoryID} },
	install: func(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
		_, err := c.ExecContext(ctx, "INSERT INTO memberships VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET project_id=excluded.project_id,repository_id=excluded.repository_id,subdirectory=excluded.subdirectory", r.ID, r.Scope.ID, r.Value.RepositoryID, r.Value.Subdirectory)
		return err
	},
	exportIdentity: true,
	export: func(x *snapshotExport) error {
		rows, err := x.conn.QueryContext(x.ctx, "SELECT id,project_id,repository_id,subdirectory FROM memberships ORDER BY id")
		if err != nil {
			return err
		}
		type membership struct{ id, pid, rid, sub string }
		var all []membership
		for rows.Next() {
			var m membership
			if err = rows.Scan(&m.id, &m.pid, &m.rid, &m.sub); err != nil {
				rows.Close()
				return err
			}
			all = append(all, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, m := range all {
			if !x.projects[m.pid] || !x.repos[m.rid] || filepath.IsAbs(m.sub) || strings.Contains(m.sub, "..") {
				return Fail("invalid-reference", "membership has an invalid shared dependency")
			}
		}
		for _, m := range all {
			if err = x.add(m.id, "membership", Scope{"project", m.pid}, map[string]any{"repositoryId": m.rid, "subdirectory": m.sub}, nil); err != nil {
				return err
			}
		}
		return nil
	},
}

var contextKind = recordKind{
	name:  "context",
	table: "records",
	validate: func(r remoteRecord, s *snapshotCheck) error {
		var p struct {
			Title      string          `json:"title"`
			Purpose    string          `json:"purpose"`
			Source     string          `json:"source"`
			Summary    *string         `json:"summary"`
			ObservedAt *string         `json:"observedAt"`
			ValidUntil string          `json:"validUntil"`
			Body       *objectManifest `json:"body"`
		}
		if err := strictPayload(r, &p); err != nil {
			return err
		}
		v := r.Value
		if r.Scope.Kind != "project" || strings.TrimSpace(v.Title) == "" || strings.TrimSpace(v.Purpose) == "" || strings.TrimSpace(v.Source) == "" || v.Body == nil {
			return invalidCatalog()
		}
		b, ok := s.files["objects/"+v.Body.SHA256]
		if !ok || len(b) > BodyLimit || v.Body.Type != "text/markdown" || v.Body.Size != len(b) || digest(b) != v.Body.SHA256 || !utf8.Valid(b) {
			return invalidCatalog()
		}
		if v.ValidUntil != "" {
			if _, err := time.Parse(time.RFC3339, v.ValidUntil); err != nil {
				return invalidCatalog()
			}
		}
		return nil
	},
	uniqueKey: func(r remoteRecord) string {
		if r.Value.Purpose == "project-summary" {
			return "summary:" + r.Scope.ID
		}
		return ""
	},
	install: func(ctx context.Context, c *sql.Conn, files map[string][]byte, r remoteRecord) error {
		v := r.Value
		d := ContextDoc{Deleted: r.Deleted, SchemaVersion: 1, ID: r.ID, Kind: r.Kind, WorkspaceID: r.WorkspaceID, Scope: r.Scope, Revision: r.Revision, PreviousRevision: r.PreviousRevision, OperationID: r.OperationID, DeviceID: r.Provenance.DeviceID, UpdatedAt: r.Provenance.UpdatedAt, Title: v.Title, Purpose: v.Purpose, Source: v.Source, Summary: v.Summary, ObservedAt: v.ObservedAt, ValidUntil: v.ValidUntil, Policy: r.Policy, Freshness: "unknown"}
		b, _ := json.Marshal(d)
		if _, err := c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,'context',?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", r.ID, r.Scope.ID, string(b)); err != nil {
			return err
		}
		body := string(files["objects/"+v.Body.SHA256])
		if _, err := c.ExecContext(ctx, "INSERT INTO context_bodies VALUES(?,?) ON CONFLICT(record_id) DO UPDATE SET body=excluded.body", r.ID, body); err != nil {
			return err
		}
		d.Body = &body
		return indexDoc(ctx, c, d)
	},
	export: func(x *snapshotExport) error {
		return x.eachRecord("context", func(id, pid, raw string, body sql.NullString) error {
			var d ContextDoc
			if err := decodeContext(raw, &d); err != nil {
				return err
			}
			if d.SchemaVersion != 1 || !validPolicy(d.Policy) {
				return Fail("invalid-policy", "unsupported record schema or policy blocks export")
			}
			if d.Policy.Storage == "local-only" {
				return nil
			}
			if d.ID != id || d.Kind != "context" || d.Scope.Kind != "project" || d.Scope.ID != pid || !x.projects[pid] || d.WorkspaceID != x.conf.WorkspaceID || !body.Valid || len(raw) > MetadataLimit || len(body.String) > BodyLimit {
				return Fail("invalid-reference", "shared context has invalid metadata, object or scope dependency")
			}
			data := []byte(body.String)
			hash := digest(data)
			x.files["objects/"+hash] = data
			payload := map[string]any{"title": d.Title, "purpose": d.Purpose, "source": d.Source, "summary": d.Summary, "observedAt": d.ObservedAt, "validUntil": d.ValidUntil, "body": map[string]any{"type": "text/markdown", "size": len(data), "sha256": hash}}
			return x.add(id, "context", d.Scope, payload, &d)
		})
	},
}

var configurationKind = recordKind{
	name:  "configuration",
	table: "configurations",
	decode: func(r *remoteRecord, payload json.RawMessage, _ map[string]json.RawMessage) error {
		config, err := decodeConfiguration(payload, false)
		r.Config = config
		return err
	},
	detailedDecodeErrors: true,
	validate: func(r remoteRecord, s *snapshotCheck) error {
		if !validConfigurationScope(r.ID, s.workspace.WorkspaceID, r.Scope, r.Config.Profile) || r.Policy.AgentDisclosure != "full" {
			return invalidCatalog()
		}
		return nil
	},
	references: noTaskDependencyErrors,
	dependencies: func(r remoteRecord) []string {
		var ids []string
		for _, m := range r.Config.SecretMappings {
			ids = append(ids, m.TaskID, m.BindingID)
		}
		for _, m := range r.Config.ResourceMappings {
			ids = append(ids, m.TaskID, m.ResourceID)
		}
		return ids
	},
	install: func(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
		if r.Deleted {
			_, err := c.ExecContext(ctx, "DELETE FROM configurations WHERE id=?", r.ID)
			return err
		}
		return saveConfiguration(ctx, c, r.ID, r.Scope, r.Config)
	},
	exportIdentity: true,
	export: func(x *snapshotExport) error {
		configs, err := exportConfigurations(x.ctx, x.conn, x.conf.WorkspaceID, x.projects)
		if err != nil {
			return err
		}
		for _, cfg := range configs {
			if err = x.add(cfg.id, "configuration", cfg.scope, cfg.value, nil); err != nil {
				return err
			}
		}
		return nil
	},
}

// Resource, Task and SecretBinding records are stored as published envelopes,
// optionally local-only, in the generic records table.
func exportEnvelopeKind(kind, disclosure string, validate func(remoteRecord) error) func(x *snapshotExport) error {
	sharedOnly := kind == secretBindingKind
	return func(x *snapshotExport) error {
		return x.eachRecord(kind, func(id, pid, raw string, body sql.NullString) error {
			r, err := decodeRecord([]byte(raw))
			if err != nil {
				return err
			}
			if r.Kind != kind || r.Scope.Kind != "project" || !validPolicy(r.Policy) || r.Policy.AgentDisclosure != disclosure ||
				sharedOnly && r.Policy.Storage != "shared" {
				return Fail("invalid-record", "invalid "+kind+" record scope or policy")
			}
			if err = validate(r); err != nil {
				return err
			}
			if r.Policy.Storage == "local-only" {
				return nil
			}
			if !validUUID(r.ID) || r.ID != id || r.Scope.ID != pid || !x.projects[pid] || r.WorkspaceID != x.conf.WorkspaceID || body.Valid {
				return Fail("invalid-reference", "shared "+kind+" has an invalid Project dependency")
			}
			if _, exists := x.files[recordPath(id)]; exists {
				return Fail("record-collision", "shared "+kind+" ID collides with another record")
			}
			if err = x.addJSON(recordPath(id), r.publishedEnvelope); err != nil {
				return err
			}
			x.ops = append(x.ops, r.OperationID)
			return nil
		})
	}
}

var resourceKind = recordKind{
	name:  "resource",
	table: "records",
	decode: func(r *remoteRecord, _ json.RawMessage, _ map[string]json.RawMessage) error {
		return strictJSON(r.Data, &r.Resource)
	},
	validate: func(r remoteRecord, _ *snapshotCheck) error {
		if r.Scope.Kind != "project" || r.Policy.AgentDisclosure != "full" || validateResourceSpec(r.Resource, true) != nil {
			return invalidCatalog()
		}
		return nil
	},
	install: installEnvelope,
	export: exportEnvelopeKind("resource", "full", func(r remoteRecord) error {
		return validateResourceSpec(r.Resource, r.Policy.Storage == "shared")
	}),
}

var taskKind = recordKind{
	name:  "task",
	table: "records",
	decode: func(r *remoteRecord, payload json.RawMessage, _ map[string]json.RawMessage) error {
		if _, err := configurationFields(payload); err != nil {
			return err
		}
		return strictJSON(payload, &r.Task)
	},
	detailedDecodeErrors: true,
	validate: func(r remoteRecord, _ *snapshotCheck) error {
		if r.Scope.Kind != "project" || r.Policy.AgentDisclosure != "full" || validateTask(r.Task) != nil {
			return invalidCatalog()
		}
		return nil
	},
	references:   noTaskDependencyErrors,
	dependencies: func(r remoteRecord) []string { return []string{r.Task.MembershipID} },
	install:      installEnvelope,
	export:       exportEnvelopeKind("task", "full", func(r remoteRecord) error { return validateTask(r.Task) }),
}

var secretBindingRecordKind = recordKind{
	name:  secretBindingKind,
	table: "records",
	decode: func(r *remoteRecord, payload json.RawMessage, fields map[string]json.RawMessage) error {
		for field, keys := range map[string][]string{
			"scope":      {"kind", "id"},
			"provenance": {"deviceId", "updatedAt"},
			"policy":     {"storage", "agentDisclosure"},
		} {
			if err := exactSecretFields(fields[field], keys...); err != nil {
				return err
			}
		}
		return decodeSecretBinding(payload, &r.SecretBinding)
	},
	validate: func(r remoteRecord, _ *snapshotCheck) error {
		if r.Scope.Kind != "project" || r.Policy != (Policy{"shared", "reference"}) || validateSecretBinding(r.SecretBinding) != nil {
			return invalidCatalog()
		}
		return nil
	},
	references: noTaskDependencyErrors,
	dependencies: func(r remoteRecord) []string {
		if r.SecretBinding.ResourceID == "" {
			return nil
		}
		return []string{r.SecretBinding.ResourceID}
	},
	install: installEnvelope,
	export: exportEnvelopeKind(secretBindingKind, "reference", func(r remoteRecord) error {
		return validateSecretBinding(r.SecretBinding)
	}),
}

var artifactRecordKind = recordKind{
	name:  artifactKind,
	table: "records",
	decode: func(r *remoteRecord, payload json.RawMessage, _ map[string]json.RawMessage) error {
		return strictJSON(payload, &r.Artifact)
	},
	validate: func(r remoteRecord, s *snapshotCheck) error {
		p := r.Artifact
		if r.Scope.Kind != "project" || r.Policy != (Policy{"shared", "reference"}) || !validArtifactPublication(p) {
			return invalidCatalog()
		}
		if prior, ok := s.runs[p.Run.ID]; ok && (prior.Scope != r.Scope || prior.Artifact.Run != p.Run) {
			return invalidCatalog()
		}
		s.runs[p.Run.ID] = r
		if p.Omitted == "" {
			b, ok := s.files[p.objectPath()]
			if !ok || int64(len(b)) != p.Size || excludedArtifactBytes(b) {
				return invalidCatalog()
			}
			s.artifactObjects[p.objectPath()] = true
		}
		return nil
	},
	install: func(ctx context.Context, c *sql.Conn, _ map[string][]byte, r remoteRecord) error {
		return installArtifactPublication(ctx, c, r)
	},
	exportIdentity: true,
	export: func(x *snapshotExport) error {
		return x.eachRecord(artifactKind, func(id, pid, raw string, body sql.NullString) error {
			r, err := decodeRecord([]byte(raw))
			if err != nil {
				return err
			}
			if !validUUID(id) || r.ID != id || r.Kind != artifactKind || r.Scope != (Scope{"project", pid}) ||
				!x.projects[pid] || r.WorkspaceID != x.conf.WorkspaceID || r.Policy != (Policy{"shared", "reference"}) ||
				!validArtifactPublication(r.Artifact) || body.Valid {
				return Fail("invalid-reference", "shared artifact has invalid metadata or Project dependency")
			}
			if _, exists := x.files[recordPath(id)]; exists {
				return Fail("record-collision", "shared artifact ID collides with another record")
			}
			x.files[recordPath(id)] = []byte(raw)
			x.ops = append(x.ops, r.OperationID)
			return nil
		})
	},
}

func installSkillKind(ctx context.Context, c *sql.Conn, files map[string][]byte, r remoteRecord) error {
	for _, path := range r.skillObjects() {
		if _, err := c.ExecContext(ctx, "INSERT OR IGNORE INTO sync_objects VALUES(?,COALESCE(?,X''))", path, files[path]); err != nil {
			return err
		}
	}
	return installSkillRecord(ctx, c, r)
}

func validateSkillKind(r remoteRecord, s *snapshotCheck) error {
	if err := r.validateSkillRecord(s.workspace.WorkspaceID); err != nil {
		return err
	}
	if r.Kind != skillPinKind || r.SkillPin.SourceKind != "private" {
		return nil
	}
	_, err := privateSkillData(r.SkillPin, func(path string) ([]byte, error) {
		if data, ok := s.files[path]; ok {
			return data, nil
		}
		return nil, Fail("incomplete", "private Skill object is missing")
	})
	return err
}

var skillPinRecordKind = recordKind{
	name:  skillPinKind,
	table: "skill_records",
	decode: func(r *remoteRecord, payload json.RawMessage, _ map[string]json.RawMessage) error {
		return strictJSON(payload, &r.SkillPin)
	},
	validate: validateSkillKind,
	install:  installSkillKind,
	export:   func(x *snapshotExport) error { return x.exportSkillRecords(skillPinKind) },
}

var skillGroupRecordKind = recordKind{
	name:  skillGroupKind,
	table: "skill_records",
	decode: func(r *remoteRecord, payload json.RawMessage, _ map[string]json.RawMessage) error {
		return strictJSON(payload, &r.SkillGroup)
	},
	validate: validateSkillKind,
	references: func(r remoteRecord, records map[string]remoteRecord) bool {
		for _, id := range skillGroupDependencies(r) {
			if !liveRecord(records, id, skillPinKind) {
				return false
			}
		}
		return true
	},
	dependencies: skillGroupDependencies,
	install:      installSkillKind,
	export:       func(x *snapshotExport) error { return x.exportSkillRecords(skillGroupKind) },
}

func queryStrings(ctx context.Context, c *sql.Conn, query string, args ...any) ([]string, error) {
	rows, err := c.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
