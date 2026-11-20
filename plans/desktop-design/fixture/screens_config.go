package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func projectScope(withProfile bool, extra ...scopeItem) []scopeItem {
	items := []scopeItem{
		{label: "PROJECT", value: prjAtlas.name, id: prjAtlas.id, icon: icFolder},
		{label: "CHECKOUT", value: chkAtlas.path, id: chkAtlas.id, icon: icLaptop},
	}
	if withProfile {
		items = append(items, scopeItem{label: "PROFILE", value: "staging", icon: icLayers})
	}
	return append(items, extra...)
}

type cfgField struct {
	name, value, origin string
}

// cfgTotal is every field the Checkout's schema defines. The list shows
// the populated ones by default; every layout states the same range.
const cfgTotal = 14

func cfgCountText() string {
	return fmt.Sprintf("Showing %d of %d fields · fields with a value", len(cfgFields), cfgTotal)
}

func cfgShowAll(c *ui.Context) { linkButton(c, fmt.Sprintf("Show all %d", cfgTotal), icChevD) }

var cfgFields = []cfgField{
	{"api.base_url", "https://staging.atlas.internal", "Profile"},
	{"api.timeout_ms", "4500", "Checkout"},
	{"db.pool_size", "20", "Profile"},
	{"db.statement_timeout", "30s", "Project"},
	{"feature.flags", "beta-ui", "Default"},
	{"log.level", "debug", "Device"},
	{"region", "eu-west-1", "Profile"},
	{"worker.concurrency", "4", "Project"},
}

const cfgPageSub = "Effective values for the selected Checkout, and which layer each one comes from."

// configPhase: 0 inspect, 1 preview, 2 receipt
func configPage(c *ui.Context, phase int) {
	pageHeader(c, "Configuration", cfgPageSub, nil)
	scopeBar(c, projectScope(true), func() { linkButton(c, "Change Checkout", nil) })
	ui.Grid(c).ColumnTracks(ui.Fr(0.92), ui.Fr(1.08)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		fieldList(c, phase)
		switch phase {
		case 0:
			layerPanel(c)
		case 1:
			configPreview(c)
		case 2:
			configReceipt(c)
		}
	})
}

func fieldList(c *ui.Context, phase int) {
	card(c).Padding(0).Gap(0).Children(func() {
		ui.Row(c).Padding(sp2+2, sp3).Gap(sp3).Background(tk.Sunken).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Radius(radiusCard, radiusCard, 0, 0).Children(func() {
			overline(c, "FIELD").Width(150)
			overline(c, "EFFECTIVE VALUE").Grow(1)
			overline(c, "ORIGIN").Width(72)
		})
		ui.Column(c).Padding(sp1).Gap(1).Children(func() {
			for _, f := range cfgFields {
				sel := f.name == "db.pool_size"
				val, origin := f.value, f.origin
				if sel && phase == 2 {
					val, origin = "10", "Project"
				}
				listRow(c, sel, func() {
					cut(mono(c, f.name).Width(146))
					mono(c, val).TextColor(tk.Muted).Grow(1).Shrink(1).LineHeight(1.35)
					ui.Row(c).Width(72).Children(func() { tag(c, origin) })
				}).Padding(sp2, sp3)
			}
		})
		listFooter(c, cfgCountText(), func() { cfgShowAll(c) })
	})
}

type layer struct {
	name, scope, value string
	shared, set, wins  bool
}

var poolLayers = []layer{
	{"Workspace default", "Shared · all Projects", "8", true, true, false},
	{"Project · atlas-api", "Shared · every Profile", "10", true, true, false},
	{"Profile · staging", "Shared · Checkouts using staging", "20", true, true, true},
	{"Device · studio-linux", "This device only", "Not set", false, false, false},
	{"Checkout · ~/code/atlas-api", "This Checkout only", "Not set", false, false, false},
}

func layerPanel(c *ui.Context) {
	card(c).Children(func() {
		ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
			mono(c, "db.pool_size").FontSize(fsSection).FontWeight(600).Grow(1)
			ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
				muted(c, "Effective")
				ui.Text(c, "20").Font(fontMono).FontSize(18).FontWeight(600)
				muted(c, "from Profile · staging")
			})
		})
		muted(c, "Layers in precedence order: a later layer overrides an earlier one. The first three are shared. Device and Checkout layers stay on each device, so other devices can override a shared value with their own.")
		ui.Column(c).Gap(0).Border(1, tk.Border).Radius(radiusControl).Dividers(1, tk.Border).Children(func() {
			for i, l := range poolLayers {
				layerRow(c, i+1, l, l.wins)
			}
		})
		ui.Column(c).Gap(sp2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ico(c, icSliders, 13, tk.Accent)
				muted(c, "Editing layer")
				strong(c, "Profile · staging")
				subtle(c, "· shared, writable")
			})
			ui.Row(c).Gap(sp2).Children(func() {
				ui.Spacer(c)
				ui.Button(c, "Edit value…")
				iconButton(c, icTrash, "Remove override…", false)
			})
		})
	})
}

func layerRow(c *ui.Context, n int, l layer, selected bool) {
	r := ui.Row(c).Gap(sp3).Padding(sp2+2, sp3).AlignItems(ui.Center)
	if selected {
		r.Background(tk.AccentSoft).AccessSelected(true).Label("Editing layer")
	}
	r.Children(func() {
		ui.Text(c, string(rune('0'+n))).FontSize(fsMeta).TextColor(tk.Subtle).Width(10)
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			strong(c, l.name)
			ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
				if l.shared {
					ico(c, icUpload, 12, tk.Muted)
				} else {
					ico(c, icLaptop, 12, tk.Muted)
				}
				subtle(c, l.scope)
			})
		})
		if l.set {
			mono(c, l.value).FontSize(fsBody)
		} else {
			subtle(c, l.value)
		}
		ui.Row(c).Width(64).Justify(ui.End).Children(func() {
			if l.wins {
				pill(c, toneOk, "Wins").Tooltip("Highest set layer: supplies the effective value")
			}
		})
	})
}

// The preview of removing the Profile override. Full-width and compact
// layouts render these same values.
const (
	cfgPreviewHeading = "Remove db.pool_size from Profile · staging"
	cfgPreviewScope   = "Shared write to Profile · staging. Checkouts that use staging inherit it unless a Device or Checkout layer overrides db.pool_size."
)

var (
	cfgPreviewEffects = []effect{
		{toneAccent, "~/code/atlas-api · staging", "20 → 10, inherited from Project · atlas-api"},
		{toneOk, "~/code/atlas-api-review", "Unchanged at 32 from Profile · prod-readonly"},
		{tonePending, "Other devices using staging", "Inherit 10 once a confirmed commit includes this, unless their own Device or Checkout layer sets it. Those layers are not visible here."},
		{tonePending, "Publication", "Queues 1 shared record. Published only when a confirmed commit includes it."},
	}
	cfgConfirm = confirmSpec{tokenLabel: "preview token", token: "9d04e6b2-71ac-4f3e-b85d-2c19a7e0f364",
		cancel: "Back to layers", confirm: "Remove from Profile · staging"}
)

func cfgPreviewNote(c *ui.Context) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		ico(c, icInfo, 14, tk.Muted)
		muted(c, "To change only this Checkout, go back and choose the Checkout layer instead.")
	})
}

func configPreview(c *ui.Context) {
	previewPanel(c, cfgPreviewHeading, cfgPreviewScope, cfgPreviewEffects, &cfgConfirm, func() { cfgPreviewNote(c) })
}

func configReceipt(c *ui.Context) {
	receiptPanel(c, receipt{
		heading: "Removed db.pool_size from Profile · staging",
		rows: []kvRow{
			{k: "Target", fn: func() { body(c, "Profile · staging in atlas-api") }},
			{k: "Scope", v: "Shared configuration"},
			{k: "Now effective", fn: func() {
				mono(c, chkAtlas.path)
				body(c, "now 10 · origin Project")
			}},
			{k: "Unchanged", fn: func() { mono(c, chkReview.path); body(c, "32") }},
		},
		ev: evidence{
			written: "Saved 10:44:52", writtenDetail: "Record " + short(pendingRecID) + " · revision 8",
			read: "Reread 10:44:53", readDetail: "Values in the field list",
			published: "Not yet", pubDetail: "Queued for the running service. Last confirmed " + confirmedSHA + ".",
			pubTone: tonePending,
		},
		actions: func() {
			iconButton(c, icInbox, "Open record in Queue", false)
			ui.Button(c, "Done")
		},
	})
}
