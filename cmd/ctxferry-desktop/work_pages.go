package main

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func init() {
	desktopPages[pageSkills] = func(d *desktop, c *ui.Context) {
		d.workHeading(c, "Skills", "核对共享意图和本机文件变更", d.skills.project)
		d.skillsPage(c)
	}
	desktopPages[pageArtifacts] = func(d *desktop, c *ui.Context) {
		d.workHeading(c, "产物", "追溯运行结果与内容位置", d.artifacts.project)
		d.artifactsPage(c)
	}
}

func (d *desktop) workHeading(c *ui.Context, title, subtitle, project string) {
	ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
		pageHeading(c, title, subtitle)
		if project != "" && desktopPages[pageProjects] != nil {
			ui.Spacer(c)
			ui.Button(c, "打开项目").OnClick(func() { d.navigate(desktopRoute{Page: pageProjects, ProjectID: project}) })
		}
	})
}

type workProjects struct {
	items           []app.Project
	loading, loaded bool
	err             error
}

func desktopJob[T any](d *desktop, read func(context.Context) (T, error), done func(T, error)) {
	if d.window == nil {
		value, err := read(context.Background())
		done(value, err)
		return
	}
	workspace, device := d.data.Workspace.ID, d.data.Device.ID
	runDesktop(d, read, func(value T, err error) {
		if d.data.Workspace.ID == workspace && d.data.Device.ID == device {
			done(value, err)
		}
	})
}

func (d *desktop) enterWorkPage(refresh bool) {
	if d.route.Page != pageSkills && d.route.Page != pageArtifacts || d.data.ReadAt.IsZero() {
		return
	}
	switch d.route.Page {
	case pageSkills:
		if d.route.ProjectID != "" && d.route.ProjectID != d.skills.project {
			d.setSkillProject(d.route.ProjectID)
		} else if !d.skills.loading && !d.skills.busy && (refresh || !d.skills.loaded) {
			d.loadSkills()
		}
	case pageArtifacts:
		if d.route.ProjectID != "" && d.route.ProjectID != d.artifacts.project {
			d.setArtifactProject(d.route.ProjectID)
		} else if !d.artifacts.loading && !d.artifacts.busy && (refresh || !d.artifacts.loaded) {
			d.loadArtifacts()
		}
	}
	d.loadWorkProjects(refresh)
}

func (d *desktop) loadWorkProjects(refresh bool) {
	p := &d.workProjects
	if p.loading || p.loaded && !refresh {
		return
	}
	p.loading = true
	desktopJob(d, d.core.ListDesktopProjects, func(projects []app.Project, err error) {
		p.items, p.err, p.loading, p.loaded = projects, err, false, true
		if err != nil {
			return
		}
		a := &d.artifacts
		if d.route.Page == pageArtifacts && a.project == "" && len(projects) > 0 {
			d.setArtifactProject(projects[0].ID)
		}
	})
}

func (d *desktop) projectPicker(c *ui.Context, id string, allowAll, disabled bool, changed func(string)) {
	if d.workProjects.err != nil {
		workMessage(c, "项目目录读取失败", d.workProjects.err)
		return
	}
	options := make([]string, 0, len(d.workProjects.items)+1)
	if allowAll {
		options = append(options, "全部 Skills")
	}
	label := "选择项目"
	if allowAll {
		label = "全部 Skills"
	}
	for _, project := range d.workProjects.items {
		options = append(options, project.Slug)
		if project.ID == id {
			label = project.Slug
		}
	}
	if id != "" && (label == "全部 Skills" || label == "选择项目") {
		label = "项目不可用"
	}
	workPicker(c, "项目", &label, options, disabled || d.workProjects.loading, func() {
		index := slices.IndexFunc(d.workProjects.items, func(p app.Project) bool { return p.Slug == label })
		if index >= 0 {
			changed(d.workProjects.items[index].ID)
		} else if allowAll && label == "全部 Skills" {
			changed("")
		}
	})
}

func workPicker(c *ui.Context, label string, value *string, choices []string, disabled bool, onChange func()) {
	ui.Column(c).Grow(1).MinWidth(120).Gap(4).Children(func() {
		ui.Text(c, label).TextColor(muted)
		ui.Select(c, value, choices).FillWidth().Label(label).Disabled(disabled || len(choices) == 0).OnChange(onChange)
	})
}

func workMessage(c *ui.Context, label string, err error) {
	e := app.Classify(err)
	message := e.Message
	switch e.Code {
	case "store-error":
		message = "无法完成请求。请检查本机目录后重试。"
	case "not-subscribed":
		message = "此项目尚未在本机订阅。"
	case "skill-plan-changed":
		message = "计划已变化。请重新扫描并核对差异。"
	case "skill-candidate-changed":
		message = "候选 pin 已变化。请刷新本地视图并重新选择。"
	case "skill-no-previous-version":
		message = "这个目标没有可恢复的上一版本。"
	case "artifact-hash-mismatch":
		message = "内容与登记的大小或 SHA-256 不一致。"
	case "output-exists":
		message = "目标文件已存在，请另选文件名。"
	}
	ui.Column(c).Gap(4).FillWidth().Padding(9, 12).Radius(6).Background(workWarningBG).Children(func() {
		ui.Text(c, label).Bold().TextColor(workWarning)
		ui.Text(c, message+" · "+e.Code).TextColor(workWarning)
	})
}

var workWarning = ui.Hex("#754a0b")
var workWarningBG = ui.Hex("#fff2d6")

func workInspector(c *ui.Context, build func()) {
	ui.Column(c).WidthPercent(35).MinWidth(210).Gap(8).Padding(14, 14).Radius(7).Background(panel).Children(build)
}

func workDetail(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(muted)
		ui.Text(c, value).FillWidth().TextColor(ink).Selectable()
	})
}
