package main

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/whynpc9/CtxFerry/internal/app"
)

type queueFilter int

const (
	queueAll queueFilter = iota
	queueConflicts
	queueBlocked
	queuePending
)

const (
	syncRepositoryTab = iota
	syncQueueTab
	syncBackupTab
)

type syncPageState struct {
	tab           int
	filter        int
	list          ui.ListState
	selected      int
	returnRoute   desktopRoute
	restoreRecord string
	scroll        ui.ScrollState

	checking   bool
	remote     remoteObservation
	remoteErr  bool
	browserErr bool

	previewing   bool
	firstPreview app.SyncResult
	previewErr   string

	backupPath       string
	includeLocal     bool
	backupReview     bool
	backupAck        bool
	backingUp        bool
	backup           app.WorkspaceBackupResult
	backupAt         time.Time
	backupSourceRead time.Time
	backupErr        string
}

type remoteObservation struct {
	config app.SyncConfig
	check  app.SyncCheck
	at     time.Time
}

func (s *syncPageState) setFilter(filter queueFilter) {
	if s.filter != int(filter) {
		s.filter = int(filter)
		s.list = ui.ListState{}
		s.selected = 0
	}
}

func (d *desktop) syncPage(c *ui.Context) {
	state := &d.syncState
	pageHeader(c, "Sync", "Repository evidence, local publication state and recovery.", func() { d.syncSummary(c) })
	if d.data.SyncErr != nil {
		banner(c, toneBlocked, "Local sync records are unavailable", "Run ctxferry doctor, then Refresh. Other local Project data remains available.", nil)
	} else if message := syncProblem(d.data.LastOutcome); message != "" {
		banner(c, toneAttention, "Repository attention", message, nil)
	}
	queueLabel := "Queue"
	if d.data.SyncErr == nil {
		queueLabel = fmt.Sprintf("Queue · %d", len(d.data.Queue))
	}
	previous := state.tab
	ui.Tabs(c, &state.tab, "Repository", queueLabel, "Backup").Label("Sync sections")
	if previous != state.tab {
		state.scroll = ui.ScrollState{}
	}
	switch state.tab {
	case syncQueueTab:
		if d.data.SyncErr == nil {
			d.queue(c)
		}
	case syncBackupTab:
		d.backupPage(c)
	default:
		d.repository(c)
	}
}

func (d *desktop) queue(c *ui.Context) {
	state := &d.syncState
	previous := state.filter
	ui.Row(c).FillWidth().Gap(sp2).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			section(c, "Publication queue")
			muted(c, "Waiting means pending with neither a conflict nor a blocked-only flag. Each stable record ID appears once.")
		})
		ui.Segmented(c.Key("queue-filter"), &state.filter, "All", "Conflicts", "Blocked", "Waiting").Label("Queue filter")
	})
	if previous != state.filter {
		filter := state.filter
		state.filter = previous
		state.setFilter(queueFilter(filter))
	}
	if d.route.ProjectID != "" {
		name := d.route.ProjectID
		for _, row := range d.data.Queue {
			if row.ProjectID == d.route.ProjectID {
				name = row.Project
				break
			}
		}
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			scopeBar(c, []scopeItem{{label: "Project", value: name, id: d.route.ProjectID, icon: icFolder}}, nil).Grow(1)
			ui.Button(c, "Clear Project filter").OnClick(func() {
				d.route.ProjectID = ""
				state.list, state.selected = ui.ListState{}, 0
			})
		})
	}
	rows := d.filteredQueue()
	restore := state.restoreRecord
	state.restoreRecord = ""
	if len(rows) == 0 {
		card(c).Children(func() {
			cardHeader(c, icInbox, "No records in this view", "", nil)
			switch {
			case len(d.data.Queue) == 0:
				muted(c, "The Workspace queue is empty: there are no waiting, conflicted or blocked records.")
			case state.filter == int(queueConflicts):
				muted(c, "No active conflicts match this filter. The last resolution receipt remains separate from publication evidence.")
			default:
				muted(c, "No records match this filter. Choose All to inspect the complete local queue.")
			}
			if state.filter != int(queueAll) {
				ui.Button(c, "Show all records").AlignSelf(ui.Start).OnClick(func() { state.setFilter(queueAll) })
			}
		})
		return
	}
	state.list.Selected = &state.selected
	if restore != "" {
		for i, row := range rows {
			if row.ID == restore {
				state.selected = i
				break
			}
		}
	}
	state.list.RowHeight = 34
	state.list.Key = func(row int) any { return rows[row].ID }
	state.list.Label = func(row int) string { return recordName(rows[row]) }
	managementColumns(c, func() {
		card(c).Padding(0).Clip().Children(func() {
			columns := []ui.TableColumn{{Title: "Record"}, {Title: "Project", Width: 130}, {Title: "State", Width: 92}}
			ui.Table(c.Key("sync-queue"), &state.list, columns, len(rows), func(row, col int) {
				r := rows[row]
				ui.Text(c, []string{recordName(r), valueOr(r.Project, "—"), recordState(r)}[col]).NoWrap()
			}).Label("Sync record queue").Grow(1).Height(tableHeight(len(rows)))
			listFooter(c, fmt.Sprintf("Showing %d of %d · stable record IDs", len(rows), len(rows)), nil)
		})
		if restore != "" {
			state.list.Focus(c)
		}
	}, func() {
		if state.selected < 0 || state.selected >= len(rows) {
			card(c).Children(func() { muted(c, "Select a record to inspect its local evidence.") })
			return
		}
		d.queueInspector(c, rows[state.selected])
	})
}

func (d *desktop) filteredQueue() []app.DesktopQueueRecord {
	var rows []app.DesktopQueueRecord
	for _, r := range d.data.Queue {
		if d.route.ProjectID != "" && r.ProjectID != d.route.ProjectID {
			continue
		}
		switch queueFilter(d.syncState.filter) {
		case queuePending:
			if !r.Waiting() {
				continue
			}
		case queueConflicts:
			if !r.Conflict {
				continue
			}
		case queueBlocked:
			if !r.Blocked || r.Conflict {
				continue
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func (d *desktop) queueInspector(c *ui.Context, r app.DesktopQueueRecord) {
	t := tonePending
	if r.Conflict {
		t = toneConflict
	} else if r.Blocked {
		t = toneBlocked
	}
	card(c).Children(func() {
		cardHeader(c, t.icon(), recordName(r), valueOr(r.Kind, "Shared record"), func() { pill(c, t, recordState(r)) })
		kvGrid(c, []kvRow{
			{k: "Record ID", v: r.ID, mono: true},
			{k: "Project", v: valueOr(r.Project, "Unknown or unavailable locally")},
			{k: "Project ID", v: valueOr(r.ProjectID, "—"), mono: true},
		})
		if r.Revision != "" {
			detail(c, "Local revision", r.Revision)
			if r.Revision == d.resolvedRevision {
				pill(c, tonePending, "New local revision · waiting")
			}
		}
		if r.Blocked {
			for _, blocked := range d.data.Sync.Blocked {
				if blocked.ID == r.ID {
					detail(c, "Blocking reason", blocked.Reason)
				}
			}
		}
		if r.Waiting() {
			evidenceList(c, evidence{
				written: "Saved locally", writtenDetail: valueOr(r.Revision, "Revision evidence unavailable"),
				read: "Read " + d.data.ReadAt.Local().Format("15:04:05"), readDetail: "Still in Waiting after the latest local reread",
				published: "Not yet", pubDetail: valueOr(d.waitingHold(), "Queued for the running service"), pubTone: tonePending,
			})
			d.commandBlock(c, desktopCommand(d.core.DataDir, "sync"), false)
			d.afterRun(c, "After sync finishes, Refresh rereads the confirmed commit, journal and queue.")
		}
		if r.Conflict {
			ui.PrimaryButton(c, "Open comparison").OnClick(func() {
				d.navigate(desktopRoute{Page: pageConflict, ProjectID: r.ProjectID, RecordID: r.ID})
			})
		} else if r.Blocked {
			d.commandBlock(c, desktopCommand(d.core.DataDir, "sync", "status"), true)
			muted(c, "Repair the named definition or dependency in the CLI, run sync, then Refresh to see whether this record can publish.")
			if r.ProjectID != "" {
				ui.Button(c, "Open Project").AlignSelf(ui.Start).OnClick(func() {
					d.navigate(desktopRoute{Page: pageProjects, ProjectID: r.ProjectID})
				})
			}
		}
	})
}

func tableHeight(rows int) float32 { return float32(min(rows, 8)*35 + 35) }

func recordName(r app.DesktopQueueRecord) string { return valueOr(r.Title, r.ID) }
func recordState(r app.DesktopQueueRecord) string {
	switch {
	case r.Conflict:
		return "Conflict"
	case r.Blocked:
		return "Blocked"
	default:
		return "Waiting"
	}
}

func syncState(state string) string {
	return valueOr(map[string]string{
		"enabled": "Waiting for first publication", "pending": "Local changes waiting", "synced": "Confirmed version available",
		"conflict": "Unresolved conflicts", "offline": "Remote unavailable during the last sync", "auth-required": "Authentication required",
		"unlock-required": "Credential helper unlock required", "history-rewritten": "Remote history rewritten",
		"incomplete": "Sync data incomplete", "unsupported-schema": "Remote format unsupported",
		"privacy-unconfirmed": "Repository privacy unconfirmed", "missing-branch": "Remote branch missing",
	}[state], "Local sync records")
}

func syncProblem(outcome string) string {
	return map[string]string{
		"offline":             "The remote was unreachable during the last sync. Local records and the last confirmed version remain inspectable.",
		"auth-required":       "The last sync requires Git or GitHub authentication. Update the local login, then check the remote again.",
		"unlock-required":     "The last sync requires an unlocked local credential helper or SSH agent.",
		"history-rewritten":   "Remote history was rewritten and safe synchronization is paused. Run ctxferry sync status for recovery.",
		"incomplete":          "The last sync found incomplete remote data. Check the remote and compare retained local records.",
		"unsupported-schema":  "The remote uses a format this version cannot read. Update CtxFerry.",
		"privacy-unconfirmed": "The remote could not be confirmed private. Review the repository before any publication.",
		"missing-branch":      "The configured remote branch was not found during the last sync.",
	}[outcome]
}

func (d *desktop) repository(c *ui.Context) {
	s, state := d.data.Sync, &d.syncState
	if d.data.SyncErr != nil {
		return
	}
	if d.publicationUnknown() {
		d.publicationRecovery(c)
	}
	switch {
	case s.Config == nil:
		card(c).Children(func() {
			cardHeader(c, icCloudOff, "No private repository configured", "Local work is fully available without a remote.", nil)
			kvGrid(c, []kvRow{{k: "Remote", v: "Not configured"}, {k: "Confirmed commit", v: "None"}, {k: "Sync service", v: d.serviceLabel()}})
		})
		d.recoveryCommand(c, "Connect a private repository", "Replace the example with a dedicated private GitHub repository. Privacy must be verified before publication.", desktopCommand(d.core.DataDir, "sync", "enable", "--remote", "git@github.com:OWNER/REPO.git"))
	default:
		card(c).Children(func() {
			cardHeader(c, icDatabase, "Sync repository", syncState(s.State), func() {
				ui.Button(c, "Check remote").Disabled(d.err != nil || d.loading || state.checking).OnClick(func() { d.remoteButton(c) })
			})
			kvGrid(c, []kvRow{
				{k: "Remote", v: s.Config.Remote, mono: true},
				{k: "Branch", v: s.Config.Branch, mono: true},
				{k: "Confirmed head", v: valueOr(s.Head, "No confirmed publication"), mono: true},
				{k: "History size", v: d.historyLabel()},
				{k: "Sync service", v: d.serviceLabel()},
			})
			ui.Row(c).Gap(sp2).Children(func() {
				ui.Button(c, "Open in browser").Disabled(d.err != nil || d.loading).OnClick(func() {
					state.browserErr = mygo.Shell.OpenExternal(d.data.RepositoryURL) != nil
				})
				if state.browserErr {
					muted(c, "The system browser could not be opened.")
				}
			})
			d.remoteStatus(c)
		})
		if h := s.History; h != nil && h.Warning != "" {
			message := "Git history is large; previously published plaintext remains in history."
			if h.AutomaticPayloadPause {
				message = "The Git history limit paused automatic publication of new artifact payloads."
			}
			banner(c, toneAttention, "History size warning", message, nil)
		}
		if !d.publicationUnknown() {
			if s.Head == "" {
				d.firstPublication(c)
			} else {
				d.routineSync(c)
			}
		}
	}
}

func (d *desktop) historyLabel() string {
	if d.data.Sync.History == nil {
		return "Not measured"
	}
	return historySize(d.data.Sync.History.RawBytes)
}

func (d *desktop) firstPublication(c *ui.Context) {
	state := &d.syncState
	check := d.currentRemoteCheck()
	verified := check != nil && check.GitReachable && check.Privacy == "private"
	card(c).Children(func() {
		cardHeader(c, icUpload, "Review first publication", "A preview describes possible effects; only sync init publishes.", nil)
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			stepNumber(c, "1", map[bool]tone{true: toneOk, false: toneUnknown}[verified])
			body(c, "Check that the remote is reachable and private.").Grow(1)
			if !verified {
				muted(c, "Use Check remote above first")
			}
		})
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			stepNumber(c, "2", toneUnknown)
			body(c, "Generate and inspect the actual export manifest.").Grow(1)
			ui.Button(c, "Review export…").Disabled(!verified || state.previewing || d.err != nil).OnClick(d.reviewFirstPublication)
		})
		if state.previewing {
			muted(c, "Building the local export preview…")
		}
		if state.previewErr != "" {
			banner(c, toneBlocked, "Preview failed", state.previewErr, nil)
		}
		if state.firstPreview.PreviewID != "" {
			preview := state.firstPreview
			previewBadge(c)
			detail(c, "Preview ID", preview.PreviewID)
			detail(c, "Files", fmt.Sprintf("%d export files with paths, sizes and SHA-256 digests", len(preview.Files)))
			ui.Scroll(c).Height(132).FillWidth().Children(func() {
				ui.Column(c).Gap(sp2).Children(func() {
					for _, file := range preview.Files {
						ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
							mono(c, file.Path).Width(220).Shrink(0)
							muted(c, fmt.Sprintf("%d B", file.Bytes)).Width(76).Shrink(0)
							monoMuted(c, file.SHA256).Grow(1).Selectable()
						})
					}
				})
			})
			banner(c, toneAttention, "Plaintext review is separate", "Inspect original shared documents and artifact payloads for pasted secrets. A manifest hash does not make sensitive content safe to publish.", nil)
			strong(c, "Creates "+d.data.Sync.Config.Branch+" only if absent. Pushed history cannot be recalled.")
			d.commandBlock(c, desktopCommand(d.core.DataDir, "sync", "init", "--preview", preview.PreviewID), true)
			d.afterRun(c, "After it finishes, Refresh rereads the confirmed commit and publication journal before unlocking Backup or Conflict.")
		}
	})
}

func (d *desktop) reviewFirstPublication() {
	state := &d.syncState
	state.previewing, state.previewErr, state.firstPreview = true, "", app.SyncResult{}
	runDesktop(d, d.core.PreviewSync, func(result app.SyncResult, err error) {
		state.previewing = false
		if err != nil {
			state.previewErr = "The export was not generated. Reread the Workspace and retry (" + app.Classify(err).Code + ")."
			return
		}
		state.firstPreview = result
	})
}

func (d *desktop) routineSync(c *ui.Context) {
	card(c).Children(func() {
		cardHeader(c, icSync, "Sync via CLI", "Run one cycle for this exact data root. A running service normally processes eligible records automatically.", nil)
		muted(c, valueOr(d.waitingHold(), "The service is running. Waiting records remain queued until a confirmed commit includes them."))
		d.commandBlock(c, desktopCommand(d.core.DataDir, "sync"), true)
		d.afterRun(c, "After it finishes, Refresh rereads the confirmed commit, journal and queue.")
	})
}

func (d *desktop) publicationRecovery(c *ui.Context) {
	if d.data.Sync.Head == "" {
		preview := d.data.PublicationPreview
		card(c).Children(func() {
			cardHeader(c, icAlert, "Resume first publication", "The first push lost its acknowledgement. Reuse the original reviewed preview ID; a new ID cannot reconcile this journal.", func() {
				pill(c, toneAttention, "Outcome unknown")
			})
			detail(c, "Original preview ID", valueOr(preview, "Unavailable · inspect sync status"))
			if preview != "" {
				d.commandBlock(c, desktopCommand(d.core.DataDir, "sync", "init", "--preview", preview), true)
			} else {
				d.commandBlock(c, desktopCommand(d.core.DataDir, "sync", "status"), true)
			}
			d.afterRun(c, "After reconciliation, Refresh rereads the confirmed commit and journal before unlocking Backup or Conflict.")
		})
		return
	}
	card(c).Children(func() {
		cardHeader(c, icAlert, "Reconcile publication", "A routine push has an uncertain outcome. Local work is preserved; Backup and Conflict remain locked until the journal clears.", func() {
			pill(c, toneAttention, "Outcome unknown")
		})
		d.commandBlock(c, desktopCommand(d.core.DataDir, "sync"), true)
		d.afterRun(c, "After reconciliation, Refresh rereads the confirmed commit and clears the journal before another write.")
	})
}

func historySize(bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f MiB (%d B)", float64(bytes)/(1<<20), bytes)
}

func (d *desktop) remoteButton(c *ui.Context) {
	state := &d.syncState
	if state.checking {
		return
	}
	state.checking, state.remoteErr, state.remote = true, false, remoteObservation{}
	workspace := d.data.Workspace.ID
	runDesktop(d, func(ctx context.Context) (remoteObservation, error) {
		config, check, err := d.core.CheckDesktopRemote(ctx)
		return remoteObservation{config, check, time.Now()}, err
	}, func(result remoteObservation, err error) {
		if workspace != d.data.Workspace.ID {
			return
		}
		current := &d.syncState
		current.checking = false
		if err != nil {
			current.remoteErr = true
			return
		}
		current.remote = result
		current.firstPreview = app.SyncResult{}
	})
}

func (d *desktop) currentRemoteCheck() *app.SyncCheck {
	s := d.data.Sync
	remote := &d.syncState.remote
	if d.data.SyncErr != nil || s.Config == nil || remote.at.IsZero() || *s.Config != remote.config {
		return nil
	}
	return &remote.check
}

func (d *desktop) remoteStatus(c *ui.Context) {
	if d.syncState.remoteErr {
		banner(c, toneBlocked, "Remote check did not complete", "Run ctxferry sync check to inspect local Git configuration, then Refresh and retry.", nil)
	}
	check := d.currentRemoteCheck()
	if check == nil {
		detail(c, "Latest check", "Not checked in this window")
		return
	}
	git := "Remote read failed"
	if check.GitReachable {
		git = "Remote readable"
	}
	privacy := valueOr(map[string]string{"private": "private repository verified", "public": "public repository · publication blocked"}[check.Privacy], "privacy unconfirmed")
	detail(c, "Latest check", git+" · "+privacy+" · "+d.syncState.remote.at.Format("15:04:05"))
	if check.Failure != "" {
		message := valueOr(map[string]string{
			"offline": "The GitHub privacy check could not be reached.", "git-error": "Git read failed; check network and authentication.",
			"auth-required": "This check requires Git or GitHub authentication.", "unlock-required": "Unlock the Git credential helper or SSH agent.",
			"privacy-unconfirmed": "The remote did not pass the privacy check.",
		}[check.Failure], "The remote check did not pass completely.")
		banner(c, toneAttention, "Remote check", message, nil)
	}
}
