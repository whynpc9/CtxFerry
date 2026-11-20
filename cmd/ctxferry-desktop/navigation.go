package main

import "github.com/egoist/mygo/ui"

const (
	pageOverview  = "overview"
	pageProjects  = "projects"
	pageFleet     = "fleet"
	pageSecrets   = "secrets"
	pageSkills    = "skills"
	pageArtifacts = "artifacts"
	pageSync      = "sync"
	pageConflict  = "conflict"
)

// desktopRoute carries IDs, never display labels. Conflict is a detail under
// Sync, so selecting the sidebar still returns to the queue's existing state.
type desktopRoute struct {
	Page, ProjectID, RecordID string
}

type desktopPage struct {
	id, title string
	icon      *ui.SVG
}

var desktopSections = []struct {
	title string
	pages []desktopPage
}{
	{"工作空间", []desktopPage{
		{pageOverview, "设备总览", navIcon("M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z")},
		{pageProjects, "项目", navIcon("M2 7h8l2 2h10v11H2z M3 7V4h7l2 3")},
	}},
	{"管理", []desktopPage{
		{pageFleet, "设备与资源", navIcon("M3 3h18v13H3z M8 21h8 M12 16v5")},
		{pageSecrets, "凭据绑定", navIcon("M14 3a5 5 0 1 1-2 9l-6 7H3v-4l7-7a5 5 0 0 1 4-5z")},
		{pageSkills, "Skills", navIcon("M12 2l10 5-10 5-10-5z M2 12l10 5 10-5 M2 17l10 5 10-5")},
		{pageArtifacts, "产物", navIcon("M3 6l9-4 9 4v13l-9 3-9-3z M3 6l9 5 9-5 M12 11v11")},
	}},
	{"维护", []desktopPage{
		{pageSync, "同步", navIcon("M3 10a9 9 0 0 1 16-5l2 3 M21 3v5h-5 M21 14a9 9 0 0 1-16 5l-2-3 M3 21v-5h5")},
	}},
}

func navIcon(path string) *ui.SVG {
	icon, err := ui.ParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" d="` + path + `"/></svg>`))
	if err != nil {
		panic(err)
	}
	return icon
}

// Each follow-up page can register its renderer in its own file. A renderer
// receives the current route and can use navigate and back on the window.
var desktopPages = map[string]func(*desktop, *ui.Context){}

func init() {
	desktopPages[pageOverview] = (*desktop).overview
	desktopPages[pageSync] = (*desktop).syncPage
}

func (d *desktop) navigate(route desktopRoute) {
	if desktopPages[route.Page] == nil || d.route == route {
		return
	}
	if route.Page == pageConflict {
		d.syncState.returnRoute = d.route
	}
	d.route = route
}

func (d *desktop) back() {
	if d.route.Page == pageConflict {
		d.route = d.syncState.returnRoute
	}
}

func (d *desktop) sidebar(c *ui.Context) {
	selected := d.route.Page
	if selected == pageConflict {
		selected = pageSync
	}
	previous := selected
	ui.Sidebar(c, &selected, func() {
		for _, group := range desktopSections {
			ui.SidebarSection(c, group.title, nil, func() {
				for _, item := range group.pages {
					entry := ui.SidebarItem(c, item.id, item.icon, item.title)
					if desktopPages[item.id] == nil {
						entry.Disabled(true)
					}
					if previous == item.id {
						entry.BorderWidth(0, 0, 0, 3).BorderColor(accent)
					}
					if d.route.Page == pageConflict && item.id == pageSync && entry.Clicked() {
						d.back()
					}
				}
			})
		}
	}).Grow(1).FillWidth()
	if selected != previous {
		d.navigate(desktopRoute{Page: selected})
	}
}
