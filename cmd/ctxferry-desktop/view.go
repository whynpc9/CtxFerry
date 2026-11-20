package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/store"
)

var (
	ink    = tk.Text
	panel  = tk.Surface
	line   = tk.Border
	accent = tk.Accent
)

func section(c *ui.Context, s string)   { h2(c, s) }
func secondary(c *ui.Context, s string) { muted(c, s) }
func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func detail(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().AlignItems(ui.Start).Gap(12).Padding(4, 0).Children(func() {
		ui.Text(c, label).Width(136).TextColor(tk.Muted).FontSize(fsMeta)
		ui.Text(c, value).Grow(1).TextColor(tk.Text).FontSize(fsBody).LineHeight(1.45)
	})
}

func notice(c *ui.Context, message string) {
	banner(c, toneAttention, "Attention", message, nil)
}

func (d *desktop) view(c *ui.Context) {
	c.SetTheme(theme())
	width, _ := c.Size()
	compact := width < 1100
	d.compact = compact
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(tk.Surface).Children(func() {
		if compact {
			d.rail(c)
		} else {
			d.wideSidebar(c)
		}
		ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			d.topBar(c, compact)
			key := d.route.Page
			if d.route.Page == pageSync {
				key = fmt.Sprintf("%s-%d", key, d.syncState.tab)
			}
			body := ui.Scroll(c.Key(key)).Grow(1).FillWidth().Background(tk.Page).Label("Page content")
			switch d.route.Page {
			case pageSync:
				body.TrackScroll(&d.syncState.scroll)
			case pageConflict:
				if d.conflict != nil {
					body.TrackScroll(&d.conflict.scroll)
				}
			case pageSecrets:
				body.TrackScroll(&d.secrets.scroll)
			case pageFleet:
				body.TrackScroll(&d.fleet.scroll)
			}
			body.Children(func() {
				var padding float32 = sp5
				var gap float32 = sp4
				if compact {
					padding, gap = sp4, sp3
				}
				ui.Column(c).FillWidth().Padding(padding, sp6).Gap(gap).Children(func() {
					if d.data.ReadAt.IsZero() {
						d.catalogState(c)
						return
					}
					if d.err != nil {
						banner(c, toneAttention, "Refresh failed; showing a stale snapshot",
							"The last successful local read is preserved. Writes, backup and remote checks stay locked until Refresh succeeds.", nil)
					}
					if render := desktopPages[d.route.Page]; render != nil {
						render(d, c)
					}
				})
			})
			if compact {
				d.compactActionBar(c)
			}
		})
	})
}

func (d *desktop) wideSidebar(c *ui.Context) {
	ui.Column(c).Width(sidebarW).Shrink(0).Background(tk.Canvas).BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).Children(func() {
		ui.Row(c).Gap(sp2).Padding(sp4, sp4, sp3, sp4).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(26, 26).Radius(7).Background(tk.Accent).Center().Children(func() {
				ico(c, icMark, 16, tk.AccentText)
			})
			ui.Text(c, "CtxFerry").FontSize(15).FontWeight(650)
		})
		ui.Column(c).Margin(0, sp3, sp3, sp3).Padding(sp1, sp2).Gap(3).Children(func() {
			overline(c, "WORKSPACE")
			if d.data.ReadAt.IsZero() {
				ui.Text(c, "Not opened").FontSize(fsBody).FontWeight(600).TextColor(tk.Muted)
			} else {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					cut(ui.Text(c, "Local Workspace").FontSize(fsBody).FontWeight(600).Grow(1))
					idChip(c, d.data.Workspace.ID)
				})
			}
			cut(mono(c, d.core.DataDir).FontSize(fsMono - 1).TextColor(tk.Muted)).Tooltip(d.core.DataDir)
		})
		d.sidebar(c)
		ui.Column(c).Padding(sp3, sp4, sp4, sp4).Gap(sp2).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icLaptop, 14, tk.Muted)
				ui.Text(c, valueOr(d.data.Device.Name, "This device")).FontSize(fsMeta).FontWeight(600).Grow(1)
				if d.data.Device.ID != "" {
					idChip(c, d.data.Device.ID)
				}
			})
			if d.data.ReadAt.IsZero() {
				footLine(c, toneUnknown, "No readable catalog at this data root")
				return
			}
			footLine(c, d.serviceTone(), "Sync service "+d.serviceLabel())
			if d.publicationUnknown() {
				footLine(c, toneAttention, "Publication outcome unknown")
			} else if d.data.Sync.Head == "" {
				footLine(c, toneUnknown, "No confirmed publication yet")
			} else {
				footLine(c, toneNeutral, "Confirmed "+shortCommit(d.data.Sync.Head)+" · "+observedTime(d.data.Sync.LastSyncAt))
			}
		})
	})
}

func footLine(c *ui.Context, t tone, s string) {
	col, _ := t.colors()
	ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
		if t == toneNeutral {
			dot(c, tk.Muted).Margin(3, 0, 0, 0)
		} else {
			ico(c, t.icon(), 12, col).Margin(2, 0, 0, 0)
		}
		ui.Text(c, s).FontSize(fsMeta).TextColor(tk.Muted).LineHeight(1.35).Grow(1).Shrink(1)
	})
}

func (d *desktop) rail(c *ui.Context) {
	ui.Column(c).Width(railW).Shrink(0).Background(tk.Canvas).BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).
		AlignItems(ui.Center).Padding(sp3, 0).Gap(sp1).Children(func() {
		ui.Box(c).Size(30, 30).Radius(8).Background(tk.Accent).Center().Margin(0, 0, sp3, 0).
			Tooltip("CtxFerry · " + d.core.DataDir).Children(func() { ico(c, icMark, 18, tk.AccentText) })
		for _, item := range []struct {
			page, label string
			icon        *ui.SVG
		}{{pageOverview, "Home", icHome}, {pageProjects, "Projects", icFolder}, {pageCatalog, "Catalog", icGrid}, {pageSync, "Sync", icSync}} {
			item := item
			on := primaryPage(d.route.Page) == item.page
			label := item.label
			if item.page == pageSync && d.attentionCount() > 0 {
				label += " · " + d.attentionText()
			}
			b := ui.ButtonBase(c).Size(40, 40).Radius(radiusControl).Center().Label(label).Tooltip(label).Disabled(d.data.ReadAt.IsZero())
			if on {
				b.Background(tk.AccentSoft).AccessSelected(true)
			}
			b.Children(func() {
				col := tk.Muted
				if on {
					col = tk.Accent
				}
				ico(c, item.icon, 18, col)
				if item.page == pageSync && d.attentionCount() > 0 {
					ui.Box(c).Absolute().Top(3).Right(2).Size(16, 16).Radius(8).Background(tk.Conflict).Center().Children(func() {
						ui.Text(c, fmt.Sprint(d.attentionCount())).FontSize(10).FontWeight(650).TextColor(tk.Surface)
					})
				}
			}).OnClick(func() { d.navigateRoot(item.page) })
		}
		ui.Spacer(c)
		ui.Box(c).Size(40, 40).Center().Tooltip(valueOr(d.data.Device.Name, "This device") + " · " + shortID(d.data.Device.ID)).Children(func() {
			ico(c, icLaptop, 18, tk.Muted)
		})
	})
}

func (d *desktop) topBar(c *ui.Context, compact bool) {
	ui.Row(c).Height(52).Shrink(0).Padding(0, sp6).Gap(sp3).AlignItems(ui.Center).Background(tk.Surface).
		BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		if back := d.backLabel(); back != "" && d.data.ReadAt.IsZero() == false {
			ui.Button(c, "").Label("Back to " + back).Tooltip("Back restores the calling scope, filter and selection").Children(func() {
				ui.Icon(c, icArrowL).FontSize(14)
				cut(ui.Text(c, back))
			}).OnClick(d.back)
			ui.Box(c).Width(1).Height(20).Background(tk.Border)
		}
		crumbs := d.breadcrumbs()
		if len(crumbs) > 0 {
			if compact && d.backLabel() != "" {
				crumbs = crumbs[len(crumbs)-1:]
			} else if compact && len(crumbs) > 3 {
				crumbs = []string{crumbs[0], "…", crumbs[len(crumbs)-1]}
			}
			chosen := len(crumbs) - 1
			ui.Breadcrumbs(c, crumbs, &chosen).Label("Location").Tooltip(strings.Join(d.breadcrumbs(), " › ")).Grow(1).Shrink(1)
		} else {
			ui.Spacer(c)
		}
		if d.data.ReadAt.IsZero() {
			return
		}
		if compact && d.route.Page != pageSync && d.attentionCount() > 0 {
			ui.Row(c).Shrink(0).Gap(5).Padding(4, sp2+2).Radius(radiusPill).Background(tk.ConflictSoft).AlignItems(ui.Center).
				Tooltip(d.attentionDetail()).Label("Sync: " + d.attentionText()).Children(func() {
				ico(c, icAlert, 12, tk.Conflict)
				cut(ui.Text(c, d.attentionText()).FontSize(fsMeta).FontWeight(560).TextColor(tk.Conflict))
			})
		}
		if d.err != nil {
			pill(c, toneAttention, "Stale · read "+d.data.ReadAt.Local().Format("15:04:05"))
		} else if !compact {
			subtle(c, "Read "+d.data.ReadAt.Local().Format("15:04:05")).Shrink(0)
		}
		label := "Refresh"
		if d.loading {
			label = "Refreshing…"
		}
		iconButton(c, icSync, label, false).Shrink(0).Label("Refresh local view").Disabled(d.loading || d.syncState.backingUp).OnClick(func() {
			d.refresh()
			if d.route.Page == pageSecrets || d.route.Page == pageFleet {
				d.refreshManagement()
			}
		})
	})
}

func (d *desktop) catalogState(c *ui.Context) {
	d.readinessView(c)
}

func pageHeading(c *ui.Context, title, subtitle string) {
	pageHeader(c, title, subtitle, nil)
}

func metrics(c *ui.Context, children func()) {
	card(c).FillWidth().Padding(sp3, sp4).Children(children)
}

func metric(c *ui.Context, label, value string, action func()) {
	ui.Column(c).Basis(0).Grow(1).Gap(4).Children(func() {
		secondary(c, label)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, value).FontSize(fsStat).FontWeight(650).TextColor(tk.Text)
			if action != nil {
				action()
			}
		})
	})
}

func observedTime(stamp *string) string {
	if stamp == nil {
		return "Not confirmed"
	}
	t, err := time.Parse(time.RFC3339Nano, *stamp)
	if err != nil {
		return "Time unavailable"
	}
	t = t.Local()
	if t.Format(time.DateOnly) == time.Now().Format(time.DateOnly) {
		return "today " + t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}

func shortCommit(head string) string {
	if len(head) > 7 {
		return head[:7]
	}
	return head
}

func (d *desktop) attentionCount() int {
	return d.conflictCount() + d.blockedCount()
}

func (d *desktop) conflictCount() int {
	if d.data.SyncErr != nil {
		return 0
	}
	seen := map[string]bool{}
	for _, record := range d.data.Queue {
		if record.Conflict {
			seen[record.ID] = true
		}
	}
	if len(seen) == 0 {
		return len(d.data.Sync.Conflicts)
	}
	return len(seen)
}

func (d *desktop) blockedCount() int {
	if d.data.SyncErr != nil {
		return 0
	}
	seen := map[string]bool{}
	for _, record := range d.data.Queue {
		if record.Blocked && !record.Conflict {
			seen[record.ID] = true
		}
	}
	if len(seen) == 0 && len(d.data.Queue) == 0 {
		return len(d.data.Sync.Blocked)
	}
	return len(seen)
}

func (d *desktop) waitingCount() int {
	if d.data.SyncErr != nil {
		return 0
	}
	return d.data.QueuePending
}

func (d *desktop) attentionText() string {
	n := d.attentionCount()
	if n == 1 {
		return "1 needs attention"
	}
	return fmt.Sprintf("%d need attention", n)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func (d *desktop) attentionDetail() string {
	parts := []string{}
	if n := d.conflictCount(); n > 0 {
		parts = append(parts, plural(n, "conflict", "conflicts"))
	}
	if n := d.blockedCount(); n > 0 {
		parts = append(parts, plural(n, "blocked", "blocked"))
	}
	if n := d.waitingCount(); n > 0 {
		parts = append(parts, plural(n, "waiting", "waiting"))
	}
	if len(parts) == 0 {
		return "All local records are published"
	}
	return strings.Join(parts, " · ")
}

func (d *desktop) serviceLabel() string {
	switch d.data.BackgroundSync.Status {
	case "running":
		return "running"
	case "installed":
		return "stopped"
	case "foreground-only":
		return "not installed"
	case "unavailable":
		return "unavailable"
	}
	return "unknown"
}

func (d *desktop) serviceTone() tone {
	if d.data.LastOutcome == "offline" {
		return toneUnknown
	}
	if d.data.BackgroundSync.Status == "running" {
		return toneOk
	}
	return toneUnknown
}

func (d *desktop) publicationUnknown() bool {
	return d.data.PublicationPending
}

func (d *desktop) waitingHold() string {
	switch {
	case d.publicationUnknown():
		return "Held until the uncertain publication is reconciled."
	case d.data.LastOutcome == "offline":
		return "Held while the remote is unreachable."
	case d.data.Sync.Head == "" && d.data.Sync.Config != nil:
		return "Held until the first publication."
	case d.data.BackgroundSync.Status != "running":
		return "Held until Sync runs."
	default:
		return ""
	}
}

func readinessError(err error) (string, string) {
	switch {
	case err == nil:
		return "", ""
	case errors.Is(err, store.ErrNotInitialized):
		return "Welcome to CtxFerry", "Set up this device by creating a Workspace or joining one. Nothing is written until you confirm."
	case errors.Is(err, store.ErrMigrationRequired):
		return "This catalog needs migration", "Open it once with a matching CtxFerry CLI, then Refresh. The desktop will not migrate it implicitly."
	case errors.Is(err, store.ErrUnsupportedSchema):
		return "This catalog is from a newer CtxFerry", "Update CtxFerry before opening this data root. Your existing catalog is preserved."
	default:
		return "The local catalog is unavailable", "Run ctxferry doctor for this data root, resolve the reported problem, then Refresh."
	}
}
