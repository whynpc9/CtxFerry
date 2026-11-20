package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func catalogChrome(back string, crumbs ...string) chrome {
	return chrome{nav: "catalog", back: back, crumbs: append([]string{"Catalog"}, crumbs...), s: snapNow}
}

// ---------- Catalog directory ----------

func catalogPage(c *ui.Context) {
	sel := "atlas-api"
	pageHeader(c, "Catalog", "Tools used across Projects. Each one opens with an explicit Project scope.", nil)
	ui.Row(c).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		overline(c, "PROJECT SCOPE")
		ui.Select(c, &sel, []string{"atlas-api", "billing-worker", "docs-site"}).Width(200).Label("Project scope")
		idChip(c, prjAtlas.id)
		muted(c, "Lists subscribed Projects only. ml-notebooks is not subscribed.").Grow(1)
		linkButton(c, "Manage subscriptions", icArrowR)
	})
	ui.Grid(c).Columns(2).Gap(sp4).Children(func() {
		toolCard(c, icServer, "Resources & environments", "Project",
			[]string{"Inspect each Resource's endpoint and where it applies", "Review the local route and probe it from this device", "Compare timestamped observations from other environments"},
			"2 Resources in atlas-api")
		toolCard(c, icKey, "Credential bindings", "Project · Profile · Task",
			[]string{"Map credential requirements to provider references", "Check the provider session, then entry readability", "Secret values stay with the provider"},
			"3 requirements · 1 unmapped in staging")
		toolCard(c, icSkill, "Skills", "Project or this device",
			[]string{"Install, update, roll back or recover an interrupted install", "Inspect per-file changes before confirming", "Also open this device's own catalog without a Project"},
			"3 Skills · 1 approved update")
		toolCard(c, icPackage, "Run artifacts", "Project",
			[]string{"Filter by run and source device", "Verify available bytes and export them to a new file", "See where metadata-only items came from"},
			"12 artifacts from 4 runs")
	})
}

func toolCard(c *ui.Context, icon *ui.SVG, t, scope string, can []string, summary string) {
	card(c).Gap(sp3).Children(func() {
		cardHeader(c, icon, t, "Scope: "+scope, func() { ui.Button(c, "Open") })
		ui.Column(c).Gap(6).Children(func() {
			for _, s := range can {
				ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
					// Plain bullets: these are capabilities, not verified facts.
					dot(c, tk.Subtle).Size(5, 5).Radius(2.5).Margin(8, 2, 0, 2)
					body(c, s)
				})
			}
		})
		ui.Row(c).Gap(sp2).Padding(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
			subtle(c, summary)
		})
	})
}

// ---------- Resources & environments ----------

// resourcesPage opens postgres-primary with the scope it was opened from:
// a Task slot (frame 13) or a credential requirement mapped to it (14b).
// Back returns to that origin with its selection.
func resourcesPage(c *ui.Context, from scopeItem) {
	env := 0
	pageHeader(c, "Resources & environments", "", nil)
	scopeBar(c, []scopeItem{
		{label: "PROJECT", value: prjAtlas.name, id: prjAtlas.id, icon: icFolder},
		{label: "PROFILE", value: "staging", icon: icLayers},
		from,
		{label: "RESOURCE", value: resPG, id: resPGID, icon: icServer},
	}, nil)
	ui.Grid(c).ColumnTracks(ui.Fr(1.1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Gap(sp3).Children(func() {
			ui.Segmented(c, &env, "This device", "ci-runner-eu", "staging-k8s").FillEqually().Label("Environment")
			card(c).Children(func() {
				cardHeader(c, icActivity, "Local route on studio-linux", "Applies to this environment. Probe reaches the actual target below.", nil)
				kvGrid(c, []kvRow{
					{k: "Configured endpoint", v: "db.staging.northwind.internal:5432", mono: true},
					{k: "Local override", fn: func() { mono(c, "127.0.0.1:15432"); tag(c, "SSH tunnel") }},
					{k: "Override set by", fn: func() {
						ico(c, icFolder, 13, tk.Muted)
						mono(c, chkAtlas.path)
						idChip(c, chkAtlas.id)
						subtle(c, "Checkout layer")
					}},
					{k: "Actual TCP target", fn: func() {
						ui.Text(c, "host 127.0.0.1 · port 15432").Font(fontMono).FontSize(fsBody).FontWeight(600)
					}},
				})
				ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusControl).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						strong(c, "Probe opens one TCP connection")
						muted(c, "From studio-linux to the actual target above. Sends no credentials.")
					})
					iconButton(c, icActivity, "Probe 127.0.0.1:15432", true)
				})
				ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusControl).Background(tk.OkSoft).AlignItems(ui.Center).Children(func() {
					ico(c, icCheckCircle, 16, tk.Ok)
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						ui.Text(c, "Reachable · connected in 12 ms").FontSize(fsBody).FontWeight(600).TextColor(tk.Ok)
						muted(c, "Probed 10:44:31 · 127.0.0.1:15432 · from studio-linux")
					})
				})
				muted(c, "Reachable does not mean the credential is readable or the Task is trusted. Those are separate checks.")
			})
		})
		ui.Column(c).Gap(sp3).Children(func() {
			card(c).Children(func() {
				cardHeader(c, icServer, resPG, "Primary Postgres for the API", nil)
				kvGrid(c, []kvRow{
					{k: "Applies to", v: "Profiles staging, prod-readonly"},
					{k: "Stage", v: "staging"},
					{k: "Used by", v: "deploy-preview, db-migrate"},
					{k: "As slot", v: "database"},
				})
			})
			card(c).Gap(sp2).Children(func() {
				h2(c, "Observations from other environments")
				muted(c, "Read only. Recorded by each environment, with its own timestamp.")
				ui.Column(c).Gap(0).Dividers(1, tk.Border).Children(func() {
					obsRow(c, "ci-runner-eu", toneOk, "Reachable", "Today 09:58")
					obsRow(c, "travel-mac", toneBlocked, "Refused", "Yesterday 18:02")
					obsRow(c, "staging-k8s", toneUnknown, "Unknown", "No observation")
				})
			})
		})
	})
}

func obsRow(c *ui.Context, env string, t tone, result, when string) {
	ui.Row(c).Gap(sp3).Padding(sp2+2, 0).AlignItems(ui.Center).Children(func() {
		mono(c, env).Grow(1)
		subtle(c, when)
		ui.Row(c).Width(110).Justify(ui.End).Children(func() { pill(c, t, result) })
	})
}

// ---------- Credential bindings ----------

func credentialsPage(c *ui.Context, req string) {
	pageHeader(c, "Credential bindings", "", nil)
	scopeBar(c, []scopeItem{
		{label: "PROJECT", value: prjAtlas.name, id: prjAtlas.id, icon: icFolder},
		{label: "PROFILE", value: "staging", icon: icLayers},
		{label: "TASK", value: taskName, id: taskID, icon: icTasks},
		{label: "REQUIREMENT", value: req, icon: icKey},
	}, nil)
	ui.Grid(c).ColumnTracks(ui.Fixed(300), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp2).Gap(sp1).Children(func() {
			ui.Box(c).Padding(sp2, sp3, sp1, sp3).Children(func() { overline(c, "DEPLOY-PREVIEW NEEDS 3") })
			reqRow(c, reqDBPass, "password · slot database", req == reqDBPass, toneOk, "Mapped")
			reqRow(c, "registry-token", "token · no slot", false, toneOk, "Mapped")
			reqRow(c, "cache-auth", "password · slot cache", req == "cache-auth", toneBlocked, "Unmapped")
		})
		if req == "cache-auth" {
			cacheAuthResolve(c)
			return
		}
		ui.Column(c).Gap(sp3).Children(func() {
			card(c).Children(func() {
				cardHeader(c, icLink, "Shared mapping", "Published with the Workspace. Holds a reference, never a value.", func() { ui.Button(c, "Change mapping…") })
				kvGrid(c, []kvRow{
					{k: "Expected type", fn: func() { tag(c, "password") }},
					{k: "Mapped alias", v: "atlas/staging/db", mono: true},
					{k: "Locator", v: "op://Engineering/atlas-staging-db/password", mono: true},
					{k: "Provider", fn: func() { body(c, "1Password · provider profile"); mono(c, "work") }},
					{k: "Resource match", fn: func() {
						ico(c, icCheckCircle, 13, tk.Ok)
						body(c, "Matches")
						linkButton(c, resPG, icArrowR).Label("Open Resource " + resPG).Tooltip("Opens the Resource; Back returns to " + reqDBPass)
						muted(c, "in slot database")
					}},
				})
			})
			card(c).Children(func() {
				cardHeader(c, icLaptop, "Checks on this device", "The second check needs the first.", nil)
				checkStep(c, "1", toneBlocked, "Provider session", "1Password, provider profile work, on studio-linux · checked 10:46:20.", func() {
					ui.Button(c, "Adopt configured session")
					iconButton(c, icExternal, "Sign in with 1Password", false)
				})
				checkStep(c, "2", toneUnknown, "Entry readability", "Needs an authenticated session first.", func() {
					ui.Button(c, "Check entry").Disabled(true)
				})
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ico(c, icLock, 13, tk.Muted)
					muted(c, "Secret values never appear in drafts, results or copied commands.")
				})
			})
		})
	})
}

// cacheAuthResolve explains why cache-auth cannot be mapped yet and resolves
// it in order: first the Resource slot it authenticates to, then the reference.
func cacheAuthResolve(c *ui.Context) {
	ui.Column(c).Gap(sp3).Children(func() {
		ui.Row(c).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Background(tk.BlockedSoft).AlignItems(ui.Start).Children(func() {
			ico(c, icBan, 16, tk.Blocked).Margin(1, 0, 0, 0)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "cache-auth has no Resource to match").FontSize(fsBody).FontWeight(620).TextColor(tk.Blocked)
				body(c, "It authenticates to slot cache. Profile staging maps no Resource to that slot, so no reference can be checked against it.")
			})
		})
		cliHandoff(c, handoff{
			title: "1 · Map slot cache in Profile staging",
			why:   "redis-cache is the only atlas-api Resource of kind redis that applies to staging.",
			prereqs: []prereq{
				{toneOk, "redis-cache " + short(resCacheID) + " · applies to staging, prod-readonly"},
			},
			commands: []string{
				cli("profile map-resource staging deploy-preview", "cache "+resCacheID, "--project atlas-api"),
			},
			after:   "Shared write: it queues one record. After it finishes, Refresh reads the mapping.",
			refresh: "Refresh",
		})
		card(c).Gap(sp2).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icLink, 16, tk.Muted)
				h2(c, "2 · Map a compatible reference").Grow(1)
				pill(c, toneUnknown, "After step 1")
			})
			ui.Row(c).Gap(sp3).Padding(sp2, sp3).Radius(radiusControl).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
						mono(c, "atlas/staging/redis")
						idChip(c, redisBindID)
						tag(c, "password")
					})
					muted(c, "Existing binding · 1Password work · op://Engineering/atlas-staging-redis/password")
				})
				ui.Button(c, "Preview mapping…").Disabled(true).Tooltip("Available once slot cache is mapped and reread")
			})
		})
	})
}

func reqRow(c *ui.Context, name, typ string, sel bool, t tone, state string) {
	listRow(c, sel, func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			mono(c, name).FontSize(fsBody).FontWeight(560)
			subtle(c, typ)
		})
		pill(c, t, state)
	})
}

func checkStep(c *ui.Context, n string, t tone, label, detail string, actions func()) {
	ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusControl).Border(1, tk.Border).AlignItems(ui.Start).Children(func() {
		stepNumber(c, n, toneNeutral)
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				strong(c, label)
				pill(c, t, map[tone]string{toneBlocked: "Not signed in", toneUnknown: "Not checked", toneOk: "Readable"}[t])
			})
			muted(c, detail)
			ui.Row(c).Gap(sp2).Margin(sp1, 0, 0, 0).Children(actions)
		})
	})
}

// ---------- Skills ----------

func skillsPage(c *ui.Context) {
	catalog := 0
	pageHeader(c, "Skills", "", nil)
	ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &catalog, "Project · atlas-api", "This device").Label("Skill catalog")
		muted(c, "Approved for atlas-api. This device lists Skills installed without a Project.")
	})
	ui.Grid(c).ColumnTracks(ui.Fixed(282), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp2).Gap(sp1).Children(func() {
			skillRow(c, skillName, "v1.3.2 → v1.4.0", true, tonePending, "Update")
			skillRow(c, "code-review", "v2.0.1", false, toneOk, "Current")
			skillRow(c, "msi-packager", "Windows only", false, toneUnknown, "Not applicable")
		})
		card(c).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, skillName).Font(fontMono).FontSize(fsSection).FontWeight(600)
				tag(c, "Shared · atlas-api")
				ui.Spacer(c)
				pill(c, tonePending, "Approved update available")
			})
			kvGrid(c, []kvRow{
				{k: "Target", v: "~/.claude/skills/release-notes", mono: true},
				{k: "Installed", fn: func() { mono(c, "v1.3.2 · 77c0b14"); muted(c, "receipt 12 Nov") }},
				{k: "Approved pin", fn: func() { mono(c, "v1.4.0 · a3f9e21"); muted(c, "approved via CLI") }},
				{k: "Source", v: "github.com/northwind/skills · release-notes/", mono: true},
			})
			banner(c, toneBlocked, "Local edits block this update",
				"templates/changelog.md was edited after install, and v1.4.0 also changes it. Restore or move your edit, then rescan.",
				func() { iconButton(c, icSync, "Rescan", false) })
			ui.Column(c).Gap(0).Border(1, tk.Border).Radius(radiusControl).Dividers(1, tk.Border).Clip().Children(func() {
				fileChange(c, "SKILL.md", "Update changes", "+12 −4", toneNeutral, false)
				fileChange(c, "templates/changelog.md", "Local drift", "+1 −1", toneBlocked, true)
				fileChange(c, "scripts/collect.sh", "Update adds", "+41", toneNeutral, false)
			})
			// The diff names the selected file directly above its lines.
			ui.Column(c).Gap(0).Radius(radiusControl).Border(1, tk.Border).Clip().Children(func() {
				ui.Row(c).Gap(sp2).Padding(sp2, sp3).Background(tk.Sunken).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
					ico(c, icFile, 13, tk.Blocked)
					ui.Text(c, "templates/changelog.md").Font(fontMono).FontSize(fsMono).FontWeight(600)
					muted(c, "· installed v1.3.2 → your edit on this device").Grow(1)
				})
				diffLine(c, " ", "1", "## {{version}} · {{date}}", 0)
				diffLine(c, "−", "2", "### Changes", -1)
				diffLine(c, "+", "2", "### What changed for operators", 1)
				diffLine(c, " ", "3", "{{#each entries}}- {{summary}} ({{pr}}){{/each}}", 0)
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Restoring replaces your edit with the installed v1.3.2 file. A preview comes first.").Grow(1)
				ui.Button(c, "Restore installed file…")
			})
			ui.Divider(c)
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Update stays disabled until a rescan finds no drift.").Grow(1)
				ui.Button(c, "Cancel")
				ui.PrimaryButton(c, "Update to v1.4.0").Disabled(true).Tooltip("Blocked by local drift")
			})
		})
	})
}

func skillRow(c *ui.Context, name, sub string, sel bool, t tone, state string) {
	listRow(c, sel, func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			mono(c, name).FontSize(fsBody).FontWeight(560)
			subtle(c, sub)
		})
		pill(c, t, state)
	})
}

func fileChange(c *ui.Context, path, kind, stat string, t tone, sel bool) {
	r := ui.Row(c).Gap(sp3).Padding(sp2, sp3).AlignItems(ui.Center)
	if sel {
		r.Background(tk.AccentSoft).AccessSelected(true)
	}
	r.Children(func() {
		if sel {
			ui.Box(c).Absolute().Left(0).Top(6).Bottom(6).Width(3).Radius(2).Background(tk.Accent)
		}
		col := tk.Muted
		if t == toneBlocked {
			col = tk.Blocked
		}
		ico(c, icFile, 14, col)
		mono(c, path).FontSize(fsBody).Grow(1)
		if t == toneBlocked {
			pill(c, toneBlocked, kind)
		} else {
			subtle(c, kind)
		}
		ui.Text(c, stat).Font(fontMono).FontSize(fsMono).TextColor(tk.Muted).Width(56).TextAlign(ui.End)
	})
}

func diffLine(c *ui.Context, mark, n, text string, kind int) {
	bg, fg := tk.Surface, tk.Text
	switch kind {
	case 1:
		bg, fg = ui.Hex("#E9F6EE"), ui.Hex("#135C39")
	case -1:
		bg, fg = ui.Hex("#FCEDEC"), ui.Hex("#8E1C14")
	}
	ui.Row(c).Gap(sp2).Padding(3, sp3).Background(bg).Children(func() {
		ui.Text(c, n).Font(fontMono).FontSize(fsMono).TextColor(tk.Subtle).Width(22).TextAlign(ui.End)
		ui.Text(c, mark).Font(fontMono).FontSize(fsMono).TextColor(fg).Width(10)
		ui.Text(c, text).Font(fontMono).FontSize(fsMono).TextColor(fg)
	})
}

// ---------- Run artifacts ----------

type artifact struct {
	name, run, source string
	content           tone
	contentText, size string
}

var artifacts = []artifact{
	{"latency-report.json", "7781 · load-test", ciDev, toneOk, "Available", "48.2 KB"},
	{"k6-summary.txt", "7781 · load-test", ciDev, toneOk, "Available", "3.1 KB"},
	{"trace.zst", "7781 · load-test", ciDev, toneUnknown, "Metadata only", "212 MB"},
	{"deploy-log.txt", "7769 · deploy-preview", deviceName, toneOk, "Available", "18.0 KB"},
	{"screenshot-diff.png", "7760 · deploy-preview", otherDev, toneUnknown, "Metadata only", "1.4 MB"},
}

// artifactTotal is every artifact recorded for atlas-api. The list loads
// the newest five first and appends older ones on request.
const artifactTotal = 12

func artifactsPage(c *ui.Context) {
	run, src := "All runs", "All source devices"
	content := 0
	pageHeader(c, "Run artifacts", "", nil)
	ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
		ui.Select(c, &run, []string{"All runs", "7781 · load-test"}).Width(170).Label("Run")
		ui.Select(c, &src, []string{"All source devices"}).Width(190).Label("Source device")
		ui.Segmented(c, &content, "All", "Available", "Metadata only").Label("Content")
		ui.Spacer(c)
		muted(c, fmt.Sprintf("atlas-api · %d artifacts, newest first", artifactTotal))
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1.25), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(0).Gap(0).Children(func() {
			ui.Row(c).Padding(sp2+2, sp3).Gap(sp3).Background(tk.Sunken).BorderWidth(0, 0, 1, 0).BorderColor(tk.Border).Radius(radiusCard, radiusCard, 0, 0).Children(func() {
				overline(c, "ARTIFACT").Grow(1)
				overline(c, "SOURCE").Width(96)
				overline(c, "CONTENT").Width(116)
				overline(c, "SIZE").Width(56)
			})
			ui.Column(c).Padding(sp1).Gap(1).Children(func() {
				for _, a := range artifacts {
					listRow(c, a.name == "latency-report.json", func() {
						ui.Column(c).Grow(1).Gap(2).Children(func() {
							mono(c, a.name).FontSize(fsBody)
							subtle(c, "run "+a.run)
						})
						cut(mono(c, a.source).TextColor(tk.Muted).Width(96))
						ui.Row(c).Width(116).Children(func() { pill(c, a.content, a.contentText) })
						ui.Text(c, a.size).FontSize(fsMeta).TextColor(tk.Muted).Width(56).TextAlign(ui.End)
					}).Padding(sp2, sp3)
				}
			})
			older := artifactTotal - len(artifacts)
			listFooter(c, fmt.Sprintf("Showing 1–%d of %d", len(artifacts), artifactTotal), func() {
				iconButton(c, icChevD, fmt.Sprintf("Load %d older", older), false).Tooltip("Appends artifacts 6–12 below this row")
			})
			ui.Row(c).Padding(sp3).Gap(sp2).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Start).Children(func() {
				ico(c, icInfo, 14, tk.Muted).Margin(1, 0, 0, 0)
				muted(c, "Metadata-only items keep their bytes on the source device. trace.zst is on ci-runner-eu, run 7781. Ask that device or its CI storage for the file.")
			})
		})
		card(c).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icPackage, 16, tk.Accent)
				ui.Text(c, "latency-report.json").Font(fontMono).FontSize(fsSection).FontWeight(600).Grow(1)
				pill(c, toneOk, "Bytes verified")
			})
			kvGrid(c, []kvRow{
				{k: "Run", fn: func() { body(c, "7781 · load-test"); idChip(c, "4e9b2c70-13d8-4a6f-b5e2-907c3a1d6f48") }},
				{k: "Source device", v: ciDev, mono: true},
				{k: "Completion", v: "Completed on ci-runner-eu, today 09:52"},
				{k: "SHA-256", v: "3fa1c09e7b2d4e61a90c5f38d1b7e2a46c0f9d83b5e17a2c4d6f08e9d27b5e84", mono: true},
				{k: "On this device", v: "48.2 KB · digest checked 10:45:10"},
			})
			ui.Divider(c)
			strong(c, "Export to a new file")
			dest := "/home/dev/exports/latency-7781.json"
			ui.Row(c).Gap(sp2).Children(func() {
				ui.TextInput(c, &dest).Grow(1).Font(fontMono).FontSize(fsMono).Label("Destination")
				ui.Button(c, "Choose…")
			})
			ui.Row(c).Gap(sp2).Padding(sp2, sp3).Radius(radiusControl).Background(tk.OkSoft).AlignItems(ui.Center).Children(func() {
				ico(c, icCheckCircle, 15, tk.Ok)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, "Exported and verified · 10:45:22").FontSize(fsBody).TextColor(tk.Ok).FontWeight(600)
					mono(c, dest).TextColor(tk.Ok)
					ui.Text(c, "New file · 48.2 KB · SHA-256 matches").FontSize(fsMeta).TextColor(tk.Ok)
				})
				linkButton(c, "Show in folder", nil)
			})
		})
	})
}
