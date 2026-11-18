package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/store"
)

const MetadataLimit = 256 * 1024
const BodyLimit = 1024 * 1024

type Scope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type Policy struct {
	Storage         string `json:"storage"`
	AgentDisclosure string `json:"agentDisclosure"`
}
type ContextDoc struct {
	SchemaVersion    int     `json:"schemaVersion"`
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	WorkspaceID      string  `json:"workspaceId"`
	Scope            Scope   `json:"scope"`
	Revision         string  `json:"revision"`
	PreviousRevision string  `json:"previousRevision"`
	OperationID      string  `json:"operationId"`
	DeviceID         string  `json:"deviceId"`
	Title            string  `json:"title"`
	Purpose          string  `json:"purpose"`
	Source           string  `json:"source"`
	UpdatedAt        string  `json:"updatedAt"`
	ObservedAt       *string `json:"observedAt"`
	ValidUntil       string  `json:"validUntil,omitempty"`
	Freshness        string  `json:"freshness"`
	Policy           Policy  `json:"policy"`
	Summary          *string `json:"summary,omitempty"`
	Body             *string `json:"body,omitempty"`
}
type PutContext struct {
	ID, IfRevision, Title, Purpose, Source, Summary, Body, Storage, Disclosure, ValidUntil string
}
type ContextResult struct {
	Project     Project      `json:"project"`
	Source      string       `json:"source"`
	UpdatedAt   string       `json:"updatedAt"`
	ObservedAt  *string      `json:"observedAt"`
	Revision    *string      `json:"revision"`
	Freshness   string       `json:"freshness"`
	LastSyncAt  *string      `json:"lastSyncAt"`
	Document    *ContextDoc  `json:"document,omitempty"`
	Documents   []ContextDoc `json:"documents"`
	Summary     *string      `json:"summary"`
	Environment *Environment `json:"environment,omitempty"`
	Truncated   bool         `json:"truncated"`
	NextCursor  string       `json:"nextCursor,omitempty"`
}
type Environment struct {
	DeviceID string `json:"deviceId"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	CWD      string `json:"cwd"`
}

// project inspect and context operations use the same resolution seam.
func (a App) contextProject(ctx context.Context, selector, cwd string) (ContextResult, error) {
	p, e := a.InspectProject(ctx, selector, cwd)
	return ContextResult{Project: p.Project, Source: "local-store", UpdatedAt: p.Project.CreatedAt, Freshness: "unknown"}, e
}
func validPolicy(p Policy) bool {
	return (p.Storage == "local-only" || p.Storage == "shared") && (p.AgentDisclosure == "full" || p.AgentDisclosure == "summary" || p.AgentDisclosure == "reference" || p.AgentDisclosure == "local-tool")
}

// projectDoc is the sole disclosure projection. Never derive summaries from bodies.
func projectDoc(d ContextDoc, body bool) (ContextDoc, error) {
	if d.SchemaVersion != 1 {
		return ContextDoc{}, Fail("unsupported-schema", "unsupported context record schema")
	}
	if !validPolicy(d.Policy) {
		return ContextDoc{}, Fail("invalid-policy", "unknown context policy; refusing disclosure")
	}
	if d.Policy.AgentDisclosure != "full" || !body {
		d.Body = nil
	}
	if d.Policy.AgentDisclosure == "reference" || d.Policy.AgentDisclosure == "local-tool" {
		d.Summary = nil
	}
	d.Freshness = "unknown"
	if d.ValidUntil != "" {
		expiry, e := time.Parse(time.RFC3339, d.ValidUntil)
		if e != nil {
			return ContextDoc{}, Fail("invalid-record", "invalid freshness timestamp")
		}
		if time.Now().After(expiry) {
			d.Freshness = "stale"
		}
	}
	return d, nil
}
func (a App) PutContext(ctx context.Context, selector, cwd string, in PutContext) (ContextResult, error) {

	release, lockErr := a.syncOwner(ctx)
	if lockErr != nil {
		return ContextResult{}, lockErr
	}
	defer release()
	r, e := a.contextProject(ctx, selector, cwd)
	if e != nil {
		return r, e
	}
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Purpose) == "" || strings.TrimSpace(in.Source) == "" {
		return r, Fail("invalid-document", "title, purpose and source are required")
	}
	if !validPolicy(Policy{in.Storage, in.Disclosure}) {
		return r, Fail("invalid-policy", "choose storage local-only/shared and disclosure full/summary/reference/local-tool")
	}
	if !utf8.ValidString(in.Body) || !utf8.ValidString(in.Title+in.Purpose+in.Source+in.Summary) {
		return r, Fail("invalid-document", "document must be UTF-8")
	}
	if len(in.Body) > BodyLimit {
		return r, Fail("size-limit", fmt.Sprintf("Markdown body exceeds %d bytes", BodyLimit))
	}
	if in.ValidUntil != "" {
		if _, e = time.Parse(time.RFC3339, in.ValidUntil); e != nil {
			return r, Fail("invalid-document", "valid-until must be RFC3339")
		}
	}
	if (in.ID == "") != (in.IfRevision == "") {
		return r, Fail("usage", "replacement requires document UUID and --if-revision; creation accepts neither")
	}
	if in.ID != "" {
		if _, e = uuid.Parse(in.ID); e != nil {
			return r, Fail("usage", "document ID must be a UUID")
		}
	}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return r, e
	}
	defer db.Close()
	e = store.Write(ctx, db, func(c *sql.Conn) error {
		id := in.ID
		previous := ""
		if id != "" {
			var raw string
			err := c.QueryRowContext(ctx, "SELECT metadata FROM records WHERE id=? AND project_id=? AND kind='context'", id, r.Project.ID).Scan(&raw)
			if errors.Is(err, sql.ErrNoRows) {
				return Fail("document-not-found", "document not found in this project")
			}
			if err != nil {
				return err
			}
			var old ContextDoc
			if err = decodeContext(raw, &old); err != nil {
				return err
			}
			if _, err = projectDoc(old, false); err != nil {
				return err
			}
			previous = old.Revision
			if previous != in.IfRevision {
				return Fail("revision-conflict", "document changed; read its current revision before replacing")
			}
		} else {
			var err error
			id, err = newID()
			if err != nil {
				return err
			}
		}
		revision, err := newID()
		if err != nil {
			return err
		}
		operation, err := newID()
		if err != nil {
			return err
		}
		d := ContextDoc{SchemaVersion: 1, ID: id, Kind: "context", Scope: Scope{"project", r.Project.ID}, Revision: revision, PreviousRevision: previous, OperationID: operation, Title: in.Title, Purpose: in.Purpose, Source: in.Source, UpdatedAt: now(), ValidUntil: in.ValidUntil, Freshness: "unknown", Policy: Policy{in.Storage, in.Disclosure}, Summary: &in.Summary}
		if err = c.QueryRowContext(ctx, "SELECT id FROM workspace WHERE singleton=1").Scan(&d.WorkspaceID); err != nil {
			return err
		}
		if err = c.QueryRowContext(ctx, "SELECT id FROM device WHERE singleton=1").Scan(&d.DeviceID); err != nil {
			return err
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		if len(raw) > MetadataLimit {
			return Fail("size-limit", fmt.Sprintf("structured context record exceeds %d bytes (excluding Markdown body)", MetadataLimit))
		}
		if in.Purpose == "project-summary" {
			var count int
			if err = c.QueryRowContext(ctx, `SELECT count(*) FROM records WHERE project_id=? AND id<>? AND json_extract(metadata,'$.purpose')='project-summary'`, r.Project.ID, id).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return Fail("summary-conflict", "project already has a project-summary document")
			}
		}
		if _, err = c.ExecContext(ctx, `INSERT INTO records VALUES(?,?,'context',?) ON CONFLICT(id) DO UPDATE SET metadata=excluded.metadata`, id, r.Project.ID, string(raw)); err != nil {
			return err
		}
		if _, err = c.ExecContext(ctx, `INSERT INTO context_bodies VALUES(?,?) ON CONFLICT(record_id) DO UPDATE SET body=excluded.body`, id, in.Body); err != nil {
			return err
		}
		d.Body = &in.Body
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
		if _, err = c.ExecContext(ctx, "DELETE FROM context_fts WHERE id=?", id); err != nil {
			return err
		}
		if _, err = c.ExecContext(ctx, "INSERT INTO context_fts(id,project_id,content) VALUES(?,?,?)", id, r.Project.ID, content); err != nil {
			return err
		}
		// Receipts contain metadata only, even for full disclosure.
		visible.Body = nil
		visible.Summary = nil
		r.Document = &visible
		r.UpdatedAt = d.UpdatedAt
		return nil
	})
	return r, e
}

func (a App) QueryContext(ctx context.Context, selector, cwd, mode, arg, cursor string) (ContextResult, error) {
	r, e := a.contextProject(ctx, selector, cwd)
	if e != nil {
		return r, e
	}
	db, e := store.Open(ctx, a.DataDir, false)
	if e != nil {
		return r, e
	}
	defer db.Close()
	if mode == "show" {
		var did string
		if e = db.QueryRowContext(ctx, "SELECT id FROM device WHERE singleton=1").Scan(&did); e != nil {
			return r, e
		}
		r.Environment = &Environment{did, runtime.GOOS, runtime.GOARCH, cwd}
	}
	bodyColumn := "''"
	if mode == "get" {
		bodyColumn = "b.body"
	}
	query := `SELECT r.metadata,` + bodyColumn + ` FROM records r JOIN context_bodies b ON b.record_id=r.id WHERE r.project_id=? AND r.kind='context'`
	args := []any{r.Project.ID}
	switch mode {
	case "get":
		query += " AND r.id=?"
		args = append(args, arg)
	case "show":
		query += " AND r.id>?"
		args = append(args, cursor)
	case "search":
		if len(arg) > 1024 || strings.TrimSpace(arg) == "" {
			return r, Fail("usage", "search requires 1 to 1024 bytes of keywords")
		}
		terms := strings.Fields(arg)
		for i, t := range terms {
			terms[i] = `"` + strings.ReplaceAll(t, `"`, `""`) + `"`
		}
		query += ` AND r.id IN (SELECT id FROM context_fts WHERE context_fts MATCH ? AND project_id=?)`
		args = append(args, strings.Join(terms, " AND "), r.Project.ID)
	default:
		return r, Fail("usage", "unknown context query")
	}
	query += " ORDER BY r.id LIMIT 101"
	rows, e := db.QueryContext(ctx, query, args...)
	if e != nil {
		return r, e
	}
	defer rows.Close()
	r.Documents = []ContextDoc{}
	for rows.Next() {
		var raw, body string
		if e = rows.Scan(&raw, &body); e != nil {
			return r, e
		}
		var d ContextDoc
		if e = decodeContext(raw, &d); e != nil {
			return r, e
		}
		d.Body = &body
		d, e = projectDoc(d, mode == "get")
		if e != nil {
			return r, e
		}
		if mode == "get" {
			r.Document = &d
			r.Documents = nil
			r.UpdatedAt = d.UpdatedAt
			continue
		}
		if len(r.Documents) == 100 {
			r.Truncated = true
			break
		}
		// Index entries are bounded metadata; get retrieves full approved fields.
		d.Title = shortText(d.Title, 160)
		d.Purpose = shortText(d.Purpose, 160)
		d.Source = shortText(d.Source, 160)
		if d.Summary != nil {
			s := shortText(*d.Summary, 512)
			d.Summary = &s
		}
		r.Documents = append(r.Documents, d)
	}
	if e = rows.Err(); e != nil {
		return r, e
	}
	rows.Close()
	if mode == "get" && r.Document == nil {
		return r, Fail("document-not-found", "document not found in this project")
	}
	if r.Truncated && len(r.Documents) > 0 {
		r.NextCursor = r.Documents[len(r.Documents)-1].ID
	}
	if mode == "show" {
		var raw string
		e = db.QueryRowContext(ctx, `SELECT metadata FROM records WHERE project_id=? AND kind='context' AND json_extract(metadata,'$.purpose')='project-summary'`, r.Project.ID).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return r, e
		}
		if e == nil {
			var d ContextDoc
			if e = decodeContext(raw, &d); e != nil {
				return r, e
			}
			d, e = projectDoc(d, false)
			if e != nil {
				return r, e
			}
			if d.Summary != nil {
				s := shortText(*d.Summary, 512)
				r.Summary = &s
			}
		}
	}
	return r, nil
}
func shortText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s + "…"
}

// Unknown fields are rejected rather than silently discarded on replacement.
func decodeContext(raw string, d *ContextDoc) error {
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(d); err != nil {
		return Fail("unsupported-schema", "context record contains unsupported fields or invalid data")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Fail("invalid-record", "context record is not a single object")
	}
	return nil
}
