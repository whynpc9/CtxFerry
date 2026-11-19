package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type SkillChangeInput struct {
	Name, Target, Ref, License, Confirm string
	Rollback                            bool
}

type SkillChangePlan struct {
	Action       string         `json:"action"`
	Target       string         `json:"target"`
	Path         string         `json:"path"`
	Mode         string         `json:"mode"`
	BeforeLink   string         `json:"beforeLink,omitempty"`
	AfterLink    string         `json:"afterLink,omitempty"`
	Status       string         `json:"status"`
	Before       SkillPackage   `json:"before"`
	After        *SkillPackage  `json:"after,omitempty"`
	Changes      []SkillChange  `json:"changes"`
	Issues       []SkillProblem `json:"issues"`
	Confirmation string         `json:"confirmation,omitempty"`
}

type SkillChange struct {
	Path   string     `json:"path"`
	Before *SkillFile `json:"before,omitempty"`
	After  *SkillFile `json:"after,omitempty"`
}

func validSkillVersion(id string) bool {
	return len(id) == 64 && skillObjectID(id)
}

func skillVersion(pkg SkillPackage) string {
	// SkillPackage contains only JSON primitives and slices of primitives.
	wire, _ := json.Marshal(pkg)
	return digest(wire)
}

func (a App) ChangeSkill(ctx context.Context, input SkillChangeInput) (SkillChangePlan, error) {
	var device string
	if input.Confirm != "" {
		var err error
		device, err = a.skillDevice(ctx)
		if err != nil {
			return SkillChangePlan{}, err
		}
		unlock, err := a.lockSkills(ctx)
		if err != nil {
			return SkillChangePlan{}, err
		}
		defer unlock()
	}
	old, err := a.openSkillProjection(input.Name, input.Target)
	if err != nil {
		return SkillChangePlan{}, err
	}
	if old.receipt.State == "" || device != "" && old.receipt.DeviceID != device {
		return SkillChangePlan{}, Fail("skill-not-installed", "this target has no Skill installation owned by this device")
	}
	if old.receipt.State != "installed" {
		return SkillChangePlan{}, Fail("skill-recovery-pending", "run skill apply in the installed mode to recover the target first")
	}
	symlink := old.receipt.Mode == "symlink"
	if err := old.loadPackage(input.Name, old.receipt.Version, symlink); err != nil {
		return SkillChangePlan{}, err
	}
	if _, err := old.inspectTarget(); err != nil {
		return SkillChangePlan{}, err
	}

	plan := SkillChangePlan{
		Action: "update", Target: old.Target, Path: old.Path, Mode: old.Mode,
		Status: old.Status, Before: old.Package, BeforeLink: old.LinkSource, Changes: []SkillChange{}, Issues: old.Issues,
	}
	next := old
	next.shared = old.receipt.Shared
	next.Status, next.Issues = "ready", []SkillProblem{}
	var payload skillPayload
	if input.Rollback {
		plan.Action = "rollback"
		if old.receipt.Previous == nil {
			return plan, Fail("skill-no-previous-version", "there is no previous installed version to restore")
		}
		if err := next.loadPackage(input.Name, *old.receipt.Previous, symlink); err != nil {
			return plan, err
		}
	} else {
		if next.shared {
			if input.Ref != "" || input.License != "" {
				return plan, Fail("usage", "approve a different shared pin with skill add --share")
			}
			pin, err := a.sharedSkillPin(ctx, input.Name)
			if errors.Is(err, sql.ErrNoRows) {
				return plan, Fail("skill-not-found", "there is no approved shared pin for this Skill")
			}
			if err != nil {
				return plan, err
			}
			next.Package = pin.SkillPin
		} else {
			next.Package, payload, err = a.prepareSkillUpdate(ctx, old.Package, input)
			if err != nil {
				return plan, err
			}
		}
		next.version = skillVersion(next.Package)
		next.source = filepath.Join(next.packageDir(input.Name, next.version), "files")
		if symlink {
			next.LinkSource = next.source
		}
	}
	next.checkRequirements()
	plan.After, plan.AfterLink = &next.Package, next.LinkSource
	plan.Changes = diffSkillFiles(old.Package.Files, next.Package.Files)
	plan.Issues = append(plan.Issues, next.Issues...)
	if len(plan.Issues) > 0 {
		plan.Status = "blocked"
		if input.Confirm != "" {
			return plan, &Error{Code: "skill-change-blocked", Message: plan.Issues[0].Message, Details: plan.Issues}
		}
		return plan, nil
	}
	if skillVersion(old.Package) == skillVersion(next.Package) {
		plan.Status = "unchanged"
	}
	// Bind approval to the complete plan, including metadata-only pin changes,
	// and to the exact installed entry and receipt used for comparison.
	wire, err := json.Marshal(struct {
		Plan    SkillChangePlan
		Receipt skillReceipt
	}{plan, old.receipt})
	if err != nil {
		return plan, err
	}
	if plan.Status == "ready" {
		plan.Confirmation = digest(wire)
	}
	if input.Confirm == "" {
		return plan, nil
	}
	if plan.Confirmation != input.Confirm {
		return plan, Fail("skill-plan-changed", "the Skill change no longer matches the preview; run the command without --confirm again")
	}
	if !input.Rollback {
		if next.shared {
			err = a.cacheSharedSkill(ctx, next)
		} else {
			err = saveSkillSnapshot(next.packageDir(input.Name, next.version), next.Package, payload.data)
		}
		if err != nil {
			return plan, err
		}
	}
	if err := next.replace(&old); err != nil {
		return plan, err
	}
	plan.Status = "installed"
	plan.Confirmation = ""
	return plan, nil
}

func (a App) prepareSkillUpdate(ctx context.Context, old SkillPackage, update SkillChangeInput) (SkillPackage, skillPayload, error) {
	input := SkillImport{License: update.License}
	switch old.SourceKind {
	case "local":
		if update.Ref != "" {
			return old, skillPayload{}, Fail("usage", "--ref applies only to Git Skills")
		}
		u, err := url.Parse(old.SourceURL)
		if err != nil || u.Scheme != "file" || u.Host != "" {
			return old, skillPayload{}, Fail("invalid-skill-state", "invalid local Skill source")
		}
		path := u.Path
		if runtime.GOOS == "windows" {
			path = strings.TrimPrefix(path, "/")
			if strings.HasPrefix(path, "/") {
				path = "/" + path // Preserve the leading pair for a UNC path.
			}
		}
		input.Local = filepath.FromSlash(path)
		if !filepath.IsAbs(input.Local) {
			return old, skillPayload{}, Fail("invalid-skill-state", "local Skill source must be an absolute path")
		}
	case "git":
		input.Git, input.Subdir, input.Ref = old.SourceURL, old.Subdir, update.Ref
	default:
		return old, skillPayload{}, Fail("invalid-skill-state", "unknown Skill source kind")
	}
	pkg := SkillPackage{SchemaVersion: 1, Subdir: ".", Platforms: old.Platforms, Dependencies: old.Dependencies}
	pkg, payload, err := a.prepareSkillPackage(ctx, pkg, input)
	if err == nil && pkg.Name != old.Name {
		err = Fail("skill-name-changed", fmt.Sprintf("the source now declares Skill %q instead of %q", pkg.Name, old.Name))
	}
	return pkg, payload, err
}

func diffSkillFiles(before, after []SkillFile) []SkillChange {
	changes := []SkillChange{}
	paths := make(map[string]SkillChange, len(before)+len(after))
	for _, file := range before {
		paths[file.Path] = SkillChange{Path: file.Path, Before: &file}
	}
	for _, file := range after {
		change := paths[file.Path]
		change.Path, change.After = file.Path, &file
		paths[file.Path] = change
	}
	for _, change := range paths {
		if change.Before == nil || change.After == nil || *change.Before != *change.After {
			changes = append(changes, change)
		}
	}
	slices.SortFunc(changes, func(a, b SkillChange) int { return strings.Compare(a.Path, b.Path) })
	return changes
}
