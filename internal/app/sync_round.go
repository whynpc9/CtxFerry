package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type workspaceFormat struct {
	Format       int    `json:"format"`
	WorkspaceID  string `json:"workspaceId"`
	Epoch        string `json:"epoch"`
	StorageMode  string `json:"storageMode"`
	RestoredFrom string `json:"restoredFrom,omitempty"`
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
	Data          json.RawMessage     `json:"-"`
	Value         recordPayload       `json:"-"`
	Config        configuration       `json:"-"`
	Resource      ResourceSpec        `json:"-"`
	Task          TaskSpec            `json:"-"`
	SecretBinding SecretBindingSpec   `json:"-"`
	Artifact      artifactPublication `json:"-"`
	SkillPin      SkillPackage        `json:"-"`
	SkillGroup    SkillGroup          `json:"-"`
}

// Ordinary objects are staged with their records before artifact payloads are budgeted.
func (r remoteRecord) ordinaryObjectPaths() []string {
	if r.Value.Body != nil {
		return []string{"objects/" + r.Value.Body.SHA256}
	}
	return r.skillObjects()
}

func (r remoteRecord) publishedObjectPaths() []string {
	if r.Kind == artifactKind && r.Artifact.Omitted == "" {
		return []string{r.Artifact.objectPath()}
	}
	return r.ordinaryObjectPaths()
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
	fields, err := configurationFields(b)
	if err != nil {
		return r, Fail("incomplete", "invalid catalog record envelope")
	}
	seen := make(map[string]bool, len(fields))
	for field := range fields {
		key := strings.ToLower(field)
		if seen[key] {
			return r, Fail("incomplete", "duplicate catalog record envelope field")
		}
		seen[key] = true
	}
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
	raw, err := json.Marshal(r.Payload)
	if err != nil {
		return r, err
	}
	r.Data = raw
	if k, ok := lookupKind(r.Kind); ok && k.decode != nil {
		return r, k.decode(&r, wire.Payload, fields)
	}
	return r, strictJSON(raw, &r.Value)
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
	if !validUUID(w.WorkspaceID) || !validUUID(w.Epoch) || w.StorageMode != "plaintext" || w.RestoredFrom != "" && !validCommitID(w.RestoredFrom) || wid != "" && w.WorkspaceID != wid || epoch != "" && w.Epoch != epoch {
		return invalid()
	}
	if len(files["README.md"]) == 0 {
		return invalid()
	}
	unique := map[string]bool{}
	ordinaryObjects := map[string]bool{}
	check := &snapshotCheck{workspace: w, files: files, runs: map[string]remoteRecord{}, artifactObjects: map[string]bool{}}
	for path, b := range files {
		if path == "workspace.json" || path == "README.md" {
			continue
		}
		if strings.HasPrefix(path, "objects/") {
			if len(path) != 72 || digest(b) != strings.TrimPrefix(path, "objects/") || len(b) > artifactExplicitLimit {
				return invalid()
			}
			continue
		}
		if !strings.HasPrefix(path, "records/") || len(b) > MetadataLimit {
			return invalid()
		}
		r, err := decodeRecord(b)
		kind, known := lookupKind(r.Kind)
		if err != nil {
			if known && kind.detailedDecodeErrors && Classify(err).Code != "unsupported-schema" {
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
		if !known {
			return w, nil, Fail("unsupported-schema", "unsupported catalog record kind")
		}
		if err := kind.validate(r, check); err != nil {
			return w, nil, err
		}
		if key := uniquenessKey(r); key != "" && !r.Deleted {
			if unique[key] {
				return invalid()
			}
			unique[key] = true
		}
		for _, path := range r.ordinaryObjectPaths() {
			ordinaryObjects[path] = true
		}
		records[r.ID] = r
	}
	var ordinaryBytes, artifactBytes int
	for path, b := range files {
		if check.artifactObjects[path] {
			artifactBytes += len(b)
			if !ordinaryObjects[path] {
				continue
			}
		}
		if strings.HasPrefix(path, "objects/") && len(b) > BodyLimit {
			return invalid()
		}
		ordinaryBytes += len(b)
	}
	if ordinaryBytes > 64*BodyLimit || artifactBytes > artifactStagingBudget {
		return w, nil, Fail("size-limit", "catalog exceeds staging limit")
	}
	for _, r := range records {
		if r.Deleted {
			continue
		}
		if r.Scope.Kind == "project" && !liveRecord(records, r.Scope.ID, "project") {
			return invalid()
		}
		if k, _ := lookupKind(r.Kind); k.references != nil && !k.references(r, records) {
			return invalid()
		}
	}
	return w, records, nil
}

type snapshotBlob struct {
	path string
	oid  string
	size int
}

func readSnapshot(ctx context.Context, dir, head string) (map[string][]byte, error) {
	listing, err := syncGitRaw(ctx, dir, nil, "ls-tree", "-r", "-l", "-z", head)
	if err != nil {
		return nil, err
	}
	if listing == "" || !strings.HasSuffix(listing, "\x00") {
		return nil, Fail("incomplete", "partial Git tree listing")
	}
	var blobs []snapshotBlob
	var total int
	for line := range strings.SplitSeq(strings.TrimSuffix(listing, "\x00"), "\x00") {
		header, path, found := strings.Cut(line, "\t")
		meta := strings.Fields(header)
		if !found || len(meta) != 4 || meta[0] != "100644" || meta[1] != "blob" || !validCommitID(meta[2]) {
			return nil, Fail("incomplete", "catalog requires regular files")
		}
		n, err := strconv.Atoi(meta[3])
		if err != nil || n < 0 {
			return nil, Fail("incomplete", "catalog blob size is missing or invalid")
		}
		limit := BodyLimit
		if strings.HasPrefix(path, "objects/") {
			limit = artifactExplicitLimit
		}
		if n > limit {
			return nil, Fail("size-limit", "catalog file exceeds limits")
		}
		total += n
		if total > artifactStagingBudget+64*BodyLimit {
			return nil, Fail("size-limit", "catalog exceeds staging limit")
		}
		blobs = append(blobs, snapshotBlob{path: path, oid: meta[2], size: n})
	}
	files := make(map[string][]byte, len(blobs))
	// Git process startup is particularly expensive on Windows. Batch small
	// blobs together while bounding the temporary cat-file output.
	const batchLimit = 16 << 20
	for len(blobs) > 0 {
		var batchBytes, count int
		var input strings.Builder
		for _, blob := range blobs {
			batchBytes += blob.size + 100 // Include the batch response header.
			if batchBytes > batchLimit {
				break
			}
			input.WriteString(blob.oid)
			input.WriteByte('\n')
			count++
		}
		out, err := syncGitRaw(ctx, dir, []byte(input.String()), "cat-file", "--batch")
		if err != nil {
			return nil, err
		}
		for _, blob := range blobs[:count] {
			header, data, found := strings.Cut(out, "\n")
			if !found || header != blob.oid+" blob "+strconv.Itoa(blob.size) ||
				len(data) <= blob.size || data[blob.size] != '\n' {
				return nil, Fail("incomplete", "partial or invalid Git blob")
			}
			if _, exists := files[blob.path]; exists {
				return nil, Fail("incomplete", "duplicate Git snapshot path")
			}
			files[blob.path] = []byte(data[:blob.size])
			out = data[blob.size+1:]
		}
		if out != "" {
			return nil, Fail("incomplete", "unexpected Git blob data")
		}
		blobs = blobs[count:]
	}
	return files, nil
}
func baselineRecords(ctx context.Context, c *sql.Conn) (map[string][]byte, error) {
	return readBaseline(ctx, c, "SELECT path,data FROM sync_baselines WHERE path LIKE 'records/%'")
}

func baselineFiles(ctx context.Context, c *sql.Conn) (map[string][]byte, error) {
	return readBaseline(ctx, c, "SELECT path,data FROM sync_baselines")
}

func readBaseline(ctx context.Context, c *sql.Conn, query string) (map[string][]byte, error) {
	files := map[string][]byte{}
	rows, err := c.QueryContext(ctx, query)
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
	base, err := baselineRecords(ctx, c)
	if err != nil {
		return err
	}
	var hasBaseline bool
	if err := c.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sync_baselines)").Scan(&hasBaseline); err != nil {
		return err
	}
	if head != "" && !hasBaseline {
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
	rows, err := c.QueryContext(ctx, "SELECT path FROM sync_baselines")
	if err != nil {
		return err
	}
	var removed []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return err
		}
		if _, exists := files[path]; !exists {
			removed = append(removed, path)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, path := range removed {
		if _, err := c.ExecContext(ctx, "DELETE FROM sync_baselines WHERE path=?", path); err != nil {
			return err
		}
	}
	for path, b := range files {
		if _, err := c.ExecContext(ctx, `INSERT INTO sync_baselines VALUES(?,COALESCE(?,X'')) ON CONFLICT(path)
			DO UPDATE SET data=excluded.data WHERE sync_baselines.data IS NOT excluded.data`, path, b); err != nil {
			return err
		}
	}
	if clear {
		if _, err := c.ExecContext(ctx, "DELETE FROM sync_outbox"); err != nil {
			return err
		}
	}
	_, err = c.ExecContext(ctx, "UPDATE sync_state SET confirmed_head=?,pending='',last_sync_at=?,outcome='synced' WHERE singleton=1", head, now())
	return err
}
func installSnapshot(ctx context.Context, c *sql.Conn, files map[string][]byte, records map[string]remoteRecord) error {
	var err error
	records, err = installableRecords(ctx, c, records)
	if err != nil {
		return err
	}
	for _, kind := range recordKinds {
		for id, r := range records {
			if r.Kind != kind.name {
				continue
			}
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
			if err := kind.install(ctx, c, files, r); err != nil {
				return err
			}
			if kind.exportIdentity {
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
			if r.Kind == "project" && !r.Deleted && (id == selector || r.Value.Slug == selector) {
				selected[id] = true
				found = true
			}
		}
		if !found {
			return r, Fail("project-not-found", "subscription project was not found")
		}
	}
	history, err := measureSyncHistory(ctx, dir, head)
	if err != nil {
		return r, err
	}
	r.History = syncHistory(history)
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
		if err := saveSyncHistory(ctx, c, history); err != nil {
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

func (a App) syncRound(ctx context.Context, db *sql.DB) (result SyncResult, err error) {
	result = SyncResult{Boundary: plaintextBoundary, State: "pending"}

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
	var previousHistory sql.NullInt64
	if err = db.QueryRowContext(ctx, "SELECT epoch,confirmed_head,pending,history_bytes FROM sync_state").Scan(&epoch, &confirmed, &pending, &previousHistory); err != nil {
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
	workspace, records, err := validateSnapshot(remote, conf.WorkspaceID, "")
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
	epochChanged := workspace.Epoch != epoch
	if epochChanged {
		if workspace.RestoredFrom == "" {
			return result, Fail("incomplete", "workspace epoch changed without a restore marker")
		}
		extends, err := newSyncBackend().Ancestor(ctx, dir, workspace.RestoredFrom, head)
		if err != nil {
			return result, err
		}
		if !extends {
			return result, Fail("incomplete", "workspace restore source is outside published history")
		}
	}
	history := previousHistory.Int64
	if !previousHistory.Valid || confirmed != head {
		history, err = measureSyncHistory(ctx, dir, head)
		if err != nil {
			return result, err
		}
	} else {
		history = syncHistorySize(history)
	}
	result.History = syncHistory(history)
	result.Epoch = workspace.Epoch
	refreshOutbox := true
	if pending != "" {
		var attempt pendingPush
		if json.Unmarshal([]byte(pending), &attempt) != nil || attempt.ExpectedHead == "" || attempt.RestoredFrom == "" && attempt.Epoch != epoch {
			return result, Fail("push-pending", "pending initialization requires its explicit preview retry")
		}
		accepted, e := newSyncBackend().Ancestor(ctx, dir, attempt.Candidate, head)
		if e != nil {
			return result, e
		}
		if accepted && !epochChanged && attempt.RestoredFrom == "" {
			candidateFiles, e := readSnapshot(ctx, dir, attempt.Candidate)
			if e != nil {
				return result, e
			}
			_, candidateRecords, e := validateSnapshot(candidateFiles, conf.WorkspaceID, epoch)
			if e != nil {
				return result, e
			}
			err = store.Write(ctx, db, func(c *sql.Conn) error {
				if e := saveBaseline(ctx, c, candidateFiles, attempt.Candidate, false); e != nil {
					return e
				}
				if _, e := c.ExecContext(ctx, "UPDATE sync_state SET history_bytes=NULL"); e != nil {
					return e
				}
				for _, r := range candidateRecords {
					if r.Kind == artifactKind && slices.Contains(attempt.OperationIDs, r.OperationID) {
						if e := installArtifactPublication(ctx, c, r); e != nil {
							return e
						}
					}
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
		} else if !epochChanged && attempt.RestoredFrom == "" {
			if _, err = db.ExecContext(ctx, "UPDATE sync_state SET pending=''"); err != nil {
				return result, err
			}
		} else {
			// Keep the journal until the new baseline commits. An acknowledged
			// old-generation edit may still have been rolled back by the restore.
			// Shared writes were blocked while the journal was active, so its
			// already persisted outbox can go straight to the planner.
			refreshOutbox = false
		}
	}
	if refreshOutbox {
		// Bootstrap existing schema-4 publishers from their actual confirmed Git snapshot.
		err = store.Write(ctx, db, func(c *sql.Conn) error {
			var hasBaseline bool
			if e := c.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sync_baselines)").Scan(&hasBaseline); e != nil {
				return e
			}
			if !hasBaseline {
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
	if err := a.stageArtifactPublications(merged, remote, selected, history); err != nil {
		return result, err
	}
	_, mergedRecords, err := validateSnapshot(merged, conf.WorkspaceID, workspace.Epoch)
	if err != nil {
		return result, err
	}
	// Missing records require a tombstone protocol, and identities keep their kind.
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		base, e := baselineRecords(ctx, c)
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
			if old.Kind == artifactKind && !sameArtifactIdentity(old.Artifact, current.Artifact) {
				return Fail("invalid-catalog", "published artifact identity cannot change")
			}
		}
		for id, r := range records {
			if r.Kind == artifactKind {
				var localOnly bool
				if e := c.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM artifacts WHERE id=?)
					AND NOT EXISTS(SELECT 1 FROM records WHERE id=? AND kind=?)`, id, id, artifactKind).Scan(&localOnly); e != nil {
					return e
				}
				if localOnly {
					return Fail("record-collision", "remote artifact collides with a local-only registration")
				}
			}
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
		if epochChanged {
			if _, err := c.ExecContext(ctx, "UPDATE sync_state SET epoch=? WHERE singleton=1", workspace.Epoch); err != nil {
				return err
			}
		}
		// Rebuild only operations that passed comparison in the current epoch.
		for _, o := range selected {
			if _, err := c.ExecContext(ctx, "UPDATE sync_outbox SET epoch=? WHERE id=?", workspace.Epoch, o.id); err != nil {
				return err
			}
		}
		if err := saveSyncHistory(ctx, c, history); err != nil {
			return err
		}
		return saveBaseline(ctx, c, remote, head, false)
	})
	if err != nil {
		return result, err
	}
	if len(selected) == 0 {
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
	candidateHistory, err := measureSyncHistory(ctx, dir, candidate)
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
	attempt := pendingPush{ExpectedHead: head, Candidate: candidate, Epoch: workspace.Epoch}
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
		if err := saveSyncHistory(ctx, c, candidateHistory); err != nil {
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
	result.Pending = len(ops) - len(selected)
	result.Head = candidate
	result.History = syncHistory(candidateHistory)
	result.State = "synced"
	for _, reason := range blocked {
		if reason == publicationBudgetReason {
			result.State = "pending"
			break
		}
	}
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
			if _, err := c.ExecContext(ctx, "INSERT INTO sync_baselines VALUES(?,COALESCE(?,X''))", path, b); err != nil {
				return err
			}
		}
		return nil
	})
}
