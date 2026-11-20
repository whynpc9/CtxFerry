package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type skillsState struct {
	project, name, target, mode, ref string
	entries                          []app.DesktopSkill
	targetState                      app.DesktopSkillTarget
	preview                          app.DesktopSkillPreview
	action                           string
	candidate                        string
	loading, loaded, busy            bool
	readID                           uint64
	err                              error
	receipt                          string
	scanned                          time.Time
	row                              int
	table                            ui.ListState
	diff                             *editor.DiffState
	showLocal                        bool
	showPackages                     bool
}

func (d *desktop) setSkillProject(project string) {
	s := &d.skills
	if s.busy || s.project == project && s.loaded {
		return
	}
	s.project, s.name, s.ref, s.receipt = project, "", "", ""
	d.route.ProjectID = project
	s.candidate = "update"
	d.loadSkills()
}

func (d *desktop) loadSkills() {
	s := &d.skills
	project := s.project
	s.readID++
	readID := s.readID
	s.loading, s.err = true, nil
	desktopJob(d, func(ctx context.Context) ([]app.DesktopSkill, error) {
		return d.core.ListDesktopSkills(ctx, project)
	}, func(entries []app.DesktopSkill, err error) {
		if s.readID != readID {
			return
		}
		s.entries, s.err, s.loading, s.loaded = entries, err, false, true
		if err != nil || len(entries) == 0 {
			s.name = ""
			return
		}
		name := s.name
		if slices.IndexFunc(entries, func(entry app.DesktopSkill) bool { return entry.Name == name }) < 0 {
			s.name = entries[0].Name
			s.candidate = "update"
		}
		if s.target == "" {
			s.target, s.mode = "codex", "Managed copy"
		}
		d.loadSkillTarget()
	})
}

func (s *skillsState) clearPreview() {
	s.preview, s.action, s.scanned = app.DesktopSkillPreview{}, "", time.Time{}
	s.row, s.table, s.diff, s.showLocal, s.showPackages = -1, ui.ListState{}, nil, false, false
}

func (d *desktop) loadSkillTarget() {
	s := &d.skills
	s.clearPreview()
	s.readID++
	readID := s.readID
	s.loading, s.err, s.targetState = true, nil, app.DesktopSkillTarget{}
	name, target, link := s.name, s.target, s.mode == "Managed symlink"
	desktopJob(d, func(ctx context.Context) (app.DesktopSkillTarget, error) {
		return d.core.ReadDesktopSkillTarget(ctx, name, target, link)
	}, func(state app.DesktopSkillTarget, err error) {
		if s.readID != readID {
			return
		}
		s.targetState, s.err, s.loading = state, err, false
		if !state.CanRollback || s.candidate == "" {
			s.candidate = "update"
		}
	})
}

func (d *desktop) previewSkill(action string) {
	s := &d.skills
	input := app.SkillChangeInput{Name: s.name, Target: s.target, Ref: strings.TrimSpace(s.ref), Rollback: action == "rollback"}
	var selected *app.SkillPackage
	if input.Rollback {
		input.Ref = ""
		selected = s.targetState.Previous
	} else if s.targetState.Shared {
		selected = s.sharedPin()
	}
	link := s.mode == "Managed symlink"
	if s.targetState.Mode != "" {
		link = s.targetState.Mode == "symlink"
	}
	rescan := s.action == action
	var selectedPath string
	if rescan && s.row >= 0 && s.row < len(s.preview.Files) {
		selectedPath = s.preview.Files[s.row].Path
	}
	s.clearPreview()
	s.busy, s.err, s.action, s.receipt = true, nil, action, ""
	desktopJob(d, func(ctx context.Context) (app.DesktopSkillPreview, error) {
		if action == "install" {
			return d.core.PreviewDesktopSkillInstall(ctx, input.Name, input.Target, link)
		}
		return d.core.PreviewDesktopSkillChange(ctx, input, selected)
	}, func(preview app.DesktopSkillPreview, err error) {
		s.preview, s.err, s.busy = preview, err, false
		defer d.enterWorkPage(false)
		if rescan && err == nil {
			s.scanned = time.Now()
		}
		if len(preview.Files) > 0 {
			s.row = 0
			if index := slices.IndexFunc(preview.Files, func(f app.DesktopSkillDiff) bool { return f.Path == selectedPath }); index >= 0 {
				s.row = index
			} else if index := slices.IndexFunc(preview.Files, func(f app.DesktopSkillDiff) bool { return f.LocalChanged }); index >= 0 {
				s.row = index
			}
			s.showLocal = preview.Files[s.row].LocalChanged
			s.updateDiff()
		}
	})
}

func (d *desktop) confirmSkill() {
	if d.err != nil {
		return
	}
	s := &d.skills
	p := s.preview
	if s.busy || s.err != nil || p.Confirmation == "" {
		return
	}
	action := s.action
	input := app.SkillChangeInput{
		Name: s.name, Target: s.target, Ref: strings.TrimSpace(s.ref), Rollback: action == "rollback", Confirm: p.Confirmation,
	}
	if input.Rollback {
		input.Ref = ""
	}
	s.busy, s.receipt = true, ""
	s.preview.Confirmation = ""
	desktopJob(d, func(ctx context.Context) (string, error) {
		if action == "install" {
			plan, err := d.core.ApplyDesktopSkill(ctx, input.Name, input.Target, p.Mode == "symlink", input.Confirm)
			return plan.Path, err
		}
		plan, err := d.core.ChangeSkill(ctx, input)
		return plan.Path, err
	}, func(path string, err error) {
		s.busy, s.err = false, err
		defer d.enterWorkPage(false)
		if err != nil {
			return
		}
		s.receipt = fmt.Sprintf("%s complete · %s · %s", skillAction(action, p.Status), d.data.Device.Name, path)
		d.loadSkillTarget()
	})
}

func (d *desktop) skillsPage(c *ui.Context) {
	s := &d.skills
	card(c).Children(func() {
		ui.Row(c).FillWidth().Gap(sp3).Wrap().Children(func() {
			d.projectPicker(c, s.project, true, s.busy || s.loading, d.setSkillProject)
			names := make([]string, 0, len(s.entries))
			for _, entry := range s.entries {
				names = append(names, entry.Name)
			}
			workPicker(c, "Skill", &s.name, names, s.busy || s.loading, func() {
				s.ref, s.receipt = "", ""
				s.candidate = "update"
				d.loadSkillTarget()
			})
			workPicker(c, "Install target", &s.target, []string{"codex", "claude-code"}, s.busy || s.loading, func() {
				s.ref, s.receipt = "", ""
				s.candidate = "update"
				d.loadSkillTarget()
			})
			if s.targetState.Installed == nil && s.targetState.Status != "recovery-pending" {
				workPicker(c, "Install mode", &s.mode, []string{"Managed copy", "Managed symlink"}, s.busy || s.loading, d.loadSkillTarget)
			}
			if pin := s.sharedPin(); pin != nil {
				ui.Column(c).MinWidth(112).Gap(4).Children(func() {
					ui.Text(c, "Approved shared pin").TextColor(tk.Muted)
					ui.Text(c, skillShortPin(pin)).Bold().TextColor(tk.Text)
				})
			}
			if s.targetState.Status == "installed" && s.targetState.Installed != nil {
				s.candidatePicker(c)
			}
		})
	})
	if s.loading {
		muted(c, "Reading the Skill catalog and local installation…")
		return
	}
	if s.err != nil {
		workMessage(c, "Skill operation did not complete", s.err)
	}
	if s.name == "" {
		if s.err == nil {
			if s.project == "" {
				muted(c, "No Skills are registered in this Workspace. Use ctxferry skill add, then Refresh.")
			} else {
				muted(c, "This Project has no shared Skills selected.")
			}
		}
		return
	}
	if s.receipt != "" {
		banner(c, toneOk, "Local installation receipt", s.receipt+" · Shared pin approval is unchanged.", nil)
	}
	managementColumns(c, func() {
		if s.action != "" {
			d.skillPlanView(c)
		}
		d.skillInstallView(c)
	}, func() {
		selected := s.row >= 0 && s.row < len(s.preview.Files)
		if selected && !s.showPackages {
			s.skillFileInspector(c)
			ui.Button(c, "View versions and source").OnClick(func() { s.showPackages = true })
		} else {
			d.skillSourceInspector(c)
			if selected {
				ui.Button(c, "Back to selected file").OnClick(func() { s.showPackages = false })
			}
		}
	})
}

func (d *desktop) skillInstallView(c *ui.Context) {
	s := &d.skills
	state := s.targetState
	card(c).Children(func() {
		cardHeader(c, icSkill, "Local installation", "User-level target on "+d.data.Device.Name, nil)
		workDetail(c, "Target path", state.Path)
		switch state.Status {
		case "installed":
			label := "Installed"
			if len(state.Issues) != 0 {
				label = "Installed receipt present · files need review"
			}
			strong(c, label+" · "+skillMode(state.Mode))
			if state.Installed != nil && !state.Shared && state.Installed.SourceKind == "git" && s.candidate != "rollback" {
				muted(c, "Candidate Git pin")
				ui.TextInput(c, &s.ref).FillWidth().Label("Candidate Git pin").Placeholder("Full commit or refs/tags/<release>").Disabled(s.busy).OnChange(s.clearPreview)
			}
			actionLabel := "Preview " + skillAction(s.candidate, "") + "…"
			noApprovedPin := s.candidate != "rollback" && state.Shared && s.sharedPin() == nil
			ui.Button(c, actionLabel).Disabled(s.busy || state.Installed == nil || noApprovedPin).OnClick(func() { d.previewSkill(s.candidate) })
		case "recovery-pending":
			banner(c, toneAttention, "Interrupted installation", "Review the recovery plan before any other change to this target.", nil)
			ui.Button(c, "Preview recovery…").Disabled(s.busy).OnClick(func() { d.previewSkill("install") })
		default:
			muted(c, "Not installed")
			ui.Button(c, "Preview install…").Disabled(s.busy || len(state.Issues) > 0).OnClick(func() { d.previewSkill("install") })
		}
		if s.action == "" {
			skillIssues(c, state.Issues)
		}
	})
}

func (s *skillsState) candidatePicker(c *ui.Context) {
	state := s.targetState
	update := "Reread local source"
	switch {
	case state.Shared:
		update = "Approved shared pin"
		if pin := s.sharedPin(); pin != nil {
			update = "Shared · " + skillShortPin(pin)
		} else {
			update += " unavailable"
		}
	case state.Installed.SourceKind == "git":
		update = "Specified Git pin"
	}
	rollback := "Previous installed version"
	if state.Previous != nil {
		rollback = "Previous local · " + skillShortPin(state.Previous)
	}
	choices, selected := []string{update}, update
	if state.CanRollback {
		choices = append(choices, rollback)
		if s.candidate == "rollback" {
			selected = rollback
		}
	}
	workPicker(c, "Candidate pin", &selected, choices, s.busy, func() {
		s.candidate = "update"
		if selected == rollback {
			s.candidate = "rollback"
		}
		s.clearPreview()
	})
}

func (d *desktop) skillPlanView(c *ui.Context) {
	s := &d.skills
	p := &s.preview
	title := skillAction(s.action, p.Status)
	ui.Column(c).FillWidth().Gap(sp3).Padding(sp4).Radius(radiusCard).Background(tk.Surface).Border(1.5, tk.BorderStrong).BorderStyle(ui.BorderDashed).Children(func() {
		ui.Row(c).FillWidth().Gap(sp2).AlignItems(ui.Center).Wrap().Children(func() {
			previewBadge(c)
			section(c, title+" plan")
			if len(p.Issues) > 0 {
				localChanges := 0
				for _, file := range p.Files {
					if file.LocalChanged {
						localChanges++
					}
				}
				message := fmt.Sprintf("%d blocking issues · %s paused", len(p.Issues), title)
				if localChanges > 0 {
					message = fmt.Sprintf("%d files have local drift · %s paused", localChanges, title)
				}
				pill(c, toneAttention, message)
			} else if !s.scanned.IsZero() {
				pill(c, toneOk, "Rescanned · 0 blockers · "+s.scanned.Local().Format("15:04:05"))
			}
		})
		if s.busy {
			muted(c, "Building "+s.name+" · "+s.target+" plan…")
			return
		}
		if p.Status == "" {
			ui.Button(c, "Rescan and build plan").OnClick(func() { d.previewSkill(s.action) })
			return
		}
		if len(p.Issues) == 0 && s.scanned.IsZero() {
			switch p.Status {
			case "unchanged":
				muted(c, "The candidate pin matches the local installation.")
			case "installed":
				muted(c, "This target already has the planned version.")
			default:
				muted(c, fmt.Sprintf("%d file records · waiting for confirmation", len(p.Files)))
			}
		}
		if len(p.Files) > 0 {
			s.table.Selected = &s.row
			s.table.Key = func(row int) any { return p.Files[row].Path }
			s.table.Label = func(row int) string { return p.Files[row].Path }
			columns := []ui.TableColumn{{Title: "Target file"}, {Title: "Plan", Width: 82}, {Title: "Local copy", Width: 110}}
			ui.Table(c, &s.table, columns, len(p.Files), func(row, col int) {
				file := p.Files[row]
				value := file.Path
				if col == 1 {
					value = skillFileAction(file)
				}
				if col == 2 {
					switch {
					case file.LocalChanged:
						value = "Local drift"
					case file.Before == nil:
						value = "Target absent"
					default:
						value = "Matches receipt"
					}
				}
				ui.Text(c, value).NoWrap().Ellipsis("…").Tooltip(value)
			}).FillWidth().Height(float32(min(8, len(p.Files))+1)*31 + 10).OnChange(func() {
				if s.row >= 0 && s.row < len(p.Files) {
					s.showLocal = p.Files[s.row].LocalChanged
				}
				s.updateDiff()
			})
		}
		ui.Row(c).FillWidth().Gap(sp4).Wrap().Children(func() {
			if p.Before != nil && p.Before.Digest != "" {
				muted(c, "Installed · "+skillShortPin(p.Before))
			}
			if p.After != nil {
				strong(c, "Candidate · "+skillShortPin(p.After))
			}
		})
		if s.diff != nil {
			s.skillDiffView(c)
		}
		scope := "Writes only the local user-level target and installation receipt."
		if s.sharedPin() != nil {
			scope += " The approved shared pin remains unchanged."
		}
		muted(c, scope)
		confirmToken(c, confirmSpec{tokenLabel: "Skill plan token", token: p.Confirmation})
		ui.Row(c).Gap(sp2).Wrap().Children(func() {
			ui.Button(c, "Rescan and build plan").OnClick(func() { d.previewSkill(s.action) })
			ui.Spacer(c)
			ui.PrimaryButton(c, "Confirm "+title).Disabled(p.Confirmation == "" || s.err != nil).OnClick(d.confirmSkill)
		})
		skillIssues(c, p.Issues)
	})
}

func (s *skillsState) sharedPin() *app.SkillPackage {
	for _, entry := range s.entries {
		if entry.Name == s.name {
			return entry.Shared
		}
	}
	return nil
}

func (s *skillsState) skillDiffView(c *ui.Context) {
	file := s.preview.Files[s.row]
	label := "Installed version → candidate version"
	switch {
	case s.showLocal:
		label = "Installed version → current local file (drift)"
	case s.action == "install":
		label = "Existing target file → version to be written"
	}
	ui.Text(c, label+" · "+file.Path).Bold().TextColor(tk.Text)
	editor.DiffView(c, s.diff).FillWidth().Height(160)
}

func (d *desktop) skillSourceInspector(c *ui.Context) {
	s := &d.skills
	card(c).Children(func() {
		cardHeader(c, icLayers, "Installation intent and source", "Approved shared intent is independent of the local receipt.", nil)
		if pin := s.sharedPin(); pin != nil {
			workDetail(c, "Shared pin", skillPin(pin))
			workDetail(c, "Shared source", skillSource(pin))
		}
		if pkg := s.targetState.Installed; pkg != nil {
			ui.Divider(c)
			workDetail(c, "Installed pin", skillPin(pkg))
			workDetail(c, "Installation source", skillSource(pkg))
			if pkg.Commit != "" {
				workDetail(c, "Installed package SHA-256", pkg.Digest)
			}
			workDetail(c, "Ownership", "This device · CtxFerry · "+skillMode(s.targetState.Mode))
		} else if pkg := s.targetState.Available; pkg != nil {
			workDetail(c, "Available pin", skillPin(pkg))
			workDetail(c, "Package source", skillSource(pkg))
		}
		if pkg := s.preview.After; pkg != nil {
			ui.Divider(c)
			workDetail(c, "Candidate pin", skillPin(pkg))
			workDetail(c, "Candidate source", skillSource(pkg))
			workDetail(c, "License", pkg.License)
			if pkg.Commit != "" {
				workDetail(c, "Candidate package SHA-256", pkg.Digest)
			}
			if s.preview.Link != "" {
				workDetail(c, "Symlink source", s.preview.Link)
			}
		}
	})
}

func (s *skillsState) skillFileInspector(c *ui.Context) {
	file := s.preview.Files[s.row]
	card(c).Children(func() {
		cardHeader(c, icFile, "Selected target file", skillFileAction(file), nil)
		workDetail(c, "Destination", filepath.Join(s.preview.Path, filepath.FromSlash(file.Path)))
		if file.LocalChanged {
			banner(c, toneAttention, "Local drift", "The current file differs from the installation receipt. Candidate and drift diffs are separate views.", nil)
		}
		skillFileDigest(c, "Installed file", file.Before)
		skillFileDigest(c, "Candidate file", file.After)
		if file.LocalChanged {
			skillFileDigest(c, "Current local file", file.Local)
			ui.Button(c, "View installed → local drift").Disabled(s.showLocal).OnClick(func() {
				s.showLocal = true
				s.updateDiff()
			})
			ui.Button(c, "View installed → candidate").Disabled(!s.showLocal).OnClick(func() {
				s.showLocal = false
				s.updateDiff()
			})
		}
	})
}

func skillFileDigest(c *ui.Context, title string, file *app.DesktopSkillText) {
	if file == nil {
		return
	}
	label := title + " SHA-256"
	if file.Exec {
		label += " · executable"
	}
	switch file.Kind {
	case "binary":
		label += " · binary"
	case "large":
		label += " · above text preview limit"
	case "unavailable":
		label = title + " · content unavailable for preview"
	}
	workDetail(c, label, file.Digest)
}

func (s *skillsState) updateDiff() {
	s.diff = nil
	if s.row < 0 || s.row >= len(s.preview.Files) {
		return
	}
	file := s.preview.Files[s.row]
	before, after := file.Before, file.After
	if s.showLocal {
		after = file.Local
	}
	if before != nil && before.Kind != "text" || after != nil && after.Kind != "text" {
		return
	}
	var oldText, newText string
	if before != nil {
		oldText = before.Contents
	}
	if after != nil {
		newText = after.Contents
	}
	if oldText == newText {
		return
	}
	s.diff = &editor.DiffState{
		Old: editor.NewBuffer(oldText), New: editor.NewBuffer(newText),
		Layout: editor.DiffUnified, ExpandUnchanged: true, Wrap: true,
	}
}

func skillIssues(c *ui.Context, issues []app.SkillProblem) {
	for _, issue := range issues {
		message := issue.Message
		switch issue.Code {
		case "skill-external-edit":
			message = "Local files differ from the installation receipt. Reconcile them externally, then rescan."
		case "platform-unsupported":
			message = "This Skill does not apply to the current platform. " + issue.Message
		}
		ui.Text(c, message+" · "+issue.Code).FillWidth().TextColor(tk.Conflict)
		for _, path := range issue.Paths {
			ui.Text(c, path).FillWidth().Selectable().TextColor(tk.Conflict)
		}
	}
}

func skillAction(action, status string) string {
	switch action {
	case "rollback":
		return "rollback"
	case "update":
		return "update"
	default:
		if status == "recovery-pending" {
			return "recovery"
		}
		return "install"
	}
}

func skillFileAction(file app.DesktopSkillDiff) string {
	switch {
	case file.Before == nil && file.After != nil:
		return "Add"
	case file.Before != nil && file.After == nil:
		return "Delete"
	case file.Before != nil && file.After != nil && (file.Before.Digest != file.After.Digest || file.Before.Exec != file.After.Exec):
		return "Replace"
	default:
		return "Keep"
	}
}

func skillMode(mode string) string {
	if mode == "symlink" {
		return "Managed symlink"
	}
	return "Managed copy"
}

func skillPin(p *app.SkillPackage) string {
	pin := "sha256:" + p.Digest
	if p.Commit != "" {
		pin = p.Commit
	}
	if p.Release != "" {
		pin = p.Release + " · " + pin
	}
	return pin
}

func skillShortPin(p *app.SkillPackage) string {
	pin := p.Digest
	if p.Commit != "" {
		pin = p.Commit
	}
	return pin[:min(12, len(pin))]
}

func skillSource(p *app.SkillPackage) string {
	if p.SourceKind == "private" {
		return "Workspace private snapshot"
	}
	if p.Subdir != "" && p.Subdir != "." {
		return p.SourceURL + " · " + p.Subdir
	}
	return p.SourceURL
}
