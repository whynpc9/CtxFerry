package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type operation struct {
	id             string
	base, proposed []byte
	op             string
}
type ConflictInfo struct {
	ID         string `json:"id"`
	ConflictID string `json:"conflictId"`
	Head       string `json:"head"`
}
type BlockedRecord struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type ConflictDetail struct {
	ConflictInfo
	Base    *ConflictVersion `json:"base"`
	Local   *ConflictVersion `json:"local"`
	Remote  *ConflictVersion `json:"remote"`
	Project string           `json:"-"`
}

// ConflictVersion is the disclosure-filtered form of a published revision.
type ConflictVersion publishedEnvelope

type ResolutionInput struct {
	Title      string  `json:"title"`
	Purpose    string  `json:"purpose"`
	Source     string  `json:"source"`
	Summary    *string `json:"summary"`
	ObservedAt *string `json:"observedAt"`
	ValidUntil string  `json:"validUntil"`
	Body       string  `json:"body"`
	Policy     Policy  `json:"policy"`
	Deleted    bool    `json:"deleted"`
}

func selectedOperation(ops []operation, id string) bool {
	for _, o := range ops {
		if o.id == id {
			return true
		}
	}
	return false
}
func dependencies(r remoteRecord) []string {
	var ids []string
	if r.Scope.Kind == "project" {
		ids = append(ids, r.Scope.ID)
	}
	if k, ok := lookupKind(r.Kind); ok && k.dependencies != nil {
		ids = append(ids, k.dependencies(r)...)
	}
	return ids
}

// Pending dependencies form commit units; unchanged parents do not couple unrelated edits.
func planOperations(remote, local map[string][]byte, ops []operation) (map[string][]byte, []operation, map[string]bool, map[string]string, error) {
	conflicts := map[string]bool{}
	blocked := map[string]string{}
	edges := map[string][]string{}
	pending := map[string]bool{}
	for _, o := range ops {
		pending[o.id] = true
	}
	for _, o := range ops {
		r, err := decodeRecord(o.proposed)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		var baseRev, remoteRev string
		for i, b := range [][]byte{o.base, remote[recordPath(o.id)]} {
			if len(b) > 0 {
				v, e := decodeRecord(b)
				if e != nil {
					return nil, nil, nil, nil, e
				}
				if i == 0 {
					baseRev = v.Revision
				} else {
					remoteRev = v.Revision
				}
			}
		}
		if !bytes.Equal(remote[recordPath(o.id)], o.proposed) && baseRev != remoteRev {
			conflicts[o.id] = true
			blocked[o.id] = "record-conflict"
		}
		versions := []remoteRecord{r}
		if len(o.base) > 0 {
			old, _ := decodeRecord(o.base)
			versions = append(versions, old)
		}
		for _, v := range versions {
			for _, id := range dependencies(v) {
				if pending[id] {
					edges[o.id] = append(edges[o.id], id)
					edges[id] = append(edges[id], o.id)
				}
			}
		}
	}
	propagate := func() {
		for changed := true; changed; {
			changed = false
			for id, links := range edges {
				if blocked[id] != "" {
					for _, other := range links {
						if blocked[other] == "" {
							blocked[other] = "dependency:" + id
							changed = true
						}
					}
				}
			}
		}
	}
	build := func() map[string][]byte {
		f := map[string][]byte{}
		for p, b := range remote {
			f[p] = b
		}
		for _, o := range ops {
			if blocked[o.id] == "" {
				f[recordPath(o.id)] = o.proposed
			}
		}
		return f
	}
	for changed := true; changed; {
		propagate()
		changed = false
		f := build()
		parsed := map[string]remoteRecord{}
		for p, b := range f {
			if strings.HasPrefix(p, "records/") {
				r, e := decodeRecord(b)
				if e != nil {
					return nil, nil, nil, nil, e
				}
				parsed[r.ID] = r
			}
		}
		for _, o := range ops {
			if blocked[o.id] != "" {
				continue
			}
			r := parsed[o.id]
			if !r.Deleted {
				for _, id := range dependencies(r) {
					v, ok := parsed[id]
					if !ok || v.Deleted {
						blocked[o.id] = "missing-dependency:" + id
						changed = true
					}
				}
			}
			// A deletion is held if any live dependent remains in the candidate.
			if r.Deleted {
				for _, v := range parsed {
					if !v.Deleted {
						for _, id := range dependencies(v) {
							if id == r.ID {
								blocked[o.id] = "live-dependent:" + v.ID
								changed = true
							}
						}
					}
				}
			}
		}
		for id, r := range parsed {
			if r.Deleted {
				continue
			}
			for _, dep := range invalidTaskDependencies(r, parsed) {
				for _, target := range []string{id, dep} {
					if pending[target] && blocked[target] == "" {
						blocked[target] = "invalid-task-dependency:" + dep
						changed = true
					}
				}
			}
		}
		// Cross-record uniqueness conflicts hold the participating pending operations.
		keys := map[string]string{}
		for id, r := range parsed {
			if r.Deleted {
				continue
			}
			key := uniquenessKey(r)
			if key == "" {
				continue
			}
			if prior := keys[key]; prior != "" {
				for _, x := range []string{id, prior} {
					if pending[x] && blocked[x] == "" {
						blocked[x] = "uniqueness:" + key
						changed = true
					}
				}
			} else {
				keys[key] = id
			}
		}
	}
	propagate()
	if err := limitPublicationUnits(remote, local, ops, edges, blocked); err != nil {
		return nil, nil, nil, nil, err
	}
	f := build()
	selected := []operation{}
	for _, o := range ops {
		if blocked[o.id] == "" {
			selected = append(selected, o)
			r, _ := decodeRecord(o.proposed)
			for _, path := range r.ordinaryObjectPaths() {
				if b, ok := local[path]; ok {
					f[path] = b
				}
			}
		}
	}
	return f, selected, conflicts, blocked, nil
}
func overlayTombstones(ctx context.Context, c *sql.Conn, files map[string][]byte) error {
	rows, e := c.QueryContext(ctx, "SELECT id,data FROM sync_tombstones")
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var b []byte
		if e = rows.Scan(&id, &b); e != nil {
			return e
		}
		files[recordPath(id)] = b
	}
	return rows.Err()
}
func requireNoConflict(ctx context.Context, c rowQuerier, id string) error {
	var n int
	if e := c.QueryRowContext(ctx, "SELECT count(*) FROM sync_conflicts WHERE id=? AND active=1", id).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return Fail("conflict-unresolved", "use sync resolve for this record")
	}
	return nil
}
func persistConflicts(ctx context.Context, c *sql.Conn, ops []operation, conflicts map[string]bool, blocked map[string]string, remote, local map[string][]byte, head string) error {
	for _, o := range ops {
		if !conflicts[o.id] {
			continue
		}
		for _, version := range []struct {
			record []byte
			files  map[string][]byte
		}{{o.base, nil}, {o.proposed, local}, {remote[recordPath(o.id)], remote}} {
			if len(version.record) == 0 {
				continue
			}
			r, err := decodeRecord(version.record)
			if err != nil {
				return err
			}
			for _, path := range r.publishedObjectPaths() {
				data, ok := version.files[path]
				if !ok {
					err := c.QueryRowContext(ctx, "SELECT data FROM sync_baselines WHERE path=?", path).Scan(&data)
					if err == sql.ErrNoRows {
						// An unpublished artifact intent may only have a local object.
						continue
					}
					if err != nil {
						return err
					}
				}
				if _, err := c.ExecContext(ctx, "INSERT OR IGNORE INTO sync_objects VALUES(?,COALESCE(?,X''))", path, data); err != nil {
					return err
				}
			}
		}
	}
	var e error
	if _, e = c.ExecContext(ctx, "DELETE FROM sync_blocked"); e != nil {
		return e
	}
	for id, reason := range blocked {
		if _, e = c.ExecContext(ctx, "INSERT INTO sync_blocked VALUES(?,?)", id, reason); e != nil {
			return e
		}
	}
	for _, o := range ops {
		if !conflicts[o.id] {
			if _, e = c.ExecContext(ctx, "UPDATE sync_conflicts SET active=0 WHERE id=? AND active=1", o.id); e != nil {
				return e
			}
			continue
		}
		var oldLocal, oldRemote []byte
		e = c.QueryRowContext(ctx, "SELECT local,remote FROM sync_conflicts WHERE id=? AND active=1", o.id).Scan(&oldLocal, &oldRemote)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if bytes.Equal(oldLocal, o.proposed) && bytes.Equal(oldRemote, remote[recordPath(o.id)]) {
			continue
		}
		if _, e = c.ExecContext(ctx, "UPDATE sync_conflicts SET active=0 WHERE id=?", o.id); e != nil {
			return e
		}
		if _, e = c.ExecContext(ctx, "INSERT INTO sync_conflicts VALUES(?,?,?,?,?,?,1)", uuid.NewString(), o.id, o.base, o.proposed, remote[recordPath(o.id)], head); e != nil {
			return e
		}
	}
	return nil
}
func conflictStatus(ctx context.Context, db projectReader) ([]ConflictInfo, []BlockedRecord, error) {
	var conflicts []ConflictInfo
	var blocked []BlockedRecord
	rows, e := db.QueryContext(ctx, "SELECT id,token,head FROM sync_conflicts WHERE active=1 ORDER BY id")
	if e != nil {
		return nil, nil, e
	}
	for rows.Next() {
		var v ConflictInfo
		if e = rows.Scan(&v.ID, &v.ConflictID, &v.Head); e != nil {
			rows.Close()
			return nil, nil, e
		}
		conflicts = append(conflicts, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	rows, e = db.QueryContext(ctx, "SELECT id,reason FROM sync_blocked ORDER BY id")
	if e != nil {
		return nil, nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v BlockedRecord
		if e = rows.Scan(&v.ID, &v.Reason); e != nil {
			return nil, nil, e
		}
		blocked = append(blocked, v)
	}
	return conflicts, blocked, rows.Err()
}
func evidenceFiles(ctx context.Context, c *sql.Conn) (map[string][]byte, error) {
	f, e := baselineFiles(ctx, c)
	if e != nil {
		return nil, e
	}
	rows, e := c.QueryContext(ctx, "SELECT path,data FROM sync_objects")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var b []byte
		if e = rows.Scan(&p, &b); e != nil {
			return nil, e
		}
		f[p] = b
	}
	return f, rows.Err()
}
func conflictView(b []byte, files map[string][]byte) (*ConflictVersion, error) {
	if len(b) == 0 {
		return nil, nil
	}
	r, e := decodeRecord(b)
	if e != nil {
		return nil, e
	}
	payload := map[string]any{}
	if e = json.Unmarshal(r.Data, &payload); e != nil {
		return nil, e
	}
	if r.Kind == "context" {
		delete(payload, "body")
		if r.Policy.AgentDisclosure == "full" && r.Value.Body != nil {
			payload["body"] = string(files["objects/"+r.Value.Body.SHA256])
		}
		if r.Policy.AgentDisclosure == "reference" || r.Policy.AgentDisclosure == "local-tool" {
			delete(payload, "summary")
		}
	}
	r.Payload = payload
	version := ConflictVersion(r.publishedEnvelope)
	return &version, nil
}
func (a App) Conflict(ctx context.Context, id string) (ConflictDetail, error) {
	var out ConflictDetail
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return out, e
	}
	defer db.Close()
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		var base, local, remote []byte
		out.ID = id
		e := c.QueryRowContext(ctx, "SELECT token,head,base,local,remote FROM sync_conflicts WHERE id=? AND active=1", id).Scan(&out.ConflictID, &out.Head, &base, &local, &remote)
		if e == sql.ErrNoRows {
			return Fail("conflict-not-found", "no active conflict for record")
		}
		if e != nil {
			return e
		}
		f, e := evidenceFiles(ctx, c)
		if e != nil {
			return e
		}
		out.Base, e = conflictView(base, f)
		if e != nil {
			return e
		}
		out.Local, e = conflictView(local, f)
		if e != nil {
			return e
		}
		out.Remote, e = conflictView(remote, f)
		if e != nil {
			return e
		}
		if v := out.Local; v != nil && v.Scope.Kind == "project" {
			e = c.QueryRowContext(ctx, "SELECT slug FROM projects WHERE id=?", v.Scope.ID).Scan(&out.Project)
			if e != nil && e != sql.ErrNoRows {
				return e
			}
		}
		return nil
	})
	return out, e
}

// Every explicit choice is a new revision against precisely the inspected remote.
func (a App) Resolve(ctx context.Context, id, token, take string, input []byte) (SyncResult, error) {
	out := SyncResult{Boundary: plaintextBoundary, State: "pending"}
	var revision string
	release, e := a.syncOwner(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return out, e
	}
	defer db.Close()
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		var pending, epoch string
		if e := c.QueryRowContext(ctx, "SELECT pending,epoch FROM sync_state").Scan(&pending, &epoch); e != nil {
			return e
		}
		if pending != "" {
			return Fail("push-pending", "reconcile pending push before resolving")
		}
		var actual string
		var local, remote []byte
		e := c.QueryRowContext(ctx, "SELECT token,local,remote FROM sync_conflicts WHERE id=? AND active=1", id).Scan(&actual, &local, &remote)
		if e == sql.ErrNoRows {
			return Fail("conflict-not-found", "no active conflict for record")
		}
		if e != nil {
			return e
		}
		if token == "" || token != actual {
			return Fail("stale-conflict", "inspect current conflict and supply --if-conflict")
		}
		chosen := local
		if take == "remote" {
			chosen = remote
		} else if take != "local" && len(input) == 0 {
			return Fail("usage", "choose --take local/remote or --file")
		}
		if len(input) > 0 && take != "" {
			return Fail("usage", "choose one resolution source")
		}
		r, e := decodeRecord(chosen)
		if e != nil {
			return e
		}
		f, e := evidenceFiles(ctx, c)
		if e != nil {
			return e
		}
		if len(input) > 0 {
			if r.Kind != "context" {
				return Fail("unsupported-schema", "replacement files currently support context only")
			}
			var in ResolutionInput
			if e = strictJSON(input, &in); e != nil {
				return e
			}
			if in.Policy.Storage != "shared" {
				return Fail("invalid-policy", "resolution must remain shared")
			}
			r.Policy = in.Policy
			r.Deleted = in.Deleted
			r.Payload = map[string]any{"title": in.Title, "purpose": in.Purpose, "source": in.Source, "summary": in.Summary, "observedAt": in.ObservedAt, "validUntil": in.ValidUntil, "body": objectManifest{"text/markdown", len(in.Body), digest([]byte(in.Body))}}
			f["objects/"+digest([]byte(in.Body))] = []byte(in.Body)
		}
		prior, e := decodeRecord(remote)
		if e != nil {
			return e
		}
		r.PreviousRevision = prior.Revision
		r.Revision = uuid.NewString()
		revision = r.Revision
		r.OperationID = uuid.NewString()
		r.Provenance.UpdatedAt = now()
		if r.Kind == "configuration" {
			r.Config.Revision = r.Revision
			r.Config.UpdatedAt = r.Provenance.UpdatedAt
			r.Payload = r.Config
		}
		if e = c.QueryRowContext(ctx, "SELECT id FROM device").Scan(&r.Provenance.DeviceID); e != nil {
			return e
		}
		if take == "local" && !r.Deleted {
			if e = includePendingDependencies(ctx, c, f, r); e != nil {
				return e
			}
		}
		if r.Kind == artifactKind && take == "local" && !r.Deleted && r.Artifact.Omitted == "" {
			p := r.Artifact
			if _, exists := f[p.objectPath()]; !exists {
				data, err := a.readLocalArtifactBytes(p)
				switch {
				case err != nil:
					p.Omitted = "payload-unavailable"
				case excludedArtifactBytes(data):
					p.Omitted = "excluded-format"
				default:
					f[p.objectPath()] = data
				}
				r.Payload = p
			}
		}
		if e = applyExplicit(ctx, c, f, r.publishedEnvelope, remote, epoch); e != nil {
			return e
		}
		if _, e = c.ExecContext(ctx, "UPDATE sync_conflicts SET active=0 WHERE token=?", token); e != nil {
			return e
		}
		if _, e = c.ExecContext(ctx, "DELETE FROM sync_blocked WHERE id=?", id); e != nil {
			return e
		}
		_, e = c.ExecContext(ctx, "UPDATE sync_state SET outcome='pending'")
		return e
	})
	if e == nil {
		out.ResolvedRevision = revision
	}
	return out, e
}

// Pending dependencies already have outbox entries. Validate the chosen record
// against those revisions here, then the regular planner publishes the closed
// set together. Resolving one conflict must never choose another by implication.
func includePendingDependencies(ctx context.Context, c *sql.Conn, files map[string][]byte, root remoteRecord) error {
	seen := map[string]bool{root.ID: true}
	queue := dependencies(root)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		path := recordPath(id)
		var base, proposed []byte
		err := c.QueryRowContext(ctx, "SELECT base,proposed FROM sync_outbox WHERE id=?", id).Scan(&base, &proposed)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			if err := requireNoConflict(ctx, c, id); err != nil {
				return err
			}
			if !bytes.Equal(base, files[path]) && !bytes.Equal(proposed, files[path]) {
				return Fail("conflict-unresolved", "sync and resolve the changed dependency before choosing this record")
			}
			files[path] = proposed
		}
		if raw := files[path]; raw != nil {
			record, err := decodeRecord(raw)
			if err != nil {
				return err
			}
			if !record.Deleted {
				queue = append(queue, dependencies(record)...)
			}
		}
	}
	return nil
}

func applyExplicit(ctx context.Context, c *sql.Conn, f map[string][]byte, r publishedEnvelope, base []byte, epoch string) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	f[recordPath(r.ID)] = b
	if e := pruneLargeEvidence(f); e != nil {
		return e
	}
	// Validate the replacement in its published context, never an unchecked identity/payload.
	_, records, e := validateSnapshot(f, r.WorkspaceID, epoch)
	if e != nil {
		return e
	}
	v := records[r.ID]
	if e = installSnapshot(ctx, c, f, map[string]remoteRecord{r.ID: v}); e != nil {
		return e
	}
	_, e = c.ExecContext(ctx, "INSERT INTO sync_outbox VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET base=excluded.base,proposed=excluded.proposed,operation_id=excluded.operation_id,epoch=excluded.epoch", r.ID, base, b, r.OperationID, epoch)
	return e
}

func (a App) ChangeDeletion(ctx context.Context, id, revision, commit string) (SyncResult, error) {
	if !validUUID(id) {
		return SyncResult{}, Fail("usage", "document ID must be a canonical UUID")
	}
	out := SyncResult{Boundary: plaintextBoundary, State: "pending"}
	release, e := a.syncOwner(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return out, e
	}
	defer db.Close()
	if e = a.prepareBaseline(ctx, db); e != nil {
		return out, e
	}
	conf, e := loadSync(ctx, db)
	if e != nil {
		return out, e
	}
	var historical map[string][]byte
	if commit != "" {
		if len(commit) != 40 || strings.Trim(commit, "0123456789abcdef") != "" {
			return out, Fail("usage", "from-commit requires a full commit SHA")
		}
		var head string
		if e = db.QueryRowContext(ctx, "SELECT confirmed_head FROM sync_state").Scan(&head); e != nil {
			return out, e
		}
		dir, e := a.managedGit(ctx)
		if e != nil {
			return out, e
		}
		if _, e = syncGit(ctx, dir, nil, "merge-base", "--is-ancestor", commit, head); e != nil {
			return out, Fail("invalid-history", "restore requires an ancestor of confirmed history")
		}
		historical, e = readSnapshot(ctx, dir, commit)
		if e != nil {
			return out, e
		}
		if _, _, e = validateSnapshot(historical, conf.WorkspaceID, ""); e != nil {
			return out, e
		}
	}
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		if e := requireNoConflict(ctx, c, id); e != nil {
			return e
		}
		var epoch, pending string
		if e := c.QueryRowContext(ctx, "SELECT epoch,pending FROM sync_state").Scan(&epoch, &pending); e != nil {
			return e
		}
		if pending != "" {
			return Fail("push-pending", "reconcile pending push first")
		}
		f, _, e := exportConn(ctx, c, conf)
		if e != nil {
			return e
		}
		base, e := baselineFiles(ctx, c)
		if e != nil {
			return e
		}
		if e := addConfirmedArtifactObjects(f, base); e != nil {
			return e
		}
		r, e := decodeRecord(f[recordPath(id)])
		if e != nil || r.Kind != "context" {
			return Fail("document-not-found", "shared document not found")
		}
		if revision == "" || r.Revision != revision {
			return Fail("revision-conflict", "supply current --if-revision")
		}
		prior := r.Revision
		if historical != nil {
			old, e := decodeRecord(historical[recordPath(id)])
			if e != nil || old.Deleted || old.Kind != r.Kind || old.Scope != r.Scope {
				return Fail("invalid-history", "history must contain the same live record")
			}
			r.Payload = old.Payload
			r.Policy = old.Policy
			r.Deleted = false
			p := "objects/" + old.Value.Body.SHA256
			f[p] = historical[p]
		} else {
			r.Deleted = true
		}
		r.PreviousRevision = prior
		r.Revision = uuid.NewString()
		r.OperationID = uuid.NewString()
		r.Provenance.UpdatedAt = now()
		if e = c.QueryRowContext(ctx, "SELECT id FROM device").Scan(&r.Provenance.DeviceID); e != nil {
			return e
		}
		return applyExplicit(ctx, c, f, r.publishedEnvelope, base[recordPath(id)], epoch)
	})
	return out, e
}

// A held local Project owns its working slug until the developer changes it.
// Keep the remote envelope in the baseline, but defer its local projection and
// dependent projections rather than violating local foreign-key/unique indexes.
func installableRecords(ctx context.Context, c *sql.Conn, records map[string]remoteRecord) (map[string]remoteRecord, error) {
	out := map[string]remoteRecord{}
	held := map[string]bool{}
	for id, r := range records {
		out[id] = r
		if r.Kind != "project" {
			continue
		}
		var owner string
		e := c.QueryRowContext(ctx, "SELECT p.id FROM projects p JOIN sync_outbox o ON o.id=p.id WHERE p.slug=? AND p.id<>?", r.Value.Slug, id).Scan(&owner)
		if e != nil && e != sql.ErrNoRows {
			return nil, e
		}
		if e == nil {
			held[id] = true
			delete(out, id)
		}
	}
	for changed := true; changed; {
		changed = false
		for id, r := range out {
			for _, dep := range dependencies(r) {
				if held[dep] {
					held[id] = true
					delete(out, id)
					changed = true
					break
				}
			}
		}
	}
	return out, nil
}

// Baseline records whose projections were deferred still belong to the published
// catalog. Their absence from a typed table is not a local deletion/policy change.
func retainDeferredRecords(ctx context.Context, c *sql.Conn, files map[string][]byte) error {
	base, e := baselineRecords(ctx, c)
	if e != nil {
		return e
	}
	for path, b := range base {
		if !strings.HasPrefix(path, "records/") || files[path] != nil {
			continue
		}
		r, e := decodeRecord(b)
		if e != nil {
			return e
		}
		kind, ok := lookupKind(r.Kind)
		if !ok {
			continue
		}
		table := kind.table
		var n int
		if e = c.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE id=?", r.ID).Scan(&n); e != nil {
			return e
		}
		if n == 0 {
			files[path] = b
			for _, object := range r.ordinaryObjectPaths() {
				var data []byte
				if e := c.QueryRowContext(ctx, "SELECT data FROM sync_baselines WHERE path=?", object).Scan(&data); e != nil {
					return e
				}
				files[object] = data
			}
		}
	}
	return nil
}
