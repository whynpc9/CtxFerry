package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/whynpc9/CtxFerry/internal/store"
)

func (a App) cacheImportedPublicSkill(ctx context.Context, pkg SkillPackage, data map[string][]byte) error {
	unlock, err := a.lockSkills(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	root, err := resolveSkillDirectory(a.skillDir())
	if err != nil {
		return err
	}
	return saveSkillSnapshot(filepath.Join(root, "versions", skillVersion(pkg)), pkg, data)
}

func sharedSkillForPlan(ctx context.Context, q rowQuerier, workspace, name string) (remoteRecord, error) {
	id := skillRecordID(workspace, skillPinKind, Scope{"workspace", workspace}, name)
	if err := requireNoConflict(ctx, q, id); err != nil {
		return remoteRecord{}, err
	}
	r, err := loadSkillRecord(ctx, q, id)
	if err != nil || r.Deleted {
		if r.Deleted {
			err = sql.ErrNoRows
		}
		return remoteRecord{}, err
	}
	return r, r.validateSkillRecord(workspace)
}

func (a App) sharedSkillPin(ctx context.Context, name string) (remoteRecord, error) {
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return remoteRecord{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return remoteRecord{}, err
	}
	defer tx.Rollback()
	var workspace string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&workspace); err != nil {
		return remoteRecord{}, err
	}
	return sharedSkillForPlan(ctx, tx, workspace, name)
}

func (a App) planSelectedSkill(ctx context.Context, p *skillProjection, name string, symlink bool) error {
	// Report the recoverable local state before considering any newer pin.
	if p.receipt.State == "pending" {
		return p.planInstalled(name, symlink)
	}
	r, err := a.sharedSkillPin(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return p.planInstalled(name, symlink)
	}
	if err != nil {
		return err
	}
	return p.planSharedPin(r.SkillPin, symlink)
}

func (p *skillProjection) planSharedPin(pkg SkillPackage, symlink bool) error {
	p.Package, p.shared = pkg, true
	p.version = skillVersion(pkg)
	dir := p.packageDir(pkg.Name, p.version)
	p.source = filepath.Join(dir, "files")
	if symlink {
		p.Mode, p.LinkSource = "symlink", p.source
	}
	if _, err := os.Lstat(p.packageDir(pkg.Name, "")); err == nil {
		p.problem("skill-name-collision", "a local-only package already uses this shared Skill name")
	} else if !os.IsNotExist(err) {
		return err
	}
	if p.receipt.State != "" && (!p.receipt.Shared || p.receipt.Version != p.version) {
		p.problem("skill-pin-changed", "the installed Skill uses a different pin; preview an update before replacing it", p.Path)
		return nil
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if p.receipt.State != "" {
				p.problem("skill-package-missing", "the local package cache used by the installation is missing", p.source)
			}
			return p.planProjection(symlink)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return Fail("invalid-skill-state", "the Skill snapshot store must be a directory")
		}
	}
	if err := p.loadPackage(pkg.Name, p.version, symlink); err != nil {
		return err
	}
	return p.planProjection(symlink)
}

func (a App) PlanSkillGroup(ctx context.Context, selection, target string, symlink bool) (SkillGroupPlan, error) {
	out := SkillGroupPlan{Plans: []SkillPlan{}}
	if _, err := skillTarget(target); err != nil {
		return out, err
	}
	db, err := store.Open(ctx, a.DataDir, false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var workspace string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM workspace").Scan(&workspace); err != nil {
		return out, err
	}
	scope, name, err := resolveSkillGroup(ctx, tx, workspace, selection)
	if err != nil {
		return out, err
	}
	if scope.Kind == "project" {
		var subscribed bool
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT subscribed FROM project_subscriptions WHERE project_id=?),1)", scope.ID).Scan(&subscribed); err != nil {
			return out, err
		}
		if !subscribed {
			return out, Fail("project-unsubscribed", "subscribe to the Project before planning its Skill group")
		}
	}
	id := skillRecordID(workspace, skillGroupKind, scope, name)
	if err := requireNoConflict(ctx, tx, id); err != nil {
		return out, err
	}
	r, err := loadSkillRecord(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && r.Deleted {
		return out, Fail("skill-group-not-found", fmt.Sprintf("Skill group %q has not been set", selection))
	}
	if err != nil {
		return out, err
	}
	if err := r.validateSkillRecord(workspace); err != nil {
		return out, err
	}
	out.Group = r.SkillGroup
	out.Group.Name = selection
	for _, name := range out.Group.Skills {
		r, err := sharedSkillForPlan(ctx, tx, workspace, name)
		if errors.Is(err, sql.ErrNoRows) {
			return out, Fail("invalid-reference", "Skill group refers to a missing shared pin")
		}
		if err != nil {
			return out, err
		}
		p, err := a.openSkillProjection(name, target)
		if err != nil {
			return out, err
		}
		if err := p.planSharedPin(r.SkillPin, symlink); err != nil {
			return out, err
		}
		out.Plans = append(out.Plans, p.SkillPlan)
	}
	return out, nil
}

func (a App) cacheSharedSkill(ctx context.Context, p skillProjection) error {
	path := filepath.Dir(p.source)
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() {
			return Fail("invalid-skill-state", "the Skill cache path must be a directory")
		}
		if err := p.loadPackage(p.Package.Name, p.version, p.Mode == "symlink"); err != nil {
			return err
		}
		if len(p.Issues) != 0 {
			return Fail("skill-package-changed", "local Skill package differs from its manifest")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	var data map[string][]byte
	pkg := p.Package
	if pkg.SourceKind == "private" {
		db, err := store.Open(ctx, a.DataDir, false)
		if err != nil {
			return err
		}
		defer db.Close()
		data, err = privateSkillData(pkg, func(path string) ([]byte, error) {
			return localSkillObject(ctx, db, path)
		})
		if err != nil {
			return err
		}
	} else {
		source := SkillPackage{Subdir: "."}
		payload, err := readGitSkill(ctx, &source, SkillImport{Git: pkg.SourceURL, Subdir: pkg.Subdir, Ref: pkg.Commit})
		if err != nil {
			return err
		}
		if err := payload.finish(); err != nil {
			return err
		}
		name, _, _, err := skillFrontMatter(payload.data["SKILL.md"])
		if err != nil || name != pkg.Name || !slices.Equal(payload.files, pkg.Files) {
			return Fail("skill-pin-mismatch", "the fetched public Skill differs from its approved manifest")
		}
		data = payload.data
	}
	return saveSkillSnapshot(path, pkg, data)
}
