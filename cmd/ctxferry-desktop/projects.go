package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

const (
	projectContext = iota
	projectResources
	projectTasks
	projectConfig
	projectSkills
	projectArtifacts
)

type projectPage struct {
	core app.App
	ctx  context.Context
	run  func(func() func())

	started, busy                                bool
	projectChoice, checkoutChoice, profileChoice string
	projects                                     []app.DesktopProject
	target                                       app.ProjectConfigurationTarget
	config                                       app.ProjectConfiguration
	tab                                          int
	field                                        int
	fieldRows                                    ui.ListState
	layer, layerChoice                           string
	catalogErr                                   error
	configErr                                    error
	actionErr                                    error
	notice                                       string

	related projectRelated
	editor  projectEditor
	device  string
}

func (p *projectPage) work(read func() func()) {
	if p.busy {
		return
	}
	p.busy = true
	p.run(func() func() {
		finish := read()
		return func() {
			p.busy = false
			finish()
		}
	})
}

func (p *projectPage) currentProject() *app.DesktopProject {
	for i := range p.projects {
		if p.projects[i].ID == p.target.ProjectID {
			return &p.projects[i]
		}
	}
	return nil
}

func (p *projectPage) refresh() {
	if p.busy {
		return
	}
	p.started = true
	p.work(func() func() {
		projects, err := p.core.ReadDesktopProjects(p.ctx)
		return func() {
			p.catalogErr = err
			if err != nil {
				return
			}
			p.projects = projects
			project := p.currentProject()
			if project == nil {
				p.selectProject("")
				return
			}
			if !slices.ContainsFunc(project.Checkouts, func(b app.Binding) bool { return b.ID == p.target.CheckoutID }) {
				p.target.CheckoutID = ""
			}
			p.readSelection()
		}
	})
}

func (p *projectPage) selectProject(id string) {
	p.target = app.ProjectConfigurationTarget{ProjectID: id}
	p.tab = projectConfig
	p.field, p.layer, p.layerChoice = 0, "device", layerLabel("device")
	p.fieldRows = ui.ListState{}
	p.notice = ""
	if project := p.currentProject(); project != nil && len(project.Checkouts) == 1 {
		p.target.CheckoutID = project.Checkouts[0].ID
	}
	p.readSelection()
}

func (p *projectPage) readSelection() {
	p.actionErr = nil
	p.editor = projectEditor{}
	p.related = projectRelated{}
	p.config, p.configErr = app.ProjectConfiguration{}, nil
	p.checkoutChoice, p.profileChoice = noProjectCheckout, noProjectProfile
	project := p.currentProject()
	if project != nil {
		p.projectChoice = p.projectLabel(*project)
		for _, binding := range project.Checkouts {
			if binding.ID == p.target.CheckoutID {
				p.checkoutChoice = binding.Path
			}
		}
	}
	if project == nil || !project.Subscribed {
		return
	}
	target := p.target
	p.work(func() func() {
		data, err := p.core.ReadProjectConfiguration(p.ctx, target)
		return func() {
			p.config, p.configErr = data, err
			if data.Profile != nil {
				p.profileChoice = data.Profile.Name
			}
			p.loadRelated()
		}
	})
}

func (p *projectPage) view(c *ui.Context) {
	if !p.started {
		p.refresh()
	}
	if p.catalogErr != nil {
		section(c, "项目列表不可用")
		projectError(c, p.catalogErr)
		return
	}
	project := p.currentProject()
	if project == nil {
		section(c, "项目")
		if p.busy {
			ui.Text(c, "正在读取本机项目…").TextColor(muted)
		} else if len(p.projects) == 0 {
			ui.Text(c, "工作区尚无项目。可在终端使用 ctxferry project bind 绑定目录。").Multiline(true)
		}
		for _, item := range p.projects {
			ui.Row(c).FillWidth().Padding(10, 12).Gap(12).Background(panel).Radius(6).Children(func() {
				if ui.Button(c, item.Slug).Disabled(p.busy).Clicked() {
					p.selectProject(item.ID)
				}
				ui.Text(c, projectBindingStatus(item)).TextColor(muted).Grow(1).Multiline(true)
			})
		}
		return
	}
	if ui.Button(c, "返回项目列表").Disabled(p.busy).Clicked() {
		p.selectProject("")
		return
	}
	ui.Text(c, "项目 · "+project.Slug).FontSize(27).Bold().TextColor(ink)
	ui.Text(c, projectBindingStatus(*project)).TextColor(muted)
	if !project.Subscribed {
		ui.Text(c, "先通过 ctxferry project subscribe 订阅此项目，再查看配置和关联记录。").Multiline(true)
		return
	}
	ui.Row(c).FillWidth().Gap(12).Padding(12).Background(panel).Radius(6).Children(func() {
		p.projectPicker(c)
		p.profilePicker(c)
		p.checkoutPicker(c, *project)
	})
	if p.notice != "" {
		ui.Text(c, p.notice).FillWidth().Multiline(true).TextColor(ink).Background(panel).Padding(8, 10).Radius(4)
	}
	if !p.editor.open && p.actionErr != nil {
		projectError(c, p.actionErr)
	}
	ui.Segmented(c, &p.tab, "Context", "资源", "Tasks", "配置", "Skills", "产物").FillWidth().Disabled(p.busy)
	if p.tab != projectConfig {
		p.loadRelated()
		p.relatedView(c)
	} else if p.configErr != nil {
		section(c, "配置暂不可用")
		projectError(c, p.configErr)
	} else if len(p.config.Fields) != 0 {
		p.configurationView(c)
	} else if p.busy {
		ui.Text(c, "正在读取配置…").TextColor(muted)
	}
	p.editorView(c, *project)
}

func projectBindingStatus(p app.DesktopProject) string {
	state := "已订阅"
	if !p.Subscribed {
		state = "未订阅"
	}
	return fmt.Sprintf("%s · 本机 %d 个 Checkout", state, len(p.Checkouts))
}

func (p *projectPage) projectPicker(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "项目").TextColor(muted)
		labels := make([]string, 0, len(p.projects))
		for _, item := range p.projects {
			labels = append(labels, p.projectLabel(item))
		}
		if ui.Select(c, &p.projectChoice, labels).MinWidth(0).FillWidth().Label("选择项目").Disabled(p.busy).Changed() {
			for _, item := range p.projects {
				if p.projectLabel(item) == p.projectChoice {
					p.selectProject(item.ID)
					break
				}
			}
		}
	})
}

func (p *projectPage) projectLabel(item app.DesktopProject) string {
	if slices.ContainsFunc(p.projects, func(other app.DesktopProject) bool {
		return other.ID != item.ID && other.Slug == item.Slug
	}) {
		return item.Slug + " · " + item.ID
	}
	return item.Slug
}

const noProjectCheckout = "未选择 Checkout"
const noProjectProfile = "未选择配置方案"

func (p *projectPage) checkoutPicker(c *ui.Context, project app.DesktopProject) {
	ui.Column(c).Grow(2).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "Checkout").TextColor(muted)
		choices := []string{noProjectCheckout}
		for _, binding := range project.Checkouts {
			choices = append(choices, binding.Path)
		}
		if ui.Select(c, &p.checkoutChoice, choices).MinWidth(0).FillWidth().Label("选择 Checkout").Disabled(p.busy || len(project.Checkouts) == 0).Changed() {
			p.target.CheckoutID = ""
			for _, binding := range project.Checkouts {
				if binding.Path == p.checkoutChoice {
					p.target.CheckoutID = binding.ID
					break
				}
			}
			p.notice = ""
			p.readSelection()
		}
		if p.config.CheckoutProblem == "checkout-unavailable" {
			ui.Text(c, "绑定目录不可用").TextColor(muted)
		}
	})
}

func (p *projectPage) profilePicker(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "配置方案").TextColor(muted)
		names := append([]string{noProjectProfile}, p.config.Profiles...)
		disabled := p.busy || p.target.CheckoutID == "" || p.config.CheckoutProblem != ""
		if ui.Select(c, &p.profileChoice, names).MinWidth(0).FillWidth().Label("选择配置方案").Disabled(disabled).Changed() {
			choice := p.profileChoice
			if choice == noProjectProfile {
				choice = ""
			}
			p.selectProfile(choice)
		}
	})
}

func (p *projectPage) selectProfile(name string) {
	target := p.target
	p.actionErr = nil
	p.work(func() func() {
		selection, err := p.core.SelectProjectCheckoutProfile(p.ctx, target, name)
		return func() {
			if err != nil {
				p.profileChoice = noProjectProfile
				if p.config.Profile != nil {
					p.profileChoice = p.config.Profile.Name
				}
				p.actionErr = err
				return
			}
			label := "已清除此 Checkout 的本机配置方案"
			if selection.Profile != nil {
				label = "本机配置方案已切换为 " + selection.Profile.Name
			}
			p.notice = label
			p.readSelection()
		}
	})
}

func projectError(c *ui.Context, err error) {
	problem := app.Classify(err)
	message := problem.Message
	switch problem.Code {
	case "store-error":
		message = "无法读取或写入本机工作区。刷新后重试。"
	case "configuration-changed", "override-not-found":
		message = "配置已变化。请重新读取配置并核对新的预览。"
	case "profile-not-found":
		message = "选中的配置方案已不存在。请为此 Checkout 重新选择方案。"
	case "store-busy":
		message = "本机工作区正忙，请重试。"
	}
	ui.Text(c, message+"（"+problem.Code+"）").Multiline(true).TextColor(ui.Hex("#99412d"))
}
