package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/whynpc9/CtxFerry/internal/store"
)

type DesktopSkill struct {
	Name   string
	Shared *SkillPackage
}

// ListDesktopSkills reads the workspace catalog or a Project's shared selection.
// Local-only packages and retained installations appear in the device-wide view.
func (a App) ListDesktopSkills(ctx context.Context, project string) ([]DesktopSkill, error) {
	db, err := store.OpenReadOnly(ctx, a.DataDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var workspace, device string
	if err := tx.QueryRowContext(ctx, "SELECT w.id,d.id FROM workspace w CROSS JOIN device d").Scan(&workspace, &device); err != nil {
		return nil, err
	}

	entries := map[string]DesktopSkill{}
	if project != "" {
		scope, name, err := resolveSkillGroup(ctx, tx, workspace, "project:"+project)
		if err != nil {
			return nil, err
		}
		if err := requireSubscription(ctx, tx, scope.ID); err != nil {
			return nil, err
		}
		id := skillRecordID(workspace, skillGroupKind, scope, name)
		if err := requireNoConflict(ctx, tx, id); err != nil {
			return nil, err
		}
		r, err := loadSkillRecord(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) || err == nil && r.Deleted {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if err := r.validateSkillRecord(workspace); err != nil {
			return nil, err
		}
		for _, name := range r.SkillGroup.Skills {
			pin, err := sharedSkillForPlan(ctx, tx, workspace, name)
			if err != nil {
				return nil, err
			}
			entries[name] = DesktopSkill{Name: name, Shared: &pin.SkillPin}
		}
	} else {
		if err := readDesktopSkillPins(ctx, tx, workspace, entries); err != nil {
			return nil, err
		}
		if err := a.readDesktopLocalSkills(device, entries); err != nil {
			return nil, err
		}
	}

	out := make([]DesktopSkill, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b DesktopSkill) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func readDesktopSkillPins(ctx context.Context, tx *sql.Tx, workspace string, entries map[string]DesktopSkill) error {
	rows, err := tx.QueryContext(ctx, "SELECT metadata FROM skill_records WHERE json_extract(metadata,'$.kind')=?", skillPinKind)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		r, err := decodeRecord(raw)
		if err != nil {
			return err
		}
		if r.Deleted {
			continue
		}
		if err := r.validateSkillRecord(workspace); err != nil {
			return err
		}
		pkg := r.SkillPin
		entries[pkg.Name] = DesktopSkill{Name: pkg.Name, Shared: &pkg}
	}
	return rows.Err()
}

func (a App) readDesktopLocalSkills(device string, entries map[string]DesktopSkill) error {
	packages, err := os.ReadDir(filepath.Join(a.skillDir(), "packages"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, dir := range packages {
		if dir.IsDir() && validSkillName(dir.Name()) {
			entry := entries[dir.Name()]
			entry.Name = dir.Name()
			entries[entry.Name] = entry
		}
	}
	dir := filepath.Join(a.skillDir(), "installs")
	receipts, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, file := range receipts {
		id, ok := strings.CutSuffix(file.Name(), ".json")
		if !ok || !validSkillVersion(id) {
			continue
		}
		var receipt skillReceipt
		if err := readSkillJSON(filepath.Join(dir, file.Name()), &receipt); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		name := filepath.Base(receipt.Path)
		if receipt.DeviceID == device && validSkillName(name) && digest([]byte(receipt.Path)) == id {
			entry := entries[name]
			entry.Name = name
			entries[name] = entry
		}
	}
	return nil
}

type DesktopSkillTarget struct {
	Path, Mode, Status  string
	Installed           *SkillPackage
	Available           *SkillPackage
	Shared, CanRollback bool
	Issues              []SkillProblem
}

// ReadDesktopSkillTarget inspects the installation receipt independently of the
// currently approved shared pin, which can be newer than the local installation.
func (a App) ReadDesktopSkillTarget(ctx context.Context, name, target string, symlink bool) (DesktopSkillTarget, error) {
	p, err := a.openSkillProjection(name, target)
	out := DesktopSkillTarget{Path: p.Path, Status: "not-installed"}
	if err != nil {
		return out, err
	}
	receipt := p.receipt
	if receipt.State == "" {
		err = a.planSelectedSkill(ctx, &p, name, symlink)
		out.Mode, out.Issues = p.Mode, p.Issues
		if p.Package.Name != "" {
			out.Available = &p.Package
		}
		return out, err
	}
	device, err := a.skillDevice(ctx)
	if err != nil {
		return out, err
	}
	if receipt.DeviceID != device {
		return out, Fail("skill-name-collision", "the installation receipt belongs to a different device")
	}
	out.Mode, out.Status, out.Shared = receipt.Mode, receipt.State, receipt.Shared
	if receipt.State == "pending" {
		out.Status = "recovery-pending"
	}
	if err := p.planInstalled(name, receipt.Mode == "symlink"); err != nil {
		return out, err
	}
	if receipt.State == "installed" || receipt.Replaces != nil {
		out.Installed = &p.Package
	}
	out.Issues = p.Issues
	out.CanRollback = receipt.State == "installed" && receipt.Previous != nil
	return out, nil
}

type DesktopSkillText struct {
	Contents string
	Digest   string
	Kind     string // text, binary, large, or unavailable
	Exec     bool
}

type DesktopSkillDiff struct {
	Path                 string
	Before, After, Local *DesktopSkillText
	LocalChanged         bool
}

type DesktopSkillPreview struct {
	Status, Path, Mode, Link, Confirmation string
	Before, After                          *SkillPackage
	Files                                  []DesktopSkillDiff
	Issues                                 []SkillProblem
}

func (p skillProjection) installConfirmation() string {
	if p.Status != "ready" && p.Status != "recovery-pending" {
		return ""
	}
	wire, _ := json.Marshal(struct {
		Plan    SkillPlan
		Receipt skillReceipt
		Shared  bool
	}{p.SkillPlan, p.receipt, p.shared})
	return digest(wire)
}

func (a App) PreviewDesktopSkillInstall(ctx context.Context, name, target string, symlink bool) (DesktopSkillPreview, error) {
	p, err := a.openSkillProjection(name, target)
	if err == nil {
		err = a.planSelectedSkill(ctx, &p, name, symlink)
	}
	if err != nil {
		return DesktopSkillPreview{}, err
	}
	out := DesktopSkillPreview{
		Status: p.Status, Path: p.Path, Mode: p.Mode, Link: p.LinkSource,
		After: &p.Package, Issues: p.Issues, Confirmation: p.installConfirmation(),
	}
	if p.Status == "installed" {
		return out, nil
	}
	data, err := a.skillDiffData(ctx, p)
	if err != nil {
		return out, err
	}
	for _, file := range p.Package.Files {
		out.Files = append(out.Files, DesktopSkillDiff{Path: file.Path, After: skillDiffText(&file, data)})
	}
	return out, nil
}

func (a App) ApplyDesktopSkill(ctx context.Context, name, target string, symlink bool, confirmation string) (SkillPlan, error) {
	if confirmation == "" {
		return SkillPlan{}, Fail("usage", "confirm a Skill installation preview before applying")
	}
	return a.applySkill(ctx, name, target, symlink, confirmation)
}

func (a App) PreviewDesktopSkillChange(ctx context.Context, input SkillChangeInput) (DesktopSkillPreview, error) {
	input.Confirm = ""
	var files []DesktopSkillDiff
	plan, err := a.changeSkill(ctx, input, func(old, next skillProjection, payload skillPayload, plan SkillChangePlan) error {
		before, err := a.skillDiffData(ctx, old)
		if err != nil {
			return err
		}
		after := payload.data
		if after == nil {
			after, err = a.skillDiffData(ctx, next)
			if err != nil {
				return err
			}
		}
		var local skillPayload
		if old.Mode == "copy" && len(old.Issues) != 0 {
			identity, err := skillIdentityIfPresent(old.Path)
			if err != nil {
				return err
			}
			if identity == old.receipt.Identity {
				if info, err := os.Lstat(old.Path); err == nil && info.IsDir() {
					snapshot, _, err := readSkillDirectory(old.Path, false)
					if err == nil {
						local = snapshot
					}
				}
			}
		}
		diff := make(map[string]DesktopSkillDiff, len(plan.Changes))
		for _, change := range plan.Changes {
			diff[change.Path] = DesktopSkillDiff{
				Path: change.Path, Before: skillDiffText(change.Before, before), After: skillDiffText(change.After, after),
			}
		}
		oldFiles, newFiles, localFiles := skillFilesByPath(old.Package.Files), skillFilesByPath(next.Package.Files), skillFilesByPath(local.files)
		for _, issue := range old.Issues {
			if issue.Code != "skill-external-edit" {
				continue
			}
			for _, path := range issue.Paths {
				rel, err := filepath.Rel(old.Path, path)
				rel = filepath.ToSlash(rel)
				if err != nil || !validateSkillPath(rel) {
					continue
				}
				current := skillDiffText(localFiles[rel], local.data)
				if local.data == nil {
					current = &DesktopSkillText{Kind: "unavailable"}
				}
				diff[rel] = DesktopSkillDiff{
					Path: rel, LocalChanged: true, Local: current,
					Before: skillDiffText(oldFiles[rel], before), After: skillDiffText(newFiles[rel], after),
				}
			}
		}
		for _, file := range diff {
			files = append(files, file)
		}
		slices.SortFunc(files, func(a, b DesktopSkillDiff) int { return strings.Compare(a.Path, b.Path) })
		return nil
	})
	return DesktopSkillPreview{
		Status: plan.Status, Path: plan.Path, Mode: plan.Mode, Link: plan.AfterLink, Confirmation: plan.Confirmation,
		Before: &plan.Before, After: plan.After, Files: files, Issues: plan.Issues,
	}, err
}

func skillFilesByPath(files []SkillFile) map[string]*SkillFile {
	out := make(map[string]*SkillFile, len(files))
	for _, file := range files {
		out[file.Path] = &file
	}
	return out
}

func (a App) skillDiffData(ctx context.Context, p skillProjection) (map[string][]byte, error) {
	info, err := os.Lstat(p.source)
	if errors.Is(err, os.ErrNotExist) && p.shared {
		return a.readSharedSkillData(ctx, p.Package)
	}
	if err != nil || !info.IsDir() {
		// The preview can still show the pinned file list and hashes.
		return nil, nil
	}
	payload, _, err := readSkillDirectory(p.source, false)
	return payload.data, err
}

func skillDiffText(file *SkillFile, data map[string][]byte) *DesktopSkillText {
	if file == nil {
		return nil
	}
	out := &DesktopSkillText{Digest: file.Digest, Exec: file.Executable}
	body, ok := data[file.Path]
	switch {
	case !ok || digest(body) != file.Digest:
		out.Kind = "unavailable"
	case len(body) > 64<<10:
		out.Kind = "large"
	case !utf8.Valid(body) || bytes.ContainsRune(body, 0) || strings.ContainsFunc(string(body), func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
	}):
		out.Kind = "binary"
	default:
		out.Kind, out.Contents = "text", string(body)
	}
	return out
}
