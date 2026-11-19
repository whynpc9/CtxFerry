package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

type SyncConfig struct {
	Remote      string `json:"remote"`
	WorkspaceID string `json:"workspaceId"`
	Branch      string `json:"branch"`
	Identity    string `json:"identity"`
}
type SyncCheck struct {
	GitReachable bool   `json:"gitReachable"`
	Privacy      string `json:"privacy"`
	Identity     string `json:"identity,omitempty"`
	Failure      string `json:"failure,omitempty"`
}
type UploadFile struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type SyncResult struct {
	Conflicts  []ConflictInfo  `json:"conflicts,omitempty"`
	Blocked    []BlockedRecord `json:"blocked,omitempty"`
	Config     *SyncConfig     `json:"config,omitempty"`
	Check      *SyncCheck      `json:"check,omitempty"`
	PreviewID  string          `json:"previewId,omitempty"`
	Files      []UploadFile    `json:"files,omitempty"`
	Boundary   string          `json:"boundary"`
	State      string          `json:"state"`
	Head       string          `json:"head,omitempty"`
	LastSyncAt *string         `json:"lastSyncAt"`
	Pending    int             `json:"pending"`
}

const plaintextBoundary = "Shared records and their bodies are uploaded in plaintext. Local-only records, checkout bindings, route facts, TCP observations, credential stores and authentication are excluded. Shared prose can contain pasted secrets; inspect it before publishing."

type publishedEnvelope struct {
	SchemaVersion    int    `json:"schemaVersion"`
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	WorkspaceID      string `json:"workspaceId"`
	Scope            Scope  `json:"scope"`
	Revision         string `json:"revision"`
	PreviousRevision string `json:"previousRevision"`
	OperationID      string `json:"operationId"`
	Deleted          bool   `json:"deleted"`
	Provenance       struct {
		DeviceID  string `json:"deviceId"`
		UpdatedAt string `json:"updatedAt"`
	} `json:"provenance"`
	Policy  Policy `json:"policy"`
	Payload any    `json:"payload"`
}
type pendingPush struct {
	ExpectedHead string   `json:"expectedHead"`
	Candidate    string   `json:"candidateCommit"`
	Epoch        string   `json:"epoch"`
	OperationIDs []string `json:"operationIds"`
	PreviewID    string   `json:"previewId"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// This separate SQLite lock is crash-released and never holds the catalog transaction
// across network IO. Shared record mutations take the same lock.
func (a App) syncOwner(ctx context.Context) (func(), error) {
	if _, err := os.Stat(store.Path(a.DataDir)); err != nil {
		return nil, store.ErrNotInitialized
	}
	return a.syncLock(ctx, "sync-owner.sqlite", "5000")
}

func (a App) syncLock(ctx context.Context, name, timeout string) (func(), error) {
	p := filepath.Join(filepath.Dir(store.Path(a.DataDir)), name)
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.ExecContext(ctx, "PRAGMA busy_timeout="+timeout); err == nil {
		_, err = db.ExecContext(ctx, "BEGIN IMMEDIATE")
	}
	if err != nil {
		db.Close()
		if Classify(err).Code == "store-busy" {
			return nil, Fail("sync-busy", "another local writer or sync owner is active; retry")
		}
		return nil, err
	}
	return func() { db.ExecContext(context.Background(), "ROLLBACK"); db.Close() }, nil
}
func syncGit(ctx context.Context, dir string, input []byte, args ...string) (string, error) {
	out, err := syncGitRaw(ctx, dir, input, args...)
	return strings.TrimSpace(out), err
}
func syncGitRaw(ctx context.Context, dir string, input []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	base := []string{"--git-dir=" + dir, "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "core.attributesFile=" + os.DevNull, "-c", "protocol.ext.allow=never", "-c", "http.followRedirects=false", "-c", "protocol.file.allow=always"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	// Preserve only intentional credential/transport and platform environment, never GIT_* overrides.
	for _, key := range []string{"PATH", "HOME", "USERPROFILE", "SystemRoot", "SYSTEMROOT", "WINDIR", "TMPDIR", "TMP", "TEMP", "GH_TOKEN", "GITHUB_TOKEN", "SSH_AUTH_SOCK"} {
		if v := os.Getenv(key); v != "" && !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=CtxFerry", "GIT_AUTHOR_EMAIL=ctxferry@localhost", "GIT_COMMITTER_NAME=CtxFerry", "GIT_COMMITTER_EMAIL=ctxferry@localhost")
	cmd.Stdin = bytes.NewReader(input)
	var out, diagnostics bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostics
	// Git diagnostics may include helper secrets or URLs: do not forward them.
	if err := cmd.Run(); err != nil {
		message := strings.ToLower(diagnostics.String())
		if len(args) > 0 && args[0] == "merge-base" {
			if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
				return "", Fail("not-ancestor", "commit does not extend history")
			}
		}
		for _, marker := range []string{"agent refused operation", "sign_and_send_pubkey: signing failed", "credential store is locked", "keychain is locked"} {
			if strings.Contains(message, marker) {
				return "", Fail("unlock-required", "unlock the local Git credential helper or SSH agent")
			}
		}
		// A lease may lose either before advertisement or during the server's
		// atomic ref update. Do not treat hook/access rejection as contention.
		staleAdvertisement := strings.Contains(out.String(), "[rejected]") && strings.Contains(out.String(), "(stale info)")
		serverRace := strings.Contains(out.String(), "[remote rejected]") && strings.Contains(out.String(), "(failed to update ref)") && strings.Contains(message, "cannot lock ref") && (strings.Contains(message, "but expected") || strings.Contains(message, "reference already exists"))
		if len(args) > 0 && args[0] == "push" && (staleAdvertisement || serverRace) {
			return "", Fail("lease-rejected", "remote changed; fetch and reconcile before publication")
		}

		for _, marker := range []string{"is corrupt", "corrupt loose object", "is empty", "inflate: data stream error", "unable to unpack", "bad object", "missing blob", "did not send all necessary objects"} {
			if strings.Contains(message, marker) {
				return "", Fail("incomplete", "Git object data is missing or corrupt; local work retained")
			}
		}
		for _, marker := range []string{"authentication failed", "permission denied", "could not read username", "terminal prompts disabled", "http 401", "http 403"} {
			if strings.Contains(message, marker) {
				return "", Fail("auth-required", "system Git authentication or repository access is required")
			}
		}
		if strings.Contains(message, "couldn't find remote ref") {
			return "", Fail("missing-branch", "configured sync branch is missing")
		}
		return "", Fail("git-error", "Git operation failed; check reachability and local Git authentication")
	}
	return syncReadResult(args, out.String()), nil
}
func (a App) managedGit(ctx context.Context) (string, error) {
	parent, err := canonical(filepath.Dir(store.Path(a.DataDir)))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(parent, "sync.git")
	if _, statErr := os.Lstat(dir); statErr == nil {
		dir, err = canonical(dir)
		if err != nil {
			return "", err
		}
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	// A data root nested in a source checkout is never a managed repository location.
	for p := dir; ; p = filepath.Dir(p) {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return "", Fail("invalid-path", "sync data must be outside source checkouts")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err = os.MkdirAll(dir, 0700); err != nil {
			return "", err
		}
		if _, err = syncGit(ctx, dir, nil, "init", "--bare", "--template="); err != nil {
			return "", err
		}
	}
	// URL rewrites and push URLs would disconnect the metadata proof from destination.
	config, err := syncGit(ctx, dir, nil, "config", "--list")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(config, "\n") {
		key := strings.ToLower(strings.SplitN(line, "=", 2)[0])
		if strings.HasPrefix(key, "url.") || strings.HasSuffix(key, ".pushurl") || strings.HasPrefix(key, "include") {
			return "", Fail("unsafe-git-config", "Git URL rewrites, includes and alternate push URLs are not supported for sync")
		}
	}
	return dir, nil
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func syncRemote(raw string) (string, string, error) {
	normalized, err := normalizeRemote(raw)
	if err != nil {
		return "", "", Fail("invalid-remote", "use a credential-free GitHub SSH or HTTPS repository URL")
	}
	// normalizeRemote is for discovery; separately restrict actual transport syntax.
	if !strings.HasPrefix(raw, "https://github.com/") && !strings.HasPrefix(raw, "git@github.com:") {
		return "", "", Fail("invalid-remote", "use GitHub HTTPS or git@github.com SSH")
	}
	name := strings.TrimPrefix(normalized, "github.com/")
	if !repoName.MatchString(name) || strings.Contains(name, "..") || strings.ContainsAny(raw, "?#@") && !strings.HasPrefix(raw, "git@github.com:") {
		return "", "", Fail("invalid-remote", "remote must not contain credentials or URL parameters")
	}
	remote := "https://github.com/" + name + ".git"
	if strings.HasPrefix(raw, "git@") {
		remote = "git@github.com:" + name + ".git"
	}
	return remote, name, nil
}
func metadataToken(ctx context.Context) string {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := os.Getenv(key); token != "" {
			return token
		}
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func privacyCheck(ctx context.Context, c SyncConfig) SyncCheck {
	r := SyncCheck{Privacy: "unknown", Failure: "auth-required"}
	api, remote := syncEndpoints(c.Remote)
	// Reachability remains independently reportable if metadata authentication fails.
	_ = remote
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	token := metadataToken(ctx)
	if token == "" {
		return r
	}
	_, name, err := syncRemote(c.Remote)
	if err != nil {
		return r
	}
	req, err := http.NewRequestWithContext(ctx, "GET", api+"/repos/"+name, nil)
	if err != nil {
		return r
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		r.Failure = "offline"
		return r
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		if resp.StatusCode >= 500 {
			r.Failure = "offline"
		}
		return r
	}
	var m struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		Private  *bool  `json:"private"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024))
	if dec.Decode(&m) != nil || dec.Decode(new(any)) != io.EOF || m.ID <= 0 || !strings.EqualFold(m.FullName, name) || m.Private == nil {
		return r
	}
	r.Identity = strconv.FormatInt(m.ID, 10) + ":" + strings.ToLower(m.FullName)
	if c.Identity != "" && c.Identity != r.Identity {
		return r
	}
	if *m.Private {
		r.Privacy = "private"
		r.Failure = ""
	} else {
		r.Privacy = "public"
		r.Failure = "privacy-unconfirmed"
	}
	return r
}
func loadSync(ctx context.Context, db *sql.DB) (SyncConfig, error) {
	var c SyncConfig
	err := db.QueryRowContext(ctx, "SELECT remote,workspace_id,branch,identity FROM sync_config WHERE singleton=1").Scan(&c.Remote, &c.WorkspaceID, &c.Branch, &c.Identity)
	if err == sql.ErrNoRows {
		return c, Fail("sync-disabled", "run sync enable first")
	}
	return c, err
}
func checkSync(ctx context.Context, dir string, c SyncConfig) SyncCheck {
	r := privacyCheck(ctx, c)
	_, remote := syncEndpoints(c.Remote)
	_, err := syncGit(ctx, dir, nil, "ls-remote", "--", remote)
	r.GitReachable = err == nil
	if err != nil && r.Privacy == "private" {
		r.Failure = Classify(err).Code
	}
	return r
}
func (a App) EnableSync(ctx context.Context, remote, branch string) (SyncResult, error) {
	r := SyncResult{Boundary: plaintextBoundary, State: "disabled"}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return r, err
	}
	defer release()
	remote, _, err = syncRemote(remote)
	if err != nil {
		return r, err
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return r, err
	}
	if branch == "" {
		return r, Fail("usage", "sync branch must not be empty")
	}
	if _, err = syncGit(ctx, dir, nil, "check-ref-format", "refs/heads/"+branch); err != nil {
		return r, Fail("usage", "invalid sync branch")
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	c := SyncConfig{Remote: remote, Branch: branch}
	if err = db.QueryRowContext(ctx, "SELECT id FROM workspace WHERE singleton=1").Scan(&c.WorkspaceID); err != nil {
		return r, err
	}
	check := checkSync(ctx, dir, c)
	r.Check = &check
	if !check.GitReachable || check.Privacy != "private" {
		r.State = "unverified"
		return r, nil
	}
	c.Identity = check.Identity
	err = store.Write(ctx, db, func(conn *sql.Conn) error {
		var n int
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sync_config").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return Fail("sync-already-enabled", "sync is already configured; destination cannot be silently replaced")
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO sync_config VALUES(1,?,?,?,?)", c.Remote, c.WorkspaceID, c.Branch, c.Identity); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, "INSERT INTO sync_state(singleton,epoch) VALUES(1,?)", uuid.NewString())
		return err
	})
	r.Config = &c
	r.State = "enabled"
	return r, err
}

// exportSnapshot is a typed allow-list. No directory walk or generic database dump
// may add files. Scope dependencies are validated before any object is exported.
func exportSnapshot(ctx context.Context, db *sql.DB, c SyncConfig) (map[string][]byte, []string, error) {
	files := map[string][]byte{}
	ops := []string{}
	err := store.Write(ctx, db, func(conn *sql.Conn) error {
		var err error
		files, ops, err = exportConn(ctx, conn, c)
		return err
	})
	return files, ops, err
}
func exportConn(ctx context.Context, conn *sql.Conn, c SyncConfig) (map[string][]byte, []string, error) {
	files := map[string][]byte{}
	ops := []string{}
	err := func() error {
		var epoch, did, stamp string
		if err := conn.QueryRowContext(ctx, "SELECT epoch FROM sync_state WHERE singleton=1").Scan(&epoch); err != nil {
			return err
		}
		if err := conn.QueryRowContext(ctx, "SELECT id,created_at FROM device WHERE singleton=1").Scan(&did, &stamp); err != nil {
			return err
		}
		addJSON := func(path string, v any) error {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			if strings.HasPrefix(path, "records/") && len(b) > MetadataLimit {
				return Fail("size-limit", "published record envelope exceeds 256 KiB")
			}
			files[path] = append(b, '\n')
			return nil
		}
		if err := addJSON("workspace.json", map[string]any{"format": 1, "workspaceId": c.WorkspaceID, "epoch": epoch, "storageMode": "plaintext"}); err != nil {
			return err
		}
		files["README.md"] = []byte("# CtxFerry sync format v1\n\n" + plaintextBoundary + "\n\nPrivate repository access covers the whole workspace; it is not end-to-end encryption. Revoking a device cannot erase downloaded plaintext. History retains deleted content. Rotate any accidentally published secret. Preserve Git history and the local catalog for recovery; do not reset the sync branch.\n")
		add := func(id, kind string, scope Scope, payload any, doc *ContextDoc) error {
			if _, err := uuid.Parse(id); err != nil {
				return Fail("invalid-record", "record ID must be a UUID")
			}
			env := publishedEnvelope{SchemaVersion: 1, ID: id, Kind: kind, WorkspaceID: c.WorkspaceID, Scope: scope, Policy: Policy{"shared", "full"}, Payload: payload}
			env.Provenance.DeviceID = did
			env.Provenance.UpdatedAt = stamp
			if doc != nil {
				env.Deleted = doc.Deleted
				env.Revision = doc.Revision
				env.PreviousRevision = doc.PreviousRevision
				env.OperationID = doc.OperationID
				env.Policy = doc.Policy
				env.Provenance.DeviceID = doc.DeviceID
				env.Provenance.UpdatedAt = doc.UpdatedAt
			} else {
				b, err := json.Marshal(payload)
				if err != nil {
					return err
				}
				var normalized any
				json.Unmarshal(b, &normalized)
				b, _ = json.Marshal(normalized)
				hash := digest(b)
				var previousHash string
				err = conn.QueryRowContext(ctx, "SELECT revision,operation_id,fingerprint,previous_revision FROM export_identities WHERE id=?", id).Scan(&env.Revision, &env.OperationID, &previousHash, &env.PreviousRevision)
				if err != nil && err != sql.ErrNoRows {
					return err
				}
				if previousHash != hash {
					// Older publishers hashed struct field order. Reuse confirmed identity when only encoding changed.
					var baseline []byte
					if e := conn.QueryRowContext(ctx, "SELECT data FROM sync_baselines WHERE path=?", recordPath(id)).Scan(&baseline); e == nil {
						if prior, e := decodeRecord(baseline); e == nil && bytes.Equal(prior.Data, b) && !prior.Deleted {
							env.Revision = prior.Revision
							env.OperationID = prior.OperationID
							env.PreviousRevision = prior.PreviousRevision
							previousHash = hash
							err = nil
							if _, e = conn.ExecContext(ctx, "UPDATE export_identities SET fingerprint=?,revision=?,operation_id=?,previous_revision=? WHERE id=?", hash, env.Revision, env.OperationID, env.PreviousRevision, id); e != nil {
								return e
							}
						}
					}
				}
				if err == sql.ErrNoRows || previousHash != hash {
					env.PreviousRevision = env.Revision
					env.Revision = uuid.NewString()
					env.OperationID = uuid.NewString()
					if _, err = conn.ExecContext(ctx, "INSERT INTO export_identities VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,operation_id=excluded.operation_id,fingerprint=excluded.fingerprint,previous_revision=excluded.previous_revision", id, env.Revision, env.OperationID, hash, env.PreviousRevision); err != nil {
						return err
					}
				}
			}
			for _, v := range []string{env.Revision, env.OperationID, env.Provenance.DeviceID} {
				if _, err := uuid.Parse(v); err != nil {
					return Fail("invalid-record", "invalid record identity or revision")
				}
			}
			ops = append(ops, env.OperationID)
			return addJSON("records/"+strings.ReplaceAll(id, "-", "")[:2]+"/"+id+".json", env)
		}
		projects := map[string]bool{}
		repos := map[string]bool{}
		rows, err := conn.QueryContext(ctx, "SELECT id,slug,created_at FROM projects ORDER BY id")
		if err != nil {
			return err
		}
		type entity struct {
			id, kind string
			scope    Scope
			payload  any
		}
		entities := []entity{}
		for rows.Next() {
			var p Project
			if err = rows.Scan(&p.ID, &p.Slug, &p.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			projects[p.ID] = true
			entities = append(entities, entity{p.ID, "project", Scope{"workspace", c.WorkspaceID}, p})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		rows, err = conn.QueryContext(ctx, "SELECT id FROM repositories ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			repos[id] = true
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		ids := []string{}
		for id := range repos {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			aliases := []string{}
			rows, err = conn.QueryContext(ctx, "SELECT remote FROM repository_aliases WHERE repository_id=? ORDER BY remote", id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var alias string
				if err = rows.Scan(&alias); err != nil {
					rows.Close()
					return err
				}
				normalized, e := normalizeRemote("https://" + alias)
				if e != nil || normalized != alias {
					rows.Close()
					return Fail("invalid-record", "invalid shared repository alias")
				}
				aliases = append(aliases, alias)
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			entities = append(entities, entity{id, "repository", Scope{"workspace", c.WorkspaceID}, map[string]any{"remoteAliases": aliases}})
		}
		rows, err = conn.QueryContext(ctx, "SELECT id,project_id,repository_id,subdirectory FROM memberships ORDER BY id")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, pid, rid, sub string
			if err = rows.Scan(&id, &pid, &rid, &sub); err != nil {
				rows.Close()
				return err
			}
			if !projects[pid] || !repos[rid] || filepath.IsAbs(sub) || strings.Contains(sub, "..") {
				rows.Close()
				return Fail("invalid-reference", "membership has an invalid shared dependency")
			}
			entities = append(entities, entity{id, "membership", Scope{"project", pid}, map[string]any{"repositoryId": rid, "subdirectory": sub}})
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, e := range entities {
			if err = add(e.id, e.kind, e.scope, e.payload, nil); err != nil {
				return err
			}
		}
		configs, err := exportConfigurations(ctx, conn, c.WorkspaceID, projects)
		if err != nil {
			return err
		}
		for _, cfg := range configs {
			if err = add(cfg.id, "configuration", cfg.scope, cfg.value, nil); err != nil {
				return err
			}
		}
		rows, err = conn.QueryContext(ctx, "SELECT r.id,r.project_id,r.kind,r.metadata,b.body FROM records r LEFT JOIN context_bodies b ON b.record_id=r.id ORDER BY r.id")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, pid, kind, raw string
			var body sql.NullString
			if err = rows.Scan(&id, &pid, &kind, &raw, &body); err != nil {
				return err
			}
			if kind == "resource" || kind == "task" || kind == secretBindingKind {
				r, err := decodeRecord([]byte(raw))
				if err != nil {
					return err
				}
				disclosure := "full"
				if kind == secretBindingKind {
					disclosure = "reference"
				}
				if r.Kind != kind || r.Scope.Kind != "project" || !validPolicy(r.Policy) || r.Policy.AgentDisclosure != disclosure ||
					kind == secretBindingKind && r.Policy.Storage != "shared" {
					return Fail("invalid-record", "invalid "+kind+" record scope or policy")
				}
				switch kind {
				case "resource":
					err = validateResourceSpec(r.Resource, r.Policy.Storage == "shared")
				case "task":
					err = validateTask(r.Task)
				case secretBindingKind:
					err = validateSecretBinding(r.SecretBinding)
				}
				if err != nil {
					return err
				}
				if r.Policy.Storage == "local-only" {
					continue
				}
				if !validUUID(r.ID) || r.ID != id || r.Scope.ID != pid || !projects[pid] || r.WorkspaceID != c.WorkspaceID || body.Valid {
					return Fail("invalid-reference", "shared "+kind+" has an invalid Project dependency")
				}
				if _, exists := files[recordPath(id)]; exists {
					return Fail("record-collision", "shared "+kind+" ID collides with another record")
				}
				if err = addJSON(recordPath(id), r.publishedEnvelope); err != nil {
					return err
				}
				ops = append(ops, r.OperationID)
				continue
			}
			if kind != "context" {
				return Fail("unsupported-schema", "unknown record kind blocks export")
			}
			var d ContextDoc
			if err = decodeContext(raw, &d); err != nil {
				return err
			}
			if d.SchemaVersion != 1 || !validPolicy(d.Policy) {
				return Fail("invalid-policy", "unsupported record schema or policy blocks export")
			}
			if d.Policy.Storage == "local-only" {
				continue
			}
			if d.ID != id || d.Kind != "context" || d.Scope.Kind != "project" || d.Scope.ID != pid || !projects[pid] || d.WorkspaceID != c.WorkspaceID || !body.Valid || len(raw) > MetadataLimit || len(body.String) > BodyLimit {
				return Fail("invalid-reference", "shared context has invalid metadata, object or scope dependency")
			}
			data := []byte(body.String)
			hash := digest(data)
			files["objects/"+hash] = data
			payload := map[string]any{"title": d.Title, "purpose": d.Purpose, "source": d.Source, "summary": d.Summary, "observedAt": d.ObservedAt, "validUntil": d.ValidUntil, "body": map[string]any{"type": "text/markdown", "size": len(data), "sha256": hash}}
			// Do not write while the cursor is open: doc envelopes reuse existing revisions.
			if err = add(id, "context", d.Scope, payload, &d); err != nil {
				return err
			}
		}
		return rows.Err()
	}()
	if err == nil {
		err = overlayTombstones(ctx, conn, files)
		if err == nil {
			err = retainDeferredRecords(ctx, conn, files)
		}
	}
	return files, ops, err
}

func snapshotFingerprint(files map[string][]byte, c SyncConfig) string {
	b, _ := json.Marshal(struct {
		Config SyncConfig
		Files  map[string][]byte
	}{c, files})
	return digest(b)
}
func (a App) PreviewSync(ctx context.Context) (SyncResult, error) {
	r := SyncResult{Boundary: plaintextBoundary, State: "preview"}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return r, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	c, err := loadSync(ctx, db)
	if err != nil {
		return r, err
	}
	r.Config = &c
	files, _, err := exportSnapshot(ctx, db, c)
	if err != nil {
		return r, err
	}
	fingerprint := snapshotFingerprint(files, c)
	r.PreviewID = fingerprint
	paths := []string{}
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		r.Files = append(r.Files, UploadFile{path, len(files[path]), digest(files[path])})
	}
	raw, err := json.Marshal(files)
	if err != nil {
		return r, err
	}
	_, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO sync_previews VALUES(?,?,?)", fingerprint, fingerprint, string(raw))
	return r, err
}

// Build trees directly from allow-listed bytes. Neither working trees nor attributes
// participate, so a published .gitattributes cannot invoke filters.
type gitTree struct {
	files map[string][]byte
	dirs  map[string]*gitTree
}

func candidateCommit(ctx context.Context, dir string, files map[string][]byte, parent ...string) (string, error) {
	root := &gitTree{map[string][]byte{}, map[string]*gitTree{}}
	for path, data := range files {
		parts := strings.Split(path, "/")
		node := root
		for _, p := range parts[:len(parts)-1] {
			if node.dirs[p] == nil {
				node.dirs[p] = &gitTree{map[string][]byte{}, map[string]*gitTree{}}
			}
			node = node.dirs[p]
		}
		node.files[parts[len(parts)-1]] = data
	}
	var write func(*gitTree) (string, error)
	write = func(node *gitTree) (string, error) {
		lines := []string{}
		for name, data := range node.files {
			hash, err := syncGit(ctx, dir, data, "hash-object", "-w", "--no-filters", "--stdin")
			if err != nil {
				return "", err
			}
			lines = append(lines, "100644 blob "+hash+"\t"+name+"\x00")
		}
		for name, child := range node.dirs {
			hash, err := write(child)
			if err != nil {
				return "", err
			}
			lines = append(lines, "040000 tree "+hash+"\t"+name+"\x00")
		}
		sort.Strings(lines)
		return syncGit(ctx, dir, []byte(strings.Join(lines, "")), "mktree", "-z")
	}
	tree, err := write(root)
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree}
	if len(parent) > 0 {
		args = append(args, "-p", parent[0])
	}
	return syncGit(ctx, dir, []byte("Publish CtxFerry plaintext workspace\n"), args...)
}
func (a App) initSync(ctx context.Context, previewID string) (r SyncResult, err error) {
	r = SyncResult{Boundary: plaintextBoundary, State: "pending"}
	if previewID == "" {
		return r, Fail("usage", "explicit sync init requires --preview <id>")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return r, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	defer func() {
		if err != nil {
			persistSyncFailure(db, err)
		}
	}()
	c, err := loadSync(ctx, db)
	if err != nil {
		return r, err
	}
	r.Config = &c
	dir, err := a.managedGit(ctx)
	if err != nil {
		return r, err
	}
	files, ops, err := exportSnapshot(ctx, db, c)
	if err != nil {
		return r, err
	}
	var fingerprint string
	if err = db.QueryRowContext(ctx, "SELECT fingerprint FROM sync_previews WHERE id=?", previewID).Scan(&fingerprint); err != nil {
		return r, Fail("preview-required", "run sync preview and supply its ID")
	}
	if fingerprint != snapshotFingerprint(files, c) {
		return r, Fail("stale-preview", "shared snapshot changed; inspect a new sync preview")
	}
	check := checkSync(ctx, dir, c)
	r.Check = &check
	if !check.GitReachable || check.Privacy != "private" {
		code := check.Failure
		if code == "" || code == "auth-required" {
			code = "privacy-unconfirmed"
		}
		return r, &Error{Code: code, Message: "upload refused: Git reachability and authenticated private repository identity are required", Details: check}
	}
	var epoch, confirmed, raw string
	if err = db.QueryRowContext(ctx, "SELECT epoch,confirmed_head,pending FROM sync_state WHERE singleton=1").Scan(&epoch, &confirmed, &raw); err != nil {
		return r, err
	}
	if confirmed != "" {
		return r, Fail("already-published", "workspace already initialized; use routine sync when available")
	}
	var pending pendingPush
	if raw != "" {
		if json.Unmarshal([]byte(raw), &pending) != nil {
			return r, Fail("invalid-pending", "pending push is invalid; preserving local state")
		}
		if pending.PreviewID != previewID {
			return r, Fail("push-pending", "a prior push must be reconciled before publishing another snapshot")
		}
	} else {
		candidate, err := candidateCommit(ctx, dir, files)
		if err != nil {
			return r, err
		}
		pending = pendingPush{ExpectedHead: "", Candidate: candidate, Epoch: epoch, OperationIDs: ops, PreviewID: previewID}
		if _, err = syncGit(ctx, dir, nil, "update-ref", "refs/ctxferry/pending", candidate); err != nil {
			return r, err
		}
		encoded, _ := json.Marshal(pending)
		// FULL synchronous catalog commit happens before any push invocation.
		if _, err = db.ExecContext(ctx, "UPDATE sync_state SET pending=? WHERE singleton=1", string(encoded)); err != nil {
			return r, err
		}
	}
	parents, err := syncGit(ctx, dir, nil, "rev-list", "--parents", "-n", "1", pending.Candidate)
	if err != nil {
		return r, err
	}
	if len(strings.Fields(parents)) != 1 || strings.Fields(parents)[0] != pending.Candidate {
		return r, Fail("invalid-pending", "initial candidate must be a root commit")
	}
	exists, err := inspectInitialization(ctx, dir, c, pending)
	if err != nil {
		return r, err
	}
	if !exists {
		if err = newSyncBackend().Publish(ctx, dir, c, pending); err != nil {
			if Classify(err).Code == "lease-rejected" {
				_, inspectErr := inspectInitialization(ctx, dir, c, pending)
				if inspectErr != nil {
					return r, inspectErr
				}
			}
			return r, err
		}
	}
	if err = store.Write(ctx, db, func(conn *sql.Conn) error { return saveBaseline(ctx, conn, files, pending.Candidate, true) }); err != nil {
		return r, err
	}
	r.State = "synced"
	r.Head = pending.Candidate
	return r, nil
}
func (a App) SyncStatus(ctx context.Context) (SyncResult, error) {
	r := SyncResult{Boundary: plaintextBoundary, State: "disabled"}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return r, err
	}
	defer db.Close()
	return readSyncStatus(ctx, db)
}

func readSyncStatus(ctx context.Context, db *sql.DB) (SyncResult, error) {
	r := SyncResult{Boundary: plaintextBoundary, State: "disabled"}
	c, err := loadSync(ctx, db)
	if err != nil {
		return r, err
	}
	r.Config = &c
	var pending, stamp, outcome string
	if err = db.QueryRowContext(ctx, "SELECT confirmed_head,pending,last_sync_at,outcome FROM sync_state WHERE singleton=1").Scan(&r.Head, &pending, &stamp, &outcome); err != nil {
		return r, err
	}
	if stamp != "" {
		r.LastSyncAt = &stamp
	}
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sync_outbox").Scan(&r.Pending); err != nil {
		return r, err
	}
	r.State = "enabled"
	if r.Head != "" {
		r.State = "synced"
	}
	if pending != "" || r.Pending > 0 {
		r.State = "pending"
	}
	if outcome != "" && outcome != "synced" {
		r.State = outcome
	}
	r.Conflicts, r.Blocked, err = conflictStatus(ctx, db)
	if err != nil {
		return r, err
	}
	if len(r.Conflicts) > 0 {
		// Keep conflict evidence visible even offline, but do not hide a
		// recovery barrier that stops safe validation or publication.
		switch outcome {
		case "history-rewritten", "incomplete", "unsupported-schema", "unlock-required", "auth-required":
		default:
			r.State = "conflict"
		}
	}
	return r, nil
}
func (a App) CheckSync(ctx context.Context) (SyncResult, error) {
	r, err := a.SyncStatus(ctx)
	if err != nil {
		return r, err
	}
	dir, err := a.managedGit(ctx)
	if err != nil {
		return r, err
	}
	check := checkSync(ctx, dir, *r.Config)
	r.Check = &check
	return r, nil
}
