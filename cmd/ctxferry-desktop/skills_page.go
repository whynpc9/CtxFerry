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
	s.candidate = "update"
	d.loadSkills()
}

func (d *desktop) loadSkills() {
	s := &d.skills
	project := s.project
	s.loading, s.err = true, nil
	desktopJob(d, func(ctx context.Context) ([]app.DesktopSkill, error) {
		return d.core.ListDesktopSkills(ctx, project)
	}, func(entries []app.DesktopSkill, err error) {
		if s.project != project {
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
			s.target, s.mode = "codex", "受管复制"
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
	s.loading, s.err, s.targetState = true, nil, app.DesktopSkillTarget{}
	name, target, link := s.name, s.target, s.mode == "受管软链"
	desktopJob(d, func(ctx context.Context) (app.DesktopSkillTarget, error) {
		return d.core.ReadDesktopSkillTarget(ctx, name, target, link)
	}, func(state app.DesktopSkillTarget, err error) {
		if s.name != name || s.target != target {
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
	link := s.mode == "受管软链"
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
		if err != nil {
			return
		}
		s.receipt = fmt.Sprintf("%s完成 · %s · %s", skillAction(action, p.Status), d.data.Device.Name, path)
		d.loadSkillTarget()
	})
}

func (d *desktop) skillsPage(c *ui.Context) {
	s := &d.skills
	ui.Column(c).FillWidth().Padding(12, 14).Gap(9).Radius(7).Background(panel).Children(func() {
		ui.Row(c).FillWidth().Gap(12).Wrap().Children(func() {
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
			workPicker(c, "安装目标", &s.target, []string{"codex", "claude-code"}, s.busy || s.loading, func() {
				s.ref, s.receipt = "", ""
				s.candidate = "update"
				d.loadSkillTarget()
			})
			if s.targetState.Installed == nil && s.targetState.Status != "recovery-pending" {
				workPicker(c, "安装方式", &s.mode, []string{"受管复制", "受管软链"}, s.busy || s.loading, d.loadSkillTarget)
			}
			if pin := s.sharedPin(); pin != nil {
				ui.Column(c).MinWidth(112).Gap(4).Children(func() {
					ui.Text(c, "共享意图 pin").TextColor(muted)
					ui.Text(c, skillShortPin(pin)).Bold().TextColor(ink)
				})
			}
			if s.targetState.Status == "installed" && s.targetState.Installed != nil {
				s.candidatePicker(c)
			}
		})
	})
	if s.loading {
		ui.Text(c, "正在读取 Skill 目录和本机状态…").TextColor(muted)
		return
	}
	if s.err != nil {
		workMessage(c, "Skill 操作未完成", s.err)
	}
	if s.name == "" {
		if s.err == nil {
			if s.project == "" {
				ui.Text(c, "工作区还没有 Skill。使用 ctxferry skill add 登记来源后刷新。").TextColor(muted)
			} else {
				ui.Text(c, "此项目尚未选择共享 Skills。").TextColor(muted)
			}
		}
		return
	}
	if s.receipt != "" {
		ui.Text(c, s.receipt).FillWidth().Padding(8, 10).Radius(6).Background(panel).TextColor(ink)
	}
	ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(12).Children(func() {
			if s.action != "" {
				d.skillPlanView(c)
				ui.Divider(c)
			}
			d.skillInstallView(c)
		})
		workInspector(c, func() {
			selected := s.row >= 0 && s.row < len(s.preview.Files)
			if selected && !s.showPackages {
				s.skillFileInspector(c)
				ui.Button(c, "查看版本与来源").OnClick(func() { s.showPackages = true })
			} else {
				d.skillSourceInspector(c)
				if selected {
					ui.Button(c, "返回所选文件").OnClick(func() { s.showPackages = false })
				}
			}
		})
	})
}

func (d *desktop) skillInstallView(c *ui.Context) {
	s := &d.skills
	state := s.targetState
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		section(c, "本机安装")
		workDetail(c, "本机用户级目标 · "+d.data.Device.Name, state.Path)
		switch state.Status {
		case "installed":
			label := "已安装"
			if len(state.Issues) != 0 {
				label = "已登记安装 · 文件需要核对"
			}
			ui.Text(c, label+" · "+skillMode(state.Mode)).Bold().TextColor(ink)
			if state.Installed != nil && !state.Shared && state.Installed.SourceKind == "git" && s.candidate != "rollback" {
				ui.Text(c, "候选 Git pin").TextColor(muted)
				ui.TextInput(c, &s.ref).FillWidth().Label("候选 Git pin").Placeholder("完整 commit 或 refs/tags/<release>").Disabled(s.busy).OnChange(s.clearPreview)
			}
			actionLabel := "预览" + skillAction(s.candidate, "") + "…"
			noApprovedPin := s.candidate != "rollback" && state.Shared && s.sharedPin() == nil
			ui.Button(c, actionLabel).Disabled(s.busy || state.Installed == nil || noApprovedPin).OnClick(func() { d.previewSkill(s.candidate) })
		case "recovery-pending":
			ui.Text(c, "发现未完成的操作。先核对恢复计划。").TextColor(workWarning)
			ui.Button(c, "预览恢复…").Disabled(s.busy).OnClick(func() { d.previewSkill("install") })
		default:
			ui.Text(c, "尚未安装").TextColor(muted)
			ui.Button(c, "预览安装…").Disabled(s.busy || len(state.Issues) > 0).OnClick(func() { d.previewSkill("install") })
		}
		if s.action == "" {
			skillIssues(c, state.Issues)
		}
	})
}

func (s *skillsState) candidatePicker(c *ui.Context) {
	state := s.targetState
	update := "重新读取本机来源"
	switch {
	case state.Shared:
		update = "已批准共享 pin"
		if pin := s.sharedPin(); pin != nil {
			update = "共享 · " + skillShortPin(pin)
		} else {
			update += "不可用"
		}
	case state.Installed.SourceKind == "git":
		update = "指定 Git pin"
	}
	rollback := "上次安装版本"
	if state.Previous != nil {
		rollback = "上次本机 · " + skillShortPin(state.Previous)
	}
	choices, selected := []string{update}, update
	if state.CanRollback {
		choices = append(choices, rollback)
		if s.candidate == "rollback" {
			selected = rollback
		}
	}
	workPicker(c, "候选 pin", &selected, choices, s.busy, func() {
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
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
		section(c, title+"计划")
		if len(p.Issues) > 0 {
			message := fmt.Sprintf("%d 项阻塞 · %s已暂停", len(p.Issues), title)
			localChanges := 0
			for _, file := range p.Files {
				if file.LocalChanged {
					localChanges++
				}
			}
			if localChanges > 0 {
				message = fmt.Sprintf("%d 个文件有本地修改 · %s已暂停", localChanges, title)
			}
			ui.Text(c, message).Bold().Padding(4, 7).Radius(4).Background(workWarningBG).TextColor(workWarning)
		} else if !s.scanned.IsZero() {
			ui.Text(c, "已重新扫描 · 0 项阻塞 · "+s.scanned.Local().Format("15:04:05")).TextColor(ink)
		}
	})
	if s.busy {
		ui.Text(c, "正在处理 "+s.name+" · "+s.target+"…").TextColor(muted)
		return
	}
	if p.Status == "" {
		ui.Button(c, "重新扫描并生成计划").OnClick(func() { d.previewSkill(s.action) })
		return
	}
	if len(p.Issues) == 0 && s.scanned.IsZero() {
		switch p.Status {
		case "unchanged":
			ui.Text(c, "候选 pin 与本机安装一致。").TextColor(muted)
		case "installed":
			ui.Text(c, "此目标已是计划版本。").TextColor(muted)
		default:
			ui.Textf(c, "%d 项文件记录 · 等待确认", len(p.Files)).TextColor(muted)
		}
	}
	if len(p.Files) > 0 {
		s.table.Selected = &s.row
		s.table.Key = func(row int) any { return p.Files[row].Path }
		s.table.Label = func(row int) string { return p.Files[row].Path }
		columns := []ui.TableColumn{{Title: "目标文件"}, {Title: "计划", Width: 72}, {Title: "本机副本", Width: 90}}
		ui.Table(c, &s.table, columns, len(p.Files), func(row, col int) {
			file := p.Files[row]
			value := file.Path
			if col == 1 {
				value = skillFileAction(file)
			}
			if col == 2 {
				switch {
				case file.LocalChanged:
					value = "本地修改"
				case file.Before == nil:
					value = "目标不存在"
				default:
					value = "与记录一致"
				}
			}
			ui.Text(c, value).NoWrap().Ellipsis("…")
		}).FillWidth().Height(float32(min(8, len(p.Files))+1)*31 + 10).OnChange(func() {
			if s.row >= 0 && s.row < len(p.Files) {
				s.showLocal = p.Files[s.row].LocalChanged
			}
			s.updateDiff()
		})
	}
	ui.Row(c).FillWidth().Gap(16).Wrap().Children(func() {
		if p.Before != nil && p.Before.Digest != "" {
			ui.Text(c, "本机已装 · "+skillShortPin(p.Before)).TextColor(muted)
		}
		if p.After != nil {
			ui.Text(c, "候选包 · "+skillShortPin(p.After)).TextColor(ink)
		}
	})
	if s.diff != nil {
		s.skillDiffView(c)
	}
	scope := "写入本机用户级目标和安装回执。"
	if s.sharedPin() != nil {
		scope += "共享安装意图保留原值。"
	}
	ui.Text(c, scope).FillWidth().TextColor(muted)
	ui.Row(c).Gap(8).Wrap().Children(func() {
		ui.Button(c, "重新扫描并生成计划").OnClick(func() { d.previewSkill(s.action) })
		ui.PrimaryButton(c, "确认"+title).Disabled(p.Confirmation == "" || s.err != nil).OnClick(d.confirmSkill)
	})
	skillIssues(c, p.Issues)
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
	label := "已安装版本 → 候选版本"
	switch {
	case s.showLocal:
		label = "已安装版本 → 当前本机文件"
	case s.action == "install":
		label = "目标文件 → 将写入的版本"
	}
	ui.Text(c, label+" · "+file.Path).Bold().TextColor(ink)
	editor.DiffView(c, s.diff).FillWidth().Height(132)
}

func (d *desktop) skillSourceInspector(c *ui.Context) {
	s := &d.skills
	section(c, "安装意图与来源")
	if pin := s.sharedPin(); pin != nil {
		workDetail(c, "共享 pin", skillPin(pin))
		workDetail(c, "共享来源", skillSource(pin))
	}
	if pkg := s.targetState.Installed; pkg != nil {
		ui.Divider(c)
		workDetail(c, "本机已装 pin", skillPin(pkg))
		workDetail(c, "安装来源", skillSource(pkg))
		if pkg.Commit != "" {
			workDetail(c, "已装包 SHA-256", pkg.Digest)
		}
		workDetail(c, "所有权", "本设备 · CtxFerry · "+skillMode(s.targetState.Mode))
	} else if pkg := s.targetState.Available; pkg != nil {
		workDetail(c, "可安装 pin", skillPin(pkg))
		workDetail(c, "包来源", skillSource(pkg))
	}
	if pkg := s.preview.After; pkg != nil {
		ui.Divider(c)
		workDetail(c, "候选 pin", skillPin(pkg))
		workDetail(c, "候选来源", skillSource(pkg))
		workDetail(c, "许可证", pkg.License)
		if pkg.Commit != "" {
			workDetail(c, "候选包 SHA-256", pkg.Digest)
		}
		if s.preview.Link != "" {
			workDetail(c, "软链来源", s.preview.Link)
		}
	}
}

func (s *skillsState) skillFileInspector(c *ui.Context) {
	file := s.preview.Files[s.row]
	section(c, "所选目标文件")
	workDetail(c, skillFileAction(file), filepath.Join(s.preview.Path, filepath.FromSlash(file.Path)))
	if file.LocalChanged {
		ui.Text(c, "本地修改 · 与安装回执不同").Bold().TextColor(workWarning)
	}
	skillFileDigest(c, "已安装文件", file.Before)
	skillFileDigest(c, "候选文件", file.After)
	if file.LocalChanged {
		skillFileDigest(c, "本机文件", file.Local)
		ui.Button(c, "查看本地修改").Disabled(s.showLocal).OnClick(func() {
			s.showLocal = true
			s.updateDiff()
		})
		ui.Button(c, "查看候选变更").Disabled(!s.showLocal).OnClick(func() {
			s.showLocal = false
			s.updateDiff()
		})
	}
}

func skillFileDigest(c *ui.Context, title string, file *app.DesktopSkillText) {
	if file == nil {
		return
	}
	label := title + " SHA-256"
	if file.Exec {
		label += " · 可执行"
	}
	switch file.Kind {
	case "binary":
		label += " · 二进制"
	case "large":
		label += " · 超过文本预览上限"
	case "unavailable":
		label = title + " · 内容不可预览"
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
			message = "本机安装与回执不同。请先在外部处理这些文件，再重新扫描。"
		case "platform-unsupported":
			message = "这个 Skill 不适用于本机平台。 " + issue.Message
		}
		ui.Text(c, message+" · "+issue.Code).FillWidth().TextColor(workWarning)
		for _, path := range issue.Paths {
			ui.Text(c, path).FillWidth().Selectable().TextColor(workWarning)
		}
	}
}

func skillAction(action, status string) string {
	switch action {
	case "rollback":
		return "回滚"
	case "update":
		return "更新"
	default:
		if status == "recovery-pending" {
			return "恢复"
		}
		return "安装"
	}
}

func skillFileAction(file app.DesktopSkillDiff) string {
	switch {
	case file.Before == nil && file.After != nil:
		return "新增"
	case file.Before != nil && file.After == nil:
		return "删除"
	case file.Before != nil && file.After != nil && (file.Before.Digest != file.After.Digest || file.Before.Exec != file.After.Exec):
		return "替换"
	default:
		return "保留"
	}
}

func skillMode(mode string) string {
	if mode == "symlink" {
		return "受管软链"
	}
	return "受管复制"
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
		return "工作区私有快照"
	}
	if p.Subdir != "" && p.Subdir != "." {
		return p.SourceURL + " · " + p.Subdir
	}
	return p.SourceURL
}
