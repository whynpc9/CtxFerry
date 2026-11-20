package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Compact window (1024x680): the minimum supported size. Each compact frame
// renders the same shared content as its full-width twin; only placement
// changes:
//   - the sidebar becomes a 56 DIP rail; its evidence moves to the top bar
//   - two-column bodies keep their columns; the second pair of a four-card
//     layout moves behind a switch that keeps both sides selectable
//   - the confirm contract (token label, cancel, confirm) is pinned in an
//     action bar under the page, so scrolling can never hide it
//   - page titles, subtitles, scope bars, counts and IDs are never dropped

func compactChrome(ch chrome) chrome {
	ch.compact = true
	return ch
}

// ---------- Home ----------

func compactTile(c *ui.Context, a attention) {
	strongC, soft := a.t.colors()
	card(c).Grow(1).Basis(0).Padding(sp3).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 28).Radius(radiusControl).Background(soft).Center().Children(func() {
				ico(c, a.t.icon(), 15, strongC)
			})
			strong(c, a.label).Grow(1)
			ui.Text(c, fmt.Sprint(a.n)).FontSize(22).FontWeight(650).FontFeatures("tnum")
		})
		muted(c, a.desc)
		ui.Row(c).Children(func() { linkButton(c, a.link, icArrowR) })
	})
}

func homeCompact(c *ui.Context) {
	s := snapNow
	pageHeader(c, "Home", homeSub, nil)
	ui.Row(c).Gap(sp3).AlignItems(ui.Stretch).Children(func() {
		for _, a := range homeAttention(s) {
			compactTile(c, a)
		}
	})
	card(c).Gap(sp1).Children(func() {
		cardHeader(c, icFolder, "Bound Projects", "", func() { linkButton(c, "All Projects", icArrowR) })
		homeProjects(c)
		muted(c, homeUnsubscribed)
	})
	ui.Row(c).Gap(sp3).Padding(sp2+2, sp3).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		ico(c, icLaptop, 15, tk.Muted)
		strong(c, deviceName)
		idChip(c, deviceID)
		cut(mono(c, dataRoot).TextColor(tk.Muted).Grow(1))
		muted(c, "Schema 14 · compatible")
	})
}

// ---------- Configuration preview ----------

// The field list folds into a field picker that keeps the selection, the
// same displayed range and the same way to show every field.
func configPreviewCompact(c *ui.Context) {
	field := "db.pool_size"
	names := make([]string, len(cfgFields))
	for i, f := range cfgFields {
		names[i] = f.name
	}
	pageHeader(c, "Configuration", cfgPageSub, nil)
	scopeBar(c, projectScope(true), func() { linkButton(c, "Change Checkout", nil) })
	// The field strip uses the wide view's own column labels: effective
	// value, origin (the winning layer) and the layer being edited.
	card(c).Padding(0).Gap(0).Children(func() {
		ui.Row(c).Gap(0).AlignItems(ui.Stretch).Dividers(1, tk.Border).Children(func() {
			cell := func(label string, grow float32, fn func()) {
				ui.Column(c).Grow(grow).Basis(0).Gap(3).Padding(sp2, sp3).Children(func() {
					overline(c, label)
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(fn)
				})
			}
			cell("FIELD", 1.25, func() {
				ui.Select(c, &field, names).Grow(1).Font(fontMono).FontSize(fsMono).Label("Field")
			})
			cell("EFFECTIVE VALUE", 0.7, func() {
				ui.Text(c, "20").Font(fontMono).FontSize(fsSection).FontWeight(600)
			})
			cell("ORIGIN", 1, func() {
				strong(c, "Profile · staging")
				pill(c, toneOk, "Wins")
			})
			cell("EDITING LAYER", 1.1, func() {
				ico(c, icSliders, 13, tk.Accent)
				strong(c, "Profile · staging")
				subtle(c, "shared, writable")
			})
		})
		listFooter(c, cfgCountText(), func() { cfgShowAll(c) })
	})
	previewPanel(c, cfgPreviewHeading, cfgPreviewScope, cfgPreviewEffects, nil, func() { cfgPreviewNote(c) })
}

func configPreviewActions(c *ui.Context) { confirmBar(c, cfgConfirm) }

// ---------- Conflict resolution ----------

// The two sides share one column behind a switch; the shown side renders
// the same card, lines and revision as the full-width layout.
func conflictCompact(c *ui.Context) {
	choice := "local"
	side := 0
	pageHeader(c, "Resolve conflict", conflictSub, nil)
	conflictScope(c)
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp3).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Gap(sp2).Children(func() {
			ui.Segmented(c, &side,
				fmt.Sprintf("Local · revision %d", conflictLocal.rev),
				fmt.Sprintf("Remote · revision %d · deleted", conflictRemote.rev)).FillEqually().Label("Side shown")
			sideCard(c, conflictLocal)
		})
		conflictChoiceCard(c, &choice)
	})
	previewStrip(c, conflictHeading, conflictEffects)
}

func conflictActions(c *ui.Context) { confirmBar(c, conflictConfirm) }

// ---------- Backup review ----------

func backupCompact(c *ui.Context) {
	s := snapCleared
	dest := backupDir
	syncHeader(c, 2, s)
	backupRoute(c, s, &dest)
	backupScope(c)
	exposurePanel(c, 2)
}

// backupActions pins step 5 under the page: acknowledgement and write.
func backupActions(c *ui.Context) { backupConfirm(c) }
