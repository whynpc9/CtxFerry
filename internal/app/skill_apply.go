package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/whynpc9/CtxFerry/internal/platform"
)

const skillStagePrefix = ".ctxferry-stage-"

type skillReceipt struct {
	SchemaVersion int           `json:"schemaVersion"`
	DeviceID      string        `json:"deviceId"`
	Path          string        `json:"path"`
	Mode          string        `json:"mode"`
	Identity      string        `json:"identity"`
	PackageDigest string        `json:"packageDigest"`
	Files         []SkillFile   `json:"files"`
	State         string        `json:"state"`
	Stage         string        `json:"stage,omitempty"`
	Version       string        `json:"version,omitempty"`
	Previous      *string       `json:"previous,omitempty"`
	Replaces      *skillReceipt `json:"replaces,omitempty"`
}

func (r skillReceipt) validate(p *skillProjection) error {
	if err := r.validateTarget(p.Path); err != nil {
		return err
	}
	if r.Version != p.version || r.PackageDigest != p.Package.Digest || !slices.Equal(r.Files, p.Package.Files) {
		return Fail("invalid-skill-state", "the local Skill receipt does not match the package pin")
	}
	return nil
}

func (r skillReceipt) validateTarget(path string) error {
	validStage := strings.HasPrefix(r.Stage, skillStagePrefix) && validateSkillPath(r.Stage) && !strings.Contains(r.Stage, "/")
	if _, err := uuid.Parse(r.DeviceID); err != nil || r.SchemaVersion != 1 || r.Path != path || r.Identity == "" ||
		(r.Mode != "copy" && r.Mode != "symlink") ||
		(r.State != "installed" && r.State != "pending") ||
		(r.State == "pending" && !validStage) || (r.State == "installed" && r.Stage != "") ||
		r.Version != "" && !validSkillVersion(r.Version) ||
		r.Previous != nil && *r.Previous != "" && !validSkillVersion(*r.Previous) ||
		r.Replaces != nil && r.State != "pending" {
		return Fail("invalid-skill-state", "the local Skill receipt is inconsistent with this package and target")
	}
	return nil
}

func (r skillReceipt) stagePath(p *skillProjection) string {
	return filepath.Join(p.root, r.Stage, "payload")
}

func (a App) ApplySkill(ctx context.Context, name, target string, symlink bool) (SkillPlan, error) {
	device, err := a.skillDevice(ctx)
	if err != nil {
		return SkillPlan{}, err
	}
	unlock, err := a.lockSkills(ctx)
	if err != nil {
		return SkillPlan{}, err
	}
	defer unlock()
	p, err := a.openSkillProjection(name, target)
	if err != nil {
		return p.SkillPlan, err
	}
	if p.receipt.Replaces != nil {
		mode := "copy"
		if symlink {
			mode = "symlink"
		}
		if err := p.recoverReplacement(device, mode); err != nil {
			return p.SkillPlan, err
		}
	}
	err = p.planInstalled(name, symlink)
	if err != nil {
		return p.SkillPlan, err
	}
	if len(p.Issues) != 0 {
		return p.SkillPlan, &Error{Code: "skill-apply-blocked", Message: p.Issues[0].Message, Details: p.Issues}
	}
	if p.receipt.State != "" && p.receipt.DeviceID != device {
		return p.SkillPlan, Fail("skill-name-collision", "the installation receipt belongs to a different device")
	}
	if p.Status == "installed" {
		return p.SkillPlan, nil
	}
	if p.receipt.State == "" {
		if err := p.stage(device); err != nil {
			return p.SkillPlan, err
		}
	}
	if err := p.publish(); err != nil {
		return p.SkillPlan, err
	}
	if err := p.finalize(); err != nil {
		return p.SkillPlan, err
	}
	p.Status = "installed"
	return p.SkillPlan, nil
}

func (p *skillProjection) stage(device string) error {
	return p.stageReplacing(device, nil)
}

func (p *skillProjection) stageReplacing(device string, old *skillReceipt) error {
	if err := mkdirSkillDirs(p.root); err != nil {
		return err
	}
	if err := mkdirSkillDirs(filepath.Dir(p.receiptPath)); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(p.root, skillStagePrefix)
	if err != nil {
		return err
	}
	// Once journaling starts, keep the staged data even if receipt publication
	// reports an error. An error may have occurred after its atomic rename.
	journaling := false
	defer func() {
		if !journaling {
			os.RemoveAll(stage)
		}
	}()
	p.receipt = skillReceipt{
		SchemaVersion: 1, DeviceID: device, Path: p.Path, Mode: p.Mode,
		PackageDigest: p.Package.Digest, Files: p.Package.Files, State: "pending", Stage: filepath.Base(stage),
		Version: p.version, Replaces: old,
	}
	if old != nil {
		p.receipt.Previous = &old.Version
	}
	payload := p.receipt.stagePath(p)
	if p.Mode == "symlink" {
		if err := os.Symlink(p.source, payload); err != nil {
			return err
		}
	} else {
		source, _, err := readSkillDirectory(p.source, false)
		if err != nil {
			return err
		}
		if err := writeSkillTree(payload, p.Package.Files, source.data); err != nil {
			return err
		}
	}
	if err := p.verifyProjection(payload); err != nil {
		return err
	}
	p.receipt.Identity, err = platform.SkillFileIdentity(payload)
	if err != nil {
		return err
	}
	if err := platform.SyncSkillDirectory(stage); err != nil {
		return err
	}
	if err := platform.SyncSkillDirectory(p.root); err != nil {
		return err
	}
	journaling = true
	return writeSkillJSON(p.receiptPath, p.receipt)
}

func (p *skillProjection) verifyProjection(path string) error {
	if err := p.checkProjection(path, "skill-recovery-conflict"); err != nil {
		return err
	}
	if p.Mode == "symlink" {
		changes, err := compareSkillTree(p.source, p.Package.Files)
		if err != nil {
			return err
		}
		if len(changes) > 0 {
			p.problem("skill-package-changed", "symlink source differs from its pinned package", changes...)
		}
	}
	if len(p.Issues) != 0 {
		return &Error{Code: "skill-apply-blocked", Message: p.Issues[0].Message, Details: p.Issues}
	}
	return nil
}

func (p *skillProjection) publish() error {
	stage := p.receipt.stagePath(p)
	id, err := platform.SkillFileIdentity(p.Path)
	if err == nil {
		// The previous process may have moved the payload and exited before
		// recording installation. Recognize only the exact staged entry.
		if id != p.receipt.Identity {
			return Fail("skill-name-collision", "target already exists; it was left intact")
		}
		if _, err := os.Lstat(stage); err == nil || !os.IsNotExist(err) {
			return Fail("skill-recovery-conflict", "cannot verify that the staged entry was moved")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		staged, err := platform.SkillFileIdentity(stage)
		if err != nil || staged != p.receipt.Identity {
			return Fail("skill-recovery-conflict", "staged Skill is missing or has been replaced")
		}
		if err := p.verifyProjection(stage); err != nil {
			return err
		}
		if err := moveSkillExclusive(stage, p.Path); err != nil {
			if _, statErr := os.Lstat(p.Path); statErr == nil {
				return Fail("skill-name-collision", "target appeared during apply and was left intact")
			}
			return err
		}
	}
	return errors.Join(platform.SyncSkillDirectory(p.root), platform.SyncSkillDirectory(filepath.Dir(stage)))
}

func (p *skillProjection) finalize() error {
	id, err := platform.SkillFileIdentity(p.Path)
	if err != nil || id != p.receipt.Identity {
		return Fail("skill-recovery-conflict", "installed entry could not be verified")
	}
	if err := p.verifyProjection(p.Path); err != nil {
		return err
	}
	stage := filepath.Dir(p.receipt.stagePath(p))
	installed := p.receipt
	installed.State, installed.Stage, installed.Replaces = "installed", "", nil
	if err := writeSkillJSON(p.receiptPath, installed); err != nil {
		return err
	}
	p.receipt = installed
	// The payload was moved. A replacement retains its previous tree here.
	_ = os.Remove(stage)
	return nil
}

func mkdirSkillDirs(path string) error {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return Fail("invalid-path", "cannot create Skill directory")
	}
	if err := mkdirSkillDirs(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil {
		if info, statErr := os.Stat(path); statErr != nil || !info.IsDir() {
			return err
		}
	}
	return platform.SyncSkillDirectory(parent)
}
