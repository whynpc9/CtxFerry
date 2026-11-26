package app

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whynpc9/CtxFerry/internal/platform"
)

//go:embed skill_entry.md
var entrySkill string

const (
	entryStart = "<!-- ctxferry:begin -->"
	entryEnd   = "<!-- ctxferry:end -->"
	entryBlock = entryStart + "\n" +
		"For project context or registered tasks, use the ctxferry Skill. Start with `ctxferry context show` from the current checkout.\n" +
		entryEnd + "\n"
)

type AgentEntryPlan struct {
	Target  string             `json:"target"`
	Action  string             `json:"action"`
	Status  string             `json:"status"`
	Writes  []AgentEntryChange `json:"writes"`
	Preview string             `json:"preview,omitempty"`
}

type AgentEntryChange struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Content string `json:"content,omitempty"`
}

// The local receipt contains only CtxFerry's own text and ownership metadata.
type entryReceipt struct {
	InstructionsName    string `json:"instructionsName"`
	Stage               string `json:"stage"`
	DirectoryIdentity   string `json:"directoryIdentity"`
	SkillIdentity       string `json:"skillIdentity"`
	SkillDigest         string `json:"skillDigest"`
	Block               string `json:"block"`
	InstructionIdentity string `json:"instructionIdentity,omitempty"`
}

type entryProjection struct {
	AgentEntryPlan
	path, instructions, receiptPath string
	receipt                         entryReceipt
	skillDir                        string
	instruction, skill              entryFile
	nextInstructions                []byte
	removeInstructionsFile          bool
}

type entryFile struct {
	info os.FileInfo
	data []byte
}

func readEntryFile(path string) (entryFile, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return entryFile{}, nil
	}
	if err != nil {
		return entryFile{}, err
	}
	if !info.Mode().IsRegular() {
		return entryFile{}, Fail("skill-entry-conflict", "expected a regular file at "+path)
	}
	f, err := os.Open(path)
	if err != nil {
		return entryFile{}, err
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil {
		return entryFile{}, err
	}
	if !os.SameFile(info, actual) {
		return entryFile{}, Fail("skill-entry-conflict", "file changed during inspection: "+path)
	}
	data, err := io.ReadAll(f)
	return entryFile{actual, data}, err
}

func (expected entryFile) verify(path string) error {
	actual, err := readEntryFile(path)
	if err != nil {
		return err
	}
	if expected.info == nil && actual.info == nil ||
		expected.info != nil && actual.info != nil && os.SameFile(expected.info, actual.info) &&
			expected.info.Mode() == actual.info.Mode() && bytes.Equal(expected.data, actual.data) {
		return nil
	}
	return Fail("skill-entry-conflict", "file changed since preview: "+path)
}

// BootstrapAgentEntry previews by default. Applying requires the token for the
// current projection, including its input bytes and target paths.
func (a App) BootstrapAgentEntry(ctx context.Context, target, action, preview string) (AgentEntryPlan, error) {
	if action != "install" && action != "uninstall" {
		return AgentEntryPlan{}, Fail("usage", "bootstrap action must be install or uninstall")
	}
	if preview != "" {
		if _, err := a.skillDevice(ctx); err != nil {
			return AgentEntryPlan{}, err
		}
		unlock, err := a.lockSkills(ctx)
		if err != nil {
			return AgentEntryPlan{}, err
		}
		defer unlock()
	}
	p, err := a.planAgentEntry(target, action)
	if err != nil || preview == "" {
		return p.AgentEntryPlan, err
	}
	if p.Status == "unchanged" {
		return p.AgentEntryPlan, nil
	}
	if preview != p.Preview {
		return p.AgentEntryPlan, Fail("skill-entry-preview-stale", "bootstrap files changed; run again without --preview to review the current changes")
	}
	if action == "install" {
		err = p.install()
		p.Status = "installed"
	} else {
		err = p.uninstall()
		p.Status = "uninstalled"
	}
	if err != nil {
		p.Status = "ready"
	}
	p.Preview = ""
	return p.AgentEntryPlan, err
}

func (a App) planAgentEntry(target, action string) (entryProjection, error) {
	p := entryProjection{AgentEntryPlan: AgentEntryPlan{Target: target, Action: action, Status: "unchanged", Writes: []AgentEntryChange{}}}
	root, instructions, err := agentEntryPaths(target)
	if err != nil {
		return p, err
	}
	local, err := resolveSkillDirectory(a.skillDir())
	if err != nil {
		return p, err
	}
	if contains(root, local) || contains(local, root) || (instructions != "" && contains(local, instructions)) {
		return p, Fail("invalid-path", "agent entry and local Skill state must be separate")
	}
	p.path = filepath.Join(root, "ctxferry")
	p.instructions = instructions
	receiptScope := instructions
	if receiptScope == "" {
		receiptScope = target
	}
	key := digest([]byte(p.path + "\x00" + receiptScope))
	receipts, err := resolveSkillDirectory(filepath.Join(local, "bootstrap"))
	if err != nil {
		return p, err
	}
	if !contains(local, receipts) || contains(root, receipts) {
		return p, Fail("invalid-path", "bootstrap receipts must be inside the local Skill store")
	}
	p.receiptPath = filepath.Join(receipts, key+".json")
	err = readSkillJSON(p.receiptPath, &p.receipt)
	switch {
	case err == nil:
		name := p.receipt.InstructionsName
		if instructions == "" {
			if name != "" || p.receipt.Block != "" || p.receipt.InstructionIdentity != "" {
				return p, Fail("invalid-skill-state", "unexpected instructions in bootstrap receipt")
			}
		} else if name != filepath.Base(instructions) && (target != "codex" || name != "AGENTS.override.md") {
			return p, Fail("invalid-skill-state", "invalid instruction filename in bootstrap receipt")
		} else {
			p.instructions = filepath.Join(filepath.Dir(instructions), name)
		}
		if err := p.inspectOwnedSkill(); err != nil {
			return p, err
		}
	case !os.IsNotExist(err):
		return p, err
	case action == "uninstall":
		return p, nil // No ownership, so leave any existing files alone.
	default:
		if err := checkEntryCollision(root); err != nil {
			return p, err
		}
	}
	if action == "install" && target == "codex" {
		override := filepath.Join(filepath.Dir(instructions), "AGENTS.override.md")
		file, err := readEntryFile(override)
		if err != nil {
			return p, err
		}
		if len(bytes.TrimSpace(file.data)) != 0 {
			instructions = override
		}
		if p.receipt.InstructionsName != "" && instructions != p.instructions {
			return p, Fail("skill-entry-conflict", "the active Codex instructions changed; preview and finish uninstall before reinstalling")
		}
		p.instructions = instructions
	}
	if p.instructions != "" {
		p.receipt.InstructionsName = filepath.Base(p.instructions)
		p.instruction, err = readEntryFile(p.instructions)
		if err != nil {
			return p, err
		}
		if err := p.planInstructions(); err != nil {
			return p, err
		}
	}
	if action == "install" && p.skillDir != p.path {
		content := entrySkill
		if p.skill.info != nil {
			content = string(p.skill.data)
		}
		p.Writes = append([]AgentEntryChange{{filepath.Join(p.path, "SKILL.md"), "create", content}}, p.Writes...)
	}
	if action == "uninstall" {
		if p.skill.info != nil {
			p.Writes = append(p.Writes, AgentEntryChange{filepath.Join(p.skillDir, "SKILL.md"), "remove", string(p.skill.data)})
		}
		if p.skillDir != "" {
			entries, err := os.ReadDir(p.skillDir)
			if err != nil {
				return p, err
			}
			if len(entries) == 0 || len(entries) == 1 && entries[0].Name() == "SKILL.md" {
				p.Writes = append(p.Writes, AgentEntryChange{p.skillDir, "remove-empty-directory", ""})
			}
		}
	}
	// An owned receipt may still require finalization after an interruption.
	if len(p.Writes) == 0 && !(action == "uninstall" || p.receipt.Stage != "") {
		return p, nil
	}
	p.Status = "ready"
	wire, err := json.Marshal(struct {
		Plan              AgentEntryPlan
		Receipt           entryReceipt
		ReceiptPath       string
		InstructionDigest string
		HasInstructions   bool
		InstructionMode   os.FileMode
	}{p.AgentEntryPlan, p.receipt, p.receiptPath, digest(p.instruction.data), p.instruction.info != nil, entryPermissions(p.instruction)})
	if err != nil {
		return p, err
	}
	p.Preview = digest(wire)
	return p, nil
}

func agentEntryPaths(target string) (root, instructions string, err error) {
	var base string
	switch target {
	case "cursor":
		base, err = agentConfigHome("", ".cursor")
	case "pi":
		base, err = agentConfigHome("PI_CODING_AGENT_DIR", filepath.Join(".pi", "agent"))
	case "codex", "claude-code":
		return agentEntryPathsWithInstructions(target)
	default:
		return "", "", Fail("usage", "bootstrap target must be codex, claude-code, cursor, or pi")
	}
	if err != nil {
		return "", "", err
	}
	root, err = resolveSkillDirectory(filepath.Join(base, "skills"))
	return root, "", err
}

func agentEntryPathsWithInstructions(target string) (root, instructions string, err error) {
	root, err = skillTarget(target)
	if err != nil {
		return "", "", err
	}
	base, err := skillTargetHome(target)
	if err != nil {
		return "", "", err
	}
	name := "CLAUDE.md"
	if target == "codex" {
		name = "AGENTS.md"
		if config := os.Getenv("CODEX_HOME"); config != "" {
			if !filepath.IsAbs(config) {
				return "", "", Fail("invalid-path", "CODEX_HOME must be absolute")
			}
			base = config
		} else {
			base = filepath.Join(filepath.Dir(base), ".codex")
		}
	}
	base, err = resolveSkillDirectory(base)
	if err != nil {
		return "", "", err
	}
	return root, filepath.Join(base, name), nil
}

func checkEntryCollision(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), "ctxferry") {
			return Fail("skill-name-collision", "ctxferry Skill already exists without a bootstrap receipt: "+filepath.Join(root, entry.Name()))
		}
	}
	return nil
}

func (p *entryProjection) inspectOwnedSkill() error {
	r := p.receipt
	if r.Stage != "" && (!strings.HasPrefix(r.Stage, skillStagePrefix) || !validateSkillPath(r.Stage) || strings.Contains(r.Stage, "/")) ||
		r.DirectoryIdentity == "" || r.SkillIdentity == "" || len(r.SkillDigest) != 64 || !skillObjectID(r.SkillDigest) ||
		r.Block != "" && !validEntryBlock(r.Block) {
		return Fail("invalid-skill-state", "invalid agent bootstrap receipt")
	}
	paths := []string{p.path}
	if r.Stage != "" {
		paths = append(paths, filepath.Join(filepath.Dir(p.path), r.Stage))
	}
	var ownedDir string
	for _, dir := range paths {
		id, err := platform.SkillFileIdentity(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if id != r.DirectoryIdentity || ownedDir != "" {
			return Fail("skill-entry-conflict", "bootstrap directory has been replaced or a target already exists: "+dir)
		}
		ownedDir = dir
	}
	p.skillDir = ownedDir
	if p.skillDir != "" {
		file := filepath.Join(p.skillDir, "SKILL.md")
		skill, err := readEntryFile(file)
		if err != nil {
			return err
		}
		p.skill = skill
		if skill.info != nil {
			id, err := platform.SkillFileIdentity(file)
			if err != nil {
				return err
			}
			if id != r.SkillIdentity || digest(skill.data) != r.SkillDigest {
				return Fail("skill-external-edit", "bootstrap Skill has been edited or replaced: "+file)
			}
		}
	}
	if p.Action == "install" && p.skill.info == nil {
		return Fail("skill-external-edit", "bootstrap Skill is missing; preview and finish uninstall before reinstalling")
	}
	return nil
}

func validEntryBlock(block string) bool {
	body := strings.ReplaceAll(block, "\r\n", "\n")
	body = strings.TrimPrefix(body, "\n")
	return strings.HasPrefix(body, entryStart+"\n") && strings.HasSuffix(body, "\n"+entryEnd+"\n") &&
		strings.Count(body, entryStart) == 1 && strings.Count(body, entryEnd) == 1
}

func (p *entryProjection) planInstructions() error {
	text, block := string(p.instruction.data), p.receipt.Block
	if strings.Contains(text, entryStart) || strings.Contains(text, entryEnd) {
		before, after, found := strings.Cut(text, block)
		if block == "" || !found || strings.Count(text, entryStart) != 1 || strings.Count(text, entryEnd) != 1 ||
			(!strings.HasPrefix(block, "\n") && !strings.HasPrefix(block, "\r\n") && before != "" && !strings.HasSuffix(before, "\n")) {
			return Fail("skill-entry-conflict", "managed instruction block is unowned, ambiguous or edited: "+p.instructions)
		}
		if p.Action == "install" {
			return nil
		}
		// Keep the line boundary if the user added text after the block.
		if before != "" && after != "" && !strings.HasSuffix(before, "\n") &&
			!strings.HasPrefix(after, "\n") && !strings.HasPrefix(after, "\r\n") {
			before += block[:strings.IndexByte(block, '\n')+1]
		}
		p.nextInstructions = []byte(before + after)
		action := "remove-block"
		if len(p.nextInstructions) == 0 && p.receipt.InstructionIdentity != "" {
			id, err := platform.SkillFileIdentity(p.instructions)
			if err != nil {
				return err
			}
			if id == p.receipt.InstructionIdentity {
				p.removeInstructionsFile = true
				action = "remove-file"
			}
		}
		p.Writes = append(p.Writes, AgentEntryChange{p.instructions, action, block})
		return nil
	}
	if p.Action == "uninstall" {
		return nil
	}
	newline := "\n"
	if at := strings.IndexByte(text, '\n'); at > 0 && text[at-1] == '\r' {
		newline = "\r\n"
	}
	block = strings.ReplaceAll(entryBlock, "\n", newline)
	if text != "" && !strings.HasSuffix(text, "\n") {
		block = newline + block
	}
	p.receipt.Block = block
	p.receipt.InstructionIdentity = ""
	p.nextInstructions = []byte(text + block)
	action := "append-block"
	if p.instruction.info == nil {
		action = "create-file"
	}
	p.Writes = append(p.Writes, AgentEntryChange{p.instructions, action, block})
	return nil
}

func entryPermissions(file entryFile) os.FileMode {
	if file.info == nil {
		return 0600
	}
	return file.info.Mode().Perm()
}

func (p *entryProjection) install() error {
	if p.receipt.DirectoryIdentity == "" {
		if err := p.stageSkill(); err != nil {
			return err
		}
	}
	if p.skillDir != p.path {
		if err := p.inspectOwnedSkill(); err != nil {
			return err
		}
		if err := checkEntryCollision(filepath.Dir(p.path)); err != nil {
			return err
		}
		if err := moveSkillExclusive(p.skillDir, p.path); err != nil {
			return err
		}
	}
	// Record the block and identity of any new instruction file before
	// publishing it. The existing private instruction text stays in its home.
	if p.nextInstructions != nil {
		if err := p.writeInstructions(); err != nil {
			return err
		}
	}
	if p.receipt.Stage != "" {
		if err := platform.SyncSkillDirectory(filepath.Dir(p.path)); err != nil {
			return err
		}
		p.receipt.Stage = ""
		return writeSkillJSON(p.receiptPath, p.receipt)
	}
	return nil
}

func (p *entryProjection) stageSkill() error {
	if err := mkdirSkillDirs(filepath.Dir(p.receiptPath)); err != nil {
		return err
	}
	root := filepath.Dir(p.path)
	if err := mkdirSkillDirs(root); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, skillStagePrefix)
	if err != nil {
		return err
	}
	file := filepath.Join(stage, "SKILL.md")
	journaling := false
	defer func() {
		if !journaling {
			_ = os.Remove(file)
			_ = os.Remove(stage)
		}
	}()
	if err := writeSkillFile(file, 0600, []byte(entrySkill)); err != nil {
		return err
	}
	p.receipt.DirectoryIdentity, err = platform.SkillFileIdentity(stage)
	if err != nil {
		return err
	}
	p.receipt.SkillIdentity, err = platform.SkillFileIdentity(file)
	if err != nil {
		return err
	}
	p.receipt.Stage = filepath.Base(stage)
	p.receipt.SkillDigest = digest([]byte(entrySkill))
	p.skillDir = stage
	if err := platform.SyncSkillDirectory(stage); err != nil {
		return err
	}
	if err := platform.SyncSkillDirectory(root); err != nil {
		return err
	}
	// A receipt write may report an error after publication. Keep its stage
	// from this point so a fresh preview can recover it.
	journaling = true
	receipt := p.receipt
	receipt.Block = ""
	return writeSkillJSON(p.receiptPath, receipt)
}

func (p *entryProjection) writeInstructions() error {
	dir := filepath.Dir(p.instructions)
	if err := mkdirSkillDirs(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".ctxferry-instructions-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(entryPermissions(p.instruction)); err == nil {
		_, err = f.Write(p.nextInstructions)
	}
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if p.Action == "install" {
		if p.instruction.info == nil {
			p.receipt.InstructionIdentity, err = platform.SkillFileIdentity(f.Name())
			if err != nil {
				return err
			}
		}
		if err := writeSkillJSON(p.receiptPath, p.receipt); err != nil {
			return err
		}
	}
	if err := p.instruction.verify(p.instructions); err != nil {
		return err
	}
	if p.instruction.info == nil {
		err = moveSkillExclusive(f.Name(), p.instructions)
	} else {
		err = platform.ReplaceSkillRecord(f.Name(), p.instructions)
	}
	if err != nil {
		return err
	}
	return platform.SyncSkillDirectory(dir)
}

func (p *entryProjection) uninstall() error {
	if p.removeInstructionsFile {
		if err := p.instruction.verify(p.instructions); err != nil {
			return err
		}
		if err := os.Remove(p.instructions); err != nil {
			return err
		}
		if err := platform.SyncSkillDirectory(filepath.Dir(p.instructions)); err != nil {
			return err
		}
	} else if p.nextInstructions != nil {
		if err := p.writeInstructions(); err != nil {
			return err
		}
	}
	if p.skillDir != "" {
		if err := p.removeOwnedSkill(); err != nil {
			return err
		}
	}
	if err := os.Remove(p.receiptPath); err != nil {
		return err
	}
	return platform.SyncSkillDirectory(filepath.Dir(p.receiptPath))
}

func (p *entryProjection) removeOwnedSkill() error {
	id, err := platform.SkillFileIdentity(p.skillDir)
	if err != nil || id != p.receipt.DirectoryIdentity {
		return Fail("skill-entry-conflict", "bootstrap directory changed after preview: "+p.skillDir)
	}
	file := filepath.Join(p.skillDir, "SKILL.md")
	if err := p.skill.verify(file); err != nil {
		return err
	}
	if p.skill.info != nil {
		if err := os.Remove(file); err != nil {
			return err
		}
		if err := platform.SyncSkillDirectory(p.skillDir); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(p.skillDir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		if err := os.Remove(p.skillDir); err != nil {
			return err
		}
		return platform.SyncSkillDirectory(filepath.Dir(p.skillDir))
	}
	return nil
}
