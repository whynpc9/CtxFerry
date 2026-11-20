package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

func projectsChrome(crumbs ...string) chrome {
	return chrome{nav: "projects", crumbs: append([]string{"Projects"}, crumbs...), s: snapNow}
}

func projectsListPage(c *ui.Context) {
	filter := 0
	query := ""
	pageHeader(c, "Projects", "Shared Project definitions, and how this device subscribes to and binds them.", func() {
		ui.SearchField(c, &query).Placeholder("Filter Projects").Width(200)
	})
	ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
		// Counts live on the group headings below, not on the filter.
		ui.Segmented(c, &filter, "All", "Subscribed", "Not subscribed").Label("Subscription filter")
		ui.Spacer(c)
		ico(c, icLayers, 13, tk.Muted)
		muted(c, "Grouped by subscription, then by name")
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1.1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp2).Gap(2).Children(func() {
			projectGroup(c, "SUBSCRIBED ON "+strings.ToUpper(deviceName)+" · 3", true)
			projectRow(c, prjAtlas, "2 Checkouts · staging, prod-readonly", "3 Tasks", false, func() { pill(c, toneConflict, "1 conflict") })
			projectRow(c, prjBilling, "1 Checkout · dev", "2 Tasks", false, func() { pill(c, toneBlocked, "1 blocked") })
			projectRow(c, prjDocs, "1 Checkout · directory unavailable", "1 Task", false, func() { pill(c, toneAttention, "Unavailable") })
			projectGroup(c, "NOT SUBSCRIBED · 1", false)
			projectRow(c, prjML, "Shared records only · subscribe to receive content", "", true, func() {})
		})
		card(c).Children(func() {
			cardHeader(c, icFolder, prjML.name, prjML.sub, nil)
			kvGrid(c, []kvRow{
				{k: "Project ID", v: prjML.id, mono: true},
				{k: "Subscription", fn: func() { pill(c, toneUnknown, "Not subscribed on "+deviceName) }},
				{k: "Shared records", v: "Kept in the Workspace. This device does not receive their content."},
				{k: "Checkouts", v: "None. Binding needs a subscription."},
			})
			ui.Divider(c)
			strong(c, "Subscribe on "+deviceName)
			cmdBlock(c, cli("project subscribe "+prjML.name))
			afterRun(c, "After it finishes, Refresh reopens it by Project ID "+short(prjML.id)+".", "")
		})
	})
}

// projectGroup heads one subscription group; rows inside are by name.
func projectGroup(c *ui.Context, label string, first bool) {
	r := ui.Row(c).Gap(6).Padding(sp2, sp2, sp1, sp2).AlignItems(ui.Center)
	if !first {
		r.Margin(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border)
	}
	r.Children(func() { overline(c, label) })
}

func projectRow(c *ui.Context, p project, line, meta string, selected bool, status func()) {
	listRow(c, selected, func() {
		ui.Box(c).Size(30, 30).Radius(radiusControl).Background(tk.Sunken).Border(1, tk.Border).Center().Shrink(0).Children(func() {
			col := tk.Muted
			if p.subscribed {
				col = tk.Accent
			}
			ico(c, icFolder, 15, col)
		})
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				strong(c, p.name)
				idChip(c, p.id)
			})
			if meta != "" {
				line += " · " + meta
			}
			muted(c, line)
		})
		status()
		ico(c, icChevR, 14, tk.Subtle)
	})
}

// ---------- Project overview ----------

func projectOverviewPage(c *ui.Context) {
	profile := "staging"
	ui.Row(c).Gap(sp3).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).Gap(sp1).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				title(c, prjAtlas.name)
				idChip(c, prjAtlas.id)
			})
			body(c, prjAtlas.sub+" · Subscribed on this device").TextColor(tk.Muted)
		})
	})
	sectionLabel(c, "CHECKOUTS ON THIS DEVICE", func() { linkButton(c, "Bind another directory", icPlus) })
	ui.Row(c).Gap(sp3).AlignItems(ui.Stretch).Children(func() {
		checkoutCard(c, chkAtlas, true, "Tasks: "+trustSummary(chkAtlas.path))
		checkoutCard(c, chkReview, false, "Tasks: "+trustSummary(chkReview.path))
	})
	ui.Row(c).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				strong(c, "Saved Profile for")
				mono(c, chkAtlas.path)
				idChip(c, chkAtlas.id)
			})
			muted(c, "Changing it affects this Checkout only. Configuration and Context reload, and Task trust is rechecked.")
		})
		ui.Select(c, &profile, []string{"staging", "prod-readonly", "dev"}).Width(160).Label("Saved Profile")
		ui.Button(c, "Preview change…").Disabled(true).Tooltip("Choose a different Profile first")
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).GapY(sp2).Children(func() {
		sectionLabel(c, "IN THIS PROJECT · FOR ~/code/atlas-api · staging", nil)
		sectionLabel(c, "RELATED IN CATALOG · OPENS WITH THIS SCOPE", nil)
		ui.Column(c).Gap(sp2).Children(func() {
			destTile(c, icSliders, "Configuration", "14 fields · 2 overrides on this Checkout", nil)
			destTile(c, icFile, "Context", "23 documents · 1 in conflict", func() { pill(c, toneConflict, "1 conflict") })
			destTile(c, icTasks, "Tasks", trustSummary(chkAtlas.path), func() { pill(c, toneAttention, "1 to recheck") })
		})
		ui.Column(c).Gap(sp2).Children(func() {
			destTile(c, icServer, "Resources & environments", "2 Resources · postgres-primary, redis-cache", nil)
			destTile(c, icKey, "Credential bindings", "3 requirements · 1 not mapped in staging", func() { pill(c, toneBlocked, "1 unmapped") })
			destTile(c, icSkill, "Skills", "3 Skills · release-notes has an approved update", func() { pill(c, tonePending, "Update") })
			destTile(c, icPackage, "Run artifacts", "12 artifacts from 4 runs", nil)
		})
	})
}

func checkoutCard(c *ui.Context, k checkout, selected bool, trust string) {
	cd := card(c).Grow(1).Basis(0).Gap(sp2).Padding(sp3, sp4)
	if selected {
		cd.Border(1.5, tk.Accent).Background(ui.Hex("#FAFDFE")).AccessSelected(true)
	}
	cd.Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			radio := tk.BorderStrong
			if selected {
				radio = tk.Accent
			}
			ui.Box(c).Size(16, 16).Radius(8).Border(selected4(selected), radio).Shrink(0)
			mono(c, k.path).FontSize(fsBody).FontWeight(560).Grow(1)
			if selected {
				pill(c, toneAccent, "Selected")
			}
			idChip(c, k.id)
		})
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Padding(0, 0, 0, 24).Children(func() {
			tag(c, "Profile "+k.profile)
			muted(c, trust).Grow(1).Shrink(1)
		})
	})
}

func selected4(on bool) float32 {
	if on {
		return 5
	}
	return 1.5
}

func destTile(c *ui.Context, icon *ui.SVG, t, sub string, status func()) {
	ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusCard).Border(1, tk.Border).AlignItems(ui.Center).Background(tk.Surface).Children(func() {
		ui.Box(c).Size(30, 30).Radius(radiusControl).Background(tk.AccentSoft).Center().Shrink(0).Children(func() {
			ico(c, icon, 15, tk.Accent)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			strong(c, t)
			cut(muted(c, sub))
		})
		if status != nil {
			status()
		}
		ico(c, icChevR, 14, tk.Subtle)
	})
}
