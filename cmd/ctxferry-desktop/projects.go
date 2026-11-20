package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

const (
	projectOverview = iota
	projectConfig
	projectContext
	projectTasks
	projectResources
	projectSkills
	projectArtifacts
)

type projectPage struct {
	host *desktop
	core app.App
	ctx  context.Context
	run  func(func() func())

	observedAt                                   time.Time
	onSelect                                     func(string)
	managementLink                               func(*ui.Context, int)
	started, busy                                bool
	projectChoice, checkoutChoice, profileChoice string
	profilePending                               string
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
	receipt *configurationReceiptState
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
				if p.target.ProjectID != "" {
					p.selectProject("")
				}
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
	if p.onSelect != nil {
		p.onSelect(id)
	}
	p.tab = projectOverview
	p.field, p.layer, p.layerChoice = 0, "device", layerLabel("device")
	p.fieldRows = ui.ListState{}
	p.notice, p.profilePending, p.receipt = "", "", nil
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
			if err == nil && p.receipt != nil && p.receipt.target == p.target {
				p.receipt.read = time.Now()
			}
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
		pageHeader(c, "Projects", "Shared Project definitions and local Checkouts.", nil)
		banner(c, toneBlocked, "The Project list is unavailable", "Refresh to reread this Workspace.", nil)
		projectError(c, p.catalogErr)
		return
	}
	project := p.currentProject()
	if project == nil {
		p.projectList(c)
		return
	}
	p.projectDetail(c, *project)
}

func (p *projectPage) projectList(c *ui.Context) {
	pageHeader(c, "Projects", "What each Project uses on this device.", nil)
	if p.busy && len(p.projects) == 0 {
		card(c).Children(func() { muted(c, "Reading local Projects…") })
		return
	}
	if len(p.projects) == 0 {
		card(c).Children(func() {
			cardHeader(c, icFolder, "No Projects yet", "Bind a local directory to create the first Project. No remote is required.", nil)
			d := p.host
			if d != nil {
				d.commandBlock(c, desktopCommand(d.core.DataDir, "project", "bind", "."), true)
				d.afterRun(c, "After binding, Refresh reads the Project and its new Checkout.")
			}
		})
		return
	}
	muted(c, "Grouped by subscription, then by name")
	p.projectGroup(c, "SUBSCRIBED ON "+strings.ToUpper(p.device), true)
	p.projectGroup(c, "NOT SUBSCRIBED", false)
}

func (p *projectPage) projectGroup(c *ui.Context, label string, subscribed bool) {
	var projects []app.DesktopProject
	for _, project := range p.projects {
		if project.Subscribed == subscribed {
			projects = append(projects, project)
		}
	}
	if len(projects) == 0 {
		return
	}
	sectionLabel(c, fmt.Sprintf("%s · %d", label, len(projects)), nil)
	card(c).Padding(0).Clip().Children(func() {
		ui.Column(c).Dividers(1, tk.Border).Children(func() {
			for _, project := range projects {
				project := project
				ui.Row(c).Gap(sp3).Padding(sp3, sp4).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(30, 30).Radius(radiusControl).Background(tk.AccentSoft).Center().Children(func() {
						ico(c, icFolder, 15, tk.Accent)
					})
					ui.Column(c).Grow(1).Gap(3).Children(func() {
						ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
							strong(c, project.Slug)
							idChip(c, project.ID)
						})
						state := fmt.Sprintf("%d local Checkouts · subscribed on this device", len(project.Checkouts))
						if !project.Subscribed {
							state = "Shared records only · subscribe to receive content"
						}
						muted(c, state)
					})
					ui.Button(c, "Open").Label("Open Project " + project.Slug).OnClick(func() { p.selectProject(project.ID) })
				})
			}
		})
	})
}

func (p *projectPage) projectDetail(c *ui.Context, project app.DesktopProject) {
	pageHeader(c, project.Slug, projectBindingStatus(project), nil)
	if !project.Subscribed {
		card(c).Children(func() {
			cardHeader(c, icFolder, "Not subscribed on this device", "The shared Project identity remains inspectable without substituting another writable target.", nil)
			kvGrid(c, []kvRow{{k: "Project", v: project.Slug}, {k: "Project ID", v: project.ID, mono: true}, {k: "Local Checkouts", v: fmt.Sprint(len(project.Checkouts))}})
			if p.host != nil {
				p.host.commandBlock(c, desktopCommand(p.host.core.DataDir, "project", "subscribe", project.Slug), true)
				p.host.afterRun(c, "After subscribing, Refresh restores this Project selection and reads its shared records.")
			}
		})
		return
	}
	p.scopeLine(c, project)
	if p.receipt != nil && p.receipt.target == p.target {
		p.configurationReceiptView(c)
	} else if p.notice != "" {
		banner(c, toneOk, "Saved locally", p.notice, nil)
	}
	if p.profilePending != "" {
		p.profileChangePreview(c, project)
	}
	if !p.editor.open && p.actionErr != nil {
		projectError(c, p.actionErr)
	}
	ui.Segmented(c, &p.tab, "Overview", "Configuration", "Context", "Tasks", "Resources", "Skills", "Artifacts").Label("Project sections").Disabled(p.busy)
	if p.tab != projectOverview && p.tab != projectConfig {
		p.loadRelated()
	}
	switch p.tab {
	case projectOverview:
		p.overviewView(c, project)
	case projectConfig:
		if p.configErr != nil {
			banner(c, toneBlocked, "Configuration is unavailable", "Reread the selected Project and Checkout before writing.", nil)
			projectError(c, p.configErr)
		} else if len(p.config.Fields) != 0 {
			p.configurationView(c)
		} else if p.busy {
			muted(c, "Reading Configuration…")
		}
	default:
		if p.managementLink != nil {
			p.managementLink(c, p.tab)
		}
		p.relatedView(c)
	}
	p.editorView(c, project)
}

func (p *projectPage) scopeLine(c *ui.Context, project app.DesktopProject) {
	items := []scopeItem{{label: "Project", value: project.Slug, id: project.ID, icon: icFolder}}
	if p.target.CheckoutID != "" {
		items = append(items, scopeItem{label: "Checkout", value: p.checkoutChoice, id: p.target.CheckoutID, icon: icFolder})
	}
	if p.config.Profile != nil {
		items = append(items, scopeItem{label: "Profile", value: p.config.Profile.Name, icon: icLayers})
	}
	scopeBar(c, items, nil)
}

func (p *projectPage) overviewView(c *ui.Context, project app.DesktopProject) {
	ui.Grid(c).ColumnTracks(ui.Fr(1.15), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icFolder, "Checkouts on this device", "Choose the exact local target before changing its Profile.", nil)
			if len(project.Checkouts) == 0 {
				muted(c, "This subscribed Project has no local Checkout. Shared records remain available where the core permits them.")
			}
			ui.Column(c).Gap(sp2).Children(func() {
				for _, checkout := range project.Checkouts {
					checkout := checkout
					selected := checkout.ID == p.target.CheckoutID
					listRow(c, selected, func() {
						ui.Column(c).Grow(1).Gap(3).Children(func() {
							ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
								mono(c, checkout.Path)
								if selected {
									pill(c, toneAccent, "Selected")
								}
							})
							ui.Row(c).Gap(sp2).Children(func() {
								idChip(c, checkout.ID)
								if selected && p.config.CheckoutProblem == "checkout-unavailable" {
									pill(c, toneAttention, "Directory unavailable")
								}
							})
						})
					}).OnClick(func() {
						p.target.CheckoutID = checkout.ID
						p.profilePending = ""
						p.readSelection()
					})
				}
			})
			ui.Divider(c)
			p.profilePicker(c)
		})
		card(c).Children(func() {
			cardHeader(c, icSliders, "Project tools", "Configuration, Context and Tasks keep this Checkout and Profile.", nil)
			p.overviewTool(c, icSliders, "Configuration", "Inspect effective values, origins and writable layers.", projectConfig)
			p.overviewTool(c, icFile, "Context", "Read available shared documents with stable scope.", projectContext)
			p.overviewTool(c, icTasks, "Tasks", "Inspect slots, credentials and execution trust.", projectTasks)
		})
	})
	card(c).Children(func() {
		cardHeader(c, icGrid, "Related Catalog records", "Open the same tool with this Project ID; Back restores this Project.", nil)
		ui.Row(c).Gap(sp3).Wrap().Children(func() {
			for _, tool := range []struct {
				page  int
				icon  *ui.SVG
				title string
			}{{projectResources, icServer, "Resources & environments"}, {projectSkills, icSkill, "Skills"}, {projectArtifacts, icPackage, "Run artifacts"}} {
				tool := tool
				linkButton(c, tool.title, icArrowR).OnClick(func() {
					if p.host == nil {
						return
					}
					page := pageFleet
					if tool.page == projectSkills {
						page = pageSkills
					} else if tool.page == projectArtifacts {
						page = pageArtifacts
					}
					p.tab = tool.page
					p.host.navigate(desktopRoute{Page: page, ProjectID: project.ID})
				})
			}
			if p.host != nil {
				linkButton(c, "Credential bindings", icArrowR).OnClick(func() {
					p.host.navigate(desktopRoute{Page: pageSecrets, ProjectID: project.ID})
				})
			}
		})
	})
}

func (p *projectPage) overviewTool(c *ui.Context, icon *ui.SVG, name, description string, tab int) {
	ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
		ico(c, icon, 16, tk.Accent)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			strong(c, name)
			muted(c, description)
		})
		linkButton(c, "Open", icChevR).OnClick(func() { p.tab = tab })
	})
}

func (p *projectPage) profileChangePreview(c *ui.Context, project app.DesktopProject) {
	current := noProjectProfile
	if p.config.Profile != nil {
		current = p.config.Profile.Name
	}
	previewPanel(c, "Change saved Profile", "Checkout "+p.checkoutChoice+" · "+p.target.CheckoutID, []effect{
		{toneAccent, project.Slug, current + " → " + p.profilePending},
		{toneNeutral, "Local only", "Reloads Configuration and Context, then recomputes Task trust for this Checkout."},
	}, nil, func() {
		ui.Row(c).Gap(sp2).Justify(ui.End).Children(func() {
			ui.Button(c, "Cancel").OnClick(func() { p.profilePending = "" })
			ui.PrimaryButton(c, "Save Profile for this Checkout").OnClick(func() {
				choice := p.profilePending
				p.profilePending = ""
				p.selectProfile(choice)
			})
		})
	})
}

func projectBindingStatus(p app.DesktopProject) string {
	state := "Subscribed on this device"
	if !p.Subscribed {
		state = "Not subscribed on this device"
	}
	return fmt.Sprintf("%s · %d local Checkouts", state, len(p.Checkouts))
}

func (p *projectPage) projectPicker(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "Project").TextColor(tk.Muted)
		labels := make([]string, 0, len(p.projects))
		for _, item := range p.projects {
			labels = append(labels, p.projectLabel(item))
		}
		if ui.Select(c, &p.projectChoice, labels).MinWidth(0).FillWidth().Label("Select Project").Disabled(p.busy).Changed() {
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

const noProjectCheckout = "No Checkout selected"
const noProjectProfile = "No Profile selected"

func (p *projectPage) checkoutPicker(c *ui.Context, project app.DesktopProject) {
	ui.Column(c).Grow(2).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "Checkout").TextColor(tk.Muted)
		choices := []string{noProjectCheckout}
		for _, binding := range project.Checkouts {
			choices = append(choices, binding.Path)
		}
		if ui.Select(c, &p.checkoutChoice, choices).MinWidth(0).FillWidth().Label("Select Checkout").Disabled(p.busy || len(project.Checkouts) == 0).Changed() {
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
			ui.Text(c, "The bound directory is unavailable").TextColor(tk.Muted)
		}
	})
}

func (p *projectPage) profilePicker(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
		ui.Text(c, "Saved Profile").TextColor(tk.Muted)
		names := append([]string{noProjectProfile}, p.config.Profiles...)
		disabled := p.busy || p.target.CheckoutID == "" || p.config.CheckoutProblem != ""
		if ui.Select(c, &p.profileChoice, names).MinWidth(0).FillWidth().Label("Select Profile").Disabled(disabled).Changed() {
			choice := p.profileChoice
			if p.config.Profile != nil {
				p.profileChoice = p.config.Profile.Name
			} else {
				p.profileChoice = noProjectProfile
			}
			if choice != noProjectProfile && choice != p.profileChoice {
				p.profilePending = choice
			}
		}
		if !p.busy && disabled && len(p.config.Profiles) != 0 {
			ui.Text(c, strings.Join(p.config.Profiles, ", ")).MinWidth(0).Multiline(true).TextColor(tk.Muted).FontSize(12)
		}
	})
}

func (p *projectPage) selectProfile(name string) {
	if p.host != nil && p.host.err != nil {
		return
	}
	p.receipt = nil
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
			checkout := "Checkout " + selection.Checkout.Path
			label := "Cleared the local Profile for " + checkout
			if selection.Profile != nil {
				label = "Saved Profile " + selection.Profile.Name + " for " + checkout
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
		message = "The local Workspace could not be read or written. Refresh and retry."
	case "configuration-changed", "override-not-found":
		message = "Configuration changed. Reread it and review a fresh preview."
	case "profile-not-found":
		message = "The selected Profile no longer exists. Choose another Profile for this Checkout."
	case "store-busy":
		message = "The local Workspace is busy. Retry after the current writer finishes."
	}
	ui.Text(c, message+" ("+problem.Code+")").Multiline(true).TextColor(tk.Blocked)
}
