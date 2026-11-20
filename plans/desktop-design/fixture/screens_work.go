package main

import (
	"github.com/egoist/mygo/ui"
)

// ---------- Context ----------

type ctxDoc struct {
	path, origin string
	state        tone
	stateText    string
}

var ctxDocs = []ctxDoc{
	{"README.md", "Project", toneNeutral, ""},
	{"architecture/overview.md", "Project", toneNeutral, ""},
	{"runbooks/deploy.md", "Profile", toneNeutral, ""},
	{"runbooks/rollback.md", "Project", toneConflict, "Conflict"},
	{"runbooks/incident.md", "Project", toneUnknown, "Content not here"},
	{"api/errors.md", "Project", toneNeutral, ""},
	{"api/rate-limits.md", "Profile", toneNeutral, ""},
	{"onboarding/local-setup.md", "Project", toneNeutral, ""},
}

func contextPage(c *ui.Context) {
	q := ""
	pageHeader(c, "Context", "Shared documents for this Project, resolved through the selected Checkout and Profile.", nil)
	scopeBar(c, projectScope(true), func() { pill(c, toneOk, "Subscribed") })
	ui.Grid(c).ColumnTracks(ui.Fr(0.9), ui.Fr(1.1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp2).Gap(sp1).Children(func() {
			ui.SearchField(c, &q).Placeholder("Filter documents").Margin(sp1, sp1, sp1, sp1)
			for _, d := range ctxDocs {
				sel := d.path == "runbooks/deploy.md"
				listRow(c, sel, func() {
					ico(c, icFile, 14, tk.Muted)
					cut(ui.Text(c, d.path).FontSize(fsBody).Grow(1))
					if d.stateText != "" {
						pill(c, d.state, d.stateText)
					}
					tag(c, d.origin)
				}).Padding(sp2, sp3)
			}
			ui.Row(c).Padding(sp2, sp3, sp1, sp3).Gap(sp2).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).Children(func() {
				ui.Column(c).Grow(1).Gap(1).Children(func() {
					muted(c, "Showing 1–8 of 23")
					subtle(c, "Next page keeps ~/code/atlas-api · staging")
				})
				ui.Button(c, "Load more")
			})
		})
		card(c).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icFile, 16, tk.Accent)
				ui.Text(c, "runbooks/deploy.md").FontSize(fsSection).FontWeight(600).Grow(1)
				pill(c, toneOk, "Content available")
			})
			kvGrid(c, []kvRow{
				{k: "Origin", fn: func() { body(c, "Profile · staging"); muted(c, "overrides the Project version") }},
				{k: "Revision", fn: func() { body(c, "12 · edited on "+otherDev+", yesterday 18:20") }},
				{k: "Document ID", fn: func() { idChip(c, "1c6e0f93-a8b2-4d57-9e40-7b3d25c8f1a6") }},
			})
			ui.Column(c).Gap(sp2).Padding(sp4).Radius(radiusControl).Background(tk.Sunken).Border(1, tk.Border).Children(func() {
				ui.Text(c, "Deploying atlas-api to staging").FontSize(fsSection).FontWeight(600)
				body(c, "Staging deploys run from the deploy-preview Task. The Task resolves its database slot through this Profile, so a preview always talks to postgres-primary on the staging network.")
				ui.Text(c, "Before you deploy").FontSize(fsBody).FontWeight(600).Margin(sp1, 0, 0, 0)
				for _, l := range []string{"1. Confirm the queue has no conflicts for atlas-api.", "2. Probe postgres-primary from this device.", "3. Recheck trust if the Task definition changed."} {
					body(c, l)
				}
				ui.Text(c, "ctxferry run deploy-preview --profile staging").Font(fontMono).FontSize(fsMono).Padding(sp2).Radius(4).Background(tk.Surface).Border(1, tk.Border)
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Content read 10:46:24 from the local catalog.").Grow(1)
				ui.Button(c, "Back to list")
			})
		})
	})
}

// ---------- Tasks ----------

func tasksPage(c *ui.Context) {
	pageHeader(c, "Tasks", "What each Task needs, resolved for the selected Checkout and Profile.", nil)
	scopeBar(c, projectScope(true), nil)
	ui.Grid(c).ColumnTracks(ui.Fixed(270), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp2).Gap(sp1).Children(func() {
			for _, t := range trustByCheckout[chkAtlas.path] {
				tn, lbl := t.state.label()
				taskRow(c, t.name, t.purpose, t.name == taskName, tn, lbl)
			}
			ui.Box(c).Padding(sp1, sp3).Children(func() {
				muted(c, trustSummary(chkAtlas.path))
			})
			ui.Box(c).Padding(sp2, sp3).Children(func() {
				subtle(c, "billing-worker has its own Task with this name. It has a different ID.")
			})
		})
		card(c).Gap(sp3).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, taskName).Font(fontMono).FontSize(fsSection).FontWeight(600)
				idChip(c, taskID)
				ui.Spacer(c)
				muted(c, "Deploys an isolated preview against staging data")
			})
			// Three separate findings.
			ui.Row(c).Gap(sp2).Children(func() {
				finding(c, icActivity, "Endpoint probe", toneOk, "Reachable · 10:44")
				finding(c, icKey, "Credentials", toneBlocked, "1 of 3 unmapped")
				finding(c, icShield, "Execution trust", toneAttention, "Changed since trusted")
			})
			sectionLabel(c, "RESOURCE SLOTS · PROFILE STAGING", nil)
			ui.Column(c).Gap(0).Border(1, tk.Border).Radius(radiusControl).Dividers(1, tk.Border).Children(func() {
				slotRow(c, "database", func() { strong(c, resPG); idChip(c, resPGID) }, func() { linkButton(c, "Open", icArrowR) })
				slotRow(c, "cache", func() { pill(c, toneBlocked, "No Resource mapped") }, func() { linkButton(c, "Resolve", icArrowR) })
			})
			sectionLabel(c, "CREDENTIAL REQUIREMENTS · 3", nil)
			ui.Column(c).Gap(0).Border(1, tk.Border).Radius(radiusControl).Dividers(1, tk.Border).Children(func() {
				slotRow(c, reqDBPass, func() { pill(c, toneOk, "Mapped"); monoMuted(c, "atlas/staging/db") }, func() { linkButton(c, "Open", icArrowR) })
				slotRow(c, "registry-token", func() { pill(c, toneOk, "Mapped"); monoMuted(c, "atlas/registry") }, func() { linkButton(c, "Open", icArrowR) })
				slotRow(c, "cache-auth", func() { pill(c, toneBlocked, "Unmapped"); muted(c, "needs slot cache first") }, func() { linkButton(c, "Resolve", icArrowR) })
			})
			sectionLabel(c, "EXECUTION ON ~/code/atlas-api", nil)
			ui.Column(c).Gap(sp2).Padding(sp3).Radius(radiusControl).Background(tk.ConflictSoft).Children(func() {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ico(c, icAlert, 14, tk.Conflict)
					ui.Text(c, "2 fields changed since you trusted this Task on 12 Nov").FontSize(fsBody).FontWeight(600).TextColor(tk.Conflict)
				})
				ui.Grid(c).ColumnTracks(ui.Fixed(110), ui.Fr(1)).GapX(sp3).GapY(4).Children(func() {
					mono(c, "command").TextColor(tk.Muted)
					mono(c, "./scripts/deploy-preview.sh --env staging --wait")
					mono(c, "inputs.region").TextColor(tk.Muted)
					mono(c, "default eu-west-1 → eu-central-1")
				})
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Trust and run continue in the CLI.").Grow(1)
				iconButton(c, icSync, "Recheck trust", false)
				iconButton(c, icTerminal, "Trust via CLI…", false).Tooltip("ctxferry task trust preview deploy-preview --profile staging --checkout ~/code/atlas-api --project atlas-api --data-dir " + dataRoot)
			})
		})
	})
}

func taskRow(c *ui.Context, name, sub string, sel bool, t tone, state string) {
	listRow(c, sel, func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			mono(c, name).FontSize(fsBody).FontWeight(560)
			muted(c, sub)
		})
		pill(c, t, state)
	})
}

func finding(c *ui.Context, icon *ui.SVG, label string, t tone, value string) {
	strongC, _ := t.colors()
	ui.Column(c).Grow(1).Basis(0).Gap(3).Padding(sp2, sp3).Radius(radiusControl).Border(1, tk.Border).Background(tk.Sunken).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ico(c, icon, 13, tk.Muted)
			subtle(c, label)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ico(c, t.icon(), 13, strongC)
			cut(ui.Text(c, value).FontSize(fsBody).FontWeight(560).TextColor(strongC))
		})
	})
}

func slotRow(c *ui.Context, name string, value func(), action func()) {
	ui.Row(c).Gap(sp3).Padding(sp2, sp3).AlignItems(ui.Center).Children(func() {
		mono(c, name).Width(120).Shrink(0)
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Grow(1).Children(value)
		action()
	})
}
