package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

func (d *desktop) backupPage(c *ui.Context) {
	s := &d.syncState
	if !s.backupAt.IsZero() {
		d.backupReceipt(c)
		return
	}
	if d.err != nil || d.loading || d.publicationUnknown() {
		message := "A fresh successful local read is required before Backup."
		if d.publicationUnknown() {
			message = "A publication has an uncertain outcome. Reconcile its journal in Repository and Refresh before Backup; local work remains preserved."
		}
		card(c).Children(func() {
			cardHeader(c, icLock, "Backup is temporarily locked", message, func() { pill(c, toneAttention, "Blocked") })
			ui.PrimaryButton(c, "Open Repository recovery").AlignSelf(ui.Start).OnClick(func() { s.tab = syncRepositoryTab })
		})
		return
	}
	if s.backingUp {
		card(c).Children(func() {
			cardHeader(c, icDrive, "Writing backup", "Navigation is locked until the core acknowledges the result.", nil)
			muted(c, "Writing the catalog, any confirmed Git history, selected local-only data and finally manifest.json…")
		})
		return
	}

	card(c).Children(func() {
		cardHeader(c, icDatabase, "1 · From Workspace", "The source snapshot used for this backup.", nil)
		kvGrid(c, []kvRow{
			{k: "Workspace", fn: func() { strong(c, "Local Workspace"); idChip(c, d.data.Workspace.ID) }},
			{k: "Data root", v: d.core.DataDir, mono: true},
			{k: "Preflight", fn: func() {
				pill(c, toneOk, "Journal clear")
				body(c, "·")
				mono(c, valueOr(shortCommit(d.data.Sync.Head), "no confirmed commit"))
				body(c, "· read "+d.data.ReadAt.Local().Format("15:04:05"))
			}},
		})
	})
	card(c).Children(func() {
		cardHeader(c, icFolder, "2 · To a new directory", "The target must be absolute and must not exist yet.", nil)
		ui.Row(c).Gap(sp2).AlignItems(ui.End).Children(func() {
			ui.Column(c).Grow(1).Gap(sp1).Children(func() {
				ui.Text(c, "Destination").FontSize(fsMeta).TextColor(tk.Muted)
				ui.TextInput(c.Key("backup-destination"), &s.backupPath).FillWidth().Label("Backup destination").Placeholder("/absolute/path/to/new-backup").Disabled(s.backupReview).OnChange(func() {
					s.backupReview, s.backupAck, s.backupErr = false, false, ""
				})
			})
			ui.Button(c, "Choose parent…").Disabled(s.backupReview || d.window == nil).OnClick(d.chooseBackupParent)
		})
		d.backupDestinationState(c)
	})
	d.backupScope(c)
	d.backupExposure(c)
	if s.backupErr != "" {
		banner(c, toneBlocked, "Backup did not complete", s.backupErr, nil)
	}
	if !s.backupReview {
		ui.Row(c).Justify(ui.End).Children(func() {
			ui.PrimaryButton(c, "Review backup").Disabled(strings.TrimSpace(s.backupPath) == "").OnClick(d.reviewBackup)
		})
		return
	}
	previewBadge(c)
	if d.compact {
		return
	}
	ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c, &s.backupAck, "I reviewed what this plaintext backup exposes").Label("Acknowledge plaintext backup exposure")
		ui.Spacer(c)
		ui.Button(c, "Back to backup options").OnClick(func() { s.backupReview, s.backupAck = false, false })
		ui.PrimaryButton(c, "Write backup").Disabled(!s.backupAck).OnClick(func() { d.createBackup(strings.TrimSpace(s.backupPath), s.includeLocal) })
	})
}

func (d *desktop) backupDestinationState(c *ui.Context) {
	path := strings.TrimSpace(d.syncState.backupPath)
	switch {
	case path == "":
		muted(c, "No destination selected.")
	case !filepath.IsAbs(path):
		banner(c, toneAttention, "Absolute path required", "Enter an absolute path to a directory that does not exist yet.", nil)
	default:
		_, err := os.Stat(path)
		switch {
		case err == nil:
			banner(c, toneBlocked, "Destination already exists", "Choose another new directory. Backup never overwrites an existing target.", nil)
		case os.IsNotExist(err):
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icCheckCircle, 13, tk.Ok)
				muted(c, "New directory · does not exist yet")
			})
		default:
			muted(c, "Destination availability is unknown; the core will validate it again before writing.")
		}
	}
}

func (d *desktop) backupScope(c *ui.Context) {
	s := &d.syncState
	card(c).Children(func() {
		cardHeader(c, icLayers, "3 · Data scope", "One option. The rest is fixed by the backup format.", nil)
		if n := d.blockedCount(); n > 0 {
			ui.Row(c).Gap(sp2).Padding(sp2, sp3).Radius(radiusControl).Background(tk.Sunken).AlignItems(ui.Center).Children(func() {
				ico(c, icTasks, 14, tk.Muted)
				body(c, fmt.Sprintf("Includes %s as saved local revisions.", plural(n, "blocked Task or record", "blocked Tasks or records"))).Grow(1)
				pill(c, toneBlocked, plural(n, "blocked", "blocked"))
			})
		}
		ui.Grid(c).ColumnTracks(ui.Fr(1.2), ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(sp2).Children(func() {
				overline(c, "ALWAYS INCLUDED")
				backupScopeLine(c, icDatabase, "Workspace, Projects, shared records, Context and Configuration")
				backupScopeLine(c, icSkill, "Skill pins, approvals and sources")
				backupScopeLine(c, icSync, "Queue state, conflicts, blocked records and tombstones")
				backupScopeLine(c, icCommit, "Git history to "+valueOr(shortCommit(d.data.Sync.Head), "the confirmed head, when one exists"))
			})
			ui.Column(c).Gap(sp2).Children(func() {
				overline(c, "OPTIONAL")
				ui.Checkbox(c, &s.includeLocal, "Local-only data on "+d.data.Device.Name).Disabled(s.backupReview).OnChange(func() {
					s.backupReview, s.backupAck = false, false
				})
				muted(c, "Checkout bindings and paths, Profile selections, subscriptions, local overrides, routes, observations, run records and available artifact bytes.")
			})
			ui.Column(c).Gap(sp2).Children(func() {
				overline(c, "NEVER INCLUDED")
				backupScopeLine(c, icKey, "Provider-held credential values, provider sessions and Git authentication")
				backupScopeLine(c, icShield, "Execution trust · rechecked per device")
				backupScopeLine(c, icSkill, "Skill installation receipts and installed Skill files")
			})
		})
	})
}

func backupScopeLine(c *ui.Context, icon *ui.SVG, text string) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
		ico(c, icon, 13, tk.Muted).Margin(2, 0, 0, 0)
		body(c, text).Grow(1)
	})
}

func (d *desktop) backupExposure(c *ui.Context) {
	localLabel := "Local-only data · not selected"
	if d.syncState.includeLocal {
		localLabel = "Local-only data · selected"
	}
	card(c).Background(tk.ConflictSoft).Children(func() {
		cardHeader(c, icAlert, "4 · Plaintext exposure", "Not encrypted. Anyone who can read the new directory can read its contents.", nil)
		ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(sp2).Children(func() {
				overline(c, "EVERY BACKUP")
				body(c, "Shared Context documents and Configuration values")
				body(c, "Skill pins and their sources")
				body(c, "Published Git history and its plaintext contents")
			})
			ui.Column(c).Gap(sp2).Children(func() {
				overline(c, localLabel)
				if d.syncState.includeLocal {
					body(c, "Checkout paths and Profiles · local overrides")
					body(c, "Resource routes and observations")
					body(c, "Run artifacts and available bytes on this device")
				} else {
					muted(c, "Enable the single local-only option to include this group.")
				}
			})
		})
		ui.Divider(c)
		strong(c, "Context, Configuration, Skill sources and Git history can contain embedded secrets. Review the original plaintext, not only its manifest.")
		muted(c, "Provider-held credential values are never included. Local-only artifact bytes are included only when that option is selected.")
	})
}

func (d *desktop) reviewBackup() {
	s := &d.syncState
	path := strings.TrimSpace(s.backupPath)
	s.backupErr = ""
	switch {
	case !filepath.IsAbs(path):
		s.backupErr = "Choose an absolute path to a new directory."
		return
	case d.err != nil || d.loading || d.publicationUnknown():
		s.backupErr = "Refresh and reconcile any uncertain publication before reviewing Backup."
		return
	}
	if _, err := os.Stat(path); err == nil {
		s.backupErr = "The destination already exists. Choose another new directory."
		return
	} else if !os.IsNotExist(err) {
		s.backupErr = "The destination cannot be checked. Choose another new directory."
		return
	}
	s.backupReview, s.backupAck = true, false
}

func (d *desktop) chooseBackupParent() {
	if d.window == nil {
		return
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: d.window, Title: "Choose a parent for the new backup directory", Directory: true})
	if err != nil || len(paths) == 0 {
		return
	}
	d.syncState.backupPath = filepath.Join(paths[0], "ctxferry-backup")
	d.syncState.backupReview, d.syncState.backupAck, d.syncState.backupErr = false, false, ""
}

func (d *desktop) createBackup(path string, includeLocal bool) {
	s := &d.syncState
	if !s.backupReview || !s.backupAck || s.backingUp {
		return
	}
	s.backingUp, s.backupErr = true, ""
	sourceRead := d.data.ReadAt
	runDesktop(d, func(ctx context.Context) (app.WorkspaceBackupResult, error) {
		return d.core.BackupWorkspace(ctx, path, includeLocal)
	}, func(result app.WorkspaceBackupResult, err error) {
		s.backingUp = false
		if err != nil {
			s.backupErr = valueOr(map[string]string{
				"destination-exists": "The destination already exists. Choose a new directory.",
				"push-pending":       "A publication outcome is uncertain. Reconcile it in Repository, Refresh, then review Backup again.",
				"sync-busy":          "Sync is using this Workspace. Wait for it to finish, Refresh and retry.",
			}[app.Classify(err).Code], "Backup was not acknowledged. Inspect the destination and reread local state before choosing another new directory.")
			s.backupReview, s.backupAck = false, false
			return
		}
		if result.WorkspaceID != d.data.Workspace.ID {
			s.backupErr = "Backup completed for a different Workspace. Refresh and verify the destination before any other action."
			d.refresh()
			return
		}
		s.backup, s.backupAt, s.backupSourceRead = result, time.Now(), sourceRead
		s.backupPath, s.backupReview, s.backupAck, s.includeLocal = "", false, false, false
	})
}

func (d *desktop) backupReceipt(c *ui.Context) {
	s := &d.syncState
	result := s.backup
	scope := "Shared records"
	if result.IncludesLocalOnly {
		scope += " and local-only data on " + d.data.Device.Name
	}
	if result.Head != "" {
		scope += " · history to " + shortCommit(result.Head)
	}
	receiptPanel(c, receipt{
		heading: "Backup written",
		rows: []kvRow{
			{k: "Destination", v: result.Destination, mono: true},
			{k: "Manifest", v: filepath.Join(result.Destination, "manifest.json"), mono: true},
			{k: "Included", v: scope},
			{k: "Catalog SHA-256", v: result.CatalogSHA256, mono: true},
			{k: "Not included", v: "Provider-held credential values, provider sessions, Git authentication, execution trust, Skill installation receipts and installed Skill files"},
		},
		ev: evidence{
			source: "Read " + s.backupSourceRead.Local().Format("15:04:05"), sourceDetail: "Confirmed " + valueOr(shortCommit(result.Head), "head unavailable") + " · journal clear",
			written: "Written " + s.backupAt.Local().Format("15:04:05"), writtenDetail: "Manifest written last, with catalog and bundle SHA-256",
			published: "Nothing", pubDetail: "A backup does not publish or change the Queue", pubTone: toneNeutral,
		},
	})
	card(c).Children(func() {
		cardHeader(c, icFile, "Recovery guide", "Restore republishes under a new epoch. Local recovery rebuilds a device in a new data directory.", nil)
		mono(c, "ctxferry workspace restore --from <backup> --if-head <head> --new-epoch <epoch>")
		mono(c, "ctxferry workspace recover-local --from <backup>")
		ui.Button(c, "Open recovery guide").AlignSelf(ui.Start).OnClick(func() {
			_ = mygo.Shell.OpenExternal("https://github.com/whynpc9/CtxFerry/blob/main/docs/content/recovery.md")
		})
		ui.Button(c, "Create another backup").AlignSelf(ui.Start).OnClick(func() {
			s.backupAt, s.backup, s.backupErr = time.Time{}, app.WorkspaceBackupResult{}, ""
		})
	})
}

func (d *desktop) backupStatus(c *ui.Context) {
	s := &d.syncState
	switch {
	case s.backingUp:
		detail(c, "Workspace backup", "Writing plaintext backup…")
	case !s.backupAt.IsZero():
		detail(c, "Backup written", s.backupAt.Format("2006-01-02 15:04:05")+" · manifest.json")
	default:
		detail(c, "Workspace backup", "No backup receipt in this window")
	}
}
