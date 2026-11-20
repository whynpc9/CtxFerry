package main

import "github.com/egoist/mygo/ui"

const (
	pageOverview  = "overview"
	pageProjects  = "projects"
	pageCatalog   = "catalog"
	pageFleet     = "fleet"
	pageSecrets   = "secrets"
	pageSkills    = "skills"
	pageArtifacts = "artifacts"
	pageSync      = "sync"
	pageConflict  = "conflict"
)

type desktopRoute struct {
	Page, ProjectID, RecordID string
}

var desktopPages = map[string]func(*desktop, *ui.Context){}

func init() {
	desktopPages[pageOverview] = (*desktop).overview
	desktopPages[pageSync] = (*desktop).syncPage
}

func catalogTool(page string) bool {
	switch page {
	case pageFleet, pageSecrets, pageSkills, pageArtifacts:
		return true
	}
	return false
}

func primaryPage(page string) string {
	if catalogTool(page) {
		return pageCatalog
	}
	if page == pageConflict {
		return pageSync
	}
	return page
}

func (d *desktop) pushOrigin(route desktopRoute) {
	if route.Page == "" || route.Page == pageConflict {
		return
	}
	if len(d.origins) > 0 && d.origins[len(d.origins)-1] == route {
		return
	}
	d.origins = append(d.origins, route)
}

func (d *desktop) navigate(route desktopRoute) {
	if desktopPages[route.Page] == nil || d.route == route || d.writingConflict() || d.syncState.backingUp {
		return
	}
	if route.Page == pageConflict {
		d.syncState.returnRoute = d.route
		d.route = route
		d.openConflict(route.RecordID)
		return
	}

	origin := d.route
	preserveOrigin := false
	switch {
	case catalogTool(route.Page) && origin.Page != pageCatalog:
		d.pushOrigin(origin)
		preserveOrigin = true
	case route.Page == pageProjects && catalogTool(origin.Page):
		d.pushOrigin(origin)
		preserveOrigin = true
	case route.Page == pageSync && origin.Page == pageOverview:
		d.pushOrigin(origin)
		preserveOrigin = true
	}
	if !preserveOrigin && (route.Page == pageOverview || route.Page == pageProjects || route.Page == pageCatalog || route.Page == pageSync) {
		d.origins = nil
	}
	d.route = route
	d.conflict = nil
	d.enterWorkPage(false)
}

func (d *desktop) navigateRoot(page string) {
	if page == pageSync && d.route.Page == pageConflict {
		d.back()
		return
	}
	d.origins = nil
	d.navigate(desktopRoute{Page: page})
}

func (d *desktop) back() {
	if d.writingConflict() || d.syncState.backingUp {
		return
	}
	if d.route.Page == pageConflict {
		p := d.conflict
		d.conflict = nil
		d.route = d.syncState.returnRoute
		d.syncState.tab = syncQueueTab
		if p != nil {
			d.syncState.restoreRecord = p.id
			if p.review {
				d.refresh()
			}
		}
		return
	}
	if len(d.origins) > 0 {
		last := len(d.origins) - 1
		route := d.origins[last]
		d.origins = d.origins[:last]
		d.route = route
		d.enterWorkPage(false)
		return
	}
	if catalogTool(d.route.Page) {
		d.route = desktopRoute{Page: pageCatalog}
		d.enterWorkPage(false)
		return
	}
	if d.route.Page == pageProjects && d.route.ProjectID != "" {
		d.route.ProjectID = ""
		d.project.selectProject("")
	}
}

func originRouteLabel(route desktopRoute) string {
	switch route.Page {
	case pageOverview:
		return "Home"
	case pageProjects:
		if route.ProjectID != "" {
			return "Project"
		}
		return "Projects"
	case pageFleet:
		return "Resources & environments"
	case pageSecrets:
		return "Credential bindings"
	case pageSkills:
		return "Skills"
	case pageArtifacts:
		return "Run artifacts"
	case pageSync:
		return "Sync"
	case pageCatalog:
		return "Catalog"
	}
	return "Back"
}

func (d *desktop) backLabel() string {
	if d.route.Page == pageConflict {
		switch queueFilter(d.syncState.filter) {
		case queueConflicts:
			return "Queue · Conflicts"
		case queueBlocked:
			return "Queue · Blocked"
		case queuePending:
			return "Queue · Waiting"
		default:
			return "Queue"
		}
	}
	if len(d.origins) > 0 {
		return originRouteLabel(d.origins[len(d.origins)-1])
	}
	if catalogTool(d.route.Page) {
		return "Catalog"
	}
	if d.route.Page == pageProjects && d.route.ProjectID != "" {
		return "Projects"
	}
	return ""
}

func (d *desktop) breadcrumbs() []string {
	page := d.route.Page
	switch page {
	case pageOverview:
		return []string{"Home"}
	case pageProjects:
		crumbs := []string{"Projects"}
		if d.route.ProjectID != "" {
			name := d.route.ProjectID
			if project := d.project.currentProject(); project != nil {
				name = project.Slug
			}
			crumbs = append(crumbs, name)
		}
		return crumbs
	case pageCatalog:
		return []string{"Catalog"}
	case pageFleet:
		return []string{"Catalog", "Resources & environments"}
	case pageSecrets:
		return []string{"Catalog", "Credential bindings"}
	case pageSkills:
		return []string{"Catalog", "Skills"}
	case pageArtifacts:
		return []string{"Catalog", "Run artifacts"}
	case pageConflict:
		return []string{"Sync", "Queue", "Resolve conflict"}
	case pageSync:
		crumbs := []string{"Sync"}
		crumbs = append(crumbs, []string{"Repository", "Queue", "Backup"}[d.syncState.tab])
		return crumbs
	}
	return nil
}

func (d *desktop) sidebar(c *ui.Context) {
	selected := primaryPage(d.route.Page)
	previous := selected
	items := []struct {
		id, title string
		icon      *ui.SVG
	}{
		{pageOverview, "Home", icHome},
		{pageProjects, "Projects", icFolder},
		{pageCatalog, "Catalog", icGrid},
		{pageSync, "Sync", icSync},
	}
	ui.Sidebar(c, &selected, func() {
		ui.SidebarSection(c, "", nil, func() {
			for _, item := range items {
				entry := ui.SidebarItem(c, item.id, item.icon, item.title).Disabled(d.data.ReadAt.IsZero())
				if previous == item.id {
					entry.BorderWidth(0, 0, 0, 3).BorderColor(tk.Accent)
				}
				if item.id == pageSync {
					entry.Children(func() {
						if n := d.attentionCount(); n > 0 {
							ui.Badge(c, d.attentionText()).Background(tk.ConflictSoft).TextColor(tk.Conflict).
								Label(d.attentionText() + ": " + d.attentionDetail()).Tooltip(d.attentionDetail())
						}
					})
				}
				if entry.Clicked() {
					switch {
					case item.id == pageCatalog && catalogTool(d.route.Page):
						d.navigateRoot(pageCatalog)
					case item.id == pageSync && d.route.Page == pageConflict:
						d.back()
					case item.id == pageCatalog && d.route.Page == pageCatalog:
					case item.id == previous && d.route.Page == item.id:
					default:
						d.navigateRoot(item.id)
					}
				}
			}
		})
	}).Grow(1).FillWidth().Padding(0, sp2)
}
