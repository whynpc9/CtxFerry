package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func syncChrome(s snap, back string, crumbs ...string) chrome {
	return chrome{nav: "sync", back: back, crumbs: append([]string{"Sync"}, crumbs...), s: s}
}

// syncHeader is the Sync title and its three sections. The Queue count is
// the same snapshot total the sidebar and Home use.
func syncHeader(c *ui.Context, sel int, s snap) {
	label := "Queue"
	if s.confirmed != "" {
		label = fmt.Sprintf("Queue · %d", s.total())
	}
	// The title carries one calm summary line beside it: the counts that
	// matter, and why waiting records are held only when they are held.
	// Actions stay in the page's own flow, so the header never competes
	// with the journey below it.
	ui.Row(c).Gap(sp5).AlignItems(ui.End).Children(func() {
		title(c, "Sync").Shrink(0)
		if s.confirmed != "" {
			syncSummary(c, s).Margin(0, 0, 7, 0)
		}
	})
	ui.Tabs(c, &sel, "Repository", label, "Backup").Label("Sync sections")
}

// syncSummary states the snapshot in one line: a toned count per non-empty
// group, in Queue order, then a hold message when waiting records cannot
// publish on their own. With nothing to report it names the confirmed
// commit instead.
func syncSummary(c *ui.Context, s snap) ui.Element {
	return ui.Row(c).Gap(sp4).AlignItems(ui.Center).Label("Sync summary").Children(func() {
		count := func(icon *ui.SVG, col ui.Color, n int, one, many string) {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ico(c, icon, 14, col)
				ui.Text(c, fmt.Sprint(n)).FontSize(fsBody).FontWeight(700).TextColor(col)
				body(c, plural(n, one, many))
			})
		}
		if s.attention() == 0 && s.waiting == 0 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ico(c, icCheckCircle, 14, tk.Ok)
				body(c, "All published")
				muted(c, "· confirmed "+s.confirmed)
			})
			return
		}
		if s.conflicts > 0 {
			count(icMerge, tk.Conflict, s.conflicts, "conflict", "conflicts")
		}
		if s.blocked > 0 {
			count(icBan, tk.Blocked, s.blocked, "blocked", "blocked")
		}
		if s.waiting > 0 {
			count(icClock, tk.Pending, s.waiting, "waiting", "waiting")
		}
		if hold := s.waitingHold(); hold != "" && s.waiting > 0 {
			ui.Box(c).Size(1, 16).Background(tk.BorderStrong)
			muted(c, capFirst(hold)+".")
		}
	})
}

// syncCLI is the one-line form of the one-shot sync command, for tooltips.
var syncCLI = "ctxferry sync --data-dir " + dataRoot

// ---------- Repository ----------

func remoteCard(c *ui.Context, s snap, checked string) {
	card(c).Children(func() {
		cardHeader(c, icDatabase, "Private repository", "", nil)
		svcTone, _ := s.serviceText()
		rows := []kvRow{
			{k: "Remote", v: remoteURL, mono: true},
			{k: "Branch", v: branch, mono: true},
			{k: "Privacy", fn: func() { pill(c, toneOk, "Private"); muted(c, "checked "+checked) }},
			{k: "Reachability", fn: func() { pill(c, toneOk, "Reachable"); muted(c, "checked "+checked) }},
		}
		if s.confirmed == "" {
			rows = append(rows, kvRow{k: "Confirmed commit", fn: func() { pill(c, toneUnknown, "None yet") }})
		} else {
			rows = append(rows, kvRow{k: "Confirmed commit", fn: func() { mono(c, s.confirmed); subtle(c, "·"); body(c, s.confirmedAt) }})
		}
		rows = append(rows, kvRow{k: "Sync service", fn: func() {
			pill(c, svcTone, s.serviceLabel())
		}})
		kvGrid(c, rows)
		ui.Row(c).Children(func() { iconButton(c, icShield, "Check reachability & privacy", false) })
	})
}

func repoFirstPublication(c *ui.Context) {
	syncHeader(c, 0, snapSetup)
	ui.Grid(c).ColumnTracks(ui.Fr(0.85), ui.Fr(1.15)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Gap(sp3).Children(func() {
			remoteCard(c, snapSetup, "3 Nov 10:05")
			note(c, "Local work and Backup do not need a remote. Queue and conflicts start after the first confirmed commit.")
		})
		card(c).Gap(sp3).Children(func() {
			cardHeader(c, icUpload, "First publication", "Review exactly what leaves this device.", nil)
			pubStep(c, "1", toneOk, "Private repository configured", "Privacy check: private · 3 Nov 10:05", "")
			pubStep(c, "2", toneOk, "Export manifest reviewed", "Preview "+short(previewID)+" · 214 files · 3.8 MB", "")
			pubStep(c, "3", toneOk, "Plaintext sources inspected", "23 shared documents, 31 artifact payloads", "")
			pubStep(c, "4", toneAccent, "Publish the reviewed bytes", "Creates "+branch+" only if absent. Pushed history cannot be recalled.",
				cli("sync init", "--preview "+previewID))
			afterRun(c, "After it finishes, Refresh reads the confirmed commit before routine sync starts.", "")
		})
	})
}

func note(c *ui.Context, s string) ui.Element {
	return ui.Row(c).Gap(sp2).Padding(sp3).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Start).Children(func() {
		ico(c, icInfo, 14, tk.Muted).Margin(1, 0, 0, 0)
		muted(c, s).Grow(1)
	})
}

func pubStep(c *ui.Context, n string, t tone, label, detail, cmd string) {
	ui.Row(c).Gap(sp3).AlignItems(ui.Start).Children(func() {
		stepNumber(c, n, t)
		ui.Column(c).Grow(1).Gap(sp1).Children(func() {
			ui.Text(c, label).FontSize(fsBody).FontWeight(600)
			muted(c, detail)
			if cmd != "" {
				cmdBlock(c, cmd).Margin(sp1, 0, 0, 0)
			}
		})
	})
}

func repoResumeFirst(c *ui.Context) {
	syncHeader(c, 0, snapSetupLost)
	banner(c, toneAttention, "Outcome unknown: the first push lost its acknowledgement",
		"Started 3 Nov 10:12:40. The 10:13:40 reread found the initialization journal pending and no confirmed commit.", nil)
	ui.Grid(c).ColumnTracks(ui.Fr(1.2), ui.Fr(0.8)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icUndo, "Resume first publication", "Same data root, same reviewed preview.", nil)
			kvGrid(c, []kvRow{
				{k: "Pending journal", fn: func() { idChip(c, journalID); body(c, "initialization · opened 10:12:38") }},
				{k: "Original preview", fn: func() { idChip(c, previewID); body(c, "reviewed 3 Nov 10:09 · 214 files") }},
			})
			cmdBlock(c, cli("sync init", "--preview "+previewID))
			ui.Row(c).Gap(sp2).Padding(sp2, sp3).Radius(radiusControl).Background(tk.ConflictSoft).AlignItems(ui.Start).Children(func() {
				ico(c, icAlert, 14, tk.Conflict).Margin(1, 0, 0, 0)
				body(c, "Reuse this preview ID. A new preview cannot reconcile the pending journal.").TextColor(tk.Conflict)
			})
			afterRun(c, "After it finishes, Refresh reads the confirmed commit and the journal.", "")
		})
		card(c).Gap(sp2).Children(func() {
			h2(c, "Locked until the journal clears")
			lockRow(c, icDrive, "Backup", "Could capture an ambiguous state")
			lockRow(c, icMerge, "Conflict resolution", "Needs a confirmed commit")
			lockRow(c, icUpload, "Routine sync", "Starts after the first commit")
			ui.Divider(c)
			muted(c, "Projects, Catalog and all local work stay available.")
		})
	})
}

func lockRow(c *ui.Context, icon *ui.SVG, label, why string) {
	ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
		ico(c, icon, 15, tk.Muted)
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			strong(c, label)
			muted(c, why)
		})
		ico(c, icLock, 14, tk.Conflict).Label("Locked")
	})
}

func unlockRow(c *ui.Context, icon *ui.SVG, label, why string) {
	ui.Row(c).Gap(sp3).Padding(sp2, 0).AlignItems(ui.Center).Children(func() {
		ico(c, icon, 15, tk.Muted)
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			strong(c, label)
			muted(c, why)
		})
		pill(c, toneOk, "Available")
	})
}

func repoRoutine(c *ui.Context) {
	s := snapMorning
	syncHeader(c, 0, s)
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		// The status block above already carries the Queue counts and why
		// records wait; this page keeps only the remote and the handoff.
		remoteCard(c, s, "09:01")
		cliHandoff(c, handoff{
			title: "Sync via CLI",
			why:   "The service is stopped. This runs one sync cycle for this data root.",
			prereqs: []prereq{
				{toneOk, "Private remote · " + branch},
				{toneOk, "Reachable · checked 09:01"},
				{toneUnknown, "Git credentials · the command checks them"},
			},
			commands: []string{cli("sync")},
			after:    "After it finishes, Refresh reads the confirmed commit, journal and Queue.",
		})
	})
}

// ---------- Queue ----------

type rec struct {
	t       tone
	state   string
	kind    string
	name    string
	project string
	id      string
	rev     string
}

var (
	recConflict = rec{toneConflict, "Conflict", "Context document", "runbooks/rollback.md", "atlas-api", "b7e05c3a-9f21-4d86-a1c4-63e8d0f2b597", "local r19 · remote r20 deleted"}
	recUnique   = rec{toneBlocked, "Blocked", "Task definition", "deploy-preview", "billing-worker", "e40d9a17-5c6b-4f38-92e0-b1a7c35f8d26", "local r3 · not published"}
	recDepend   = rec{toneBlocked, "Blocked", "Context index", "runbooks/", "atlas-api", "58c1f2e9-0a7d-4b63-8e45-d9b2c6071af3", "waits on b7e05c3a"}
	recSaved    = rec{tonePending, "Waiting", "Configuration", "Profile staging", "atlas-api", pendingRecID, "local r8"}
	// waiting holds the six Waiting records of snapNow, newest first.
	waiting = []rec{
		recSaved,
		{tonePending, "Waiting", "Context document", "api/rate-limits.md", "atlas-api", "0d4e7a28-61bc-4f95-a3d0-e72b8c5f1964", "local r6"},
		{tonePending, "Waiting", "Skill pin", "code-review", "atlas-api", "c92f3b06-7e15-4a8d-b6c9-4d01e85a27f3", "local r2"},
		{tonePending, "Waiting", "Credential mapping", "registry-token", "atlas-api", "7a3e91d5-2c08-4b74-9f6a-15d8c3e0b42a", "local r4"},
		{tonePending, "Waiting", "Task definition", "invoice-retry", "billing-worker", "f61b0d84-3a97-4e2c-8d15-a0c7e94b3f68", "local r1"},
		{tonePending, "Waiting", "Context document", "onboarding/local-setup.md", "atlas-api", "93c0a5e7-1d64-4b28-af09-6e7b2d18c435", "local r3"},
	}
)

// recRow is one queue record on a single line: state, name, kind, Project
// and short ID. Revisions and reasons live in the detail panel, so all
// records of a page fit without truncating any group.
func recRow(c *ui.Context, r rec, sel bool) { recRowK(c, r, sel, true) }

// recRowK omits the kind tag in narrow lists; the detail panel names it.
func recRowK(c *ui.Context, r rec, sel, kind bool) {
	listRow(c, sel, func() {
		strongC, _ := r.t.colors()
		ico(c, r.t.icon(), 15, strongC).Label(r.state)
		cut(ui.Text(c, r.name).FontSize(fsBody).FontWeight(560)).Shrink(1)
		if kind {
			cut(subtle(c, r.kind))
		}
		ui.Spacer(c)
		cut(subtle(c, r.project))
		idChip(c, r.id)
	}).Padding(7, sp3)
}

func groupLabel(c *ui.Context, t tone, label string) {
	strongC, _ := t.colors()
	ui.Row(c).Padding(sp2, sp3, sp1, sp3).Gap(sp2).AlignItems(ui.Center).Children(func() {
		dot(c, strongC)
		overline(c, label)
	})
}

// queueFilter selects a group. Its counts are in the summary line above,
// so the segments carry names only, in the same order. The origin note
// says where the filter came from, so Back and the restored filter can be
// traced.
func queueFilter(c *ui.Context, s snap, sel int, origin func()) {
	ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &sel, "All", "Conflicts", "Blocked", "Waiting").Label("Queue filter")
		ui.Row(c).Grow(1).Gap(sp2).AlignItems(ui.Center).Children(origin)
	})
}

// queueAll is the Queue opened from the Sync tab: every group, with the
// record saved in frame 09 selected.
func queueAll(c *ui.Context) {
	s := snapNow
	syncHeader(c, 1, s)
	queueFilter(c, s, 0, func() { muted(c, "Each record is counted once: conflict, then blocked, then waiting.") })
	ui.Grid(c).ColumnTracks(ui.Fr(1.1), ui.Fr(0.9)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp1).Gap(0).Children(func() {
			groupLabel(c, toneConflict, fmt.Sprintf("CONFLICTS · %d", s.conflicts))
			recRow(c, recConflict, false)
			groupLabel(c, toneBlocked, fmt.Sprintf("BLOCKED · %d", s.blocked))
			recRow(c, recUnique, false)
			recRow(c, recDepend, false)
			groupLabel(c, tonePending, fmt.Sprintf("WAITING · %d", s.waiting))
			for i, r := range waiting {
				recRow(c, r, i == 0)
			}
			queuePager(c, s.total())
		})
		ui.Column(c).Gap(sp3).Children(func() {
			card(c).Children(func() {
				ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Profile staging").FontSize(fsSection).FontWeight(600).Grow(1)
					pill(c, tonePending, "Waiting")
				})
				kvGrid(c, []kvRow{
					{k: "Record", fn: func() { idChip(c, recSaved.id); tag(c, recSaved.kind) }},
					{k: "Project", v: "atlas-api"},
					{k: "Change", v: "Removed db.pool_size"},
				})
				evidenceList(c, evidence{
					written: "Saved 10:44:52", writtenDetail: "Revision 8 · studio-linux",
					read: "Read " + s.read, readDetail: "Still in Waiting",
					published: "Not yet", pubDetail: capFirst(waitingHoldOr(s, "queued for the running service")),
					pubTone: tonePending,
				})
				ui.Row(c).Gap(sp2).Children(func() {
					ui.Spacer(c)
					iconButton(c, icSliders, "Open in Configuration", false)
				})
			})
			ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					strong(c, "Sync service running")
					muted(c, "A record counts as published only when a confirmed commit includes it. Last confirmed: "+s.confirmed+" at "+s.confirmedAt+".")
				})
				iconButton(c, icTerminal, "Run sync now…", false).Tooltip(syncCLI)
			})
		})
	})
}

// queueConflicts is the start of the conflict journey: Home's Conflicts
// tile opens the Queue with the Conflicts filter. Back returns to Home.
func queueConflicts(c *ui.Context) {
	s := snapNow
	syncHeader(c, 1, s)
	queueFilter(c, s, 1, func() {
		muted(c, "Filter from Home · Conflicts").Grow(1)
		linkButton(c, "Show all", nil)
	})
	ui.Grid(c).ColumnTracks(ui.Fr(0.85), ui.Fr(1.15)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp1).Gap(0).Children(func() {
			groupLabel(c, toneConflict, fmt.Sprintf("CONFLICTS · %d", s.conflicts))
			recRowK(c, recConflict, true, false)
			ui.Box(c).Padding(sp2, sp3).Children(func() {
				muted(c, "1 blocked record waits on this conflict. It is listed under Blocked.")
			})
			queuePager(c, s.conflicts)
		})
		card(c).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, recConflict.name).FontSize(fsSection).FontWeight(600).Grow(1)
				pill(c, toneConflict, "Conflict")
			})
			kvGrid(c, []kvRow{
				{k: "Record", fn: func() { idChip(c, recConflict.id); tag(c, recConflict.kind) }},
				{k: "Project", v: "atlas-api"},
				{k: "Local", v: "revision 19 · edited on studio-linux today 10:02"},
				{k: "Remote", v: "revision 20 · deleted on travel-mac yesterday 18:40"},
				{k: "Dependent", fn: func() { mono(c, "runbooks/"); body(c, "index · blocked until resolved") }},
			})
			ui.Row(c).Gap(sp2).Children(func() {
				ui.Spacer(c)
				iconButton(c, icMerge, "Open comparison", true)
			})
		})
	})
}

// The billing-worker block is a reason the sync planner really produces:
// invalid-task-dependency (internal/app/task_profile.go). The Task points at
// a Repository membership that another device deleted. The repair is the
// verified CLI form "task update <name-or-uuid> --membership --if-revision".
const (
	deletedMembership = "a2c7e910-4b5d-4f83-96e1-3d0f8b72c5a4"
	billingMembership = "5c1e8f04-9b2a-4d17-8e63-c40f7a1d92b5"
)

// dependentRow is a blocked record whose blocker is another record. The link
// to the blocker sits inside the row it explains.
func dependentRow(c *ui.Context, r rec, blocker rec) {
	ui.Column(c).Gap(2).Padding(7, sp3).Children(func() {
		ui.Row(c).Gap(sp3).AlignItems(ui.Center).Children(func() {
			strongC, _ := r.t.colors()
			ico(c, r.t.icon(), 15, strongC).Label(r.state)
			cut(ui.Text(c, r.name).FontSize(fsBody).FontWeight(560)).Shrink(1)
			ui.Spacer(c)
			cut(subtle(c, r.project))
			idChip(c, r.id)
		})
		// The blocker's full path is the link itself, on its own line, so
		// it is never cut by the row's width.
		ui.Column(c).Gap(2).Padding(0, 0, 0, 27).AlignItems(ui.Start).Children(func() {
			muted(c, "Waits on the conflict on")
			ui.ButtonBase(c).Gap(4).Padding(2, 0).Cursor(ui.CursorPointer).Label("Open the conflict on " + blocker.name + " that blocks " + r.name).Children(func() {
				ui.Text(c, blocker.name).Font(fontMono).FontSize(fsMono).FontWeight(560).TextColor(tk.Accent)
				ico(c, icArrowR, 13, tk.Accent)
			})
		})
	})
}

// recordCell names one side of a blocking relation with its full identity.
func recordCell(c *ui.Context, label, kind, name, id, state string, t tone) {
	ui.Column(c).Grow(1).Basis(0).Gap(3).Padding(sp2+2, sp3).Children(func() {
		overline(c, label)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			strong(c, name)
			idChip(c, id)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			col, _ := t.colors()
			ico(c, t.icon(), 12, col)
			muted(c, kind+" · "+state)
		})
	})
}

func queueBlocked(c *ui.Context) {
	s := snapNow
	syncHeader(c, 1, s)
	queueFilter(c, s, 2, func() {
		muted(c, "Filter from Home · Blocked").Grow(1)
		linkButton(c, "Show all", nil)
	})
	ui.Grid(c).ColumnTracks(ui.Fr(0.85), ui.Fr(1.15)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Padding(sp1).Gap(0).Children(func() {
			groupLabel(c, toneBlocked, fmt.Sprintf("BLOCKED · %d", s.blocked))
			recRowK(c, recUnique, true, false)
			dependentRow(c, recDepend, recConflict)
			queuePager(c, s.blocked)
		})
		card(c).Children(func() {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "deploy-preview").Font(fontMono).FontSize(fsSection).FontWeight(600)
				subtle(c, "Task in billing-worker")
				ui.Spacer(c)
				pill(c, toneBlocked, "Blocked")
			})
			ui.Column(c).Gap(sp1).Padding(sp3).Radius(radiusControl).Background(tk.BlockedSoft).Children(func() {
				ui.Text(c, "Its Repository membership was deleted").FontSize(fsBody).FontWeight(620).TextColor(tk.Blocked)
				body(c, "travel-mac deleted the membership this Task runs in. A Task publishes only against a live membership of its own Project.")
			})
			ui.Row(c).Gap(0).Radius(radiusControl).Border(1, tk.Border).Dividers(1, tk.Border).AlignItems(ui.Stretch).Children(func() {
				recordCell(c, "BLOCKED RECORD", "Task · local revision 3", "deploy-preview", recUnique.id, "not published", toneBlocked)
				recordCell(c, "MISSING DEPENDENCY", "Membership · revision 5", "billing-worker", deletedMembership, "deleted", toneBlocked)
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Reason").AlignSelf(ui.Start).Shrink(0)
				mono(c, "invalid-task-dependency:"+deletedMembership).FontSize(fsMono - 1).TextColor(tk.Muted).Grow(1)
			})
			ui.Divider(c)
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icTasks, 15, tk.Accent)
				strong(c, "Repair task "+short(recUnique.id)+" in billing-worker").Grow(1)
				linkButton(c, "Open Task in billing-worker", icArrowR)
			})
			cmdBlock(c, cli("task update "+recUnique.id, "--membership "+billingMembership, "--if-revision 3"))
			muted(c, "Sets the current billing-worker membership, "+short(billingMembership)+". Execution change: each device re-trusts deploy-preview before running it with credentials.")
			afterRun(c, "After it finishes, Refresh reads the Queue.", "")
		})
	})
}

// ---------- Conflict ----------

// The conflict on runbooks/rollback.md, read once and rendered verbatim by
// the full-width and compact layouts: same revisions, same lines, same
// choices, same effects and the same token.
type conflictSide struct {
	label, device string
	rev           int
	when          string
	deleted       bool
}

const (
	conflictSub     = "Keep one whole side, or replace the document with new content. The result is saved locally, then queued for publication."
	conflictHeading = "Keep local version of runbooks/rollback.md"
)

var (
	conflictLocal  = conflictSide{"LOCAL · THIS DEVICE", deviceName, 19, "Edited today 10:02", false}
	conflictRemote = conflictSide{"REMOTE", otherDev, 20, "Deleted yesterday 18:40", true}
	conflictLines  = []string{
		"# Rolling back atlas-api",
		"1. Pause deploy-preview in staging.",
		"2. Restore the previous image tag.",
		"3. Run db-migrate --down 1 if the schema moved.",
	}
	conflictChoices = []struct{ v, label, detail string }{
		{"local", "Keep local version", "Restores the document with this device's revision 19."},
		{"remote", "Apply remote deletion", "Deletes the document on every device once published."},
		{"replace", "Replace document…", "Writes new content. Needs a non-deleted side."},
	}
	conflictEffects = []effect{
		{toneAccent, "Document", "Restored with local content (revision 19 text)"},
		{tonePending, "New local revision", "21, saved locally and queued for publication"},
		{tonePending, "Dependent runbooks/ index", "Unblocked and queued for publication"},
	}
	conflictConfirm = confirmSpec{tokenLabel: "conflict token", token: conflictTok, cancel: "Cancel", confirm: "Keep local version"}
)

func conflictScope(c *ui.Context) {
	scopeBar(c, []scopeItem{
		{label: "RECORD", value: recConflict.name, id: recConflict.id, icon: icFile},
		{label: "KIND", value: recConflict.kind},
		{label: "PROJECT", value: prjAtlas.name, id: prjAtlas.id, icon: icFolder},
	}, func() { muted(c, "Conflict token"); idChip(c, conflictTok) })
}

func conflictChoiceCard(c *ui.Context, choice *string) {
	card(c).Gap(sp2).Children(func() {
		h2(c, "Choose the result")
		ui.RadioGroup(c, func() {
			for _, o := range conflictChoices {
				choiceRow(c, choice, o.v, o.label, o.detail)
			}
		}).Label("Resolution")
	})
}

func conflictPage(c *ui.Context) {
	choice := "local"
	pageHeader(c, "Resolve conflict", conflictSub, nil)
	conflictScope(c)
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).Children(func() {
		sideCard(c, conflictLocal)
		sideCard(c, conflictRemote)
	})
	ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		conflictChoiceCard(c, &choice)
		previewPanel(c, conflictHeading, "", conflictEffects, &conflictConfirm, nil)
	})
}

func sideCard(c *ui.Context, sd conflictSide) {
	k := card(c).Gap(sp2)
	if sd.deleted {
		k.Stripes(tk.Text.Alpha(0.035), 6, 6, 45)
	}
	k.Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			overline(c, sd.label).Grow(1)
			if sd.deleted {
				pill(c, toneBlocked, "Deleted")
			} else {
				pill(c, toneAccent, "Edited")
			}
		})
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			strong(c, fmt.Sprintf("%s · revision %d", sd.device, sd.rev))
			muted(c, "· "+sd.when)
		})
		if sd.deleted {
			ui.Column(c).Grow(1).Center().Gap(sp1).Children(func() {
				ico(c, icTrash, 18, tk.Muted)
				muted(c, "Deleted on "+sd.device+". No content on this side.")
			})
			return
		}
		// Lines wrap rather than truncate, so every layout shows the same bytes.
		ui.Column(c).Gap(3).Padding(sp2, sp3).Radius(radiusControl).Background(tk.Sunken).Children(func() {
			for _, l := range conflictLines {
				mono(c, l).TextColor(tk.Muted).LineHeight(1.45)
			}
		})
	})
}

func choiceRow(c *ui.Context, sel *string, value, label, detail string) {
	r := ui.Row(c).Gap(sp3).Padding(sp2+2, sp3).Radius(radiusControl).Border(1, tk.Border).AlignItems(ui.Start)
	if *sel == value {
		r.Border(1.5, tk.Accent).Background(ui.Hex("#FAFDFE"))
	}
	r.Children(func() {
		ui.RadioBase(c, sel, value).Label(label).Children(func() {
			radioDot(c, *sel == value)
		}).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			strong(c, label)
			muted(c, detail)
		})
	})
}

func radioDot(c *ui.Context, on bool) {
	if on {
		ui.Box(c).Size(16, 16).Radius(8).Border(5, tk.Accent)
	} else {
		ui.Box(c).Size(16, 16).Radius(8).Border(1.5, tk.BorderStrong)
	}
}

// conflictReceiptQueue: after confirming, CtxFerry returns to the Queue it
// came from with the Conflicts filter restored; Back still leads to Home.
func conflictReceiptQueue(c *ui.Context) {
	s := snapResolved
	syncHeader(c, 1, s)
	receiptPanel(c, receipt{
		heading: "Kept local version of runbooks/rollback.md",
		rows: []kvRow{
			{k: "Record", fn: func() { idChip(c, recConflict.id); body(c, "Context document · atlas-api") }},
			{k: "Choice", fn: func() { body(c, "Keep local version · confirmed against token"); idChip(c, conflictTok) }},
			{k: "Result", v: "runbooks/ index unblocked"},
		},
		ev: evidence{
			written: "Saved 10:51:06", writtenDetail: "Local revision 21",
			read: "Reread " + s.read, readDetail: "Conflicts filter now empty",
			published: "Not yet", pubDetail: capFirst(waitingHoldOr(s, "queued for the running service")) + ". Last confirmed " + s.confirmed + ".",
			pubTone: tonePending,
		},
	})
	queueFilter(c, s, 1, func() {
		muted(c, "Filter from Home · Conflicts, restored").Grow(1)
		linkButton(c, "Show all", nil)
	})
	card(c).Gap(sp2).Padding(sp5).Children(func() {
		ui.Column(c).Center().Gap(sp2).Children(func() {
			ico(c, icCheckCircle, 22, tk.Ok)
			strong(c, "No conflicts")
			muted(c, fmt.Sprintf("Both records moved to Waiting, which now has %d.", s.waiting))
			ui.Row(c).Gap(sp2).Margin(sp1, 0, 0, 0).Children(func() {
				ui.Button(c, fmt.Sprintf("Show Waiting · %d", s.waiting))
			})
		})
	})
}

// ---------- Backup ----------

func backupBlocked(c *ui.Context) {
	s := snapOffline
	syncHeader(c, 2, s)
	banner(c, toneUnknown, "Offline · the private repository is not reachable",
		"Local work is preserved. Last confirmed commit "+s.confirmed+" at "+s.confirmedAt+".", nil)
	ui.Grid(c).ColumnTracks(ui.Fr(1.1), ui.Fr(0.9)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icDrive, "Backup is blocked", "", func() { pill(c, toneBlocked, "Blocked") })
			kvGrid(c, []kvRow{
				{k: "Journal", fn: func() { idChip(c, routineJrnlID); body(c, "routine sync · outcome unknown") }},
				{k: "Started", v: "Today 11:04:12 · 8 records · acknowledgement lost"},
				{k: "Why it blocks", v: "A backup now could capture a state that is neither published nor clearly local."},
				{k: "Not a blocker", v: "Waiting and blocked records."},
			})
			ui.Row(c).Gap(sp2).Children(func() {
				ui.Spacer(c)
				iconButton(c, icDatabase, "Open recovery in Repository", true)
			})
		})
		card(c).Gap(sp3).Children(func() {
			h2(c, "Before Backup accepts a write")
			pubStep(c, "1", toneNeutral, "Reconnect", "Recovery needs the remote.", "")
			pubStep(c, "2", toneNeutral, "Reconcile in Repository", "One-shot sync for this data root.", "")
			pubStep(c, "3", toneNeutral, "Refresh shows the evidence", "Journal cleared, fresh local read, confirmed commit reread.", "")
		})
	})
}

// repoReconcile: an uncertain routine push, reached from Backup's recovery link.
func repoReconcile(c *ui.Context) {
	s := snapOnline
	syncHeader(c, 0, s)
	banner(c, toneAttention, "Outcome unknown: a routine push lost its acknowledgement",
		"Started today 11:04:12. Confirmed commit is still "+s.confirmed+". Back online since 11:11.", nil)
	ui.Grid(c).ColumnTracks(ui.Fr(1.2), ui.Fr(0.8)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		cliHandoff(c, handoff{
			title: "Reconcile publication",
			why:   "One-shot sync. It checks the remote, then confirms or retries the pending push.",
			prereqs: []prereq{
				{toneOk, "Remote reachable · checked 11:12:05"},
				{toneAttention, "Pending journal " + short(routineJrnlID) + " · routine sync"},
			},
			commands: []string{cli("sync")},
			after:    "After it finishes, Refresh reads the data root, confirmed commit and journal.",
		})
		card(c).Gap(sp2).Children(func() {
			h2(c, "Waiting on this journal")
			lockRow(c, icDrive, "Backup", "You came from Backup")
			lockRow(c, icMerge, "Conflict resolution", "Needs the reconciled commit")
		})
	})
}

// repoCleared is the refresh after the CLI sync: the evidence that unlocks
// Backup, each item with its own time.
func repoCleared(c *ui.Context) {
	s := snapCleared
	syncHeader(c, 0, s)
	// Recovery was a detour from Backup, so the next step returns there.
	banner(c, toneOk, "Journal cleared · publication confirmed",
		"The refresh at "+s.read+" reread this data root and the remote. Backup can continue.",
		func() { iconButton(c, icArrowL, "Return to Backup", true) })
	ui.Grid(c).ColumnTracks(ui.Fr(1.2), ui.Fr(0.8)).GapX(sp4).AlignItems(ui.Start).Children(func() {
		card(c).Children(func() {
			cardHeader(c, icCheckCircle, "Evidence from the refresh", "", nil)
			kvGrid(c, []kvRow{
				{k: "Journal", fn: func() { idChip(c, routineJrnlID); pill(c, toneOk, "Cleared · 11:12:40") }},
				{k: "Confirmed commit", fn: func() { mono(c, s.confirmed); subtle(c, "·"); body(c, "reread from the remote "+s.read) }},
				{k: "Previous commit", fn: func() { mono(c, confirmedSHA); subtle(c, "·"); body(c, "10:31") }},
				{k: "Published", v: "8 records · Waiting is now 0"},
				{k: "Local read", v: "Today " + s.read + " · " + dataRoot},
			})
		})
		card(c).Gap(sp2).Children(func() {
			h2(c, "Unlocked")
			unlockRow(c, icDrive, "Backup", "Accepts a write from this read")
			unlockRow(c, icMerge, "Conflict resolution", "Compares against "+s.confirmed)
		})
	})
}

// ---------- Backup review ----------
//
// One set of components serves both widths, in one reading order:
// source (with the preflight evidence that allowed the write), destination,
// every data scope option, the exact exposure for that selection, then the
// acknowledgement and the write. The scope follows the CLI exactly:
// `workspace backup --to <new dir> [--include-local-only]` has one option.

const backupAckText = "I reviewed what this backup of " + wsName + " exposes"

// backupLocal is the single scope option, --include-local-only. The review
// frames show it selected, so the exposure lists what it adds.
const backupLocal = true

func passedLine(c *ui.Context, text string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ico(c, icCheckCircle, 13, tk.Ok).Label("Passed")
		ui.Text(c, text).FontSize(fsMeta).TextColor(tk.Muted)
	})
}

// backupRoute is source and destination side by side. The source carries
// the preflight evidence: the cleared journal and the fresh read the backup
// is taken from.
func backupRoute(c *ui.Context, s snap, dest *string) {
	ui.Row(c).Radius(radiusCard).Border(1, tk.Border).Background(tk.Surface).Dividers(1, tk.Border).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(4).Padding(sp3, sp4).Background(tk.Sunken).Radius(radiusCard, 0, 0, radiusCard).Children(func() {
			overline(c, "1 · FROM WORKSPACE")
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icDatabase, 14, tk.Muted)
				strong(c, wsName)
				idChip(c, wsID)
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				muted(c, "Data root").Shrink(0)
				cut(mono(c, dataRoot).TextColor(tk.Muted))
			})
			passedLine(c, "Journal cleared 11:12:40 · confirmed "+s.confirmed+" · read "+s.read)
		})
		ui.Column(c).Grow(1).Basis(0).Gap(sp2).Padding(sp3, sp4).Children(func() {
			overline(c, "2 · TO A NEW DIRECTORY")
			ui.Row(c).Gap(sp2).Children(func() {
				ui.TextInput(c, dest).Grow(1).Font(fontMono).FontSize(fsMono).Label("New backup directory")
				ui.Button(c, "Choose…")
			})
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				ico(c, icCheckCircle, 13, tk.Ok)
				ui.Text(c, "Does not exist yet. Existing directories are rejected.").FontSize(fsMeta).TextColor(tk.Ok)
			})
		})
	})
}

func scopeLine(c *ui.Context, icon *ui.SVG, col ui.Color, text string) {
	ui.Row(c).Gap(sp2).AlignItems(ui.Start).Children(func() {
		if icon == nil {
			dot(c, tk.Subtle).Size(5, 5).Radius(2.5).Margin(7, 2, 0, 2)
		} else {
			ico(c, icon, 13, col).Margin(2, 0, 0, 0)
		}
		ui.Text(c, text).FontSize(fsMeta).TextColor(tk.Text)
	})
}

// backupScope shows every scope decision in three columns: what the format
// always includes (with the blocked Task named by Project and revision),
// the one option, and what a backup never contains.
func backupScope(c *ui.Context) {
	local := backupLocal
	card(c).Gap(sp2).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			overline(c, "3 · DATA SCOPE")
			muted(c, "One option. The rest is fixed by the backup format.")
		})
		ui.Grid(c).ColumnTracks(ui.Fr(1.15), ui.Fr(1), ui.Fr(1)).GapX(sp4).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, "Always included").FontSize(fsMeta).FontWeight(650)
				scopeLine(c, icBan, tk.Blocked, "billing-worker / deploy-preview, revision 3 · blocked, kept as saved")
				scopeLine(c, nil, tk.Subtle, "Projects, memberships and shared records")
				scopeLine(c, nil, tk.Subtle, "Shared Configuration and Skill pins")
				scopeLine(c, nil, tk.Subtle, "Queue state and history to "+snapCleared.confirmed)
			})
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, "Optional").FontSize(fsMeta).FontWeight(650)
				ui.Checkbox(c, &local, "Local-only data on "+deviceName)
				ui.Text(c, "Checkout bindings and Profiles, Device and Checkout overrides, subscriptions, Resource routes, run artifacts with their bytes").
					FontSize(fsMeta).TextColor(tk.Muted).Padding(0, 0, 0, 26)
			})
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, "Never included").FontSize(fsMeta).FontWeight(650)
				scopeLine(c, icX, tk.Muted, "Secret values and provider sessions")
				scopeLine(c, icX, tk.Muted, "Git authentication")
				scopeLine(c, icX, tk.Muted, "Execution trust · rechecked per device")
				scopeLine(c, icX, tk.Muted, "Installed Skill files and receipts")
			})
		})
	})
}

// exposurePanel lists exactly what a reader of the new directory can read
// for the selected scope: the always-included content, then what the
// local-only option adds. It changes with the option.
func exposurePanel(c *ui.Context, cols int) {
	ui.Column(c).Gap(sp2).Padding(sp3, sp4).Radius(radiusCard).Background(tk.ConflictSoft).Border(1, tk.Conflict.Alpha(0.25)).Children(func() {
		ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
			ico(c, icEye, 15, tk.Conflict)
			ui.Text(c, "4 · Plaintext exposure").FontSize(fsBody).FontWeight(650).TextColor(tk.Conflict)
			body(c, "Not encrypted. Anyone who can read the new directory can read:")
		})
		item := func(l string) {
			ui.Row(c).Gap(sp2).AlignItems(ui.Center).Children(func() {
				dot(c, tk.Conflict).Size(6, 6).Radius(3)
				ui.Text(c, l).FontSize(fsMeta).TextColor(tk.Text)
			})
		}
		// Two groups, so the list mirrors the scope: what every backup
		// exposes, and what the selected local-only option adds.
		ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fr(float32(cols-1))).GapX(sp5).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(5).Children(func() {
				overline(c, "EVERY BACKUP")
				for _, l := range []string{"23 Context documents", "Configuration values for 3 Projects", "3 Skill pins and their sources"} {
					item(l)
				}
			})
			ui.Column(c).Gap(5).Children(func() {
				if !backupLocal {
					overline(c, "LOCAL-ONLY DATA · NOT SELECTED")
					return
				}
				overline(c, "LOCAL-ONLY DATA · SELECTED")
				ui.Grid(c).Columns(cols - 1).GapX(sp5).GapY(5).Children(func() {
					for _, l := range []string{"4 Checkout paths and their Profiles", "2 Device and Checkout overrides", "1 Resource route · 127.0.0.1:15432", "Run artifacts · 1.2 GB on this device"} {
						item(l)
					}
				})
			})
		})
	})
}

func backupWriteButtons(c *ui.Context) {
	ui.Button(c, "Cancel")
	iconButton(c, icDrive, "Write backup to new directory", true)
}

// backupConfirm is step 5, after every scope option and the exposure.
func backupConfirm(c *ui.Context) {
	ack := true
	ui.Checkbox(c, &ack, backupAckText)
	ui.Spacer(c)
	backupWriteButtons(c)
}

func backupReview(c *ui.Context) {
	s := snapCleared
	dest := backupDir
	syncHeader(c, 2, s)
	backupRoute(c, s, &dest)
	backupScope(c)
	exposurePanel(c, 3)
	ui.Row(c).Gap(sp2).Padding(sp3, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tk.Border).AlignItems(ui.Center).Children(func() {
		backupConfirm(c)
	})
}

func backupReceipt(c *ui.Context) {
	s := snapCleared
	syncHeader(c, 2, s)
	receiptPanel(c, receipt{
		heading: "Backup written",
		rows: []kvRow{
			{k: "Destination", v: backupDir, mono: true},
			{k: "Manifest", v: backupDir + "/manifest.json", mono: true},
			{k: "Workspace", fn: func() { body(c, wsName); idChip(c, wsID) }},
			{k: "Included", v: "Shared records and history to " + s.confirmed + ", local-only data on " + deviceName + " · 1.25 GB"},
			{k: "Not included", v: "Secret values, provider sessions, Git authentication, execution trust, installed Skill files"},
			{k: "Catalog SHA-256", v: "c81e0d4b2a7f95c3e16d08b4f7a29c5e3b1d86f0a4c72e9d5b38f1c06fa2903e", mono: true},
		},
		ev: evidence{
			source: "Read " + s.read, sourceDetail: "Confirmed " + s.confirmed + " · journal clear",
			written: "Written 11:14:02", writtenDetail: "Manifest last, with catalog and bundle SHA-256",
			published: "Nothing", pubDetail: "A backup does not publish or change the Queue",
			pubTone: toneNeutral,
		},
		actions: func() {
			iconButton(c, icFolder, "Show in folder", false)
			ui.Button(c, "Done")
		},
	})
	ui.Row(c).Gap(sp3).Padding(sp3).Radius(radiusCard).Background(tk.Sunken).Border(1, tk.Border).AlignItems(ui.Center).Children(func() {
		ico(c, icInfo, 14, tk.Muted)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			strong(c, "Recover from this backup with the CLI")
			muted(c, "Restore republishes it under a new epoch. Local recovery rebuilds a device in a new data directory. The guide gives each command with its required flags.")
		})
		linkButton(c, "Open recovery guide", icExternal)
	})
}

// queuePager closes every queue list: how many records are shown and the
// page controls. Pages hold 50 records; Previous and Next are disabled when
// everything fits on one page.
func queuePager(c *ui.Context, n int) {
	listFooter(c, fmt.Sprintf("Showing %d of %d · 50 per page", n, n), func() {
		ui.Button(c, "Previous").Disabled(true).Tooltip("First page")
		ui.Button(c, "Next").Disabled(true).Tooltip("Last page")
	}).Margin(sp1, 0, 0, 0)
}

// waitingHoldOr returns what holds waiting records, or running when nothing does.
func waitingHoldOr(s snap, running string) string {
	if h := s.waitingHold(); h != "" {
		return h
	}
	return running
}
