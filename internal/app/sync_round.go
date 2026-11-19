package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type workspaceFormat struct {
	Format      int    `json:"format"`
	WorkspaceID string `json:"workspaceId"`
	Epoch       string `json:"epoch"`
	StorageMode string `json:"storageMode"`
}
type recordPayload struct {
	Project
	RemoteAliases []string        `json:"remoteAliases,omitempty"`
	RepositoryID  string          `json:"repositoryId,omitempty"`
	Subdirectory  string          `json:"subdirectory,omitempty"`
	Title         string          `json:"title,omitempty"`
	Purpose       string          `json:"purpose,omitempty"`
	Source        string          `json:"source,omitempty"`
	Summary       *string         `json:"summary,omitempty"`
	ObservedAt    *string         `json:"observedAt,omitempty"`
	ValidUntil    string          `json:"validUntil,omitempty"`
	Body          *objectManifest `json:"body,omitempty"`
}
type objectManifest struct {
	Type   string `json:"type"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256"`
}
type remoteRecord struct {
	publishedEnvelope
	Data          json.RawMessage   `json:"-"`
	Value         recordPayload     `json:"-"`
	Config        configuration     `json:"-"`
	Resource      ResourceSpec      `json:"-"`
	Task          TaskSpec          `json:"-"`
	SecretBinding SecretBindingSpec `json:"-"`
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		if strings.HasPrefix(err.Error(), "json: unknown field ") {
			return Fail("unsupported-schema", "unknown catalog field")
		}
		return Fail("incomplete", "malformed or partial catalog JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return Fail("incomplete", "malformed or partial catalog JSON")
	}
	return nil
}
func recordPath(id string) string {
	return "records/" + strings.ReplaceAll(id, "-", "")[:2] + "/" + id + ".json"
}
func validUUID(id string) bool { u, e := uuid.Parse(id); return e == nil && u.String() == id }
func decodeRecord(b []byte) (remoteRecord, error) {
	var r remoteRecord
	wire := struct {
		*publishedEnvelope
		Payload json.RawMessage `json:"payload"`
	}{publishedEnvelope: &r.publishedEnvelope}
	if err := strictJSON(b, &wire); err != nil {
		return r, err
	}
	if r.SchemaVersion != 1 {
		return r, Fail("unsupported-schema", "unsupported record schema")
	}
	if wire.Payload != nil {
		if err := json.Unmarshal(wire.Payload, &r.Payload); err != nil {
			return r, err
		}
	}
	if r.Kind == "configuration" {
		if _, err := configurationFields(b); err != nil {
			return r, err
		}
		config, err := decodeConfiguration(wire.Payload, false)
		if err != nil {
			return r, err
		}
		r.Config = config
	}
	raw, err := json.Marshal(r.Payload)
	if err != nil {
		return r, err
	}
	r.Data = raw
	switch r.Kind {
	case "configuration":
		return r, nil
	case "resource":
		err = strictJSON(raw, &r.Resource)
	case secretBindingKind:
		err = decodeSecretBinding(wire.Payload, &r.SecretBinding)
	case "task":
		if _, err = configurationFields(wire.Payload); err == nil {
			err = strictJSON(wire.Payload, &r.Task)
		}
	default:
		err = strictJSON(raw, &r.Value)
	}
	return r, err
}
func validateSnapshot(files map[string][]byte, wid, epoch string) (workspaceFormat, map[string]remoteRecord, error) {
	var w workspaceFormat
	records := map[string]remoteRecord{}
	invalid := func() (workspaceFormat, map[string]remoteRecord, error) {
		return w, nil, Fail("incomplete", "catalog hash or reference validation failed")
	}
	if err := strictJSON(files["workspace.json"], &w); err != nil {
		return w, nil, err
	}
	if w.Format != 1 {
		return w, nil, Fail("unsupported-schema", "unsupported workspace format")
	}
	if !validUUID(w.WorkspaceID) || !validUUID(w.Epoch) || w.StorageMode != "plaintext" || wid != "" && w.WorkspaceID != wid || epoch != "" && w.Epoch != epoch {
		return invalid()
	}
	if len(files["README.md"]) == 0 {
		return invalid()
	}
	slugs := map[string]bool{}
	summaries := map[string]bool{}
	for path, b := range files {
		if path == "workspace.json" || path == "README.md" {
			continue
		}
		if strings.HasPrefix(path, "objects/") {
			if len(path) != 72 || digest(b) != strings.TrimPrefix(path, "objects/") || len(b) > BodyLimit {
				return invalid()
			}
			continue
		}
		if !strings.HasPrefix(path, "records/") || len(b) > MetadataLimit {
			return invalid()
		}
		r, err := decodeRecord(b)
		if err != nil {
			if (r.Kind == "configuration" || r.Kind == "task") && Classify(err).Code != "unsupported-schema" {
				return w, nil, Fail("incomplete", "invalid "+r.Kind+" record: "+Classify(err).Message)
			}
			return w, nil, err
		}
		if r.SchemaVersion != 1 {
			return w, nil, Fail("unsupported-schema", "unsupported record schema")
		}
		if !validUUID(r.ID) || path != recordPath(r.ID) || r.WorkspaceID != w.WorkspaceID || !validUUID(r.Revision) || !validUUID(r.OperationID) || !validUUID(r.Provenance.DeviceID) || r.PreviousRevision != "" && !validUUID(r.PreviousRevision) || r.Policy.Storage != "shared" || !validPolicy(r.Policy) {
			return invalid()
		}
		if _, err = time.Parse(time.RFC3339Nano, r.Provenance.UpdatedAt); err != nil {
			return invalid()
		}
		v := r.Value
		switch r.Kind {
		case "project":
			var p Project
			if err := strictJSON(r.Data, &p); err != nil {
				return w, nil, err
			}
			if p.ID != r.ID || !validSlug.MatchString(p.Slug) || len(p.Slug) > 63 || (!r.Deleted && slugs[p.Slug]) || r.Scope != (Scope{"workspace", w.WorkspaceID}) {
				return invalid()
			}
			if !r.Deleted {
				slugs[p.Slug] = true
			}
			if _, err = time.Parse(time.RFC3339Nano, p.CreatedAt); err != nil {
				return invalid()
			}
		case "repository":
			var p struct {
				RemoteAliases []string `json:"remoteAliases"`
			}
			if err := strictJSON(r.Data, &p); err != nil {
				return w, nil, err
			}
			if r.Scope != (Scope{"workspace", w.WorkspaceID}) {
				return invalid()
			}
			for _, alias := range v.RemoteAliases {
				n, e := normalizeRemote("https://" + alias)
				if e != nil || n != alias {
					return invalid()
				}
			}
		case "membership":
			var p struct {
				RepositoryID string `json:"repositoryId"`
				Subdirectory string `json:"subdirectory"`
			}
			if err := strictJSON(r.Data, &p); err != nil {
				return w, nil, err
			}
			if r.Scope.Kind != "project" || !validUUID(v.RepositoryID) || v.Subdirectory == "" || filepath.IsAbs(v.Subdirectory) || strings.Contains(v.Subdirectory, "..") || strings.Contains(v.Subdirectory, "\\") {
				return invalid()
			}
		case "configuration":
			if !validConfigurationScope(r.ID, w.WorkspaceID, r.Scope, r.Config.Profile) || r.Policy.AgentDisclosure != "full" {
				return invalid()
			}
		case "context":
			var p struct {
				Title      string          `json:"title"`
				Purpose    string          `json:"purpose"`
				Source     string          `json:"source"`
				Summary    *string         `json:"summary"`
				ObservedAt *string         `json:"observedAt"`
				ValidUntil string          `json:"validUntil"`
				Body       *objectManifest `json:"body"`
			}
			if err := strictJSON(r.Data, &p); err != nil {
				return w, nil, err
			}
			if r.Scope.Kind != "project" || strings.TrimSpace(v.Title) == "" || strings.TrimSpace(v.Purpose) == "" || strings.TrimSpace(v.Source) == "" || v.Body == nil {
				return invalid()
			}
			b, ok := files["objects/"+v.Body.SHA256]
			if !ok || v.Body.Type != "text/markdown" || v.Body.Size != len(b) || digest(b) != v.Body.SHA256 || !utf8.Valid(b) {
				return invalid()
			}
			if v.ValidUntil != "" {
				if _, err = time.Parse(time.RFC3339, v.ValidUntil); err != nil {
					return invalid()
				}
			}
			if !r.Deleted && v.Purpose == "project-summary" {
				if summaries[r.Scope.ID] {
					return invalid()
				}
				summaries[r.Scope.ID] = true
			}
		case secretBindingKind:
			if r.Scope.Kind != "project" || r.Policy != (Policy{"shared", "reference"}) || validateSecretBinding(r.SecretBinding) != nil {
				return invalid()
			}
		case "task":
			if r.Scope.Kind != "project" || r.Policy.AgentDisclosure != "full" || validateTask(r.Task) != nil {
				return invalid()
			}
		case "resource":
			if r.Scope.Kind != "project" || r.Policy.AgentDisclosure != "full" || validateResourceSpec(r.Resource, true) != nil {
				return invalid()
			}
		default:
			return w, nil, Fail("unsupported-schema", "unsupported catalog record kind")
		}
		records[r.ID] = r
	}
	for _, r := range records {
		if r.Deleted {
			continue
		}
		if r.Scope.Kind == "project" {
			if p, ok := records[r.Scope.ID]; !ok || p.Kind != "project" || p.Deleted {
				return invalid()
			}
		}
		if len(invalidTaskDependencies(r, records)) != 0 {
			return invalid()
		}
		if r.Kind == "membership" {
			if p, ok := records[r.Value.RepositoryID]; !ok || p.Kind != "repository" || p.Deleted {
				return invalid()
			}
		}
	}
	return w, records, nil
}
func readSnapshot(ctx context.Context, dir, head string) (map[string][]byte, error) {
	listing, err := syncGitRaw(ctx, dir, nil, "ls-tree", "-r", "-z", head)
	if err != nil {
		return nil, err
	}
	if listing == "" || !strings.HasSuffix(listing, "\x00") {
		return nil, Fail("incomplete", "partial Git tree listing")
	}
	files := map[string][]byte{}
	total := 0
	for _, line := range strings.Split(listing, "\x00") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			return nil, Fail("incomplete", "invalid Git tree")
		}
		meta := strings.Fields(parts[0])
		if len(meta) != 3 || meta[0] != "100644" || meta[1] != "blob" {
			return nil, Fail("incomplete", "catalog requires regular files")
		}
		size, err := syncGit(ctx, dir, nil, "cat-file", "-s", meta[2])
		if err != nil {
			return nil, err
		}
		var n int
		if json.Unmarshal([]byte(size), &n) != nil || n < 0 || n > BodyLimit {
			return nil, Fail("size-limit", "catalog file exceeds limits")
		}
		total += n
		if total > 64*BodyLimit {
			return nil, Fail("size-limit", "catalog exceeds staging limit")
		}
		data, err := syncGitRaw(ctx, dir, nil, "cat-file", "blob", meta[2])
		if err != nil {
			return nil, err
		}
		if len(data) != n {
			return nil, Fail("incomplete", "partial Git blob")
		}
		files[parts[1]] = []byte(data)
	}
	return files, nil
}
func baselineFiles(ctx context.Context, c *sql.Conn) (map[string][]byte, error) {
	files := map[string][]byte{}
	rows, err := c.QueryContext(ctx, "SELECT path,data FROM sync_baselines")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var b []byte
		if err = rows.Scan(&p, &b); err != nil {
			return nil, err
		}
		files[p] = b
	}
	return files, rows.Err()
}

// Queue a complete desired record against its original published base in the same local transaction.
func queueChanges(ctx context.Context, c *sql.Conn) error {
	var conf SyncConfig
	err := c.QueryRowContext(ctx, "SELECT remote,workspace_id,branch,identity FROM sync_config WHERE singleton=1").Scan(&conf.Remote, &conf.WorkspaceID, &conf.Branch, &conf.Identity)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	var pending, epoch, head string
	if err = c.QueryRowContext(ctx, "SELECT pending,epoch,confirmed_head FROM sync_state").Scan(&pending, &epoch, &head); err != nil {
		return err
	}
	if pending != "" {
		return Fail("push-pending", "reconcile the pending push before editing shared catalog")
	}
	base, err := baselineFiles(ctx, c)
	if err != nil {
		return err
	}
	if head != "" && len(base) == 0 {
		return Fail("sync-required", "run sync once to upgrade the published baseline before editing")
	}
	files, _, err := exportConn(ctx, c, conf)
	if err != nil {
		return err
	}
	// Withdraw unpublished records that the user reclassified as local-only.
	pendingRows, err := c.QueryContext(ctx, "SELECT id FROM sync_outbox WHERE base IS NULL OR length(base)=0")
	if err != nil {
		return err
	}
	withdrawn := []string{}
	for pendingRows.Next() {
		var id string
		if err = pendingRows.Scan(&id); err != nil {
			pendingRows.Close()
			return err
		}
		if files[recordPath(id)] == nil {
			withdrawn = append(withdrawn, id)
		}
	}
	err = pendingRows.Err()
	pendingRows.Close()
	if err != nil {
		return err
	}
	for _, id := range withdrawn {
		if _, err = c.ExecContext(ctx, "DELETE FROM sync_outbox WHERE id=?", id); err != nil {
			return err
		}
	}
	for path, old := range base {
		if strings.HasPrefix(path, "records/") && files[path] == nil && len(old) > 0 {
			return Fail("published-policy-change", "published shared records require explicit deletion before changing storage")
		}
	}
	for path, b := range files {
		if !strings.HasPrefix(path, "records/") {
			continue
		}
		r, err := decodeRecord(b)
		if err != nil {
			return err
		}
		var active int
		if err = c.QueryRowContext(ctx, "SELECT count(*) FROM sync_conflicts WHERE id=? AND active=1", r.ID).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			continue
		}
		old := base[path]
		if len(old) > 0 {
			prior, err := decodeRecord(old)
			if err != nil {
				return err
			}
			if prior.Revision == r.Revision {
				if _, err = c.ExecContext(ctx, "DELETE FROM sync_outbox WHERE id=?", r.ID); err != nil {
					return err
				}
				continue
			}
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO sync_outbox(id,base,proposed,operation_id,epoch) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET proposed=excluded.proposed,operation_id=excluded.operation_id", r.ID, old, b, r.OperationID, epoch); err != nil {
			return err
		}
	}
	return nil
}
func saveBaseline(ctx context.Context, c *sql.Conn, files map[string][]byte, head string, clear bool) error {
	if _, err := c.ExecContext(ctx, "DELETE FROM sync_baselines"); err != nil {
		return err
	}
	for path, b := range files {
		if _, err := c.ExecContext(ctx, "INSERT INTO sync_baselines VALUES(?,?)", path, b); err != nil {
			return err
		}
	}
	if clear {
		if _, err := c.ExecContext(ctx, "DELETE FROM sync_outbox"); err != nil {
			return err
		}
	}
	_, err := c.ExecContext(ctx, "UPDATE sync_state SET confirmed_head=?,pending='',last_sync_at=?,outcome='synced' WHERE singleton=1", head, now())
	return err
}
func installSnapshot(ctx context.Context, c *sql.Conn, files map[string][]byte, records map[string]remoteRecord) error {
	var err error
	records, err = installableRecords(ctx, c, records)
	if err != nil {
		return err
	}
	for _, kind := range []string{"project", "repository", "membership", "context", "configuration", "resource", "task", secretBindingKind} {
		for id, r := range records {
			if r.Kind != kind {
				continue
			}
			v := r.Value
			if r.Deleted {
				b, _ := json.Marshal(r.publishedEnvelope)
				if _, err := c.ExecContext(ctx, "INSERT INTO sync_tombstones VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", id, b); err != nil {
					return err
				}
			} else {
				if _, err := c.ExecContext(ctx, "DELETE FROM sync_tombstones WHERE id=?", id); err != nil {
					return err
				}
			}
			var err error
			switch kind {
			case "project":
				_, err = c.ExecContext(ctx, "INSERT INTO projects VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET slug=excluded.slug", id, v.Slug, v.CreatedAt)
			case "repository":
				_, err = c.ExecContext(ctx, "INSERT OR IGNORE INTO repositories VALUES(?,?)", id, r.Provenance.UpdatedAt)
				if err != nil {
					return err
				}
				if _, err = c.ExecContext(ctx, "DELETE FROM repository_aliases WHERE repository_id=?", id); err != nil {
					return err
				}
				for _, alias := range v.RemoteAliases {
					if _, err = c.ExecContext(ctx, "INSERT INTO repository_aliases VALUES(?,?)", id, alias); err != nil {
						return err
					}
				}
			case "membership":
				_, err = c.ExecContext(ctx, "INSERT INTO memberships VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET project_id=excluded.project_id,repository_id=excluded.repository_id,subdirectory=excluded.subdirectory", id, r.Scope.ID, v.RepositoryID, v.Subdirectory)
			case "configuration":
				if r.Deleted {
					_, err = c.ExecContext(ctx, "DELETE FROM configurations WHERE id=?", id)
				} else {
					err = saveConfiguration(ctx, c, id, r.Scope, r.Config)
				}
			case "resource", "task", secretBindingKind:
				var b []byte
				b, err = json.Marshal(r.publishedEnvelope)
				if err != nil {
					return err
				}
				_, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", id, r.Scope.ID, kind, string(b))
			case "context":
				d := ContextDoc{Deleted: r.Deleted, SchemaVersion: 1, ID: id, Kind: kind, WorkspaceID: r.WorkspaceID, Scope: r.Scope, Revision: r.Revision, PreviousRevision: r.PreviousRevision, OperationID: r.OperationID, DeviceID: r.Provenance.DeviceID, UpdatedAt: r.Provenance.UpdatedAt, Title: v.Title, Purpose: v.Purpose, Source: v.Source, Summary: v.Summary, ObservedAt: v.ObservedAt, ValidUntil: v.ValidUntil, Policy: r.Policy, Freshness: "unknown"}
				b, _ := json.Marshal(d)
				_, err = c.ExecContext(ctx, "INSERT INTO records VALUES(?,?,'context',?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata", id, r.Scope.ID, string(b))
				if err != nil {
					return err
				}
				body := string(files["objects/"+v.Body.SHA256])
				if _, err = c.ExecContext(ctx, "INSERT INTO context_bodies VALUES(?,?) ON CONFLICT(record_id) DO UPDATE SET body=excluded.body", id, body); err != nil {
					return err
				}
				d.Body = &body
				if err = indexDoc(ctx, c, d); err != nil {
					return err
				}
			}
			if err != nil {
				return err
			}
			if kind != "context" && kind != "resource" && kind != "task" && kind != secretBindingKind {
				b, _ := json.Marshal(r.Payload)
				if _, err = c.ExecContext(ctx, "INSERT INTO export_identities VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,operation_id=excluded.operation_id,fingerprint=excluded.fingerprint,previous_revision=excluded.previous_revision", id, r.Revision, r.OperationID, digest(b), r.PreviousRevision); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func indexDoc(ctx context.Context, c *sql.Conn, d ContextDoc) error {
	if _, err := c.ExecContext(ctx, "DELETE FROM context_fts WHERE id=?", d.ID); err != nil {
		return err
	}
	if d.Deleted {
		return nil
	}
	var subscribed int
	err := c.QueryRowContext(ctx, "SELECT subscribed FROM project_subscriptions WHERE project_id=?", d.Scope.ID).Scan(&subscribed)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && subscribed == 0 {
		return nil
	}
	visible, err := projectDoc(d, true)
	if err != nil {
		return err
	}
	content := visible.Title + " " + visible.Purpose + " " + visible.Source
	if visible.Summary != nil {
		content += " " + *visible.Summary
	}
	if visible.Body != nil {
		content += " " + *visible.Body
	}
	_, err = c.ExecContext(ctx, "INSERT INTO context_fts VALUES(?,?,?)", d.ID, d.Scope.ID, content)
	return err
}
func (a App) Join(ctx context.Context, remote, branch string, subscriptions []string, all bool) (SyncResult, error) {
	r := SyncResult{Boundary: plaintextBoundary, State: "pending"}
	remote, _, err := syncRemote(remote)
	if err != nil {
		return r, err
	}
	if branch == "" || strings.HasPrefix(branch, "-") {
		return r, Fail("usage", "invalid sync branch")
	}
	db, err := store.Open(ctx, a.DataDir, true)
	if err != nil {
		return r, err
	}
	defer db.Close()
	release, err := a.syncOwner(ctx)
	if err != nil {
		return r, err
	}
	defer release()
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM workspace").Scan(&count); err != nil {
		return r, err
	}
	if count != 0 {
		return r, Fail("already-initialized", "join requires a fresh application data directory")
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return r, err
	}
	if _, err = syncGit(ctx, dir, nil, "check-ref-format", "refs/heads/"+branch); err != nil {
		return r, Fail("usage", "invalid sync branch")
	}
	conf := SyncConfig{Remote: remote, Branch: branch}
	check := checkSync(ctx, dir, conf)
	r.Check = &check
	if !check.GitReachable || check.Privacy != "private" {
		return r, Fail("privacy-unconfirmed", "join requires reachable Git and confirmed private repository identity")
	}
	conf.Identity = check.Identity
	head, files, err := newSyncBackend().Fetch(ctx, dir, conf)
	if err != nil {
		return r, err
	}
	w, records, err := validateSnapshot(files, "", "")
	if err != nil {
		return r, err
	}
	conf.WorkspaceID = w.WorkspaceID
	selected := map[string]bool{}
	for _, selector := range subscriptions {
		found := false
		for id, r := range records {
			if r.Kind == "project" && (id == selector || r.Value.Slug == selector) {
				selected[id] = true
				found = true
			}
		}
		if !found {
			return r, Fail("project-not-found", "subscription project was not found")
		}
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		var n int
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM workspace").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return Fail("already-initialized", "workspace already initialized")
		}
		stamp := now()
		name, _ := os.Hostname()
		if _, err := c.ExecContext(ctx, "INSERT INTO workspace VALUES(1,?,?)", w.WorkspaceID, stamp); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "INSERT INTO device VALUES(1,?,?,?,?,?)", uuid.NewString(), name, runtime.GOOS, runtime.GOARCH, stamp); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "INSERT INTO sync_state(singleton,epoch) VALUES(1,?)", w.Epoch); err != nil {
			return err
		}
		if _, err := c.ExecContext(ctx, "INSERT INTO sync_config VALUES(1,?,?,?,?)", remote, w.WorkspaceID, branch, conf.Identity); err != nil {
			return err
		}
		if err := installSnapshot(ctx, c, files, records); err != nil {
			return err
		}
		for id, r := range records {
			if r.Kind == "project" {
				if _, err := c.ExecContext(ctx, "INSERT INTO project_subscriptions VALUES(?,?)", id, all || selected[id]); err != nil {
					return err
				}
			}
		}
		if err := reindex(ctx, c); err != nil {
			return err
		}
		return saveBaseline(ctx, c, files, head, true)
	})
	r.Config = &conf
	r.Head = head
	r.State = "synced"
	return r, err
}
func fetchSnapshot(ctx context.Context, dir string, c SyncConfig) (string, map[string][]byte, error) {
	_, remote := syncEndpoints(c.Remote)
	if _, err := syncGit(ctx, dir, nil, "fetch", "--no-tags", "--no-recurse-submodules", "--", remote, "refs/heads/"+c.Branch); err != nil {
		return "", nil, err
	}
	head, err := syncGit(ctx, dir, nil, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", nil, err
	}
	files, err := readSnapshot(ctx, dir, head)
	return head, files, err
}
func reindex(ctx context.Context, c *sql.Conn) error {
	rows, err := c.QueryContext(ctx, "SELECT r.metadata,b.body FROM records r JOIN context_bodies b ON b.record_id=r.id")
	if err != nil {
		return err
	}
	docs := []ContextDoc{}
	for rows.Next() {
		var raw, body string
		if err = rows.Scan(&raw, &body); err != nil {
			rows.Close()
			return err
		}
		var d ContextDoc
		if err = decodeContext(raw, &d); err != nil {
			rows.Close()
			return err
		}
		d.Body = &body
		docs = append(docs, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range docs {
		if err = indexDoc(ctx, c, d); err != nil {
			return err
		}
	}
	return nil
}

func (a App) syncRound(ctx context.Context) (result SyncResult, err error) {
	result = SyncResult{Boundary: plaintextBoundary, State: "pending"}
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
	defer func() {
		if err != nil {
			persistSyncFailure(db, err)
		}
	}()
	conf, err := loadSync(ctx, db)
	if err != nil {
		return result, err
	}
	result.Config = &conf
	var epoch, confirmed, pending string
	if err = db.QueryRowContext(ctx, "SELECT epoch,confirmed_head,pending FROM sync_state").Scan(&epoch, &confirmed, &pending); err != nil {
		return result, err
	}

	dir, err := a.managedGit(ctx)
	if err != nil {
		return result, err
	}
	check := privacyCheck(ctx, conf)
	result.Check = &check
	if check.Privacy != "private" {
		code := check.Failure
		if code == "" {
			code = "privacy-unconfirmed"
		}
		return result, Fail(code, "authenticated private repository identity could not be confirmed")
	}
	head, remote, err := newSyncBackend().Fetch(ctx, dir, conf)
	if err != nil {
		return result, err
	}
	_, records, err := validateSnapshot(remote, conf.WorkspaceID, epoch)
	if err != nil {
		return result, err
	}
	if confirmed == "" {
		return result, Fail("initialization-required", "use explicit sync init for first publication")
	}
	extends, err := newSyncBackend().Ancestor(ctx, dir, confirmed, head)
	if err != nil {
		return result, err
	}
	if !extends {
		return result, Fail("history-rewritten", "remote head no longer extends confirmed history")
	}
	if pending != "" {
		var attempt pendingPush
		if json.Unmarshal([]byte(pending), &attempt) != nil || attempt.Epoch != epoch || attempt.ExpectedHead == "" {
			return result, Fail("push-pending", "pending initialization requires its explicit preview retry")
		}
		accepted, e := newSyncBackend().Ancestor(ctx, dir, attempt.Candidate, head)
		if e != nil {
			return result, e
		}
		if accepted {
			candidateFiles, e := readSnapshot(ctx, dir, attempt.Candidate)
			if e != nil {
				return result, e
			}
			if _, _, e = validateSnapshot(candidateFiles, conf.WorkspaceID, epoch); e != nil {
				return result, e
			}
			err = store.Write(ctx, db, func(c *sql.Conn) error {
				if e := saveBaseline(ctx, c, candidateFiles, attempt.Candidate, false); e != nil {
					return e
				}
				for _, id := range attempt.OperationIDs {
					if _, e := c.ExecContext(ctx, "DELETE FROM sync_outbox WHERE operation_id=?", id); e != nil {
						return e
					}
				}
				return nil
			})
			if err != nil {
				return result, err
			}
		} else {
			// The remote still extends confirmed history and does not contain C.
			// Keep original operations and compare them again against fetched business baselines.
			if _, err = db.ExecContext(ctx, "UPDATE sync_state SET pending=''"); err != nil {
				return result, err
			}
		}
	}
	// Bootstrap existing schema-4 publishers from their actual confirmed Git snapshot.
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		base, e := baselineFiles(ctx, c)
		if e != nil {
			return e
		}
		if len(base) == 0 {
			files, e := readSnapshot(ctx, dir, confirmed)
			if e != nil {
				return e
			}
			if _, _, e = validateSnapshot(files, conf.WorkspaceID, epoch); e != nil {
				return e
			}
			if e = saveBaseline(ctx, c, files, confirmed, false); e != nil {
				return e
			}
		}
		return queueChanges(ctx, c)
	})
	if err != nil {
		return result, err
	}
	ops := []operation{}
	rows, err := db.QueryContext(ctx, "SELECT id,base,proposed,operation_id FROM sync_outbox ORDER BY id")
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var o operation
		if err = rows.Scan(&o.id, &o.base, &o.proposed, &o.op); err != nil {
			rows.Close()
			return result, err
		}
		ops = append(ops, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	local, _, err := exportSnapshot(ctx, db, conf)
	if err != nil {
		return result, err
	}
	merged, selected, conflicts, blocked, err := planOperations(remote, local, ops)
	if err != nil {
		return result, err
	}
	_, mergedRecords, err := validateSnapshot(merged, conf.WorkspaceID, epoch)
	if err != nil {
		return result, err
	}
	// Missing records require a tombstone protocol, and identities keep their kind.
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		base, e := baselineFiles(ctx, c)
		if e != nil {
			return e
		}
		for path, b := range base {
			if !strings.HasPrefix(path, "records/") {
				continue
			}
			old, e := decodeRecord(b)
			if e != nil {
				return e
			}
			current, ok := records[old.ID]
			if !ok || current.Kind != old.Kind || current.Scope != old.Scope {
				return Fail("invalid-catalog", "published records cannot disappear or change kind or scope")
			}
		}
		for id, r := range records {
			var kind, projectID, storage string
			e := c.QueryRowContext(ctx, `SELECT kind,project_id,json_extract(metadata,'$.policy.storage') FROM records WHERE id=?`, id).Scan(&kind, &projectID, &storage)
			if e == sql.ErrNoRows {
				continue
			}
			if e != nil {
				return e
			}
			if storage != "shared" || r.Kind != kind || r.Scope != (Scope{"project", projectID}) {
				return Fail("record-collision", "remote record collides with local work of a different scope, kind or policy")
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	// Preserve all pending overlays while accepting validated unrelated remote changes.
	for _, o := range ops {
		delete(records, o.id)
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if err := persistConflicts(ctx, c, ops, conflicts, blocked, remote, local, head); err != nil {
			return err
		}
		if err := installSnapshot(ctx, c, remote, records); err != nil {
			return err
		}
		return saveBaseline(ctx, c, remote, head, false)
	})
	if err != nil {
		return result, err
	}
	if len(selected) == 0 {
		err = store.Write(ctx, db, func(c *sql.Conn) error { return saveBaseline(ctx, c, remote, head, false) })
		if err != nil {
			return result, err
		}
		result.Head = head
		result.State = "synced"
		if len(conflicts) > 0 {
			result.State = "conflict"
			_, err = db.ExecContext(ctx, "UPDATE sync_state SET outcome='conflict'")
		}
		return result, err
	}
	candidate, err := candidateCommit(ctx, dir, merged, head)
	if err != nil {
		return result, err
	}
	parents, err := syncGit(ctx, dir, nil, "rev-list", "--parents", "-n", "1", candidate)
	if err != nil {
		return result, err
	}
	if parents != candidate+" "+head {
		return result, Fail("invalid-candidate", "candidate must have the fetched head as its only parent")
	}
	attempt := pendingPush{ExpectedHead: head, Candidate: candidate, Epoch: epoch}
	for _, o := range selected {
		attempt.OperationIDs = append(attempt.OperationIDs, o.op)
	}
	encoded, _ := json.Marshal(attempt)
	if _, err = syncGit(ctx, dir, nil, "update-ref", "refs/ctxferry/pending", candidate); err != nil {
		return result, err
	}
	if _, err = db.ExecContext(ctx, "UPDATE sync_state SET pending=?", string(encoded)); err != nil {
		return result, err
	}
	if err = newSyncBackend().Publish(ctx, dir, conf, attempt); err != nil {
		return result, err
	}
	// Install only acknowledged records, keeping conflicting local working versions.
	for _, o := range ops {
		if !selectedOperation(selected, o.id) {
			delete(mergedRecords, o.id)
		}
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		if err := installSnapshot(ctx, c, merged, mergedRecords); err != nil {
			return err
		}
		if err := saveBaseline(ctx, c, merged, candidate, false); err != nil {
			return err
		}
		for _, o := range selected {
			if _, err := c.ExecContext(ctx, "DELETE FROM sync_outbox WHERE id=? AND operation_id=?", o.id, o.op); err != nil {
				return err
			}
		}
		if len(conflicts) > 0 {
			_, err := c.ExecContext(ctx, "UPDATE sync_state SET outcome='conflict'")
			return err
		}
		return nil
	})
	result.Head = candidate
	result.State = "synced"
	if len(conflicts) > 0 {
		result.State = "conflict"
	}
	return result, err
}
func (a App) Subscribe(ctx context.Context, selector string, subscribed bool) (map[string]any, error) {
	release, err := a.syncOwner(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var p Project
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		var e error
		p, e = selectProject(ctx, c, selector)
		if e != nil {
			return e
		}
		if _, e = c.ExecContext(ctx, "INSERT INTO project_subscriptions VALUES(?,?) ON CONFLICT(project_id) DO UPDATE SET subscribed=excluded.subscribed", p.ID, subscribed); e != nil {
			return e
		}
		return reindex(ctx, c)
	})
	return map[string]any{"project": p, "subscribed": subscribed}, err
}

// Existing publishers already retain the confirmed snapshot in managed Git.
func (a App) prepareBaseline(ctx context.Context, db *sql.DB) error {
	var head, pending, epoch string
	err := db.QueryRowContext(ctx, "SELECT confirmed_head,pending,epoch FROM sync_state").Scan(&head, &pending, &epoch)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if head == "" || pending != "" {
		return nil
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sync_baselines").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	conf, err := loadSync(ctx, db)
	if err != nil {
		return err
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return err
	}
	files, err := readSnapshot(ctx, dir, head)
	if err != nil {
		return err
	}
	if _, _, err = validateSnapshot(files, conf.WorkspaceID, epoch); err != nil {
		return err
	}
	return store.Write(ctx, db, func(c *sql.Conn) error {
		for path, b := range files {
			if _, err := c.ExecContext(ctx, "INSERT INTO sync_baselines VALUES(?,?)", path, b); err != nil {
				return err
			}
		}
		return nil
	})
}
