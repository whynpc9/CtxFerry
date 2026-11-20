package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func homeChrome(s snap) chrome {
	return chrome{nav: "home", crumbs: []string{"Home"}, s: s}
}

// attentionTile is one of the three separate queue counts on Home. Each
// opens the Queue with the matching filter.
func attentionTile(c *ui.Context, t tone, count int, label, desc, link string) {
	strongC, soft := t.colors()
	card(c).Grow(1).Basis(0).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 28).Radius(radiusControl).Background(soft).Center().Children(func() {
				ico(c, t.icon(), 15, strongC)
			})
			strong(c, label)
		})
		ui.Text(c, fmt.Sprint(count)).FontSize(fsStat).FontWeight(650).FontFeatures("tnum")
		muted(c, desc)
		ui.Row(c).Children(func() { linkButton(c, link, icArrowR) })
	})
}

// Home's content, shared by the full-width and compact layouts.
const (
	homeSub          = "What needs attention on this device."
	homeUnsubscribed = "ml-notebooks is not subscribed here."
)

type attention struct {
	t                 tone
	n                 int
	label, desc, link string
}

func homeAttention(s snap) []attention {
	return []attention{
		{tonePending, s.waiting, "Waiting", "Saved here, not yet published.", "Queue · Waiting"},
		{toneConflict, s.conflicts, "Conflicts", "Keep a side or replace the document.", "Queue · Conflicts"},
		{toneBlocked, s.blocked, "Blocked", "Needs a fix before it can publish.", "Queue · Blocked"},
	}
}

func homeProjects(c *ui.Context) {
	ui.Column(c).Gap(0).Dividers(1, tk.Border).Children(func() {
		homeProjectRow(c, prjAtlas, []checkout{chkAtlas, chkReview}, nil)
		homeProjectRow(c, prjBilling, []checkout{chkBilling}, nil)
		homeProjectRow(c, prjDocs, []checkout{chkDocs}, func() {
			pill(c, toneAttention, "Directory missing")
		})
	})
}

func homePage(c *ui.Context, s snap) {
	pageHeader(c, "Home", homeSub, nil)
	ui.Row(c).Gap(sp4).AlignItems(ui.Stretch).Children(func() {
		for _, a := range homeAttention(s) {
			attentionTile(c, a.t, a.n, a.label, a.desc, a.link)
		}
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1.3), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Gap(sp2).Children(func() {
			cardHeader(c, icFolder, "Bound Projects", "", func() {
				linkButton(c, "All Projects", icArrowR)
			})
			homeProjects(c)
			muted(c, homeUnsubscribed)
		})
		card(c).Children(func() {
			cardHeader(c, icLaptop, "This device", "", nil)
			svcTone, _ := s.serviceText()
			kvGrid(c, []kvRow{
				{k: "Device", fn: func() { strong(c, deviceName); idChip(c, deviceID) }},
				{k: "Data root", v: dataRoot, mono: true},
				{k: "Catalog", v: "Schema 14 · compatible"},
				{k: "Last local read", fn: func() {
					body(c, "Today "+s.read)
					if s.stale {
						pill(c, toneAttention, "Refresh failed "+staleFailedAt[:5])
					}
				}},
				{k: "Sync service", fn: func() { pill(c, svcTone, s.serviceLabel()) }},
				{k: "Last confirmed", fn: func() { mono(c, s.confirmed); subtle(c, "·"); body(c, s.confirmedAt) }},
			})
		})
	})
}

func homeProjectRow(c *ui.Context, p project, cks []checkout, status func()) {
	ui.Row(c).Gap(sp3).Padding(sp3, 0).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(120).Shrink(0).Gap(3).AlignItems(ui.Start).Children(func() {
			strong(c, p.name)
			idChip(c, p.id)
		})
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			for _, k := range cks {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ico(c, icFolder, 13, tk.Muted)
					cut(mono(c, k.path).Grow(1))
					tag(c, k.profile)
				})
			}
		})
		ui.Row(c).Shrink(0).Justify(ui.End).Children(func() {
			if status != nil {
				status()
			} else {
				linkButton(c, "Open", icChevR)
			}
		})
	})
}
