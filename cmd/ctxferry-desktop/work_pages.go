package main

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func init() {
	desktopPages[pageCatalog] = (*desktop).catalogPage
	desktopPages[pageSkills] = func(d *desktop, c *ui.Context) {
		d.workHeading(c, "Skills", "Compare approved pins, local installations and candidate file changes.", d.skills.project)
		d.skillsPage(c)
	}
	desktopPages[pageArtifacts] = func(d *desktop, c *ui.Context) {
		d.workHeading(c, "Run artifacts", "Inspect provenance, content availability and verified exports.", d.artifacts.project)
		d.artifactsPage(c)
	}
}

func (d *desktop) catalogPage(c *ui.Context) {
	pageHeader(c, "Catalog", "Tools and records shared across Projects.", nil)
	if d.workProjects.err != nil {
		workMessage(c, "The Project directory could not be read", d.workProjects.err)
	}
	card(c).Children(func() {
		cardHeader(c, icFolder, "Project scope", "Scoped tools use subscribed Projects only. Device-wide Skills remain available without a Project.", nil)
		if len(d.workProjects.items) == 0 && !d.workProjects.loading {
			muted(c, "No subscribed Projects yet. Open Projects to subscribe or bind one; the device Skill catalog is still available below.")
			linkButton(c, "Open Projects", icArrowR).AlignSelf(ui.Start).OnClick(func() { d.navigateRoot(pageProjects) })
		} else {
			d.projectPicker(c, d.catalogProject, false, d.workProjects.loading, func(id string) { d.catalogProject = id })
		}
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).GapY(sp4).AlignItems(ui.Stretch).Children(func() {
		d.catalogToolCard(c, pageFleet, icServer, "Resources & environments", "Inspect configured endpoints, local routes, per-environment observations and exact TCP probe targets.", true)
		d.catalogToolCard(c, pageSecrets, icKey, "Credential bindings", "Map provider references without exposing values, then check authentication and entry readability separately.", true)
		d.catalogToolCard(c, pageSkills, icSkill, "Skills", "Compare approved pins with installed files, inspect candidate and drift diffs, and confirm local changes.", true)
		d.catalogToolCard(c, pageArtifacts, icPackage, "Run artifacts", "Filter runs, inspect provenance and digests, and verify or export available bytes to a new file.", true)
	})
	card(c).Children(func() {
		cardHeader(c, icSkill, "Device Skill catalog", "Includes local packages and installation receipts outside a single Project selection.", nil)
		linkButton(c, "Open device-wide Skills", icArrowR).AlignSelf(ui.Start).OnClick(func() {
			d.navigate(desktopRoute{Page: pageSkills})
		})
	})
}

func (d *desktop) catalogToolCard(c *ui.Context, page string, icon *ui.SVG, title, description string, scoped bool) {
	card(c).Children(func() {
		cardHeader(c, icon, title, description, nil)
		if scoped && d.catalogProject == "" {
			muted(c, "Choose a subscribed Project above to open this tool with an explicit scope.")
			return
		}
		linkButton(c, "Open "+title, icArrowR).AlignSelf(ui.Start).OnClick(func() {
			d.navigate(desktopRoute{Page: page, ProjectID: d.catalogProject})
		})
	})
}

func (d *desktop) workHeading(c *ui.Context, title, subtitle, project string) {
	pageHeader(c, title, subtitle, func() {
		if project != "" && desktopPages[pageProjects] != nil {
			linkButton(c, "Open Project", icArrowR).OnClick(func() { d.navigate(desktopRoute{Page: pageProjects, ProjectID: project}) })
		}
	})
	if project != "" {
		name := project
		for _, p := range d.workProjects.items {
			if p.ID == project {
				name = p.Slug
			}
		}
		scopeBar(c, []scopeItem{{label: "Project", value: name, id: project, icon: icFolder}}, nil)
	}
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
	if d.data.ReadAt.IsZero() {
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
		if d.catalogProject == "" && len(projects) > 0 {
			d.catalogProject = projects[0].ID
		}
		a := &d.artifacts
		if d.route.Page == pageArtifacts && a.project == "" && len(projects) > 0 {
			d.setArtifactProject(projects[0].ID)
		}
	})
}

func (d *desktop) projectPicker(c *ui.Context, id string, allowAll, disabled bool, changed func(string)) {
	if d.workProjects.err != nil {
		workMessage(c, "The Project directory could not be read", d.workProjects.err)
		return
	}
	options := make([]string, 0, len(d.workProjects.items)+1)
	allLabel := "Device catalog"
	if allowAll {
		options = append(options, allLabel)
	}
	label := "Select Project"
	if allowAll && id == "" {
		label = allLabel
	}
	for _, project := range d.workProjects.items {
		options = append(options, project.Slug)
		if project.ID == id {
			label = project.Slug
		}
	}
	if id != "" && label == "Select Project" {
		label = "Project unavailable"
	}
	workPicker(c, "Project scope", &label, options, disabled || d.workProjects.loading, func() {
		index := slices.IndexFunc(d.workProjects.items, func(p app.Project) bool { return p.Slug == label })
		if index >= 0 {
			changed(d.workProjects.items[index].ID)
		} else if allowAll && label == allLabel {
			changed("")
		}
	})
}

func workPicker(c *ui.Context, label string, value *string, choices []string, disabled bool, onChange func()) {
	ui.Column(c).Grow(1).MinWidth(120).Gap(4).Children(func() {
		ui.Text(c, label).TextColor(tk.Muted).FontSize(fsMeta)
		ui.Select(c, value, choices).FillWidth().Label(label).Disabled(disabled || len(choices) == 0).OnChange(onChange)
	})
}

func workMessage(c *ui.Context, label string, err error) {
	e := app.Classify(err)
	message := e.Message
	switch e.Code {
	case "store-error":
		message = "The request could not complete. Check the local data root and retry."
	case "not-subscribed":
		message = "This Project is not subscribed on the current device."
	case "skill-plan-changed":
		message = "The plan changed. Rescan and review the new diff."
	case "skill-candidate-changed":
		message = "The candidate pin changed. Refresh and select it again."
	case "skill-no-previous-version":
		message = "This target has no previous version available for rollback."
	case "artifact-hash-mismatch":
		message = "The content does not match its registered size or SHA-256."
	case "output-exists":
		message = "The destination file already exists. Choose another filename."
	}
	ui.Column(c).Gap(4).FillWidth().Padding(sp3, sp4).Radius(radiusCard).Background(tk.ConflictSoft).Children(func() {
		ui.Text(c, label).Bold().TextColor(tk.Conflict)
		ui.Text(c, message+" · "+e.Code).TextColor(tk.Conflict)
	})
}

var workWarning = tk.Conflict
var workWarningBG = tk.ConflictSoft

func workInspector(c *ui.Context, build func()) {
	card(c).WidthPercent(35).MinWidth(240).Children(build)
}

func workDetail(c *ui.Context, label, value string) {
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Text(c, label).FontSize(fsMeta).TextColor(tk.Muted)
		ui.Text(c, value).FillWidth().TextColor(tk.Text).Selectable().LineHeight(1.4)
	})
}
