package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
	"github.com/whynpc9/CtxFerry/internal/provider"
)

type overviewState struct {
	list     ui.ListState
	selected int
}

type overviewProject struct {
	id, name           string
	paths              []string
	pending, conflicts int
}

func (d *desktop) overview(c *ui.Context) {
	pageHeader(c, "Home", "What needs attention on this device.", nil)
	ui.Row(c).Gap(sp4).AlignItems(ui.Stretch).Children(func() {
		d.homeAttentionTile(c, tonePending, d.waitingCount(), "Waiting", "Saved here, not yet published.", queuePending)
		d.homeAttentionTile(c, toneConflict, d.conflictCount(), "Conflicts", "Keep a side or replace the document.", queueConflicts)
		d.homeAttentionTile(c, toneBlocked, d.blockedCount(), "Blocked", "Needs a fix before it can publish.", queueBlocked)
	})
	if d.data.SyncErr != nil {
		banner(c, toneAttention, "Sync evidence is unavailable", "Project bindings remain readable. Queue counts are unknown until Refresh succeeds; no empty state is inferred.", nil)
	} else if message := syncProblem(d.data.LastOutcome); message != "" {
		banner(c, toneAttention, "Publication needs attention", message, nil)
	}

	ui.Grid(c).ColumnTracks(ui.Fr(1.3), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Gap(sp2).Children(func() {
			cardHeader(c, icFolder, "Bound Projects", "Directories connected to this device.", func() {
				linkButton(c, "All Projects", icArrowR).OnClick(func() { d.navigateRoot(pageProjects) })
			})
			switch {
			case d.data.BindingsErr != nil:
				banner(c, toneBlocked, "Project bindings could not be read", "Refresh to retry. No Project list has been substituted.", nil)
			case len(d.data.Bindings) == 0:
				ui.Column(c).Gap(sp2).Padding(sp3, 0).Children(func() {
					strong(c, "No bound directories yet")
					muted(c, "Bind a directory with ctxferry project bind ., then Refresh to open its Project here.")
					d.commandBlock(c, desktopCommand(d.core.DataDir, "project", "bind", "."), true)
					d.afterRun(c, "After binding, Refresh reads the new Project and Checkout.")
				})
			default:
				ui.Column(c).Gap(0).Dividers(1, tk.Border).Children(func() {
					for _, project := range overviewProjects(d.data) {
						project := project
						ui.Row(c).Gap(sp3).Padding(sp3, 0).AlignItems(ui.Start).Children(func() {
							ui.Column(c).Width(140).Shrink(0).Gap(3).AlignItems(ui.Start).Children(func() {
								strong(c, project.name)
								idChip(c, project.id)
							})
							ui.Column(c).Grow(1).Gap(6).Children(func() {
								for _, path := range project.paths {
									ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
										ico(c, icFolder, 13, tk.Muted)
										cut(mono(c, path).Grow(1)).Tooltip(path)
										if info, err := os.Stat(path); err != nil || !info.IsDir() {
											pill(c, toneAttention, "Directory missing")
										}
									})
								}
							})
							linkButton(c, "Open", icChevR).Label("Open Project " + project.name).OnClick(func() {
								d.navigate(desktopRoute{Page: pageProjects, ProjectID: project.id})
							})
						})
					}
				})
			}
		})
		card(c).Children(func() {
			cardHeader(c, icLaptop, "This device", "Local evidence, separate from publication.", nil)
			kvGrid(c, []kvRow{
				{k: "Device", fn: func() { strong(c, d.data.Device.Name); idChip(c, d.data.Device.ID) }},
				{k: "System", v: d.data.Device.OS + " · " + d.data.Device.Arch},
				{k: "Data root", v: d.core.DataDir, mono: true},
				{k: "Catalog", v: "Schema 14 · compatible"},
				{k: "Last local read", v: d.data.ReadAt.Local().Format("2006-01-02 15:04:05")},
				{k: "Sync service", fn: func() { pill(c, d.serviceTone(), d.serviceLabel()) }},
				{k: "Last confirmed", fn: func() {
					if d.data.Sync.Head == "" {
						body(c, "No confirmed publication")
					} else {
						mono(c, shortCommit(d.data.Sync.Head))
						subtle(c, "·")
						body(c, observedTime(d.data.Sync.LastSyncAt))
					}
				}},
			})
		})
	})
}

func (d *desktop) homeAttentionTile(c *ui.Context, t tone, count int, label, description string, filter queueFilter) {
	strongC, soft := t.colors()
	card(c).Grow(1).Basis(0).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 28).Radius(radiusControl).Background(soft).Center().Children(func() {
				ico(c, t.icon(), 15, strongC)
			})
			strong(c, label)
		})
		value := strconv.Itoa(count)
		if d.data.SyncErr != nil {
			value = "—"
		}
		ui.Text(c, value).FontSize(fsStat).FontWeight(650).FontFeatures("tnum")
		muted(c, description)
		linkButton(c, "Queue · "+label, icArrowR).OnClick(func() {
			d.syncState.tab = syncQueueTab
			d.syncState.setFilter(filter)
			d.navigate(desktopRoute{Page: pageSync})
		})
	})
}

func overviewProjects(data app.DesktopSnapshot) []overviewProject {
	indices := map[string]int{}
	var projects []overviewProject
	for _, b := range data.Bindings {
		i, ok := indices[b.ProjectID]
		if !ok {
			i = len(projects)
			indices[b.ProjectID] = i
			projects = append(projects, overviewProject{id: b.ProjectID, name: b.Project})
		}
		projects[i].paths = append(projects[i].paths, b.Path)
	}
	for _, r := range data.Queue {
		if i, ok := indices[r.ProjectID]; ok {
			if r.Waiting() {
				projects[i].pending++
			}
			if r.Conflict {
				projects[i].conflicts++
			}
		}
	}
	return projects
}

func (d *desktop) queueLink(c *ui.Context, filter queueFilter, project, label string, visible bool) func() {
	if !visible {
		return nil
	}
	return func() {
		linkButton(c, label, icArrowR).OnClick(func() {
			d.syncState.tab = syncQueueTab
			d.syncState.setFilter(filter)
			d.navigate(desktopRoute{Page: pageSync, ProjectID: project})
		})
	}
}

func (d *desktop) connectionDetails(c *ui.Context) {
	s := d.data
	switch {
	case s.SyncErr != nil:
		detail(c, "Git authentication", "Unknown · sync evidence is unavailable")
	case s.Sync.Config == nil:
		detail(c, "Git authentication", "No sync repository configured")
	default:
		git := "Current authentication not checked · check the remote from Sync"
		switch s.LastOutcome {
		case "auth-required":
			git = "The last sync required authentication"
		case "unlock-required":
			git = "The last sync required an unlocked credential helper"
		}
		if check := d.currentRemoteCheck(); check != nil {
			switch {
			case check.GitReachable:
				git = "Remote read succeeded · authentication mechanism not inferred"
			case check.Failure == "auth-required":
				git = "Remote check requires authentication"
			case check.Failure == "unlock-required":
				git = "Remote check requires an unlocked credential helper"
			default:
				git = "Remote read unavailable · authentication not verified"
			}
		}
		detail(c, "Git authentication", git)
	}
	switch {
	case s.ProvidersErr != nil:
		detail(c, "Provider authentication", "Unknown · bindings could not be read")
	case len(s.Providers) == 0:
		detail(c, "Provider authentication", "No provider profiles referenced by bound Projects")
	default:
		for _, p := range s.Providers {
			detail(c, p.Profile.Provider, p.Profile.Name+" · "+providerStatus(p))
		}
	}
	detail(c, "Background sync", d.serviceLabel())
}

func providerStatus(p app.DoctorProvider) string {
	switch {
	case p.Headless == "scoped-runtime-session-configured":
		return "Scoped runtime session configured · login not verified"
	case p.Status == "unavailable" && p.StoredSession == provider.CredentialAbsent && p.Headless == "not-configured":
		return "No saved session · sign in if access is needed"
	case p.StoredSession == provider.CredentialPresent:
		return "Local session record present · login not verified"
	case p.Status == "unavailable":
		return "Local authentication unavailable · run ctxferry doctor"
	default:
		return "Local login state unknown"
	}
}

func (d *desktop) syncSummary(c *ui.Context) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		if d.conflictCount() > 0 {
			ico(c, icMerge, 14, tk.Conflict)
			strong(c, fmt.Sprint(d.conflictCount())).TextColor(tk.Conflict)
			muted(c, "conflicts")
		}
		if d.blockedCount() > 0 {
			ico(c, icBan, 14, tk.Blocked)
			strong(c, fmt.Sprint(d.blockedCount())).TextColor(tk.Blocked)
			muted(c, "blocked")
		}
		if d.waitingCount() > 0 {
			ico(c, icClock, 14, tk.Pending)
			strong(c, fmt.Sprint(d.waitingCount())).TextColor(tk.Pending)
			muted(c, "waiting")
		}
		if d.attentionCount() == 0 && d.waitingCount() == 0 {
			ico(c, icCheckCircle, 14, tk.Ok)
			muted(c, "All published")
		}
		if hold := d.waitingHold(); hold != "" && d.waitingCount() > 0 {
			ui.Box(c).Width(1).Height(16).Background(tk.BorderStrong)
			muted(c, hold)
		}
	})
}
