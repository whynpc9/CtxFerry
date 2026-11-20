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
	p.host = d
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
		page, label = pageFleet, "Open Resources & environments"
	case projectSkills:
		page, label = pageSkills, "Open Skills"
	case projectArtifacts:
		page, label = pageArtifacts, "Open Run artifacts"
	default:
		return
	}
	if desktopPages[page] == nil {
		return
	}
	card(c).Children(func() {
		cardHeader(c, icGrid, "Catalog handoff", "The Project ID is supplied automatically; Back restores this Project section.", nil)
		linkButton(c, label, icArrowR).AlignSelf(ui.Start).Disabled(d.project.busy).OnClick(func() {
			d.navigate(desktopRoute{Page: page, ProjectID: d.project.target.ProjectID})
		})
	})
}
