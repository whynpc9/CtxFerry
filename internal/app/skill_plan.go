package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/platform"
)

type SkillPlan struct {
	Package    SkillPackage   `json:"package"`
	Target     string         `json:"target"`
	Path       string         `json:"path"`
	Mode       string         `json:"mode"`
	LinkSource string         `json:"linkSource,omitempty"`
	Status     string         `json:"status"`
	Writes     []SkillWrite   `json:"writes"`
	Issues     []SkillProblem `json:"issues"`
}

type SkillWrite struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Digest     string `json:"digest,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

type SkillProblem struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Paths   []string `json:"paths,omitempty"`
}

func (p *SkillPlan) problem(code, message string, paths ...string) {
	p.Issues = append(p.Issues, SkillProblem{code, message, paths})
	p.Status = "blocked"
}

type skillProjection struct {
	SkillPlan
	root, source, receiptPath string
	receipt                   skillReceipt
}

func skillTarget(target string) (string, error) {
	switch target {
	case "codex", "claude-code":
	default:
		return "", Fail("usage", "target must be codex or claude-code")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(home) {
		return "", Fail("invalid-path", "the user home must be an absolute path")
	}
	base := filepath.Join(home, ".agents")
	if target == "claude-code" {
		base = filepath.Join(home, ".claude")
		if config := os.Getenv("CLAUDE_CONFIG_DIR"); config != "" {
			if !filepath.IsAbs(config) {
				return "", Fail("invalid-path", "CLAUDE_CONFIG_DIR must be absolute")
			}
			base = config
		}
	}
	return resolveSkillDirectory(filepath.Join(base, "skills"))
}

// Resolve existing ancestors so a changed home or adapter symlink selects a
// new physical path and cannot reuse a receipt for the old location.
func resolveSkillDirectory(path string) (string, error) {
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			resolved, err = canonical(resolved)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		// A dangling symlink is an existing entry and must not be treated as a directory to create.
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return "", Fail("invalid-path", "Skill root has an unavailable directory or link")
		}
		missing = append(missing, filepath.Base(path))
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

func (a App) PlanSkill(name, target string, symlink bool) (SkillPlan, error) {
	projection, err := a.planSkill(name, target, symlink)
	return projection.SkillPlan, err
}

func (a App) planSkill(name, target string, symlink bool) (skillProjection, error) {
	var p skillProjection
	if !validSkillName(name) {
		return p, Fail("usage", "a valid Skill name is required")
	}
	root, err := skillTarget(target)
	if err != nil {
		return p, err
	}
	local, err := resolveSkillDirectory(a.skillDir())
	if err != nil {
		return p, err
	}
	if contains(root, local) || contains(local, root) {
		return p, Fail("invalid-path", "Skill target and local package store must be separate")
	}
	packageDir := filepath.Join(local, "packages", name)
	p.root = root
	p.source = filepath.Join(packageDir, "files")
	p.receiptPath = filepath.Join(local, "installs", digest([]byte(filepath.Join(root, name)))+".json")
	p.SkillPlan = SkillPlan{
		Target: target, Path: filepath.Join(root, name), Mode: "copy",
		Status: "ready", Writes: []SkillWrite{}, Issues: []SkillProblem{},
	}
	if symlink {
		p.Mode, p.LinkSource = "symlink", p.source
	}
	for _, dir := range []string{filepath.Dir(packageDir), packageDir} {
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			return p, Fail("skill-not-found", fmt.Sprintf("Skill %q has not been added", name))
		}
		if err != nil {
			return p, err
		}
		if !info.IsDir() {
			return p, Fail("invalid-skill-state", "the Skill package path must be a directory")
		}
	}
	if err := readSkillJSON(filepath.Join(packageDir, "manifest.json"), &p.Package); err != nil {
		if os.IsNotExist(err) {
			return p, Fail("skill-not-found", fmt.Sprintf("Skill %q has not been added", name))
		}
		return p, err
	}
	if err := p.Package.validate(name); err != nil {
		return p, err
	}
	changes, err := compareSkillTree(p.source, p.Package.Files)
	if err != nil {
		return p, err
	}
	if len(changes) != 0 {
		p.problem("skill-package-changed", "local Skill package differs from its manifest", changes...)
	}

	if err := readSkillJSON(p.receiptPath, &p.receipt); err != nil && !os.IsNotExist(err) {
		return p, err
	}
	installed, err := p.inspectTarget()
	if err != nil {
		return p, err
	}
	if installed && p.receipt.State == "installed" && len(p.Issues) == 0 {
		p.Status = "installed"
	}
	p.checkRequirements()
	if p.Status == "ready" {
		if p.receipt.State == "pending" {
			p.Status = "recovery-pending"
		}
		if !installed {
			if symlink {
				p.Writes = append(p.Writes, SkillWrite{Path: p.Path, Kind: "symlink", Digest: p.Package.Digest})
			} else {
				for _, file := range p.Package.Files {
					p.Writes = append(p.Writes, SkillWrite{
						Path: filepath.Join(p.Path, filepath.FromSlash(file.Path)), Kind: "file",
						Digest: file.Digest, Executable: file.Executable,
					})
				}
			}
		}
	}
	return p, nil
}

func (pkg SkillPackage) validate(name string) error {
	files, err := json.Marshal(pkg.Files)
	if err != nil {
		return err
	}
	if pkg.Name != name || pkg.SchemaVersion != 1 || len(pkg.Files) == 0 || len(pkg.Files) > skillMaxFiles ||
		digest(files) != pkg.Digest {
		return Fail("invalid-skill-state", "the Skill manifest does not match its name, schema or pin")
	}
	previous := ""
	hasSkill := false
	for _, file := range pkg.Files {
		if !validateSkillPath(file.Path) || file.Path <= previous || len(file.Digest) != 64 || !skillObjectID(file.Digest) {
			return Fail("invalid-skill-state", "the Skill manifest contains an invalid file entry")
		}
		previous = file.Path
		hasSkill = hasSkill || file.Path == "SKILL.md"
	}
	if !hasSkill {
		return Fail("invalid-skill-state", "the Skill manifest must include SKILL.md")
	}
	return nil
}

func (p *skillProjection) checkRequirements() {
	if platforms := p.Package.Platforms; len(platforms) > 0 &&
		!slices.Contains(platforms, runtime.GOOS) && !slices.Contains(platforms, runtime.GOOS+"/"+runtime.GOARCH) {
		p.problem("platform-unsupported", fmt.Sprintf("Skill requires %s; current platform is %s/%s",
			strings.Join(platforms, ", "), runtime.GOOS, runtime.GOARCH))
	}
	for _, dependency := range p.Package.Dependencies {
		if _, err := exec.LookPath(dependency); err != nil {
			p.problem("dependency-missing", fmt.Sprintf("required executable %q is unavailable on PATH", dependency))
		}
	}
}

func (p *skillProjection) inspectTarget() (bool, error) {
	entries, err := os.ReadDir(p.root)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, entry := range entries {
		if entry.Name() != p.Package.Name && strings.EqualFold(entry.Name(), p.Package.Name) {
			p.problem("skill-name-collision", "a case-insensitive name collision exists in the target", filepath.Join(p.root, entry.Name()))
			return false, nil
		}
	}

	id, err := platform.SkillFileIdentity(p.Path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if p.receipt.State == "" {
		if id != "" {
			p.problem("skill-name-collision", "target already exists without a CtxFerry ownership receipt", p.Path)
		}
		return false, nil
	}
	r := p.receipt
	if err := r.validate(p); err != nil {
		return false, err
	}
	if r.Mode != p.Mode {
		p.problem("skill-mode-conflict", fmt.Sprintf("existing installation or recovery uses %s", r.Mode), p.Path)
	}
	if r.State == "pending" && id == "" {
		staged, err := platform.SkillFileIdentity(r.stagePath(p))
		if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if staged != r.Identity {
			p.problem("skill-recovery-conflict", "staged Skill is missing or has been replaced", r.stagePath(p))
			return false, nil
		}
		return false, p.checkProjection(r.stagePath(p), "skill-recovery-conflict")
	}
	if id != r.Identity {
		code := "skill-external-edit"
		message := "installed Skill is missing or has been replaced"
		if r.State == "pending" {
			code, message = "skill-name-collision", "target appeared during an interrupted apply"
		}
		p.problem(code, message, p.Path)
		return false, nil
	}
	if r.State == "pending" {
		if _, err := os.Lstat(r.stagePath(p)); err == nil {
			p.problem("skill-recovery-conflict", "both the staged and target entries are present", p.Path)
		} else if !os.IsNotExist(err) {
			return false, err
		}
		if err := p.checkProjection(p.Path, "skill-external-edit"); err != nil {
			return false, err
		}
		// No files are written when apply only finalizes the journal.
		return true, nil
	}
	return true, p.checkProjection(p.Path, "skill-external-edit")
}

func (p *skillProjection) checkProjection(path, code string) error {
	var changes []string
	if p.receipt.Mode == "symlink" {
		source, err := os.Readlink(path)
		if err != nil || source != p.source {
			changes = []string{path}
		}
	} else {
		var err error
		changes, err = compareSkillTree(path, p.receipt.Files)
		if err != nil {
			return err
		}
		for i, changed := range changes {
			changes[i] = filepath.Join(path, filepath.FromSlash(changed))
		}
	}
	if len(changes) != 0 {
		p.problem(code, "projection differs from its recorded contents", changes...)
	}
	return nil
}

func compareSkillTree(dir string, expected []SkillFile) ([]string, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) || err == nil && !info.IsDir() {
		return []string{"."}, nil
	}
	if err != nil {
		return nil, err
	}
	payload, dirs, err := readSkillDirectory(dir, false)
	if err != nil {
		var invalid *Error
		if errors.As(err, &invalid) {
			return []string{"."}, nil
		}
		return nil, err
	}
	known := make(map[string]SkillFile, len(expected))
	knownDirs := make(map[string]bool)
	for _, file := range expected {
		known[file.Path] = file
		for parent := filepath.ToSlash(filepath.Dir(file.Path)); parent != "."; parent = filepath.ToSlash(filepath.Dir(parent)) {
			knownDirs[parent] = true
		}
	}
	var changes []string
	for _, file := range payload.files {
		if original, ok := known[file.Path]; !ok || !sameSkillFile(original, file) {
			changes = append(changes, file.Path)
		}
		delete(known, file.Path)
	}
	for path := range known {
		changes = append(changes, path)
	}
	for _, dir := range dirs {
		if !knownDirs[dir] {
			changes = append(changes, dir)
		}
	}
	slices.Sort(changes)
	return slices.Compact(changes), nil
}
