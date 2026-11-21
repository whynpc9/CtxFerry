package main

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type setupState struct {
	join          bool
	createConfirm bool
	creating      bool
	remote        string
	subscriptions string
	checkout      string
	err           string
}

func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// joinArgs subscribes to the listed Projects, or to all of them when none is listed.
func joinArgs(remote, subscriptions string) []string {
	args := []string{"join", remote}
	for _, subscription := range strings.FieldsFunc(subscriptions, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		args = append(args, "--subscribe", subscription)
	}
	if len(args) == 2 {
		args = append(args, "--all-projects")
	}
	return args
}

func desktopCommand(dataRoot string, args ...string) string {
	parts := []string{"ctxferry"}
	parts = append(parts, args...)
	parts = append(parts, "--data-dir", dataRoot)
	for i := 1; i < len(parts); i++ {
		parts[i] = shellArgument(parts[i])
	}
	return strings.Join(parts, " ")
}

func (d *desktop) commandBlock(c *ui.Context, command string, primary bool) ui.Element {
	label := "Copy command"
	if d.copiedCommand == command {
		label = "Copied"
	}
	return ui.Column(c).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ico(c, icTerminal, 13, tk.Muted)
			muted(c, "Run in a terminal").Grow(1)
			iconButton(c, icCopy, label, primary).Label("Copy command: " + command).Tooltip("Copies the full command, including --data-dir").OnClick(func() {
				mygo.Clipboard.WriteText(command)
				d.copiedCommand = command
			})
		})
		ui.Row(c).Background(ui.Hex("#11161C")).Radius(radiusControl).Padding(sp2+2, sp3).Gap(sp3).AlignItems(ui.Start).Children(func() {
			ui.Text(c, "$").Font(fontMono).FontSize(fsMono).TextColor(ui.Hex("#94A0AE")).Shrink(0)
			ui.Text(c, command).Font(fontMono).FontSize(fsMono).TextColor(ui.Hex("#E6EDF3")).LineHeight(1.5).Grow(1).Selectable()
		})
	})
}

func (d *desktop) afterRun(c *ui.Context, text string) ui.Element {
	return ui.Row(c).Gap(sp2).Padding(sp2, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
		muted(c, text).Grow(1).Shrink(1)
		quietButton(c, icSync, "Refresh").OnClick(d.refresh)
	})
}

func (d *desktop) readinessView(c *ui.Context) {
	if d.loading && d.err == nil {
		pageHeader(c, "Opening this Workspace", "Reading the local catalog and its device evidence.", nil)
		card(c).Children(func() {
			cardHeader(c, icDatabase, "Data root", d.core.DataDir, nil)
		})
		return
	}
	heading, subtitle := readinessError(d.err)
	pageHeader(c, heading, subtitle, nil)
	card(c).Children(func() {
		cardHeader(c, icDatabase, "Data root", "Every action on this screen uses this exact location.", nil)
		mono(c, d.core.DataDir).FillWidth().Selectable()
		ui.Divider(c)
		muted(c, "Wrong location? Restart CtxFerry with CTXFERRY_DATA_DIR set to another data root. Every copied command carries the same explicit --data-dir.")
	})
	state := app.ClassifyCatalog(d.err)
	switch state {
	case app.CatalogUninitialized:
		d.setupView(c)
	case app.CatalogMigrationRequired:
		d.recoveryCommand(c, "Migrate with the matching CLI", "Open the catalog once with the matching CtxFerry CLI. The migration preserves the data root.", desktopCommand(d.core.DataDir, "project", "list"))
	case app.CatalogUnsupportedSchema:
		banner(c, toneUnknown, "Update required", "No write action is available for a newer schema.", nil)
	default:
		d.recoveryCommand(c, "Diagnose this data root", "The desktop does not expose raw database diagnostics. Use the CLI report to identify the failed catalog check.", desktopCommand(d.core.DataDir, "doctor", "--format", "json"))
	}
	if state != app.CatalogReady && state != app.CatalogUninitialized {
		ui.Row(c).Justify(ui.End).Children(func() {
			iconButton(c, icSync, "Refresh", false).OnClick(d.refresh)
		})
	}
}

func (d *desktop) setupView(c *ui.Context) {
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icDatabase, "Create a Workspace", "Start locally; configure private publication later.", nil)
			body(c, "Initialize the catalog on this device.")
			body(c, "Bind a directory to create the first Project.")
			body(c, "Nothing is written until you confirm.")
			if !d.setup.createConfirm {
				ui.PrimaryButton(c, "Create Workspace").OnClick(func() {
					d.setup.join = false
					d.setup.createConfirm = true
				})
			}
		})
		card(c).Children(func() {
			cardHeader(c, icDownload, "Join a Workspace", "Read an existing private repository onto this device.", nil)
			body(c, "Subscribe to Projects by slug or ID.")
			body(c, "Bind directories after Refresh resolves their Project IDs.")
			ui.Divider(c)
			muted(c, "Requires Git access to the private repository · not checked yet")
			ui.Button(c, "Join a Workspace…").OnClick(func() {
				d.setup.join = true
				d.setup.createConfirm = false
			})
		})
	})
	if d.setup.err != "" {
		banner(c, toneBlocked, "Setup did not complete", d.setup.err, nil)
	}
	if d.setup.createConfirm && !d.setup.join {
		previewPanel(c, "Create a local Workspace", "Target data root: "+d.core.DataDir, []effect{
			{toneAccent, "Local catalog", "Creates a new Workspace and this device's identity; no remote is configured."},
			{toneNeutral, "Publication", "Nothing is uploaded. Projects and Sync remain available locally."},
		}, nil, func() {
			ui.Row(c).Gap(sp2).Justify(ui.End).Children(func() {
				ui.Button(c, "Cancel").Disabled(d.setup.creating).OnClick(func() { d.setup.createConfirm = false })
				ui.PrimaryButton(c, "Create Workspace here").Disabled(d.setup.creating).OnClick(d.createWorkspace)
			})
		})
	}
	if d.setup.join {
		d.joinView(c)
	}
}

func (d *desktop) createWorkspace() {
	if d.setup.creating {
		return
	}
	d.setup.creating, d.setup.err = true, ""
	runDesktop(d, d.core.Init, func(_ app.InitResult, err error) {
		d.setup.creating = false
		if err != nil {
			d.setup.err = "The Workspace was not created. Check the data root and try again (" + app.Classify(err).Code + ")."
			return
		}
		d.refresh()
	})
}

func (d *desktop) joinView(c *ui.Context) {
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icLink, "Join details", "Stage 1 of 2 · join and subscribe. Bind after Refresh.", nil)
			ui.Text(c, "Private repository").FontSize(fsMeta).TextColor(tk.Muted)
			ui.TextInput(c, &d.setup.remote).FillWidth().Label("Private repository").Placeholder("git@github.com:OWNER/REPO.git")
			ui.Text(c, "Subscribe to (comma-separated slugs or IDs)").FontSize(fsMeta).TextColor(tk.Muted)
			ui.TextInput(c, &d.setup.subscriptions).FillWidth().Label("Project subscriptions").Placeholder("atlas-api, billing-worker")
			ui.Text(c, "Checkout directory (optional, for stage 2)").FontSize(fsMeta).TextColor(tk.Muted)
			ui.TextInput(c, &d.setup.checkout).FillWidth().Label("Checkout directory").Placeholder("/absolute/path/to/checkout")
			muted(c, "Copying changes nothing. Join runs only in your terminal.")
		})
		card(c).Children(func() {
			cardHeader(c, icTerminal, "Join in two stages", "Review the exact command before opening a terminal.", nil)
			remote := strings.TrimSpace(d.setup.remote)
			if remote == "" {
				banner(c, toneUnknown, "Repository required", "Enter the private Git remote to generate the join command.", nil)
				return
			}
			d.commandBlock(c, desktopCommand(d.core.DataDir, joinArgs(remote, d.setup.subscriptions)...), true)
			d.afterRun(c, "After it finishes, Refresh reads the joined catalog and resolves each Project ID. Bind a Checkout from Projects.")
		})
	})
}

func (d *desktop) recoveryCommand(c *ui.Context, heading, description, command string) {
	card(c).Children(func() {
		cardHeader(c, icTerminal, heading, description, nil)
		d.commandBlock(c, command, true)
		d.afterRun(c, "After it finishes, Refresh rereads this same data root.")
	})
}
