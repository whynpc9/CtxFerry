package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// chrome describes the application shell around a page.
type chrome struct {
	nav    string   // "home", "projects", "catalog", "sync"; "" when gated
	crumbs []string // location trail, last item is the current view
	back   string   // origin restored by Back; "" at a destination root
	s      snap     // evidence shared by sidebar, top bar and page

	// compact renders the 1024x680 layout: a 56 DIP icon rail, and the
	// sidebar's evidence moved into the top bar so it stays visible.
	compact bool
	// actions is an optional bar pinned under the scrolling page. Compact
	// frames put the confirm control of a preview here so that it is
	// never scrolled out of view.
	actions func()
	// pageExplainsAttention is set when the page itself carries the
	// attention legend (Sync pages with tabs), so the compact top bar does
	// not repeat it.
	pageExplainsAttention bool
}

// shell lays out sidebar | (top bar / page). The page scrolls; frames are
// composed so that their content fits the 1280x800 window.
func shell(c *ui.Context, ch chrome, page func()) {
	c.SetTheme(theme())
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(tk.Surface).Children(func() {
		if ch.compact {
			rail(c, ch)
		} else {
			sidebar(c, ch)
		}
		ui.Column(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			topBar(c, ch)
			ui.Scroll(c).Grow(1).Background(tk.Page).Label("Page content").Children(func() {
				col := ui.Column(c).Padding(sp5, sp6, sp6, sp6).Gap(sp4)
				if ch.compact {
					col.Padding(sp4, sp4, sp4, sp4).Gap(sp3)
				}
				col.Children(page)
			})
			if ch.actions != nil {
				ui.Row(c).Shrink(0).Gap(sp2).Padding(sp3, sp4).AlignItems(ui.Center).Background(tk.Sunken).
					BorderWidth(1, 0, 0, 0).BorderColor(tk.BorderStrong).Children(ch.actions)
			}
		})
	})
}

func sidebar(c *ui.Context, ch chrome) {
	s := ch.s
	ui.Column(c).Width(sidebarW).Shrink(0).Background(tk.Canvas).BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).Children(func() {
		ui.Row(c).Gap(sp2).Padding(sp4, sp4, sp3, sp4).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(26, 26).Radius(7).Background(tk.Accent).Center().Children(func() {
				ico(c, icMark, 16, tk.AccentText)
			})
			ui.Text(c, "CtxFerry").FontSize(15).FontWeight(650)
		})
		ui.Column(c).Margin(0, sp3, sp3, sp3).Padding(sp1, sp2).Gap(3).Children(func() {
			overline(c, "WORKSPACE")
			if s.gate {
				ui.Text(c, "Not opened").FontSize(fsBody).FontWeight(600).TextColor(tk.Muted)
			} else {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					cut(ui.Text(c, wsName).FontSize(fsBody).FontWeight(600).Grow(1))
					idChip(c, wsID)
				})
			}
			cut(mono(c, "~/.local/share/ctxferry").FontSize(fsMono - 1).TextColor(tk.Muted)).Tooltip(dataRoot)
		})

		sel := ch.nav
		ui.Sidebar(c, &sel, func() {
			ui.SidebarSection(c, "", nil, func() {
				ui.SidebarItem(c, "home", icHome, "Home").Disabled(s.gate)
				ui.SidebarItem(c, "projects", icFolder, "Projects").Disabled(s.gate)
				ui.SidebarItem(c, "catalog", icGrid, "Catalog").Disabled(s.gate)
				ui.SidebarItem(c, "sync", icSync, "Sync").Disabled(s.gate).Children(func() {
					if n := s.attention(); n > 0 {
						ui.Badge(c, s.attentionText()).Background(tk.ConflictSoft).TextColor(tk.Conflict).
							Label(s.attentionText() + ": " + s.attentionDetail()).
							Tooltip(s.attentionDetail())
					}
				})
			})
		}).Grow(1).Padding(0, sp2)

		// Evidence footer: every line comes from ch.s.
		ui.Column(c).Padding(sp3, sp4, sp4, sp4).Gap(sp2).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icLaptop, 14, tk.Muted)
				ui.Text(c, deviceName).FontSize(fsMeta).FontWeight(600).Grow(1)
				if !s.gate {
					idChip(c, deviceID)
				}
			})
			if s.gate {
				footLine(c, toneUnknown, "No catalog at this data root")
				return
			}
			if s.offline {
				footLine(c, toneUnknown, "Offline · remote unreachable")
			} else {
				t, txt := s.serviceText()
				footLine(c, t, txt)
			}
			switch {
			case s.unknown:
				footLine(c, toneAttention, "Publication outcome unknown")
			case s.confirmed == "":
				footLine(c, toneUnknown, "No confirmed publication yet")
			default:
				footLine(c, toneNeutral, "Confirmed "+s.confirmed+" · "+s.confirmedAt)
			}
			// The read time lives only in the top bar, next to Refresh.
		})
	})
}

// footLine pairs a status shape with text, so the line never relies on colour.
func footLine(c *ui.Context, t tone, s string) {
	col, _ := t.colors()
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		if t == toneNeutral {
			dot(c, tk.Muted)
		} else {
			ico(c, t.icon(), 12, col)
		}
		ui.Text(c, s).FontSize(fsMeta).TextColor(tk.Muted).LineHeight(1.35).Grow(1).Shrink(1)
	})
}

func topBar(c *ui.Context, ch chrome) {
	ui.Row(c).Height(52).Shrink(0).Padding(0, sp6).Gap(sp3).AlignItems(ui.Center).Background(tk.Surface).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Children(func() {
		if ch.back != "" {
			ui.Button(c, "").Label("Back to " + ch.back).Tooltip("Back restores the calling scope, filter and selection").Children(func() {
				ui.Icon(c, icArrowL).FontSize(14)
				cut(ui.Text(c, ch.back))
			})
			ui.Box(c).Width(1).Height(20).Background(tk.Border)
		}
		if len(ch.crumbs) > 0 {
			crumbs := ch.crumbs
			if ch.compact && ch.back != "" {
				// Compact with Back: Back names the origin, so only the
				// current item stays; the full trail is in the tooltip.
				crumbs = crumbs[len(crumbs)-1:]
			} else if ch.compact && len(crumbs) > 3 {
				// Compact, deeper than three: root, an ellipsis for the middle, and the current item.
				crumbs = []string{crumbs[0], "…", crumbs[len(crumbs)-1]}
			}
			chosen := len(crumbs) - 1
			ui.Breadcrumbs(c, crumbs, &chosen).Label("Location").Tooltip(strings.Join(ch.crumbs, " › ")).Grow(1).Shrink(1)
		} else {
			ui.Spacer(c)
		}
		if ch.s.gate {
			return
		}
		if ch.compact {
			if !ch.pageExplainsAttention {
				attentionKey(c, ch.s)
			}
			compactEvidence(c, ch.s)
			ui.Button(c, "").Label("Refresh").Tooltip("Refresh · reread this data root").Shrink(0).Children(func() {
				ui.Icon(c, icSync).FontSize(14)
			})
			return
		}
		if ch.s.stale {
			pill(c, toneAttention, "Stale · read "+ch.s.read)
		} else {
			subtle(c, "Read "+ch.s.read).Shrink(0)
		}
		iconButton(c, icSync, "Refresh", false).Shrink(0)
	})
}

// compactEvidence is the sidebar footer folded into one top-bar strip:
// service, confirmed commit and read time, each with its own shape.
func compactEvidence(c *ui.Context, s snap) {
	ui.Row(c).Shrink(0).Gap(sp3).Padding(4, sp3).Radius(radiusPill).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		t, _ := s.serviceText()
		col, _ := t.colors()
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ico(c, t.icon(), 12, col)
			cut(ui.Text(c, "Service "+strings.ToLower(s.serviceLabel())).FontSize(fsMeta).TextColor(tk.Muted))
		})
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ico(c, icCommit, 12, tk.Muted)
			cut(ui.Text(c, s.confirmed+" · "+s.confirmedAt).Font(fontMono).FontSize(fsMono - 1).TextColor(tk.Muted))
		})
		cut(ui.Text(c, "Read "+s.read).FontSize(fsMeta).TextColor(tk.Muted))
	})
}

// attentionKey explains the rail's Sync badge in words at compact widths:
// the same count, colour and phrase as the wide sidebar badge.
func attentionKey(c *ui.Context, s snap) {
	if s.attention() == 0 {
		return
	}
	ui.Row(c).Shrink(0).Gap(5).Padding(4, sp2+2).Radius(radiusPill).Background(tk.ConflictSoft).AlignItems(ui.Center).
		Tooltip(s.attentionDetail()).Label("Sync: " + s.attentionText()).Children(func() {
		ico(c, icAlert, 12, tk.Conflict)
		cut(ui.Text(c, s.attentionText()).FontSize(fsMeta).FontWeight(560).TextColor(tk.Conflict))
	})
}

// rail is the compact sidebar: brand, the four destinations as labelled
// icon buttons, and the device. Labels live in tooltips and accessible
// names; the attention badge keeps its count.
func rail(c *ui.Context, ch chrome) {
	s := ch.s
	ui.Column(c).Width(railW).Shrink(0).Background(tk.Canvas).BorderWidth(0, 1, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Padding(sp3, 0).Gap(sp1).Children(func() {
		ui.Box(c).Size(30, 30).Radius(8).Background(tk.Accent).Center().Margin(0, 0, sp3, 0).
			Tooltip(wsName + " · " + dataRoot).Children(func() { ico(c, icMark, 18, tk.AccentText) })
		for _, d := range []struct {
			key, label string
			icon       *ui.SVG
		}{{"home", "Home", icHome}, {"projects", "Projects", icFolder}, {"catalog", "Catalog", icGrid}, {"sync", "Sync", icSync}} {
			on := ch.nav == d.key
			label := d.label
			if d.key == "sync" && s.attention() > 0 {
				label = "Sync · " + s.attentionText()
			}
			b := ui.ButtonBase(c).Size(40, 40).Radius(radiusControl).Center().Label(label).Tooltip(label)
			if on {
				b.Background(tk.AccentSoft).AccessSelected(true)
			}
			b.Children(func() {
				col := tk.Muted
				if on {
					col = tk.Accent
				}
				ico(c, d.icon, 18, col)
				if d.key == "sync" && s.attention() > 0 {
					ui.Box(c).Absolute().Top(3).Right(2).Size(16, 16).Radius(8).Background(tk.Conflict).Center().Children(func() {
						ui.Text(c, fmt.Sprint(s.attention())).FontSize(10).FontWeight(650).TextColor(tk.Surface)
					})
				}
			})
		}
		ui.Spacer(c)
		ui.Box(c).Size(40, 40).Center().Tooltip(deviceName + " · " + short(deviceID)).Children(func() {
			ico(c, icLaptop, 18, tk.Muted)
		})
	})
}
