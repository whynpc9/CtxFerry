package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/whynpc9/CtxFerry/internal/platform"
)

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
