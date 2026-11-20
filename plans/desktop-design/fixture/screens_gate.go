package main

import (
	"github.com/egoist/mygo/ui"
)

func gateChrome() chrome {
	return chrome{crumbs: []string{"Workspace readiness"}, s: snap{gate: true}}
}

// readinessPanel is the shell-level gate: it states the data root and what
// was found there before any destination opens.
func readinessPanel(c *ui.Context) {
	card(c).Children(func() {
		ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(28, 28).Radius(radiusControl).Background(tk.UnknownSoft).Center().Children(func() {
				ico(c, icDatabase, 15, tk.Unknown)
			})
			h2(c, "Data root").Grow(1)
			pill(c, toneUnknown, "Fresh · no catalog")
		})
		kvGrid(c, []kvRow{
			{k: "Path", v: dataRoot, mono: true},
			{k: "Read", v: "Today 10:41:52 · directory exists and is empty"},
			{k: "Catalog", v: "None yet, so nothing to migrate"},
		})
		ui.Row(c).Gap(sp2).Padding(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
			ico(c, icTerminal, 13, tk.Muted)
			muted(c, "Wrong location? Restart CtxFerry with").Shrink(0)
			mono(c, "--data-dir <path>").TextColor(tk.Muted)
			muted(c, "to open another data root. Every command shown later carries the same flag.")
		})
	})
}

func gatePage(c *ui.Context, join bool) {
	ui.Column(c).Gap(sp4).Children(func() {
		if join {
			pageHeader(c, "Join a Workspace", "Two stages: join and subscribe, then bind a directory. Nothing is written until you run a command.", nil)
			readinessStrip(c)
			joinChoice(c)
			joinForm(c)
			return
		}
		pageHeader(c, "Welcome to CtxFerry", "Set up this device by creating a Workspace or joining one. Nothing is written until you confirm.", nil)
		readinessPanel(c)
		ui.Row(c).Gap(sp4).AlignItems(ui.Stretch).Children(func() {
			gateOption(c, true, icPlus, "Create a Workspace",
				"Start a new Workspace here. It works locally without a remote.",
				[]gatePoint{{icDatabase, "Initialize the catalog at this data root"}, {icFolder, "Bind a directory to create the first Project"}, {icUpload, "Publish later from Sync › Repository"}},
				"", func() { ui.PrimaryButton(c, "Create Workspace") })
			gateOption(c, false, icLink, "Join an existing Workspace",
				"Use the private repository another device already publishes to.",
				[]gatePoint{{icDownload, "Read the published Workspace"}, {icFolder, "Subscribe to Projects by slug"}, {icLink, "Bind directories by the resolved Project ID"}},
				"Git access to the private repository · not checked yet", func() { ui.Button(c, "Join a Workspace…") })
		})
	})
}

// joinChoice replaces the two option cards once Join is chosen: Create is
// reduced to a way back, with no competing primary action.
func joinChoice(c *ui.Context) {
	choice := "join"
	ui.RadioGroup(c, func() {
		ui.Row(c).Gap(sp3).Children(func() {
			for _, o := range []struct{ v, label, sub string }{
				{"create", "Create a Workspace", "Start new here"},
				{"join", "Join an existing Workspace", "Use the private repository"},
			} {
				r := ui.Row(c).Grow(1).Basis(0).Gap(sp3).Padding(sp3, sp4).Radius(radiusCard).Border(1, tk.Border).AlignItems(ui.Center)
				if choice == o.v {
					r.Border(1.5, tk.Accent).Background(ui.Hex("#FAFDFE"))
				}
				r.Children(func() {
					ui.RadioBase(c, &choice, o.v).Label(o.label).Children(func() { radioDot(c, choice == o.v) })
					strong(c, o.label)
					muted(c, o.sub)
				})
			}
		})
	}).Label("Workspace setup")
}

// readinessStrip is the compact form of the readiness panel once a path
// has been chosen.
func readinessStrip(c *ui.Context) {
	ui.Row(c).Gap(sp3).Padding(sp2+2, sp4).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		ico(c, icDatabase, 15, tk.Unknown)
		overline(c, "DATA ROOT")
		mono(c, dataRoot)
		ui.Spacer(c)
		pill(c, toneUnknown, "Fresh · no catalog")
		muted(c, "Read 10:41:52")
	})
}

// gatePoint is one thing an option does, marked with the icon of what it
// touches. Check marks are reserved for facts that have been verified.
type gatePoint struct {
	icon *ui.SVG
	text string
}

// gateOption lists what a path does and, apart from that, what it requires
// and has not been checked: the requirement carries the open/pending icon.
func gateOption(c *ui.Context, selected bool, icon *ui.SVG, t, sub string, points []gatePoint, requires string, action func()) {
	k := card(c).Grow(1).Basis(0).Gap(sp3)
	if selected {
		k.Border(1.5, tk.Accent).Background(ui.Hex("#FAFDFE"))
	}
	k.Children(func() {
		cardHeader(c, icon, t, sub, nil)
		ui.Column(c).Gap(6).Children(func() {
			for _, p := range points {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ico(c, p.icon, 14, tk.Muted)
					body(c, p.text)
				})
			}
		})
		if requires != "" {
			ui.Row(c).Gap(sp2).Padding(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
				ico(c, icDashed, 14, tk.Unknown)
				subtle(c, "Requires")
				body(c, requires)
			})
		}
		ui.Spacer(c)
		ui.Row(c).Children(action)
	})
}

// Stale snapshot: a refresh failed after a successful read.
func staleHomePage(c *ui.Context) {
	banner(c, toneAttention, "Refresh failed at "+staleFailedAt+". Showing the "+snapStale.read+" snapshot.",
		"Writes, Backup and remote checks are off until a reread succeeds.",
		func() {
			ui.Button(c, "Run doctor…").Tooltip("ctxferry doctor --data-dir " + dataRoot)
			iconButton(c, icSync, "Retry read", true)
		})
	homePage(c, snapStale)
}

// joinForm collects what the join needs on this device. The commands on the
// right are generated from these fields and always carry the data root.
func joinForm(c *ui.Context) {
	repo := remoteURL
	scope := 0
	projects := "atlas-api, billing-worker"
	path := "~/code/atlas-api"
	bindProject := "atlas-api"
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1.15)).GapX(sp4).AlignItems(ui.Stretch).Children(func() {
		card(c).Gap(sp3).Children(func() {
			cardHeader(c, icLink, "Join details", "The commands update as you edit.", nil)
			ui.Field(c, "Private repository", func() {
				ui.TextInput(c, &repo).Font(fontMono).FontSize(fsMono)
			})
			ui.Field(c, "Subscribe to", func() {
				ui.Column(c).Gap(sp2).Children(func() {
					ui.Segmented(c, &scope, "Chosen Projects", "All Projects").Label("Subscription")
					ui.TextInput(c, &projects).Font(fontMono).FontSize(fsMono).Label("Project slugs or IDs")
					muted(c, "Slugs or IDs, separated by commas. More can be added later.")
				})
			})
			ui.Field(c, "Checkout directory on this device", func() {
				ui.Row(c).Gap(sp2).Children(func() {
					ui.TextInput(c, &path).Grow(1).Font(fontMono).FontSize(fsMono).Label("Checkout directory")
					ui.Button(c, "Choose…")
				})
			})
			ui.Field(c, "Bind it to Project", func() {
				ui.Column(c).Gap(sp2).Children(func() {
					ui.Select(c, &bindProject, []string{"atlas-api", "billing-worker"}).Label("Project for this directory")
					muted(c, "From your subscription list. Its Project ID is read after join.")
				})
			})
		})
		card(c).Gap(sp3).Children(func() {
			cardHeader(c, icTerminal, "Join in two stages", "Copying changes nothing. join writes only to this fresh data root.", nil)
			stepLabel(c, "1", "Join and subscribe by slug", nil)
			ui.Column(c).Gap(6).Padding(0, 0, 0, 22).Children(func() {
				for _, p := range []prereq{
					{toneOk, "Data root is fresh · checked 10:41:52"},
					{toneUnknown, "Git access to the repository · checked by join"},
				} {
					ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
						col, _ := p.t.colors()
						ico(c, p.t.icon(), 14, col)
						body(c, p.label)
					})
				}
			})
			cmdBlock(c, cli("join", repo, "--subscribe atlas-api", "--subscribe billing-worker"))
			afterRun(c, "After it finishes, Refresh reads the joined catalog and resolves each slug to its Project ID.", "")
			ui.Divider(c)
			stepLabel(c, "2", "Bind "+path+" to "+bindProject, func() { pill(c, toneUnknown, "After Refresh") })
			muted(c, "Bind names the Project by the ID that Refresh reads for "+bindProject+", not by its slug. The command appears here once that ID is known.").Padding(0, 0, 0, 22)
		})
	})
}

func stepLabel(c *ui.Context, n, label string, trailing func()) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		ui.Text(c, n).FontSize(fsMeta).FontWeight(650).TextColor(tk.Accent).Width(14).TextAlign(ui.Center)
		strong(c, label).Grow(1)
		if trailing != nil {
			trailing()
		}
	})
}

// joinedBindPage is stage 2 of Join: join has run, Refresh has read the
// joined catalog, and each subscribed slug now has the Project ID that the
// bind command names.
func joinedBindPage(c *ui.Context) {
	s := snapJoined
	path := "~/code/atlas-api"
	bindProject := prjAtlas.name + " · " + short(prjAtlas.id)
	ui.Column(c).Gap(sp4).Children(func() {
		pageHeader(c, "Join a Workspace", "Joined "+wsName+". Bind a directory to finish.", nil)
		ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
			stageChip(c, "1", "Join and subscribe", "Done "+joinedAt, true)
			ui.Box(c).Height(1).Width(32).Background(tk.BorderStrong)
			stageChip(c, "2", "Bind a Checkout", "Now", false)
		})
		ui.Grid(c).ColumnTracks(ui.Fr(1.1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(sp4).Children(func() {
				card(c).Children(func() {
					cardHeader(c, icCheckCircle, "Joined", "Read back from this data root.", nil)
					kvGrid(c, []kvRow{
						{k: "Workspace", fn: func() { strong(c, wsName); idChip(c, wsID) }},
						{k: "Written locally", v: "join · " + joinedAt + " · catalog and 2 subscriptions"},
						{k: "Read back", v: "Today " + s.read + " · by Refresh"},
						{k: "Confirmed commit", fn: func() { mono(c, s.confirmed); subtle(c, "·"); body(c, "fetched, published by another device") }},
					})
				})
				card(c).Gap(sp2).Children(func() {
					h2(c, "Project IDs for your slugs")
					muted(c, "Read from the joined catalog at "+s.read+".")
					ui.Column(c).Gap(0).Dividers(1, tk.Border).BorderWidth(1, 0, 1, 0).BorderColor(tk.Border).Margin(sp1, 0, 0, 0).Children(func() {
						for _, p := range []project{prjAtlas, prjBilling} {
							ui.Row(c).Gap(sp3).Padding(sp2+2, 0).AlignItems(ui.Center).Children(func() {
								ui.Column(c).Gap(3).Grow(1).Shrink(1).Children(func() {
									ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
										strong(c, p.name)
										muted(c, p.sub)
									})
									mono(c, p.id).TextColor(tk.Muted)
								})
								pill(c, toneOk, "Subscribed")
							})
						}
					})
				})
			})
			card(c).Gap(sp3).Children(func() {
				cardHeader(c, icLink, "Bind "+path, "Makes this directory a Checkout of atlas-api on "+deviceName+".", nil)
				ui.Field(c, "Checkout directory", func() {
					ui.Row(c).Gap(sp2).Children(func() {
						ui.TextInput(c, &path).Grow(1).Font(fontMono).FontSize(fsMono).Label("Checkout directory")
						ui.Button(c, "Choose…")
					})
				})
				ui.Field(c, "Project", func() {
					ui.Select(c, &bindProject, []string{bindProject, prjBilling.name + " · " + short(prjBilling.id)}).Label("Project for this directory")
				})
				cmdBlock(c, cli("project bind "+path, "--project "+prjAtlas.id))
				afterRun(c, "After it finishes, Refresh opens atlas-api with this Checkout.", "")
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ico(c, icInfo, 13, tk.Muted)
					muted(c, "Bind later from Projects. Nothing is bound until the command runs.").Grow(1)
					linkButton(c, "Open Home", icArrowR)
				})
			})
		})
	})
}

// stageChip is one stage of a short, linear setup sequence.
func stageChip(c *ui.Context, n, label, state string, done bool) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
		t := toneAccent
		if done {
			t = toneOk
		}
		stepNumber(c, n, t)
		ui.Column(c).Gap(0).Children(func() {
			strong(c, label)
			subtle(c, state)
		})
	})
}
