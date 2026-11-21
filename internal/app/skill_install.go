package app

// The Skill install module owns one target's lifecycle behind two entry
// points: a plan (planSelectedSkill, previewed by the CLI and the desktop) and
// ApplySkill / applySkill(confirmation), which first recovers an interrupted
// replacement. Inside it, a projection is staged beside the target, verified,
// published by an atomic receipt rename and finalized; a replacement parks the
// previous projection so recovery can restore or retire it. The confirmation
// token that binds a desktop preview to its apply is built and checked here.

import (
	"context"
	"encoding/json"
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
	Shared        bool          `json:"shared,omitempty"`
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
		r.Shared && r.Version == "" ||
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
	return a.applySkill(ctx, name, target, symlink, "")
}

func (a App) applySkill(ctx context.Context, name, target string, symlink bool, confirmation string) (SkillPlan, error) {
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
	recovering := p.receipt.Replaces != nil
	if confirmation != "" {
		if err := a.planSelectedSkill(ctx, &p, name, symlink); err != nil {
			return p.SkillPlan, err
		}
		if confirmation != p.installConfirmation() {
			return p.SkillPlan, Fail("skill-plan-changed", "the Skill installation no longer matches the preview")
		}
	}
	if recovering {
		mode := "copy"
		if symlink {
			mode = "symlink"
		}
		if err := p.recoverReplacement(device, mode); err != nil {
			return p.SkillPlan, err
		}
		// Rebuild the post-recovery projection without the preview's status and file list.
		if confirmation != "" {
			p, err = a.openSkillProjection(name, target)
			if err != nil {
				return p.SkillPlan, err
			}
		}
	}
	if recovering {
		err = p.planInstalled(name, symlink)
	} else if confirmation == "" {
		err = a.planSelectedSkill(ctx, &p, name, symlink)
	}
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
		if p.shared {
			if err := a.cacheSharedSkill(ctx, p); err != nil {
				return p.SkillPlan, err
			}
		}
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
		Version: p.version, Shared: p.shared, Replaces: old,
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

// installConfirmation binds a preview to the exact plan, receipt and sharing
// scope it showed. applySkill recomputes it after planning again and refuses
// to act when anything the person confirmed has changed.
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

func (p *skillProjection) previousPath() string {
	return filepath.Join(filepath.Dir(p.receipt.stagePath(p)), "previous")
}

func (p *skillProjection) replace(old *skillProjection) error {
	if err := p.stageReplacing(old.receipt.DeviceID, &old.receipt); err != nil {
		// The pending receipt may already be visible. The target is unchanged
		// and Apply can retire the interrupted journal.
		return err
	}
	return p.completeReplacement(old)
}

func (p *skillProjection) completeReplacement(old *skillProjection) error {
	err := p.switchProjection(old)
	if err == nil {
		return nil
	}
	// The atomic receipt rename is the commit point. A flush error can be
	// reported after it; consult the on-disk receipt before restoring.
	var recorded skillReceipt
	if readErr := readSkillJSON(p.receiptPath, &recorded); readErr != nil {
		return errors.Join(err, readErr)
	}
	if recorded.State == "installed" && recorded.Identity == p.receipt.Identity {
		return nil
	}
	return errors.Join(err, p.recoverReplacement(old.receipt.DeviceID, old.Mode))
}

func (p *skillProjection) switchProjection(old *skillProjection) error {
	id, err := platform.SkillFileIdentity(p.Path)
	if err != nil || id != old.receipt.Identity {
		return Fail("skill-external-edit", "installed entry changed after the preview")
	}
	parked := p.previousPath()
	if err := moveSkillExclusive(p.Path, parked); err != nil {
		return err
	}
	id, err = platform.SkillFileIdentity(parked)
	if err != nil || id != old.receipt.Identity {
		return errors.Join(
			Fail("skill-recovery-conflict", "the moved entry could not be verified"),
			moveSkillExclusive(parked, p.Path),
		)
	}
	if err := old.verifyProjection(parked); err != nil {
		return err
	}
	// Make the backup durable before publishing a new entry.
	if err := errors.Join(platform.SyncSkillDirectory(filepath.Dir(parked)), platform.SyncSkillDirectory(p.root)); err != nil {
		return err
	}
	if err := p.publish(); err != nil {
		return err
	}
	return p.finalize()
}

// An interrupted replacement with a pending receipt always restores the old
// projection. Identity checks and exclusive moves preserve both trees if an
// outside writer occupies either path.
func (p *skillProjection) recoverReplacement(device, mode string) error {
	r, old := p.receipt, p.receipt.Replaces
	if err := r.validateTarget(p.Path); err != nil {
		return err
	}
	if old == nil || old.State != "installed" || old.Identity == r.Identity ||
		old.DeviceID != device || r.DeviceID != device || old.Mode != mode || r.Mode != mode ||
		r.Previous == nil || *r.Previous != old.Version {
		return Fail("skill-recovery-conflict", "the replacement journal does not match this installation")
	}
	if err := old.validateTarget(p.Path); err != nil {
		return err
	}
	target, err := skillIdentityIfPresent(p.Path)
	if err != nil {
		return err
	}
	parked := p.previousPath()
	previous, err := skillIdentityIfPresent(parked)
	if err != nil {
		return err
	}
	switch {
	case target == old.Identity && previous == "":
		// The old entry is already in place.
	case previous == old.Identity && (target == "" || target == r.Identity):
		if target == r.Identity {
			if err := moveSkillExclusive(p.Path, r.stagePath(p)); err != nil {
				return err
			}
		}
		if err := moveSkillExclusive(parked, p.Path); err != nil {
			return err
		}
	default:
		return Fail("skill-recovery-conflict", "an unexpected entry prevents restoration; the target and staged data were preserved")
	}
	if err := errors.Join(platform.SyncSkillDirectory(p.root), platform.SyncSkillDirectory(filepath.Dir(parked))); err != nil {
		return err
	}
	if err := writeSkillJSON(p.receiptPath, old); err != nil {
		return err
	}
	p.receipt = *old
	return nil
}

func skillIdentityIfPresent(path string) (string, error) {
	id, err := platform.SkillFileIdentity(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return id, err
}
