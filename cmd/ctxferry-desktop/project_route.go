package main

import (
	"context"

	"github.com/egoist/mygo/ui"
)

func init() {
	desktopPages[pageProjects] = (*desktop).projectsView
}

func (d *desktop) projectsView(c *ui.Context) {
	p := &d.project
	if p.run == nil {
		p.core, p.ctx = d.core, d.ctx
		p.run = func(work func() func()) {
			runDesktop(d, func(context.Context) (func(), error) {
				return work(), nil
			}, func(finish func(), _ error) {
				finish()
			})
		}
	}
	p.device = d.data.Device.Name
	p.onSelect = func(id string) {
		if d.route.Page == pageProjects {
			d.route.ProjectID = id
		}
	}
	p.managementLink = d.projectManagementLink

	if !p.busy && !p.editor.open && !p.observedAt.Equal(d.data.ReadAt) {
		p.observedAt = d.data.ReadAt
		p.refresh()
	}
	if id := d.route.ProjectID; id != "" && id != p.target.ProjectID && !p.busy && p.catalogErr == nil {
		for _, project := range p.projects {
			if project.ID == id {
				p.selectProject(id)
				break
			}
		}
	}
	p.view(c)
}

func (d *desktop) projectManagementLink(c *ui.Context, tab int) {
	var page, label string
	switch tab {
	case projectResources:
		page, label = pageFleet, "打开设备与资源"
	case projectSkills:
		page, label = pageSkills, "打开 Skills 管理"
	case projectArtifacts:
		page, label = pageArtifacts, "打开产物管理"
	default:
		return
	}
	if desktopPages[page] == nil {
		return
	}
	if ui.Button(c, label).AlignSelf(ui.Start).Disabled(d.project.busy).Clicked() {
		d.navigate(desktopRoute{Page: page, ProjectID: d.project.target.ProjectID})
	}
}
