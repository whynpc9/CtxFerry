package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type NoteFinding struct {
	Type  string `json:"type"`
	Field string `json:"field,omitempty"`
}

type NoteScan struct {
	Findings []NoteFinding `json:"findings"`
}

type NoteImport struct {
	Title, Summary string
	Policy         Policy
}

type NoteImportReceipt struct {
	DocumentID string `json:"documentId"`
	Policy     Policy `json:"policy"`
	NoteScan
}

var (
	noteField       = regexp.MustCompile("(?m)^[ \\t]*(?:[-*][ \\t]+)?(?:export[ \\t]+)?[\"`]?(?P<field>[A-Za-z_][A-Za-z0-9_.-]{0,63})[\"`]?[ \\t]*[:=][ \\t]*[^ \\t\\r\\n]")
	credentialField = regexp.MustCompile(`(?i)(?:password|passwd|secret|token|apikey|privatekey|clientsecret|databaseurl|authkey)$`)
	credentialToken = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|sk-(?:proj-)?[A-Za-z0-9_-]{16,}|(?:AKIA|ASIA)[A-Z0-9]{16})\b`)
	privateKey      = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`)
)

// ScanOnboardingNote reads only the selected note and returns no matched values.
func (a App) ScanOnboardingNote(path string) (NoteScan, error) {
	_, body, err := readOnboardingNote(path)
	if err != nil {
		return NoteScan{}, err
	}
	return scanOnboardingNote(body), nil
}

func scanOnboardingNote(body string) NoteScan {
	report := NoteScan{Findings: []NoteFinding{}}
	seen := map[string]bool{}
	for _, match := range noteField.FindAllStringSubmatch(body, -1) {
		field := match[1]
		normalized := strings.NewReplacer("_", "", "-", "", ".", "").Replace(field)
		if !seen[field] && credentialField.MatchString(normalized) && !credentialToken.MatchString(field) {
			seen[field] = true
			report.Findings = append(report.Findings, NoteFinding{Type: "credential-like-field", Field: field})
		}
	}
	if credentialToken.MatchString(body) {
		report.Findings = append(report.Findings, NoteFinding{Type: "credential-like-token"})
	}
	if privateKey.MatchString(body) {
		report.Findings = append(report.Findings, NoteFinding{Type: "private-key"})
	}
	return report
}

func readOnboardingNote(path string) (string, string, error) {
	invalid := func() error {
		return Fail("invalid-note", "choose a regular UTF-8 Markdown or text note (.md, .markdown or .txt)")
	}
	base := strings.ToLower(filepath.Base(path))
	switch filepath.Ext(base) {
	case ".md", ".markdown", ".txt":
	default:
		return "", "", invalid()
	}
	if strings.HasPrefix(base, ".env.") {
		return "", "", invalid()
	}
	parent, err := canonical(filepath.Dir(path))
	if err != nil {
		return "", "", invalid()
	}
	path = filepath.Join(parent, filepath.Base(path))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", invalid()
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", invalid()
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", "", invalid()
	}
	body, err := io.ReadAll(io.LimitReader(f, BodyLimit+1))
	if err != nil || !utf8.Valid(body) {
		return "", "", invalid()
	}
	if len(body) > BodyLimit {
		return "", "", Fail("size-limit", "note exceeds the context body limit of 1 MiB")
	}
	return path, string(body), nil
}

// ImportOnboardingNote classifies and scans the actual bytes written. The scan
// is advisory. The receipt, document and shared outbox update commit together.
func (a App) ImportOnboardingNote(ctx context.Context, path string, in NoteImport) (NoteImportReceipt, error) {
	var receipt NoteImportReceipt
	if !validPolicy(in.Policy) {
		return receipt, Fail("invalid-policy", "choose storage local-only/shared and disclosure full/summary/reference/local-tool")
	}
	if strings.TrimSpace(in.Title) == "" || !utf8.ValidString(in.Title+in.Summary) {
		return receipt, Fail("invalid-note", "a UTF-8 note title is required")
	}
	release, err := a.syncOwner(ctx)
	if err != nil {
		return receipt, err
	}
	defer release()
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return receipt, err
	}
	defer db.Close()
	state, err := readOnboarding(ctx, db)
	if err != nil {
		return receipt, err
	}
	if state.Step != "notes" {
		return receipt, Fail("onboarding-step", "confirm the Project before importing notes; completed onboarding is closed to imports")
	}
	if _, err = selectProject(ctx, db, state.ProjectID); err != nil {
		return receipt, err
	}
	path, body, err := readOnboardingNote(path)
	if err != nil {
		return receipt, err
	}
	receipt = NoteImportReceipt{Policy: in.Policy, NoteScan: scanOnboardingNote(body)}
	wire, err := json.Marshal(struct {
		Import NoteImport
		Body   string
	}{in, body})
	if err != nil {
		return receipt, err
	}
	fingerprint := digest(wire)
	if in.Policy.Storage == "shared" {
		if err = a.prepareBaseline(ctx, db); err != nil {
			return receipt, err
		}
	}
	err = store.Write(ctx, db, func(c *sql.Conn) error {
		var previous string
		err := c.QueryRowContext(ctx, "SELECT fingerprint,document_id FROM onboarding_notes WHERE path=?", path).
			Scan(&previous, &receipt.DocumentID)
		if err == nil {
			if previous != fingerprint {
				return Fail("note-already-imported", "this note is already imported; edit its context document to change its content or policy")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		input := PutContext{
			Title: in.Title, Summary: in.Summary, Body: body, Purpose: "note", Source: "onboarding-import",
			Storage: in.Policy.Storage, Disclosure: in.Policy.AgentDisclosure,
		}
		var document ContextDoc
		if err = writeContext(ctx, c, state.ProjectID, input, &document); err != nil {
			return err
		}
		if in.Policy.Storage == "shared" {
			if err = queueChanges(ctx, c); err != nil {
				return err
			}
		}
		receipt.DocumentID = document.ID
		_, err = c.ExecContext(ctx, "INSERT INTO onboarding_notes(path,fingerprint,document_id) VALUES(?,?,?)", path, fingerprint, document.ID)
		return err
	})
	return receipt, err
}
