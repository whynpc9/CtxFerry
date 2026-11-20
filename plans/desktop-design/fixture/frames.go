package main

import "github.com/egoist/mygo/ui"

// allFrames is the ordered inventory, grouped by destination. Each frame
// names the snapshot (data.go) its shell and page are read from.
func allFrames() []frame {
	return []frame{
		{id: "01", slug: "readiness-create-or-join", title: "Workspace readiness: create or join", dest: "Shell gate",
			state: "Welcome; fresh empty data root; no catalog; --data-dir hint inside the Data root card; Create selected; capability icons, no check marks; Join requires Git access, not checked yet",
			view:  func(c *ui.Context) { shell(c, gateChrome(), func() { gatePage(c, false) }) }},
		{id: "02", slug: "readiness-join-handoff", title: "Workspace readiness: join handoff", dest: "Shell gate",
			state: "Join chosen; stage 1 of 2: join and subscribe by slug, Copy command primary, Refresh quiet; Bind waits for the Project ID",
			view:  func(c *ui.Context) { shell(c, gateChrome(), func() { gatePage(c, true) }) }},
		{id: "02b", slug: "readiness-join-bind", title: "Workspace readiness: bind after join", dest: "Shell gate",
			state: "snapJoined 10:43:31; join written 10:43:20; Refresh resolved atlas-api and billing-worker to Project IDs; bind names atlas-api by ID",
			view: func(c *ui.Context) {
				shell(c, chrome{crumbs: []string{"Workspace readiness"}, s: snapJoined}, func() { joinedBindPage(c) })
			}},
		{id: "03", slug: "home-stale-snapshot", title: "Home: refresh failed after a successful read", dest: "Home",
			state: "snapStale: 10:49:12 refresh failed; 10:46:20 snapshot kept; writes, Backup and remote checks off",
			view:  func(c *ui.Context) { shell(c, homeChrome(snapStale), func() { staleHomePage(c) }) }},
		{id: "04", slug: "home", title: "Home", dest: "Home",
			state: "snapNow 10:46:20: 6 waiting, 1 conflict, 2 blocked; tiles open Queue with that filter; Conflicts tile says keep a side or replace",
			view:  func(c *ui.Context) { shell(c, homeChrome(snapNow), func() { homePage(c, snapNow) }) }},
		{id: "05", slug: "projects-unsubscribed", title: "Projects: list with an unsubscribed Project", dest: "Projects",
			state: "snapNow; 4 Projects; grouped by subscription, then by name; ml-notebooks selected, not subscribed on this device; labelled Copy command, quiet Refresh",
			view:  func(c *ui.Context) { shell(c, projectsChrome(), func() { projectsListPage(c) }) }},
		{id: "06", slug: "project-overview", title: "Project overview", dest: "Projects",
			state: "snapNow; atlas-api; 2 Checkouts with different Profiles; ~/code/atlas-api selected; Task trust 1 trusted, 1 to recheck, 1 not trusted, read from the same data as 11",
			view:  func(c *ui.Context) { shell(c, projectsChrome("atlas-api"), func() { projectOverviewPage(c) }) }},
		{id: "07", slug: "configuration", title: "Configuration: layer inspection", dest: "Projects",
			state: "snapEdit 10:42:08; db.pool_size wins from Profile staging; showing 8 of 14 fields (with a value), Show all 14; Device and Checkout layers named as local to each device",
			view: func(c *ui.Context) {
				ch := projectsChrome("atlas-api", "Configuration")
				ch.s = snapEdit
				shell(c, ch, func() { configPage(c, 0) })
			}},
		{id: "08", slug: "configuration-preview", title: "Configuration: scope-aware preview", dest: "Projects",
			state: "snapEdit; remove override from shared Profile staging; other devices inherit 10 only after a confirmed commit and unless their own Device or Checkout layer sets it; 8 of 14 fields",
			view: func(c *ui.Context) {
				ch := projectsChrome("atlas-api", "Configuration")
				ch.s = snapEdit
				shell(c, ch, func() { configPage(c, 1) })
			}},
		{id: "09", slug: "configuration-receipt", title: "Configuration: local receipt, awaiting publication", dest: "Projects",
			state: "snapSaved 10:44:53; saved 10:44:52; record 2f9a6c14 waiting (5 → 6); evidence Written 10:44:52 / Read back 10:44:53 / Published not yet, queued for the running service",
			view: func(c *ui.Context) {
				ch := projectsChrome("atlas-api", "Configuration")
				ch.s = snapSaved
				shell(c, ch, func() { configPage(c, 2) })
			}},
		{id: "10", slug: "context", title: "Context", dest: "Projects",
			state: "snapNow; 23 documents under ~/code/atlas-api · staging; deploy.md open; conflict and missing-content rows",
			view:  func(c *ui.Context) { shell(c, projectsChrome("atlas-api", "Context"), func() { contextPage(c) }) }},
		{id: "11", slug: "tasks", title: "Tasks", dest: "Projects",
			state: "snapNow; same trust data as 06: deploy-preview Recheck, db-migrate Trusted, load-test Not trusted; slot cache unmapped; 3 credential requirements, cache-auth unmapped",
			view:  func(c *ui.Context) { shell(c, projectsChrome("atlas-api", "Tasks"), func() { tasksPage(c) }) }},
		{id: "12", slug: "catalog", title: "Catalog directory", dest: "Catalog",
			state: "snapNow; Project scope atlas-api (subscribed only); four tools with plain bullets",
			view:  func(c *ui.Context) { shell(c, catalogChrome(""), func() { catalogPage(c) }) }},
		{id: "13", slug: "resources-environments", title: "Resources & environments, opened from a Task", dest: "Catalog",
			state: "snapNow; from deploy-preview slot database; tunnel override set by Checkout ~/code/atlas-api, named at the probe; probe reachable",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("Task deploy-preview", "Resources & environments", resPG), func() {
					resourcesPage(c, scopeItem{label: "FROM TASK · SLOT", value: "deploy-preview · database", icon: icTasks})
				})
			}},
		{id: "14", slug: "credential-bindings", title: "Credential bindings, opened from a Task", dest: "Catalog",
			state: "snapNow; 3 requirements as on frame 11; db-password mapped; Resource match links postgres-primary (opens 14b); provider not signed in",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("Task deploy-preview", "Credential bindings", reqDBPass), func() { credentialsPage(c, reqDBPass) })
			}},
		{id: "14b", slug: "resources-from-credential", title: "Resources & environments, opened from a credential requirement", dest: "Catalog",
			state: "snapNow; opened from the postgres-primary link in db-password's Resource match on 14; Back returns to Credential bindings with db-password selected; same route and probe as 13",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("Credential "+reqDBPass, "Resources & environments", resPG), func() {
					resourcesPage(c, scopeItem{label: "FROM CREDENTIAL", value: reqDBPass + " · slot database", icon: icKey})
				})
			}},
		{id: "15", slug: "credential-cache-auth-slot", title: "Credential bindings: resolve cache-auth through its slot", dest: "Catalog",
			state: "snapNow; cache-auth unmapped because slot cache has no Resource; map-resource handoff with labelled Copy command and quiet Refresh, then reference",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("Task deploy-preview", "Credential bindings", "cache-auth"), func() { credentialsPage(c, "cache-auth") })
			}},
		{id: "16", slug: "skills-update-drift", title: "Skills: update blocked by local drift", dest: "Catalog",
			state: "snapNow; release-notes v1.3.2 → approved v1.4.0; templates/changelog.md drifted; confirm disabled",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("atlas-api", "Skills", skillName), func() { skillsPage(c) })
			}},
		{id: "17", slug: "run-artifacts", title: "Run artifacts: verified export and metadata-only item", dest: "Catalog",
			state: "snapNow; showing 1–5 of 12 (newest first, stated once above the table), Load 7 older; latency-report.json exported and verified; trace.zst metadata only",
			view: func(c *ui.Context) {
				shell(c, catalogChrome("atlas-api", "Run artifacts"), func() { artifactsPage(c) })
			}},
		{id: "18", slug: "repository-first-publication", title: "Repository: first publication", dest: "Sync",
			state: "snapSetup 3 Nov; sync service not installed (sidebar and card); no confirmed commit; preview reviewed; step 4 states that sync init creates ctxferry-v1 only if absent and pushed history cannot be recalled; Copy command primary, Refresh quiet",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapSetup, "", "Repository"), func() { repoFirstPublication(c) })
			}},
		{id: "19", slug: "repository-resume-first-publication", title: "Repository: resume first publication", dest: "Sync",
			state: "snapSetupLost; first push lost its acknowledgement; init journal pending; original preview ID reused; Copy command primary, Refresh quiet (was primary)",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapSetupLost, "", "Repository"), func() { repoResumeFirst(c) })
			}},
		{id: "20", slug: "repository-routine-sync", title: "Repository: routine sync via CLI", dest: "Sync",
			state: "snapMorning 09:02; service stopped; confirmed 5d07b3a; summary line: 3 waiting · Held until Sync runs; one-shot sync handoff with Copy command primary",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapMorning, "", "Repository"), func() { repoRoutine(c) })
			}},
		{id: "21", slug: "queue-all", title: "Queue: all records, from the Sync tab", dest: "Sync",
			state: "snapNow; All filter; 9 records; summary line: 1 conflict, 2 blocked, 6 waiting, no hold message (the running service publishes); 2f9a6c14 selected with Written / Read back / Published rows; no Back",
			view:  func(c *ui.Context) { shell(c, syncChrome(snapNow, "", "Queue"), func() { queueAll(c) }) }},
		{id: "22", slug: "queue-conflicts-from-home", title: "Queue: Conflicts filter, from Home", dest: "Sync",
			state: "snapNow; started from Home's Conflicts tile; Back to Home; conflict selected; summary line carries the counts; filter segments carry none",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapNow, "Home", "Queue", "Conflicts"), func() { queueConflicts(c) })
			}},
		{id: "23", slug: "queue-blocked-from-home", title: "Queue: Blocked filter, from Home", dest: "Sync",
			state: "snapNow; Back to Home; billing-worker Task e40d9a17 blocked by deleted membership a2c7e910; repair via task update; runbooks/ row shows the whole runbooks/rollback.md link; labelled Copy command, quiet Refresh, Open Task in billing-worker",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapNow, "Home", "Queue", "Blocked"), func() { queueBlocked(c) })
			}},
		{id: "24", slug: "conflict-resolution", title: "Conflict resolution", dest: "Sync",
			state: "snapNow; Back to Queue · Conflicts; description covers keeping a side or replacing the document; Keep local version chosen; remote option named Apply remote deletion; preview against conflict token; keyboard focus on choice",
			focus: 7,
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapNow, "Queue · Conflicts", "Queue", "Conflicts", "runbooks/rollback.md"), func() { conflictPage(c) })
			}},
		{id: "25", slug: "conflict-receipt-queue", title: "Conflict receipt in the restored Conflicts filter", dest: "Sync",
			state: "snapResolved 10:51:07; Conflicts filter restored and empty; badge 1 needs attention (1 blocked); Back to Home; receipt evidence Saved 10:51:06 / Reread 10:51:07 / Published not yet, queued for the running service",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapResolved, "Home", "Queue", "Conflicts"), func() { conflictReceiptQueue(c) })
			}},
		{id: "26", slug: "backup-blocked-offline", title: "Backup: blocked by an uncertain publication, offline", dest: "Sync",
			state: "snapOffline 11:06; routine journal 3d8f1a62 outcome unknown; Backup blocked; 8 waiting held until the uncertain journal is reconciled; recovery in Repository",
			view:  func(c *ui.Context) { shell(c, syncChrome(snapOffline, "", "Backup"), func() { backupBlocked(c) }) }},
		{id: "27", slug: "repository-reconcile-publication", title: "Repository: reconcile an uncertain routine push", dest: "Sync",
			state: "snapOnline 11:12:05; Back to Backup; journal pending; waiting held until reconciled; one-shot sync handoff with Copy command primary",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapOnline, "Backup", "Repository"), func() { repoReconcile(c) })
			}},
		{id: "28", slug: "repository-journal-cleared", title: "Repository: journal cleared after refresh", dest: "Sync",
			state: "snapCleared 11:12:41; journal cleared 11:12:40; commit 9a2e7f1 reread; summary line shows 1 blocked; Return to Backup is the primary action in the cleared banner; Back to Backup",
			view: func(c *ui.Context) {
				shell(c, syncChrome(snapCleared, "Backup", "Repository"), func() { repoCleared(c) })
			}},
		{id: "29", slug: "backup-review", title: "Backup: preflight evidence, scope and plaintext review", dest: "Sync",
			state: "snapCleared; reading order 1 source with preflight evidence, 2 destination, 3 data scope (always included with billing-worker / deploy-preview, revision 3; the one option, local-only data, selected; never included), 4 exposure for that selection, then acknowledgement and write after every option; summary line shows 1 blocked",
			view:  func(c *ui.Context) { shell(c, syncChrome(snapCleared, "", "Backup"), func() { backupReview(c) }) }},
		{id: "30", slug: "backup-receipt", title: "Backup: receipt with destination and manifest", dest: "Sync",
			state: "snapCleared; written 11:14:02 with local-only data; manifest path and catalog digest; never-included list as in 29; evidence Source snapshot read 11:12:41 / Written 11:14:02 / Published nothing; recovery guide carries the commands and flags",
			view:  func(c *ui.Context) { shell(c, syncChrome(snapCleared, "", "Backup"), func() { backupReceipt(c) }) }},
		// Compact window, 1024x680: the minimum supported size.
		{id: "31", slug: "compact-home", title: "Compact: Home", dest: "Home", w: compactW, h: compactH,
			state: "snapNow at 1024x680; icon rail with Sync badge 3; evidence strip in the top bar; tiles keep separate counts",
			view: func(c *ui.Context) {
				shell(c, compactChrome(homeChrome(snapNow)), func() { homeCompact(c) })
			}},
		{id: "32", slug: "compact-configuration-preview", title: "Compact: Configuration preview with pinned confirm", dest: "Projects", w: compactW, h: compactH,
			state: "snapEdit at 1024x680; same preview as 08; field strip labels effective value, origin (wins) and editing layer; 8 of 14; cross-device effect qualified by Device and Checkout layers; confirm pinned",
			view: func(c *ui.Context) {
				ch := compactChrome(projectsChrome("atlas-api", "Configuration"))
				ch.s, ch.actions = snapEdit, func() { configPreviewActions(c) }
				shell(c, ch, func() { configPreviewCompact(c) })
			}},
		{id: "33", slug: "compact-conflict-resolution", title: "Compact: Conflict resolution with pinned confirm", dest: "Sync", w: compactW, h: compactH,
			state: "snapNow at 1024x680; same description, revisions, lines, choices and effects as 24; sides behind a switch; token and confirm pinned",
			view: func(c *ui.Context) {
				ch := compactChrome(syncChrome(snapNow, "Queue · Conflicts", "Queue", "Conflicts", "runbooks/rollback.md"))
				ch.actions = func() { conflictActions(c) }
				shell(c, ch, func() { conflictCompact(c) })
			}},
		{id: "34", slug: "compact-backup-review", title: "Compact: Backup review with pinned write", dest: "Sync", w: compactW, h: compactH,
			state: "snapCleared at 1024x680; same components and reading order as 29; exposure in two groups; acknowledgement and write pinned after every option; the whole review fits above the pinned bar",
			view: func(c *ui.Context) {
				ch := compactChrome(syncChrome(snapCleared, "", "Backup"))
				ch.actions = func() { backupActions(c) }
				ch.pageExplainsAttention = true
				shell(c, ch, func() { backupCompact(c) })
			}},
	}
}
