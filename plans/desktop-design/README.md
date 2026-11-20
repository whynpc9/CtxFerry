# CtxFerry desktop redesign: round 10 design proposal

**Designer:** Opus 5.5, acting as the design sub-agent. **Date:** 2026-11-20.
**Requirements:** `plans/desktop-information-architecture.md` in `whynpc9/CtxFerry` at `fb3d659`.
**Starting point:** the frozen round 9 snapshot (`../round-09-frozen`, unchanged; its `SHA256SUMS` and `manifest.json` still verify). This is the final scheduled round.
**Rendering:** pure MyGo Native UI (`github.com/egoist/mygo v0.10.0`, package `ui`). No HTML, CSS, browser, web view or rasterized web mockup was used.

Round 10 has **36** high-fidelity frames rendered at 2× from a standalone Go fixture with synthetic, internally consistent Workspace data:

- Frames 01–30, including **02b** and **14b**, are at 1280×800 DIP (2560×1600 PNG).
- Frames 31–34 show four workflows at the minimum compact window, 1024×680 DIP (2048×1360 PNG).
- Frame IDs are unchanged from round 9.
- 9 frames changed: 03, 04, 18, 24, 29, 30, 31, 33 and 34. The other 27 are byte-identical. The functional scope and every workflow are the same.

This round answers the round 9 findings:

- Backup now follows one reading order at both widths, and its data scope matches what the CtxFerry core actually backs up.
- The conflict description covers replacement.
- A final pass over first-time and irreversible actions tightened frame 18.

## Contents

| Path | What it is |
| --- | --- |
| `README.md` | This proposal: changes, inventory, traced journeys, rules, reproduction, limitations |
| `frames/NN-slug.png` | 36 frames, in inventory order (02b follows 02, 14b follows 14; 31–34 at the compact window size) |
| `frames/manifest.json` | Per-frame ID, file, title, destination, data state, pixel size and SHA-256 (written by the fixture) |
| `contact-sheets/sheet-{1,2,3,4}.png` | Nine frames per sheet, rendered natively from the frame PNGs |
| `fixture/` | Complete runnable Go fixture: `go.mod` and `go.sum` (pinned), sources, vendored fonts and their licences. No build output is shipped |
| `SHA256SUMS` | SHA-256 of every other file in this round |

## What changed from round 9

### 1. Backup scope follows the core, not the earlier mock-up (frames 29, 30, 34)

Before changing the layout, I checked which inclusions are configurable:

- The spec (`plans/desktop-information-architecture.md`, Backup row) asks for "optional local-only data and plaintext exposure".
- The core command is `workspace backup --to <new dir> [--include-local-only]`, defined in `internal/cli/workspace.go`.
- The scope itself is set by `copyBackupCatalog` and `BackupWorkspace` in `internal/app/workspace_backup.go`.

Earlier rounds showed four checkboxes (shared records, "local-only data · bindings, overrides, trust", "Skill installation receipts" and "Run artifact bytes"). Three of these did not match the core:

| Content | Core behaviour | Shown in round 10 as |
| --- | --- | --- |
| Workspace, Projects, repositories, memberships, shared records and their Context bodies, shared Configuration, Skill records (pins and approvals), Queue state (outbox, conflicts, blocked, tombstones) | Always copied | **Always included**, with the blocked Task named first |
| Published Git history to the confirmed head | Always bundled when a head exists (`published.bundle`) | **Always included** ("history to 9a2e7f1") |
| Device settings, local-only records, Checkout bindings, local Configuration overrides, Profile selections, Project subscriptions, Resource routes and observations, run artifact records and **their bytes** | Copied only with `--include-local-only` | **Optional**: one checkbox, "Local-only data on studio-linux", with its contents listed |
| Credential stores, provider sessions, Git authentication | Excluded (the `backupBoundary` text) | **Never included** |
| Execution trust | Excluded; stored device-locally and never exported | **Never included** · "rechecked per device" (round 9 wrongly listed trust under local-only data) |
| Skill installation receipts and installed Skill files | Not in the catalog (`skillDir()/installs`); the backup copies only the catalog, bundle and artifacts | **Never included** (round 9 wrongly offered them as an option) |
| Run artifact bytes | Not separately optional; copied with local-only data | Part of the optional local-only data |

### 2. One reading order at both widths (frames 29, 34)

Both layouts use the same components, numbered in the order a person decides:

1. **From Workspace:** Northwind Labs, its data root, and the preflight evidence on one line: "Journal cleared 11:12:40 · confirmed 9a2e7f1 · read 11:12:41". This replaces the separate preflight strip.
2. **To a new directory:** the path, Choose…, and the "does not exist yet" check.
3. **Data scope:** three columns, Always included · Optional · Never included, under "One option. The rest is fixed by the backup format." The first always-included line qualifies the blocked Task by Project and revision: "billing-worker / deploy-preview, revision 3 · blocked, kept as saved".
4. **Plaintext exposure:** "Not encrypted. Anyone who can read the new directory can read:", then exact counts in two groups that mirror the scope:
   - **Every backup:** 23 Context documents · Configuration values for 3 Projects · 3 Skill pins and their sources
   - **Local-only data · selected:** 4 Checkout paths and their Profiles · 2 Device and Checkout overrides · 1 Resource route · 127.0.0.1:15432 · Run artifacts · 1.2 GB on this device

   Clearing the option drops the second group, which then reads "Local-only data · not selected".
5. **Acknowledgement and write.**
   - **Wide (29):** a ruled row after the exposure, so the confirmation sits after every scope option and after the artifact bytes.
   - **Compact (34):** the pinned bar. Every step, 1 to 4, fits above the bar. `-check`, including the sliced-line rule from round 9, passes.

The receipt (30) now matches:

- **Included:** "Shared records and history to 9a2e7f1, local-only data on studio-linux · 1.25 GB".
- **Not included:** the never-included list.
- The receipt row is now "Catalog SHA-256", and WRITTEN reads "Manifest last, with catalog and bundle SHA-256". This matches the manifest's real `catalogSha256` and `bundleSha256` fields.

### 3. Conflict description covers replacement (frames 24, 33; Home tiles 03, 04, 31)

- **Resolve conflict (24, 33):** "Keep one whole side, or replace the document with new content. The result is saved locally, then queued for publication." It still fits at compact width.
- **Home Conflicts tile:** "Keep a side or replace the document."

### 4. Audit of first-time and irreversible actions

| Action | Frame | Consequence shown before the action |
| --- | --- | --- |
| Create Workspace | 01 | "Nothing is written until you confirm"; the Data root card shows the target |
| Join, then Bind | 02, 02b | Two stages; Bind names the resolved Project ID |
| First publication | 18 | Step 4 now says: "Creates ctxferry-v1 only if absent. Pushed history cannot be recalled." (from `sync init`: "create the sync branch with an expected-absent lease") |
| Shared Configuration removal | 08, 32 | Preview with the scope across devices; confirm names the layer and token |
| Conflict resolution, including Apply remote deletion | 24, 33 | Each option states its effect ("Deletes the document on every device once published."); confirm against the token |
| Backup write | 29, 34 | New directory only; exact exposure; acknowledgement names the Workspace |

The evidence vocabulary is consistent across 09, 21, 25 and 30: SOURCE SNAPSHOT, WRITTEN, READ BACK and PUBLISHED, with READ BACK only where the destination really was reread.

## Carried over: what changed from round 8 to round 9

#### 1. Frame 34: the scope evidence is high, and nothing is sliced

Both reviewers saw the pinned bar cut through "The 1 blocked record is included as a local revision". The cut was made by the page's scroll edge, just above the bar. Viewport checks could not see it, because the line was inside the window.

- **The evidence comes first in the scope.** `backupBlockedNote` is a tinted note directly under "Data scope", before the choices. It reads: "Includes the 1 blocked record, deploy-preview, as its local revision 3. Backup does not need it repaired." Frames 29 and 34 share the same component.
- **The compact layout fits above the bar.** The Backup card holds the route, beside the exposure panel, and both stretch to one height. Data scope is a full-width card below them, with the heading and note on the left and the four choices on the right. At 1024×680 the whole review now ends about 18 DIP above the pinned bar, with no scrolling. The bar sits outside the page `ui.Scroll`, so it never covers content; content that does not fit scrolls to its end above the bar.
- **A check for sliced lines.** The page scroll carries `Label("Page content")`. `-check` reads its visible bounds through `Tester.Access`. It fails on any text whose visible box, as `Tester.Find` reports it after clipping, ends on the page's bottom edge. That edge is the window, or the pinned bar. I ran it on a scratch copy of the frozen round 8 fixture, changed only by this check and the scroll label. It reports exactly the reviewers' defect: `34 compact-backup-review: "The 1 blocked record is included as a local revision." sliced by the page edge at 624`. Round 9 passes on all 36 frames.
- **Compact frames inspected at full resolution:** 31, 32, 33 and 34 were read at 2048×1360. No content is hidden under the bar or the scroll edge. The last element in 32 (the preview's dashed border) ends 8 DIP above the bar.

#### 2. One calm summary line replaces the three-cell status block (all Sync pages with a confirmed commit)

The round 8 block took header height and offered a "Next" that competed with each page's own journey. It is replaced by one line on the title row, aligned to the "Sync" title, so the header is only the title row and the tabs:

> **Sync**   ⑂ **1** conflict   ⊘ **2** blocked   ◷ **6** waiting

- **Counts:** one per non-empty group, in Queue order, each with its status icon and a toned count (Conflict, Blocked and Pending on the Page tint are all ≥ 6.2:1). With nothing to report, it reads "✓ All published · confirmed 9a2e7f1".
- **Hold message, only when useful:** after a hairline, the reason waiting records cannot publish on their own. Examples: "Held until the uncertain journal is reconciled." (26, 27), "Held until Sync runs." (20). When the running service publishes them, no message is shown. The Queue evidence still says "Queued for the running service".
- **No next action in the header.** Each page's flow carries its own next step: the Queue list and detail, the reconcile handoff, the Backup steps and form. The Queue filter segments keep their names only, and the counts stay in the summary line.

#### 3. Backup's journey comes first (frames 28–30, 34)

- **28 (journal cleared, reached from Backup):** the cleared banner now carries **Return to Backup** as its primary action, and its text ends "Backup can continue.". The Unlocked card no longer repeats the button. The "Still in Queue: 1 blocked record" row was removed.
- **29, 30, 34:** no Queue repair appears anywhere. The summary line shows "1 blocked" as a quiet fact. The scope note says the record is included and needs no repair. Round 8's "Next: Repair deploy-preview · Open blocked record" is gone with the status block.

#### 4. Frame 14 links the mapped Resource, and Back returns to the requirement (frames 14, new 14b)

- **14:** "Resource match" now reads "✓ Matches **postgres-primary →** in slot database". The link has the accessible name "Open Resource postgres-primary" and the tooltip "Opens the Resource; Back returns to db-password".
- **14b (new):** Resources & environments opened from that link. Back reads "Credential db-password" and restores Credential bindings with db-password selected. The scope line names the origin, "From credential · db-password · slot database". The route, probe and observations are those of frame 13, which keeps its Task origin.
- **13 and 14b:** "Used by" no longer wraps mid-name. It is split into "Used by: deploy-preview, db-migrate" and "As slot: database".

## Carried over: what changed from round 7 to round 8

#### 1. A Sync status block replaces the one-line legend (frames 20–23, 25–30, 34; superseded in round 9 by the summary line)

Both reviewers found the far-right legend too quiet to carry attention, waiting and recovery facts. Every Sync page with a confirmed commit now has a status block beside the title and section tabs. It takes the space the legend used, so no page grows taller, and it keeps the same three cells at 1280 and at 1024 DIP:

| Cell | Content | Rule |
| --- | --- | --- |
| **Needs attention** | A bold total in a fixed 20 DIP count column, then one row per non-zero part ("1 Conflict · choose a side", "2 Blocked · needs a repair") | Counts are right-aligned in one column, so a total and its parts line up. Zero parts are omitted. With none: a check and "Nothing needs attention" |
| **Waiting** | The waiting count in the same column, then the hold reason from `waitingHoldOr()` ("Queued for the running service.", "Held until the uncertain journal is reconciled.", "Held until Sync runs.") | When nothing waits: "Nothing waiting · Published up to 9a2e7f1." The reason comes from the same state that locks actions |
| **Next** | One action from `snap.next()`, and a link when the action is on another page | Order: offline → uncertain publication → conflicts → blocked → stopped service. On the page where the action is (Repository), the link becomes "Run the one-shot sync below." |

Duplicated counts were removed:

- The Queue filter is now `All · Conflicts · Blocked · Waiting`, without counts, and in the block's order. Group headings in the list still state their own counts.
- Frame 20's Queue card (count pills and hold note) is gone, because the block says the same thing.
- The compact top-bar attention key stays suppressed on Sync pages. The sidebar badge remains as navigation.
- The `Queue · n` tab keeps its total, because it labels a destination.

The block is a `ui.Row` card with `Dividers`. It is labelled "Sync status" for assistive technology, and each count is a separate text node.

#### 2. Check marks only for verified facts (frames 01, 12)

- **Frame 01:** each point in the Create and Join cards now carries the icon of what it touches: database for the catalog, folder for Projects, upload for publication, download for reading the published Workspace, link for Bind. Git access is not a capability, so it moved below a hairline as "Requires · Git access to the private repository · not checked yet", with the dashed pending icon. The points are also verb-first ("Initialize the catalog…").
- **Frame 12:** the Catalog tool lists use small neutral bullets.
- Check marks now appear only next to evidence that something was checked, such as "Reachable · checked 09:01", "Saved 10:44:52" or "Bytes verified".

#### 3. Frame 30: Source snapshot, and the recovery flags left to the guide

- The fixture checked `BackupWorkspace` in `internal/app/workspace_backup.go`. It hashes the catalog and bundle after writing them and records those hashes in the manifest. It does not compare the destination with the source. So the receipt no longer claims a read-back.
- **New evidence order:** SOURCE SNAPSHOT ("Read 11:12:41 · Confirmed 9a2e7f1 · journal clear", commit icon, neutral tone), then WRITTEN ("Written 11:14:02 · Manifest last, with each file's SHA-256"), then PUBLISHED ("Nothing").
- READ BACK is still used where the destination really was reread: the local catalog in frames 09, 21 and 25. `evidence` gained `source` and `sourceDetail`, and the strip shows READ BACK only when no source snapshot is given.
- The duplicate "Confirmed commit" row was removed from the receipt.
- The recovery note no longer splits `--from`, `--if-head` and `--new-epoch` across prose. It says what each recovery does ("Restore republishes it under a new epoch. Local recovery rebuilds a device in a new data directory.") and that "the guide gives each command with its required flags". "Open recovery guide" holds the exact commands.

#### 4. Frames 24 and 33: the remote option states its consequence

The choices are now **Keep local version** ("Restores the document with this device's revision 19.") and **Apply remote deletion** ("Deletes the document on every device once published."). Replace document reads "Writes new content…" to match. The receipt in frame 25 says "Keep local version · confirmed against token", which matches the confirm button.

#### 5. Frame 05: the order is stated and visible

The four Projects were already alphabetical, and the subscribed ones were also first, so the old label "Sorted by name" was ambiguous. Changes:

- The list has two group headings, "SUBSCRIBED ON STUDIO-LINUX · 3" and "NOT SUBSCRIBED · 1", separated by a hairline. Rows are sorted by name inside each group.
- The toolbar label reads "Grouped by subscription, then by name".
- The filter segments lost their counts, which the headings carry.
- The ml-notebooks row lost its "Not subscribed" pill, which repeated its heading. Its line now reads "Shared records only · subscribe to receive content".

#### 6. Hierarchy and density pass (13, 14, 16, 17, 23, 27, all Sync pages)

Each screen was inspected at native size:

- **13:** the target `127.0.0.1:15432` appeared five times. The probe description now says "to the actual target above", the result line is "Probed 10:44:31 · 127.0.0.1:15432 · from studio-linux", and "Used by" reads "Slot database in deploy-preview and db-migrate" instead of wrapping mid-pair.
- **14:** the provider check no longer repeats its "Not signed in" pill in prose; it states what was checked and when.
- **16:** the diff header no longer repeats the "Local drift" pill shown on the selected row; it reads "installed v1.3.2 → your edit on this device".
- **17:** "newest first" is stated once, above the table.
- **23, 27:** the Next action uses short wording ("Repair deploy-preview", "Reconcile the publication"), so the block keeps its height at 1280 DIP.

## Carried over: what changed from round 6 to round 7

#### 1. Publication wording: "queued" until a confirmed commit (frames 21–23, 25 and every Sync legend)

Round 6 said waiting records were "sent by the running service", which reads as if they had already been published. Round 7 uses one rule: **a record is published only when a confirmed commit includes it.** Until then it is *queued*.

- `snap.waitingHold()` now returns **"queued for the running service"** when the service is running. The held states are unchanged: uncertain journal, offline, no first publication, service stopped.
- **Legends (21–23):** "3 need attention · 1 conflict · 2 blocked · 6 waiting, queued for the running service".
- **Frame 21 detail:** the selected record shows three rows: **Written** (Saved 10:44:52, revision 8 on studio-linux), **Read back** (Read 10:46:20, still in Waiting) and **Published** (Not yet, queued for the running service). The service note says "A record counts as published only when a confirmed commit includes it. Last confirmed: 8c41d2e at 10:31." Its action is now verb-first: **Run sync now…**.
- **Frame 25 receipt:** Written "Saved 10:51:06 · local revision 21", Read back "Reread 10:51:07", Published "Not yet · Queued for the running service. Last confirmed 8c41d2e."
- **Conflict copy (24, 33):** the subtitle reads "The result is saved locally, then queued for publication". The effects say "21, saved locally and queued for publication" and "Unblocked and queued for publication".

#### 2. Cross-device inheritance is qualified by local layers (frames 07, 08, 32)

The real model keeps Device and Checkout layers on each device (`internal/app/configuration_impact.go`), so this device cannot see other devices' overrides. The frames now say so:

- **Frame 07:** "Layers in precedence order: a later layer overrides an earlier one. The first three are shared. Device and Checkout layers stay on each device, so other devices can override a shared value with their own." The editing-layer footer is now two rows: the layer on the first, the actions on the second. It no longer wraps into fragments.
- **Frames 08 and 32, scope line:** "Shared write to Profile · staging. Checkouts that use staging inherit it unless a Device or Checkout layer overrides db.pool_size."
- **Frames 08 and 32, other devices:** "Inherit 10 once a confirmed commit includes this, unless their own Device or Checkout layer sets it. Those layers are not visible here."
- **Frames 08 and 32, publication:** "Queues 1 shared record. Published only when a confirmed commit includes it."

#### 3. Join in two stages, with the Project resolved by ID before Bind (frames 02 and new 02b)

I checked the real CLI before naming commands. `join <git-remote>` takes `--subscribe <Project UUID or slug>`, and the root `--project` flag takes an explicit Project UUID or slug (`internal/cli/sync.go`, `cli.go`).

- **Frame 02 (stage 1): "Join a Workspace"**
  - The form subscribes by the slugs people recognise, `atlas-api, billing-worker`. The Project picker notes "From your subscription list. Its Project ID is read after join."
  - The right card is **"Join in two stages"**:
    - **1 · Join and subscribe by slug:** prerequisites, the `ctxferry join … --subscribe atlas-api --subscribe billing-worker --data-dir …` command with **Copy command**, then "After it finishes, Refresh reads the joined catalog and resolves each slug to its Project ID."
    - **2 · Bind ~/code/atlas-api to atlas-api:** marked **After Refresh**. It gives no command yet: "Bind names the Project by the ID that Refresh reads for atlas-api, not by its slug."
- **Frame 02b (stage 2, new):** join wrote the catalog at 10:43:20, and Refresh read it at 10:43:31.
  - A stage line shows "Join and subscribe · Done 10:43:20" and "Bind a Checkout · Now".
  - **Joined** card: Workspace Northwind Labs `5b0e2c91`. Written locally: join at 10:43:20, catalog and 2 subscriptions. Read back: 10:43:31, by Refresh. Confirmed commit `8c41d2e`, fetched and published by another device.
  - **Project IDs for your slugs:** each slug, its description and its full Project ID: `atlas-api` → `9f3a7c21-6b48-4e0d-b5a2-1c7e84d39f60`, `billing-worker` → `3b8e5d02-9c71-4a46-8f3b-d24e1a60c7f9`.
  - **Bind card:** the directory, the Project picker showing slug and ID, and `ctxferry project bind ~/code/atlas-api --project 9f3a7c21-6b48-4e0d-b5a2-1c7e84d39f60 --data-dir …`, followed by the Refresh step. A quiet escape reads "Bind later from Projects. Nothing is bound until the command runs" and offers **Open Home**.

#### 4. CLI handoffs: a labelled Copy command, then a quiet Refresh (frames 02, 02b, 05, 15, 18–20, 23, 27)

Every command block is now one component (`cmdBlock`):

- **Above the command:** "Run in a terminal" and a native, labelled **Copy command** button. Its accessible name includes the command, for example "Copy command: ctxferry sync init".
- **The command itself:** the dark block, with no bare copy icon inside it.
- **Primary action:** Copy command is the card's primary action. The second command in a handoff gets a secondary button.

Every handoff then ends with one return step (`afterRun`):

- **The note:** a hairline, then a sentence that starts "After it finishes, Refresh reads …" and names what Refresh will read.
- **Refresh:** a quiet, borderless accent action on the right. Refresh is never primary and never competes with Copy command while the operator still has to run the command.

Specific changes:

- 19: Refresh was primary and is now quiet.
- 02: "Copy both commands" is gone, because the two stages are separate.
- 23: "Copy repair command" became the standard **Copy command**. **Open Task in billing-worker** moved into the repair header so the card fits.
- 15: "Refresh mapping" became **Refresh**, with the note "Shared write: it queues one record. After it finishes, Refresh reads the mapping."

#### 5. Read, write and publication evidence have fixed names (receipts 09, 25, 30; detail 21; join 02b)

A new `evidence` component always shows three facts in the same order and with the same words:

| Label | Meaning | Example (frame 09) |
| --- | --- | --- |
| **WRITTEN** | What this device saved locally | Saved 10:44:52 · Record 2f9a6c14 · revision 8 |
| **READ BACK** | What a reread of the data root found | Reread 10:44:53 · values in the field list |
| **PUBLISHED** | Whether a confirmed commit includes it | Not yet · Queued for the running service. Last confirmed 8c41d2e. |

- Full-width receipts use a three-cell strip (`evidenceStrip`). Narrow detail columns use the same rows stacked (`evidenceList`, frame 21).
- Backup (30) reads Written "11:14:02 · 1,284 files to a new directory", Read back "From the 11:12:41 read", and Published "Nothing · A backup does not publish or change the Queue".
- Receipts no longer carry a separate time line or a coloured publication banner, because the strip holds both. Frame 09 drops its duplicate Record row, and "Verified" became "Now effective".

#### 6. Welcome and CLI information grouping (frame 01)

- The gate now opens with **"Welcome to CtxFerry"** and "Set up this device by creating a Workspace or joining one. Nothing is written until you confirm."
- The `--data-dir` hint moved from a loose footnote into the **Data root** card, under a hairline: "Wrong location? Restart CtxFerry with `--data-dir <path>` to open another data root. Every command shown later carries the same flag." The duplicate "Last successful read: Never" row was removed.
- The option actions are verb-first: **Create Workspace** (was "Create Workspace here") and **Join a Workspace…** (was "Show join steps"). The Join option lists "Subscribe to Projects by slug" and "Bind directories by the resolved Project ID", matching frames 02 and 02b.

#### 7. Editorial and hierarchy pass (all frames)

- **Calm contrast between layers:** the scrolling page sits on a faint tint, `Page #F8F9FB`, while the top bar and cards stay white. Cards lift off the page without shadows or heavier borders, and nested boxes stay out.
- **Stronger section titles:** card and section titles are 16/650 (was 640).
- **Verb-first labels:** Run sync now…, Open recovery guide, Create Workspace, Join a Workspace…, Copy command, Refresh. The frame 05 heading became "Subscribe on studio-linux", so it no longer repeats "CLI" above "Run in a terminal".
- **Shorter copy where it wrapped:** the frame 23 banner is two lines, the repair note is one sentence, and the configuration effects are one or two lines.
- **Contrast:** six new pairs cover the page tint, links and the attention legend on it, and the "not yet published" evidence colour. All 42 pairs pass, lowest 5.11.

## Carried over: what changed from round 5 to round 6

#### 1. Frame 23: the whole blocking path is visible

- The runbooks/ row no longer squeezes its blocker into one line. Under the row's name it shows "Waits on the conflict on", then **`runbooks/rollback.md →`** as a standalone link on its own line, start-aligned with the text above it.
- The link is a `ButtonBase` labelled "Open the conflict on runbooks/rollback.md that blocks runbooks/ index". The path is never cut; if the column narrows, it wraps.
- The reason row is now a wrapping label and value: "Reason" beside the full `invalid-task-dependency:a2c7e910-4b5d-4f83-96e1-3d0f8b72c5a4`. Round 5 cut it to `a2c7e910…` and relied on a tooltip.

#### 2. Waiting copy comes from the state that governs actions (frames 20, 25–30, 34)

One function, `snap.waitingHold()`, decides why waiting records are not being published. The legend, the Queue card and the conflict receipt all read it, as do the action locks:

| State (same fields that lock actions) | Copy |
| --- | --- |
| Publication outcome unknown (`unknown`) | "held until the uncertain journal is reconciled" |
| Remote unreachable (`offline`) | "held while the remote is unreachable" |
| No confirmed commit yet | "held until the first publication" |
| Sync service not running | "held until Sync runs" |
| Service running, remote reachable, journal clear | "sent by the running service" |

- **Frames 26 and 27** (offline, then online with the journal still uncertain) now read "1 needs attention · 1 blocked · 8 waiting, held until the uncertain journal is reconciled". Round 5 said "8 waiting publish on their own", which promised publication that the same state blocks.
- **Frame 20** (service stopped): the Queue card adds "Waiting records are held until Sync runs." It previously showed only "3 waiting".
- **Frame 25** (service running): the receipt reads "Waiting for publication, sent by the running service."
- **Zero clauses are omitted.** `attentionDetail()` drops any part whose count is 0. Frames 28–30 and 34 (0 waiting, 0 conflicts) read just "1 needs attention · 1 blocked". The evidence row "Waiting is now 0" in frame 28 stays, because it reports the result of the refresh.
- The separator between the attention count and its detail is now a separate glyph with even spacing on both sides. Round 5 had a double gap before the "·".

#### 3. Frames 06 and 11 share one trust record

- `trustByCheckout` in `data.go` holds each Task's trust state per Checkout. Both frames read it; neither has its own text.
- **Three states, three labels**, each with its own tone and icon:
  - **Trusted** (ok, check-circle): trusted, and the definition has not changed since.
  - **Recheck** (attention, triangle): trusted once, but the execution definition changed since.
  - **Not trusted** (unknown, dashed circle): never trusted on this Checkout.
- **Frame 11:** the Task list shows deploy-preview **Recheck**, db-migrate **Trusted** and load-test **Not trusted**, with a summary line underneath: "1 trusted · 1 to recheck · 1 not trusted".
- **Frame 06:** each Checkout card shows the same summary for its own Checkout:
  - `~/code/atlas-api`: "Tasks: 1 trusted · 1 to recheck · 1 not trusted"
  - `~/code/atlas-api-review`: "Tasks: 3 not trusted"
  - The Tasks tile repeats the selected Checkout's summary with a "1 to recheck" pill.
- The checkout card moved its "Selected" pill into the header row, so the trust summary gets the full second line and no longer wraps.

#### 4. No essential evidence is cut with an ellipsis (new `-audit` check)

Round 5's `-check` only proved that text stayed inside the window. This round audits the text itself:

- Every one-line text in the fixture goes through one helper, `cut()`. Normally it applies `SingleLine` and `Ellipsis`.
- `go run . -audit` renders all 34 frames twice: once normally, and once with `cut()` letting the text wrap. Any text that grows more than 2 DIP taller when it is allowed to wrap was being cut, and is reported with its frame and string. The command exits non-zero if it finds any.
- The first run found five cuts, all now fixed:
  - 06: the Tasks tile summary
  - 07–09: the `https://` value in the field list
  - 20: "Confirmed 5d07b3a · yesterday 18:12" in the sidebar footer
- Digests are shown in full where they are evidence. The manifest SHA-256 in frame 30 and the artifact SHA-256 in frame 17 are now complete 64-character values.
- **Compact breadcrumbs:** a three-level trail is no longer collapsed. Frame 32 now reads "Projects › atlas-api › Configuration" instead of "Projects › … › Configuration". Only trails deeper than three collapse.
- **Not covered by the audit:** `TextInput` and `Select` values, and `Breadcrumbs` items, which MyGo truncates internally. These were checked by eye in every frame; none is cut.

#### 5. Visual system: calmer surfaces, stronger type, fewer labels

The round 5 baseline used cards inside cards, a labelled box for every scope field and a chip for every ID. Round 6 removes those layers across the whole set:

- **Stronger type contrast.** The page title is 26/680 with −0.5 tracking (was 22/650). Card titles are 16/640 (was 15/600). Subtitles are 14 with line height 1.45. The gap between the title and the body is larger, so each page has one clear entry point.
- **Scope line instead of a scope box.** The bordered, overlined scope bar is now an unboxed line under the subtitle: "Project atlas-api 9f3a7c21 | Checkout ~/code/atlas-api 0e7b4d92 | Profile staging", each value with its object icon. It uses sentence-case labels, hairline separators and a bottom rule (07–17, 24, 32, 33).
- **Plain IDs.** ID chips lost their bordered, filled box. They are now subtle mono text with the full ID in the tooltip, so a row reads as name first, ID second.
- **Quieter tags.** Tags (Profile, record kind) are a soft fill with no border.
- **Fewer nested surfaces:**
  - Wells (code excerpts, notes) lost their border.
  - Effect tables are ruled rows with top and bottom hairlines, not a box inside the preview.
  - The sidebar Workspace block is no longer a card. Its "Destinations" section label is gone, since the four items need no heading.
- **Wrapping where text is evidence.** Sidebar footer lines and field-list values wrap instead of being cut.
- **Consistent separators.** Every "value · detail" pair (confirmed commit and time, journal state) uses one separate subtle "·" with equal spacing.

## Carried over: what changed from round 4 to round 5

#### 1. Frame 13: the probe names the Checkout that set its override

- The route card gains an **Override set by** row: `~/code/atlas-api`, its Checkout ID chip and "Checkout layer". The override itself keeps only its kind, "SSH tunnel".
- At the probe, the text now reads "From studio-linux to 127.0.0.1:15432, the override set by Checkout ~/code/atlas-api. Sends no credentials."
- The result line repeats the path: "target 127.0.0.1:15432 via ~/code/atlas-api".

#### 2. Frame 23: a blocked record the CLI can actually repair

Before naming any action, I read `internal/cli` and `internal/app`:
- **Uniqueness:** the sync planner blocks on uniqueness only for Project slugs and Project summaries (`sync_conflicts.go`). Round 4's "Task name already used" was not a real block reason.
- **Task dependencies:** the planner does block a Task whose Repository membership is deleted or belongs to another Project (`invalid-task-dependency:<id>` in `task_profile.go`).
- **Repair command:** the fix uses `ctxferry task update <name-or-uuid> --membership <uuid> --if-revision <n>`, all real flags (`task.go`).

Frame 23 now shows that case:
- **Two records, both identified:**
  - BLOCKED RECORD: Task deploy-preview `e40d9a17`, local revision 3, not published.
  - MISSING DEPENDENCY: the billing-worker membership `a2c7e910`, revision 5, deleted.
  - The reason line shows the real code, `invalid-task-dependency:a2c7e910…`, with the full ID in its tooltip.
- **A repair aimed at an exact target:** the header is "Repair task e40d9a17 in billing-worker", beside the current membership chip `5c1e8f04`.
  - The command is `ctxferry task update e40d9a17-… --membership 5c1e8f04-… --if-revision 3 --data-dir /home/dev/.local/share/ctxferry`.
  - **Copy repair command** is the only primary action. Refresh comes back from the CLI.
- **Consequence stated:** the CLI help calls a membership change an execution change. Every device re-enables the Task with `task trust` before it runs with credentials. `project inspect billing-worker` lists the membership.
- **Scoped alternative:** an "Open Task in billing-worker" link.
- **runbooks/ owns its link:** the row now carries its own second line, "Waits on the conflict on runbooks/rollback.md", with **Open conflict**. It no longer floats under the list.
- Frame 21 lists the same record and ID; its row still reads "deploy-preview · Task definition · billing-worker".

#### 3. Frame 32: effective value, origin and editing layer are labelled

- The compact field strip now uses the wide view's column labels in four cells:
  - **FIELD**: the picker.
  - **EFFECTIVE VALUE**: 20.
  - **ORIGIN**: Profile · staging with a **Wins** pill.
  - **EDITING LAYER**: Profile · staging, "shared, writable", with the edit icon.
- "Showing 8 of 14 fields · fields with a value" and **Show all 14** close the strip, as in frames 07 and 08.
- Wide frame 07 makes the same distinction:
  - The header reads "Effective 20 from Profile · staging".
  - The Wins pill's tooltip reads "Highest set layer: supplies the effective value".
  - The footer names the "Editing layer: Profile · staging · shared, writable".
  - The selected layer row carries the accessible label "Editing layer".

#### 4. Sync badge: "need attention" at every width (frames 21, 25 and all frames)

- One rule, `snap.attentionText()`: the badge counts **conflicts + blocked**, the records that need a person. Waiting records publish on their own and are not counted.
- **Wide:** the sidebar badge reads **"3 need attention"** (or "1 needs attention") instead of a bare number. Every Sync page with tabs adds a legend beside its title: "3 need attention · 1 conflict, 2 blocked · 6 waiting publish on their own". The badge and the Queue total (9) can no longer be confused.
- **Compact:** the rail keeps the numeric badge, and its accessible name and tooltip are "Sync · 3 need attention". The top bar shows the same phrase as a key with the same colour and alert icon. On Sync pages that already show the legend (34), the key is left out to avoid repeating it.
- Frame 25 after the resolution shows "1 needs attention · 1 blocked · 8 waiting publish on their own"; zero counts are omitted. (Round 6 replaces "publish on their own" with copy derived from the snapshot; see above.)

#### Polish pass

- **Duplicated chrome removed:** the sidebar footer no longer repeats "Schema 14 · read hh:mm:ss"; the read time lives once, in the top bar beside Refresh. The footer is three lines: device, service and confirmed commit (or the publication outcome).
- **Compact breadcrumbs with Back:** when Back names the origin, the compact trail shows only the current item (frame 33: "← Queue · Conflicts | runbooks/rollback.md"). The full trail is in the tooltip.
- **Quieter metadata:** queue record kinds are plain subtle text instead of bordered tags, so the name, Project and ID carry the row (21).
- **Denser effect tables:** effect rows use 8 DIP vertical padding, so preview panels are shorter (08, 24, 32) without losing a line.
- **Icon rhythm:** attention tiles (04, 31) use the same 28 DIP icon well as card headers.
- **One primary per decision:** frame 23's repair has a single primary action ("Copy repair command"); Refresh and the in-app alternative are secondary.

## Carried over: what changed from round 3 to round 4

#### 1. Conflict text is identical at both widths (frames 24 and 33)

- The conflict is defined once in the fixture (`conflictLocal`, `conflictRemote`, `conflictLines`, `conflictChoices`, `conflictEffects`, `conflictConfirm`). Both layouts render those values. Nothing is reworded or shortened for the compact width.
- Both frames show the same revision 19 lines, ending "3. Run db-migrate --down 1 if the schema moved.", plus the same device, revision and time ("studio-linux · revision 19 · Edited today 10:02").
- Document lines **wrap** instead of truncating, so a narrower column can never cut the bytes.
- Frame 33 now has the conflict token in its scope bar, as frame 24 does. The side switch names both revisions: "Local · revision 19" and "Remote · revision 20 · deleted".
- The three choices use the same two-line labels and details. The preview names the same three effects; at the compact width they sit side by side in columns instead of rows, so all three are visible above the pinned bar.

#### 2. Backup names its source beside the destination (frames 29 and 34)

- One shared form backs both frames. Its route block reads top to bottom:
  - **From Workspace:** Northwind Labs, its ID chip, and the data root `/home/dev/.local/share/ctxferry`.
  - **To a new directory:** the destination field, Choose…, and the "does not exist yet" check.
- The plaintext-exposure panel names the Workspace ("The backup of Northwind Labs is not encrypted…").
- So does the acknowledgement: "I reviewed what this backup of Northwind Labs exposes".
- The compact frame keeps the full data scope, the blocked-record note and the secrets sentence that round 3 had dropped.

#### 3. Run artifacts: older items are reachable (frame 17)

- The list ends with the shared list footer: **"Showing 1–5 of 12 · newest first"** and a **Load 7 older** button, which appends the older artifacts below the list.
- The filter row states the total: "atlas-api · 12 artifacts, newest first".

#### 4. Configuration: the token label is complete (frame 08)

- Every preview now closes with one **confirm footer**. A full line, "Confirms against preview token 9d04e6b2", sits directly above Back to layers and Remove from Profile · staging. Neither the label nor the buttons are squeezed.
- Frame 24 uses the same footer ("Confirms against conflict token a51f7e08"). The compact action bars (32, 33) show the same label in one row beside the buttons.

#### 5. One field count in every layout (frames 07, 08, 32)

- The field list states **"Showing 8 of 14 fields · fields with a value"** and offers **Show all 14**. The 8 is the number of rows actually listed, and 14 is every field the schema defines.
- The compact frame folds the list into a field picker (`Select`, so the field stays selectable) and shows the same count and the same Show all 14 action. It no longer says "All 8 fields".

#### Responsive parity audit (31–34 against 04, 08, 24, 29)

Each compact twin was checked against its full-width frame for four things:

| Twin | Content parity | Confirmation scope | Selectability | Critical labels |
| --- | --- | --- | --- | --- |
| 31 = 04 | Same tiles with their one-line descriptions, Bound Projects, the ml-notebooks note; device evidence in the top bar | No confirm | Tiles, Projects and links | Counts 6/1/2 |
| 32 = 08 | Same heading, scope sentence, four effects and the "Checkout layer instead" note; Change Checkout kept | Same token and button labels, pinned | Field picker, layer via Back to layers | Full token label |
| 33 = 24 | Same lines, revisions, choices, effects and token | Same token and buttons, pinned | Side switch, three radio choices | Full token label; both revisions named |
| 34 = 29 | Same route, scope, exposure and acknowledgement | Acknowledgement names the Workspace | Checkboxes, Choose… | Source Workspace and data root beside the destination |

Compact pages also keep the page subtitle (Home, Configuration, Resolve conflict), which round 3 had dropped.

#### Visual system

- **One confirmation contract** (`confirmSpec`): a key icon, "Confirms against <token kind>", the ID chip, then Cancel and the primary button that names its target. It renders as a panel footer at full width and as the pinned bar at the compact width.
- **One list footer** (`listFooter`): the displayed range on the left, the way to see more on the right. It is used by Configuration fields, Run artifacts and every Queue list.
- **One preview badge** (`previewBadge`) shared by the full panel and the compact strip.
- **Effect rows** use a 196 DIP target column, so effect targets no longer wrap (for example "~/code/atlas-api-review" in frame 08).
- **Field picker** uses the mono face, like field names everywhere else.

## Carried over: what changed from round 2 to round 3

Kept for history. Where round 4 changed a rule (the compact field list, the compact preview, the token label), the section above and the rules below take precedence.

#### 1. Join is a form, and the commands are generated from it (frame 02)

- Choosing Join opens **Join details** with editable native fields:
  - **Private repository** (`TextInput`)
  - **Subscribe to**: a Chosen/All Projects switch plus a slugs-or-IDs field
  - **Checkout directory on this device**, with a Choose… button
  - **Bind it to Project** (`Select`)
- **Generated commands** sits beside the form:
  - Step 1 is `ctxferry join <repository> --subscribe atlas-api --subscribe billing-worker`.
  - Step 2 is `ctxferry project bind ~/code/atlas-api --project atlas-api`.
  - Each value comes from a field, and each command ends with the explicit `--data-dir` shown in the data-root strip above.
  - Step 2's label names the directory and the Project.
- **Copy both commands** is the only primary action. Copying changes nothing, and Create stays a radio tile with no primary button.

#### 2. Skills: the diff names its file, and selection matches the drift (frame 16)

- The changed-files list selects **templates/changelog.md**, the drifted file. It uses the same accent bar and `AccessSelected` as every other list.
- The diff opens with a file header **directly above its lines**: the path, "installed v1.3.2 → this device" and a "Local drift" pill. Banner, selected row, header and diff all name the same file.
- The banner explains why the update is blocked: the file was edited locally, and v1.4.0 also changes it.
- **Restore installed file…** (with a preview first) and **Rescan** are the two ways out. "Update to v1.4.0" stays disabled until a rescan finds no drift.

#### 3. Queue: every record is reachable (frames 21–23)

- Queue rows are now one line each: state icon with an accessible label, name, kind tag, Project and ID chip. Revisions and reasons move to the detail panel.
- Frame 21 lists **all nine records**: 1 conflict, 2 blocked and all 6 waiting. Nothing is left as a "3 more" line.
- Every queue list ends with a pager: "Showing 9 of 9 · 50 per page", with Previous and Next disabled because everything fits on one page.
- In the narrower filtered lists (22, 23) rows omit the kind tag, so names never truncate.

#### 4. Compact window (new frames 31–34)

At the minimum 1024×680 window the layout follows rules that are now rendered, not just described:

- **Icon rail:** the sidebar becomes a 56 DIP rail.
  - It has labelled icon buttons with tooltips and accessible names, and the selected destination uses the accent-soft fill.
  - The Sync badge keeps its count.
  - The Workspace name and data root are in the brand mark's tooltip, and the device in the device icon's.
- **Evidence in the top bar:** the sidebar's evidence moves into a top-bar strip showing service state (icon plus text), confirmed commit and time, and read time. Refresh becomes an icon button with a label.
- **Breadcrumbs:** they collapse to root › … › current, with the full trail in a tooltip. Back keeps its named origin.
- **Pinned action bar:** critical controls are pinned in a bar under the scrolling page, so they cannot scroll out of view:
  - Configuration: confirm token, Back to layers, Remove from Profile · staging.
  - Conflict: confirm token, Cancel, Keep local version.
  - Backup: the exposure acknowledgement, Cancel, Write backup.
- **Detail views:**
  - Configuration folds its field list into the selected field ("All 8 fields" opens it).
  - Conflict puts the Local and Remote sides behind a switch and keeps the three choices visible.
  - The preview becomes one line of effects.
- **Kept at every width:** scope bars, IDs, preflight evidence and plaintext exposure are never hidden.

#### Clarity and density

- **Gate pages** use the full content width.
- **Skills:** the explanation beside the catalog switch is one sentence.
- **Queue:** single-line rows raise visible records from 5 to 9 in frame 21.
- **Contrast:** two new pairs are checked: the rail badge (6.58) and action-bar text (7.13). All 35 pairs pass 4.5:1.

## Carried over: what changed from round 1 to round 2

### One snapshot drives each frame

Round 1 set sidebar flags, top-bar text and page copy by hand, so they could disagree (frame 17 showed "stopped" in the sidebar and "Not installed" on the page). Round 2 adds a `snap` type in `fixture/data.go`. It holds the read time, service state, confirmed commit, queue counts, and the offline, unknown-outcome and stale flags.

Each frame names one snapshot, and these all read from it:

- the sidebar badge and footer
- the top-bar read time
- the Sync `Queue · n` tab
- the queue filter counts
- Home's tiles and device card
- the remote card's service row

The timeline is below. The inventory is ordered by destination, but every snapshot sits at a point on one day.

| Snapshot | Read | Service | Confirmed | Waiting / Conflicts / Blocked | Frames |
| --- | --- | --- | --- | --- | --- |
| `snapSetup` | 3 Nov 10:11:02 | not installed | none | queue not started | 18 |
| `snapSetupLost` | 3 Nov 10:13:40 | not installed | none, init outcome unknown | queue not started | 19 |
| `snapMorning` | 09:02:15 | stopped | 5d07b3a · yesterday 18:12 | 3 / 0 / 0 | 20 |
| `snapEdit` | 10:42:08 | running | 8c41d2e · 10:31 | 5 / 1 / 2 | 07, 08 |
| `snapSaved` | 10:44:53 | running | 8c41d2e · 10:31 | 6 / 1 / 2 | 09 |
| `snapNow` | 10:46:20 | running | 8c41d2e · 10:31 | 6 / 1 / 2 | 04–06, 10–17, 21–24 |
| `snapStale` | 10:46:20 kept; refresh failed 10:49:12 | running | 8c41d2e · 10:31 | 6 / 1 / 2 | 03 |
| `snapResolved` | 10:51:07 | running | 8c41d2e · 10:31 | 8 / 0 / 1 | 25 |
| `snapOffline` | 11:06:02 | offline; routine outcome unknown | 8c41d2e · 10:31 | 8 / 0 / 1 | 26 |
| `snapOnline` | 11:12:05 | running; routine outcome unknown | 8c41d2e · 10:31 | 8 / 0 / 1 | 27 |
| `snapCleared` | 11:12:41 | running | **9a2e7f1 · 11:12:40** | 0 / 0 / 1 | 28–30 |

### Fixes requested in review

1. **Task credentials agree (frames 11, 14, 15).**
   - Tasks lists all three credential requirements: db-password and registry-token are mapped, and cache-auth is unmapped with "needs slot cache first". Its findings strip says "Credentials · 1 of 3 unmapped".
   - Credential bindings (14) lists the same three requirements with their type and slot.
   - The new frame 15 opens cache-auth. It explains that slot cache has no Resource in Profile staging. Step 1 is a `profile map-resource staging deploy-preview cache <redis-cache UUID>` handoff, and step 2 maps the existing compatible reference `atlas/staging/redis`. Step 2 is disabled until the slot is mapped and reread.
2. **One conflict journey (frames 04, 21–25).**
   - The journey starts from Home's **Conflicts** tile. Frame 22 is the Queue with the Conflicts filter, and Back goes to Home.
   - "Open comparison" leads to frame 24, where Back reads "Queue · Conflicts".
   - After confirming, frame 25 shows the receipt above the **restored Conflicts filter**, now empty. Back still goes to Home, the origin of the restored filter.
   - The populated all-records Queue is kept as frame 21. It opens from the Sync tab with no Back and selects waiting record 2f9a6c14, which was saved in frame 09.
3. **Backup only accepts a write after fresh evidence (frames 26–30).**
   - 26: Backup is blocked offline by routine journal 3d8f1a62.
   - 27: the reconcile handoff, with Back to Backup.
   - 28 (new): after refresh, the **journal cleared** at 11:12:40, the new confirmed commit **9a2e7f1 was reread** at 11:12:41, and the **local read** is 11:12:41. Backup and conflict resolution show as Available.
   - 29: Backup opens with a preflight strip showing the same three facts.
   - 30: the receipt records the read it was written from.
4. **Join has a non-mutating handoff (frame 02).**
   - Choosing Join turns the two option cards into a compact radio row. The Create card and its "Create Workspace here" primary action are not shown.
   - The only primary action is **Copy commands**, which copies `join` and `project bind`. The card states that copying does not change the device. "Refresh data root" is secondary.
5. **Service state comes from the snapshot (frame 18).** The sidebar footer ("Sync service not installed") and the remote card ("Not installed") both call `snap.serviceText()`.

### Contrast

- Muted, subtle and status colours were darkened.
- `go run . -contrast` computes WCAG 2.x ratios for 33 text/surface pairs and fails below 4.5:1. All 33 pass.
- Every pair is listed, including muted text on the selected-row fill and each tinted well, the command prompt on the dark command block, and pill text on each pill fill.

| Pair | Ratio | Pair | Ratio |
| --- | --- | --- | --- |
| Muted `#4A5563` on Surface / Sunken / Canvas | 7.58 / 7.13 / 6.89 | Waiting pill `#4353B8` on `#ECEEFB` | 5.74 |
| Muted on selected row `#E3F1F4` | 6.55 | Conflict / attention `#94480A` on `#FDF1E1` | 5.91 |
| Subtle `#5B6573` on Surface / Sunken / Canvas | 5.91 / 5.56 / 5.37 | Blocked `#B42318` on `#FDECEA` | 5.75 |
| Subtle on selected row | 5.11 | OK `#136B41` on `#E6F4EC` | 5.77 |
| Accent `#0B6B83` on Surface / Sunken | 6.10 / 5.74 | Unknown `#525A66` on `#EEF0F3` | 6.10 |
| Primary label on Accent | 6.10 | Command prompt `#94A0AE` on `#11161C` | 6.84 |

ID chips use Muted on Sunken (7.13). Queue row meta lines use Subtle on Surface (5.91) and on the selected row (5.11).

### Simpler hierarchy

- The Sync page has only its title above the tabs (the round 1 subtitle is gone).
- Remote cards drop the repeated subtitle, and the service row is one pill.
- Home's device card shows the service as one pill and other facts as plain text.
- Long instructions became one sentence, for example "Refresh rereads the confirmed commit and journal."
- Queue rows merge Project, short ID and revision into one meta line.
- The Repository lock card folds "still available" into one line.
- The Join footer is one sentence.
- The step actions on Credential bindings sit under their text, so the step title no longer wraps.
- Status always pairs an icon with text. Lock icons carry the accessible label "Locked", and preflight checks carry "Passed".

### Commands

- Every copyable command is built by `cli()` in `fixture/components.go`. It ends each command with the same `--data-dir /home/dev/.local/share/ctxferry` and breaks argument groups with a `\` continuation.
- Commands use real slugs where the UI shows a shortened ID: `--subscribe atlas-api`, `--project atlas-api`, `project subscribe ml-notebooks`.
- Full UUIDs appear only where the CLI requires one: the preview ID for `sync init`, and the Resource UUID for `map-resource`.
- Restore is not shown as a copyable command. The receipt names `ctxferry workspace restore`, says it republishes under a new epoch and requires `--from`, `--if-head` and `--new-epoch`, mentions `workspace recover-local --from`, and links to the recovery guide.

All spellings were checked against `internal/cli` at `fb3d659`:

| Command in frames | Frame | Source |
| --- | --- | --- |
| `ctxferry join <git-remote> --subscribe <UUID or slug>` (`--all-projects` mentioned) | 02 | `sync.go: join` |
| `ctxferry project bind <path>` with root flag `--project <UUID or slug>` | 02 | `cli.go: bind`, root persistent `--project` |
| `ctxferry doctor` (tooltip) | 03 | `doctor.go` |
| `ctxferry project subscribe <project>` (selector accepts slug) | 05 | `cli.go` subscribe loop → `selectProject` |
| `ctxferry task trust preview <task> --profile --checkout` (tooltip) | 11 | `task.go: trustCommand` |
| `ctxferry profile map-resource <profile> <task> <slot> <resource-uuid>` | 15 | `configuration.go` |
| `ctxferry sync init --preview <preview ID>` | 18, 19 | `sync.go: init` |
| `ctxferry sync` (one-shot) | 20, 21 (tooltip), 27 | `sync.go: group RunE` |
| `ctxferry workspace restore --from --if-head --new-epoch`; `workspace recover-local --from` (text only) | 30 | `workspace.go` |
| `--data-dir` on every copied command | all | root persistent flag in `cli.go` |

`ctxferry run deploy-preview --profile staging` in frame 10 is the text of a shared Context document, not a copy action. A shared document cannot carry one device's data root.

## Frame inventory

| # | Frame | Destination | Snapshot and state | Round 9 |
| --- | --- | --- | --- | --- |
| 01 | Workspace readiness: create or join | Shell gate | Welcome; fresh empty data root; no catalog; --data-dir hint inside the Data root card; Create selected; capability icons, no check marks; Join requires Git access, not checked yet | 01 (unchanged) |
| 02 | Workspace readiness: join handoff | Shell gate | Join chosen; stage 1 of 2: join and subscribe by slug, Copy command primary, Refresh quiet; Bind waits for the Project ID | 02 (unchanged) |
| 02b | Workspace readiness: bind after join | Shell gate | snapJoined 10:43:31; join written 10:43:20; Refresh resolved atlas-api and billing-worker to Project IDs; bind names atlas-api by ID | 02b (unchanged) |
| 03 | Home: refresh failed after a successful read | Home | snapStale: 10:49:12 refresh failed; 10:46:20 snapshot kept; writes, Backup and remote checks off | 03 (changed) |
| 04 | Home | Home | snapNow 10:46:20: 6 waiting, 1 conflict, 2 blocked; tiles open Queue with that filter; Conflicts tile says keep a side or replace | 04 (changed) |
| 05 | Projects: list with an unsubscribed Project | Projects | snapNow; 4 Projects; grouped by subscription, then by name; ml-notebooks selected, not subscribed on this device; labelled Copy command, quiet Refresh | 05 (unchanged) |
| 06 | Project overview | Projects | snapNow; atlas-api; 2 Checkouts with different Profiles; ~/code/atlas-api selected; Task trust 1 trusted, 1 to recheck, 1 not trusted, read from the same data as 11 | 06 (unchanged) |
| 07 | Configuration: layer inspection | Projects | snapEdit 10:42:08; db.pool_size wins from Profile staging; showing 8 of 14 fields (with a value), Show all 14; Device and Checkout layers named as local to each device | 07 (unchanged) |
| 08 | Configuration: scope-aware preview | Projects | snapEdit; remove override from shared Profile staging; other devices inherit 10 only after a confirmed commit and unless their own Device or Checkout layer sets it; 8 of 14 fields | 08 (unchanged) |
| 09 | Configuration: local receipt, awaiting publication | Projects | snapSaved 10:44:53; saved 10:44:52; record 2f9a6c14 waiting (5 → 6); evidence Written 10:44:52 / Read back 10:44:53 / Published not yet, queued for the running service | 09 (unchanged) |
| 10 | Context | Projects | snapNow; 23 documents under ~/code/atlas-api · staging; deploy.md open; conflict and missing-content rows | 10 (unchanged) |
| 11 | Tasks | Projects | snapNow; same trust data as 06: deploy-preview Recheck, db-migrate Trusted, load-test Not trusted; slot cache unmapped; 3 credential requirements, cache-auth unmapped | 11 (unchanged) |
| 12 | Catalog directory | Catalog | snapNow; Project scope atlas-api (subscribed only); four tools with plain bullets | 12 (unchanged) |
| 13 | Resources & environments, opened from a Task | Catalog | snapNow; from deploy-preview slot database; tunnel override set by Checkout ~/code/atlas-api, named at the probe; probe reachable | 13 (unchanged) |
| 14 | Credential bindings, opened from a Task | Catalog | snapNow; 3 requirements as on frame 11; db-password mapped; Resource match links postgres-primary (opens 14b); provider not signed in | 14 (unchanged) |
| 14b | Resources & environments, opened from a credential requirement | Catalog | snapNow; opened from the postgres-primary link in db-password's Resource match on 14; Back returns to Credential bindings with db-password selected; same route and probe as 13 | 14b (unchanged) |
| 15 | Credential bindings: resolve cache-auth through its slot | Catalog | snapNow; cache-auth unmapped because slot cache has no Resource; map-resource handoff with labelled Copy command and quiet Refresh, then reference | 15 (unchanged) |
| 16 | Skills: update blocked by local drift | Catalog | snapNow; release-notes v1.3.2 → approved v1.4.0; templates/changelog.md drifted; confirm disabled | 16 (unchanged) |
| 17 | Run artifacts: verified export and metadata-only item | Catalog | snapNow; showing 1–5 of 12 (newest first, stated once above the table), Load 7 older; latency-report.json exported and verified; trace.zst metadata only | 17 (unchanged) |
| 18 | Repository: first publication | Sync | snapSetup 3 Nov; sync service not installed (sidebar and card); no confirmed commit; preview reviewed; step 4 states that sync init creates ctxferry-v1 only if absent and pushed history cannot be recalled; Copy command primary, Refresh quiet | 18 (changed) |
| 19 | Repository: resume first publication | Sync | snapSetupLost; first push lost its acknowledgement; init journal pending; original preview ID reused; Copy command primary, Refresh quiet (was primary) | 19 (unchanged) |
| 20 | Repository: routine sync via CLI | Sync | snapMorning 09:02; service stopped; confirmed 5d07b3a; summary line: 3 waiting · Held until Sync runs; one-shot sync handoff with Copy command primary | 20 (unchanged) |
| 21 | Queue: all records, from the Sync tab | Sync | snapNow; All filter; 9 records; summary line: 1 conflict, 2 blocked, 6 waiting, no hold message (the running service publishes); 2f9a6c14 selected with Written / Read back / Published rows; no Back | 21 (unchanged) |
| 22 | Queue: Conflicts filter, from Home | Sync | snapNow; started from Home's Conflicts tile; Back to Home; conflict selected; summary line carries the counts; filter segments carry none | 22 (unchanged) |
| 23 | Queue: Blocked filter, from Home | Sync | snapNow; Back to Home; billing-worker Task e40d9a17 blocked by deleted membership a2c7e910; repair via task update; runbooks/ row shows the whole runbooks/rollback.md link; labelled Copy command, quiet Refresh, Open Task in billing-worker | 23 (unchanged) |
| 24 | Conflict resolution | Sync | snapNow; Back to Queue · Conflicts; description covers keeping a side or replacing the document; Keep local version chosen; remote option named Apply remote deletion; preview against conflict token; keyboard focus on choice | 24 (changed) |
| 25 | Conflict receipt in the restored Conflicts filter | Sync | snapResolved 10:51:07; Conflicts filter restored and empty; badge 1 needs attention (1 blocked); Back to Home; receipt evidence Saved 10:51:06 / Reread 10:51:07 / Published not yet, queued for the running service | 25 (unchanged) |
| 26 | Backup: blocked by an uncertain publication, offline | Sync | snapOffline 11:06; routine journal 3d8f1a62 outcome unknown; Backup blocked; 8 waiting held until the uncertain journal is reconciled; recovery in Repository | 26 (unchanged) |
| 27 | Repository: reconcile an uncertain routine push | Sync | snapOnline 11:12:05; Back to Backup; journal pending; waiting held until reconciled; one-shot sync handoff with Copy command primary | 27 (unchanged) |
| 28 | Repository: journal cleared after refresh | Sync | snapCleared 11:12:41; journal cleared 11:12:40; commit 9a2e7f1 reread; summary line shows 1 blocked; Return to Backup is the primary action in the cleared banner; Back to Backup | 28 (unchanged) |
| 29 | Backup: preflight evidence, scope and plaintext review | Sync | snapCleared; reading order 1 source with preflight evidence, 2 destination, 3 data scope (always included with billing-worker / deploy-preview, revision 3; the one option, local-only data, selected; never included), 4 exposure for that selection, then acknowledgement and write after every option; summary line shows 1 blocked | 29 (changed) |
| 30 | Backup: receipt with destination and manifest | Sync | snapCleared; written 11:14:02 with local-only data; manifest path and catalog digest; never-included list as in 29; evidence Source snapshot read 11:12:41 / Written 11:14:02 / Published nothing; recovery guide carries the commands and flags | 30 (changed) |
| 31 | Compact: Home | Home | snapNow at 1024x680; icon rail with Sync badge 3; evidence strip in the top bar; tiles keep separate counts | 31 (changed) |
| 32 | Compact: Configuration preview with pinned confirm | Projects | snapEdit at 1024x680; same preview as 08; field strip labels effective value, origin (wins) and editing layer; 8 of 14; cross-device effect qualified by Device and Checkout layers; confirm pinned | 32 (unchanged) |
| 33 | Compact: Conflict resolution with pinned confirm | Sync | snapNow at 1024x680; same description, revisions, lines, choices and effects as 24; sides behind a switch; token and confirm pinned | 33 (changed) |
| 34 | Compact: Backup review with pinned write | Sync | snapCleared at 1024x680; same components and reading order as 29; exposure in two groups; acknowledgement and write pinned after every option; the whole review fits above the pinned bar | 34 (changed) |

In the last column, **changed** means the frame's PNG differs from round 9, and **unchanged** means it is byte-identical. The state column is the fixture's own description from `frames.go`.

`go run . -list` prints the same inventory from `fixture/frames.go`, and `frames/manifest.json` records it with checksums.

### Traced journeys (same IDs across frames)

- **Configuration → Queue:**
  - 08 previews removing db.pool_size from Profile staging.
  - 09's receipt shows record 2f9a6c14 waiting at revision 8, and Waiting goes from 5 to 6.
  - 21 selects that record in the all-records Queue ("Removed db.pool_size"; Written "Saved 10:44:52", Published "Not yet").
  - 07, 08 and 32 all state "Showing 8 of 14 fields · fields with a value".
- **Task trust:** 06 and 11 read the same `trustByCheckout` record. The selected Checkout `~/code/atlas-api` shows "1 trusted · 1 to recheck · 1 not trusted" on its card, on the Tasks tile, and under the Task list in 11, where each Task carries its own Trusted, Recheck or Not trusted label.
- **Task → Catalog:**
  - 11's database slot opens 13 (Back "Task deploy-preview").
  - 11's db-password row opens 14, and both cache rows open 15, which are the same three requirements.
- **Conflict:** 04 (Conflicts tile) → 22 (filter Conflicts, Back Home) → 24 (Back "Queue · Conflicts", token a51f7e08) → 25 (Conflicts filter restored, Back Home). Both the conflict and its dependent runbooks/ index move to Waiting, so the counts go from 6/1/2 to 8/0/1.
- **Publication recovery:**
  - 26: Backup is blocked; journal 3d8f1a62 started 11:04:12 with 8 records.
  - 26 and 27: the legend reads "8 waiting, held until the uncertain journal is reconciled".
  - 27: Back to Backup; `ctxferry sync`.
  - 28: the journal is cleared, 9a2e7f1 is reread, and Waiting is 0, so the legend drops its waiting clause.
  - 29: preflight shows the same evidence.
  - 30: the receipt names 9a2e7f1 and the 11:12:41 read.
- **Join:** 01 (Join a Workspace…) → 02 (stage 1: join and subscribe by slug) → 02b (Refresh read the catalog at 10:43:31; atlas-api resolves to 9f3a7c21-6b48-4e0d-b5a2-1c7e84d39f60, which the bind command names). 02b's confirmed commit 8c41d2e at 10:31 is the one every later frame shows.
- **First publication:** 18 (preview 7c1e93a4, reviewed) → 19 (same preview ID reused to resolve init journal e93b0c57).
- **Compact twins:** the compact frames use the same snapshots as their standard frames:
  - 31 = 04 (6/1/2, read 10:46:20)
  - 32 = 08 (token 9d04e6b2)
  - 33 = 24 (token a51f7e08, Back "Queue · Conflicts")
  - 34 = 29 (journal cleared 11:12:40, 9a2e7f1 reread 11:12:41, Northwind Labs 5b0e2c91 from /home/dev/.local/share/ctxferry)
  - Each pair renders the same fixture values, not copies of them. See "Responsive parity audit" above.

## Layout and resize rules

- **Window:** designed at 1280×800 DIP; minimum 1024×680.
- **Sidebar:** fixed at 236 DIP (MyGo `Sidebar`).
  - It holds the brand, the Workspace identity block (name, ID, data root; no card), the four destinations, and an evidence footer.
  - The footer shows device and ID, service state, and confirmed commit or outcome. Footer lines wrap rather than truncate.
  - At the compact width it becomes a 56 DIP icon rail, and its evidence moves into a top-bar strip (frames 31–34). Production switches below 1100 DIP of window width.
- **Top bar:** 52 DIP. It holds an optional Back (named origin), Breadcrumbs, the read time (or an amber "Stale" pill) and Refresh.
- **Page:** a vertical `Scroll` on the `Page` tint (`#F8F9FB`) under a white top bar, with 20/24/24/24 padding and a 16 DIP block gap. The order is title, then scope bar or Sync tabs, then content. Subtitles appear only where they state the page's purpose.
- **Master–detail:** a two-track `Grid` (fixed 270–300 DIP list or `Fr` ratios between 0.85:1.15 and 1.2:0.8) with a 16 DIP gutter. Below 1100 DIP of content width the detail stacks under the list.
- **Text:** key/value values wrap. Only labels that are not evidence may be single-line, and `-audit` proves no rendered one-line text is actually cut. Short IDs show the full ID in a tooltip; digests and reason codes that are evidence are shown in full. Commands break between argument groups, never inside an ID.
- **Compact window (1024×680):** the page padding is 16 DIP and the block gap 12. Breadcrumbs deeper than three levels collapse to root › … › current; with a Back button, only the current item is shown. The confirm contract moves into a pinned action bar.
  - Placement may change; content may not. Titles, subtitles, scope bars, counts, IDs, document text and effect wording are the same values as at full width.
  - Two-column bodies keep their columns. A second pair of cards (the conflict's Remote side) moves behind a switch that keeps it selectable.
  - A list folds into a picker that keeps the selection, the displayed range and the "show all" action.
  - A preview with up to three effects lays them out in columns (`previewStrip`); a longer one keeps the row table.
- **Sidebar footer:** device, service, and confirmed commit or publication outcome. The read time appears only in the top bar.
- **Sync badge:** "n need attention" (conflicts + blocked) in the wide sidebar; a numeric rail badge plus the same phrase as a top-bar key at the compact width; a legend beside the Sync title on tabbed Sync pages. The legend omits zero clauses, and its waiting clause comes from `snap.waitingHold()`, the same state that locks actions.
- **Fit:** every frame fits its window (1280×800, or 1024×680 for 31–34) without scrolling (`-check`).

## Typography

The UI face is **Inter**. IDs, paths, commands, digests and field names use **JetBrains Mono**. Both are vendored variable fonts registered with `ui.RegisterFont`.

| Role | Size / weight | Use |
| --- | --- | --- |
| Title | 26 / 680, −0.5 tracking | Page title |
| Subtitle | 14 / 400, line height 1.45, Muted | Page purpose |
| Section | 16 / 650, −0.15 tracking | Card and panel titles |
| Body | 13 / 400, line height 1.45 | Text, table cells, controls |
| Strong | 13 / 600 | Row titles |
| Meta | 12 / 400, Muted or Subtle | Secondary lines, timestamps |
| Overline | 11 / 600, +0.6 tracking, uppercase, Subtle | Group labels (scope labels are sentence-case Meta) |
| Mono | 12 / 400 (11 Subtle for short IDs, no box) | IDs, paths, commands, digests |
| Stat | 26 / 650, tabular figures | Home counts |

## Tokens

All colours live in `fixture/tokens.go`. The MyGo theme is derived from them (`LightTheme().WithAccent`, `Radius` 6, `Spacing` 4, `FontSize` 13, `Font` Inter).

| Token | Value | Token | Value |
| --- | --- | --- | --- |
| Canvas (sidebar) | `#F3F4F6` | Accent | `#0B6B83` (hover `#08586C`, soft `#E3F1F4`) |
| Page (round 7) | `#F8F9FB` | | |
| Surface (top bar, cards) | `#FFFFFF` | Waiting | `#4353B8` on `#ECEEFB` |
| Sunken | `#F7F8FA` | Conflict / attention | `#94480A` on `#FDF1E1` (round 1: `#A2550A`) |
| Border / strong | `#E2E5E9` / `#C9CED6` | Blocked | `#B42318` on `#FDECEA` |
| Text | `#15181D` | OK / verified | `#136B41` on `#E6F4EC` (round 1: `#17784A`) |
| Muted | `#4A5563` (round 1: `#55606E`) | Unknown | `#525A66` on `#EEF0F3` (round 1: `#5F6773`) |
| Subtle | `#5B6573` (round 1: `#7D8794`) | Command prompt | `#94A0AE` on `#11161C` |
| Focus ring | `#1A8FB0`, 2 DIP, offset 2 | | |

- **Spacing:** a 4 DIP grid (4, 8, 12, 16, 20, 24, 32).
- **Radii:** 8 for cards, 6 for controls, 4 for chips, pill for status.
- **Borders and depth:** 1 DIP borders, no shadows.
- **Selected and preview states:** a selected card uses a 1.5 DIP accent border. A preview uses a dashed 1.5 DIP border.

## Icons

- **Style:** 24×24 outline SVGs, 1.8 stroke, round caps, drawn with `currentColor` through `ui.Icon`. Size is 13–16 DIP inline.
- **Source:** Lucide vocabulary (ISC) redrawn in `fixture/icons.go`, plus a custom ferry mark.
- **One meaning, one icon:**
  - Statuses: Waiting = clock, Conflict = merge, Blocked = ban, Verified/OK = check-circle, Unknown = dashed circle, Attention = triangle.
  - Objects and destinations: Home = house, Projects = folder, Catalog = grid, Sync = refresh arrows, Configuration = sliders, Context = file, Tasks = list-checks, Resource = server, Credential = key, Skill = bolt, Artifact = package.
  - Actions: CLI = terminal, Lock = lock.
- **Check marks are evidence:** a check or check-circle appears only beside a fact that was verified (saved, reread, reachable, bytes verified). Capability lists use the icon of the object they touch (frame 01) or a neutral bullet (frame 12). An unmet prerequisite uses the dashed pending icon.
- **Accessibility:** icons are decorative unless they stand alone. Icon-only controls carry `Label` and `Tooltip`, for example "Copy command". Status icons that stand without text in a row carry a `Label`: queue rows ("Waiting", "Conflict", "Blocked"), lock rows ("Locked") and preflight checks ("Passed").

## Focus and keyboard rules

- **Focus ring:** theme ring, 2 DIP `#1A8FB0`, offset 2, shown for keyboard focus only.
- **Frame 24:** captured after 7 Tab presses. This was re-probed after the round 8 changes and still lands on the "Keep local version" radio, as the PNG shows.
- **Tab order:** sidebar (one stop, arrows move between destinations), Back, breadcrumb links, Refresh, then page content in reading order. Segmented controls, Tabs and radio groups are one stop each.
- **Confirm buttons:** in previews the confirm button is last and never the Enter default when focus is elsewhere.
- **Selection:** an accent-soft fill plus a 3 DIP accent bar, exposed as `AccessSelected`.
- **Disabled actions:** each says why, for example "Preview mapping…" ("Available once slot cache is mapped and reread"), "Check entry" and "Update to v1.4.0".

## Component mapping (MyGo `ui` → design role)

| Design element | MyGo widget(s) |
| --- | --- |
| Navigation | `ui.Sidebar`, `SidebarSection`, `SidebarItem` + `ui.Badge` with `Label` and `Tooltip` from `attentionDetail()` ("1 conflict · 2 blocked · 6 waiting, sent by the running service") |
| Compact rail | `ui.ButtonBase` icon buttons with `Label`, `Tooltip` and `AccessSelected`; count badge as an absolute `ui.Box` |
| Pinned action bar | `ui.Row` after the page `ui.Scroll` (labelled "Page content" for the slice check), `Shrink(0)`, top border; never overlays content |
| Attention badge and key | `ui.Badge` with `Label`/`Tooltip` (sidebar); `ui.Row` pill with `ui.Icon` alert (compact key, off on Sync pages); text from `snap.attentionText()` / `attentionDetail()` |
| Sync summary line | `syncSummary`: a `ui.Row` with `Label("Sync summary")` on the title row (`AlignItems(End)` with the title); per group `ui.Icon` + bold toned count `ui.Text` + label; a 1×16 `BorderStrong` hairline `ui.Box` before the muted hold message from `snap.waitingHold()` |
| Blocked-record pair | `recordCell` ×2 in a `ui.Row` with `Dividers`: overline, name, ID chip, kind and state |
| Dependent row | `dependentRow`: record row plus an indented `ui.Column` (start-aligned): muted "Waits on the conflict on", then a standalone link `ui.ButtonBase` with the full mono path, an arrow icon and an explicit `Label` |
| Task trust | `taskRow` with a trust pill from `trustState.label()` (Trusted / Recheck / Not trusted) + `trustSummary()` line; the same summary on Checkout cards and the Tasks tile |
| Compact field strip | Four labelled cells in a `ui.Row` with `Dividers` (`ui.Select`, mono value, origin + Wins pill, editing layer) + `listFooter` |
| List footer | `listFooter`: displayed range + actions. Queue pager (`ui.Button` Previous / Next, `Disabled`, `Tooltip`); "Load 7 older" (`ui.Button` with icon); "Show all 14" (link) |
| Confirmation contract | `confirmSpec` → `confirmFooter` (panel) or `confirmBar` (pinned action bar): `ui.Icon` key, `ui.Text`, ID chip, `ui.Button`, `ui.PrimaryButton` |
| Compact field picker | `ui.Select` with the mono font and an accessible `Label("Field")` |
| Backup route | `backupRoute`: two `ui.Column` cells in a `ui.Row` with `Dividers`: step 1 source (overline, icon, name, ID chip, mono data root, passed preflight line) and step 2 destination (`ui.TextInput` with `Label`, `ui.Button`, validation) |
| Backup scope and exposure | `backupScope`: card with a `ui.Grid` of three columns (Always included with `scopeLine` rows, Optional with one `ui.Checkbox`, Never included with `icX` rows). `exposurePanel(cols)`: tinted `ui.Column`, two labelled groups in a `ui.Grid`. `backupConfirm`: acknowledgement `ui.Checkbox`, Cancel and the write `ui.PrimaryButton`, inline (wide) or in the pinned bar (compact) |
| Location trail / Back | `ui.Breadcrumbs`; `ui.Button` with arrow icon and origin label |
| Sync sections | `ui.Tabs` (Repository / Queue · n / Backup); n = `snap.total()` |
| Filters | `ui.Segmented` (names only; counts live in the summary line or group headings), `ui.Select`, `ui.SearchField` |
| Workspace choice | `ui.RadioGroup` + `ui.RadioBase` tiles (frame 02), choice rows (frame 24) |
| Lists | `listRow` (Row + selection bar, `AccessSelected`) |
| Key/value | `ui.Grid` with `ColumnTracks(Fixed(136), Fr(1))` |
| Scope line | Unboxed `ui.Row` with 1×18 hairline separators and a bottom border: sentence-case Meta label, `ui.Icon`, value, short ID |
| Preflight strip | `ui.Row` with `Dividers`, overline + value cells |
| Cards, banners, preview, receipt (receipt = heading, key/value, `evidenceStrip`, actions) | `ui.Column` / `ui.Row` with `Background`, `Border`, `BorderStyle(BorderDashed)`, `Radius`, `Clip` |
| Status pill, tag, ID | Pill: `ui.Row` + `ui.Icon` + `ui.Text`. Tag: soft fill, no border. ID: subtle mono `ui.Text` in a `ui.Row` with a `Tooltip` holding the full ID |
| CLI handoff | `cliHandoff` card: prerequisites; `cmdBlock`: "Run in a terminal" + labelled `ui.PrimaryButton` **Copy command** (accessible name includes the command) above a dark `ui.Row` with mono `ui.Text`; `afterRun`: hairline, "After it finishes, Refresh reads …" and a quiet `ui.ButtonBase` Refresh |
| Evidence | `evidenceStrip` (three cells in a `ui.Row` with `Dividers`) or `evidenceList` (stacked rows): WRITTEN, READ BACK, PUBLISHED, each with an icon, a toned value and a muted detail. When a write came from an earlier read (backup), the strip is SOURCE SNAPSHOT, WRITTEN, PUBLISHED |
| Join stages | `stageChip` ×2 (step number or check, label, state) joined by a hairline `ui.Box` |
| Actions | `ui.Button`, `ui.PrimaryButton`, `ui.ButtonBase` (links), `Disabled`, `Tooltip` |
| Inputs | `ui.TextInput`, `ui.Field`, `ui.Checkbox` |
| Diff | Native tinted rows (frame 16); `editor.DiffView` is the production candidate |
| Deleted side | `Stripes` |
| Contact sheets | `ui.Image` + `ui.DecodeBitmapScaled`, rendered by `ui.Render` |

## Reproducing the images

Requirements: Go 1.27.4+ (tested with go1.27.4 linux/amd64). No cgo, browser or GPU is needed for PNG rendering.

```sh
cd /tmp/ctxferry-desktop-council.OIIH4Aca/round-10/fixture
go run . -contrast                                   # WCAG ratios; exits non-zero below 4.5:1
go run . -check                                      # no text beyond each window, no line sliced by the page edge or pinned bar
go run . -audit                                      # no one-line text cut with an ellipsis; exits non-zero if any
go run . -out ../frames                              # 36 PNGs at 2x + frames/manifest.json
go run . -out ../frames -contact ../contact-sheets   # 4 contact sheets
go run . -list                                       # inventory
go run . -out ../frames -only 24                     # one frame
go run . -window 32                                  # one frame in a native MyGo window (compact size)
cd .. && sha256sum -c SHA256SUMS                     # verify the delivered files
```

PNG encoding is deterministic for a given Go toolchain, fixture and font files. On a matching toolchain, rerunning the commands reproduces the checksums in `frames/manifest.json`.

**Pinned dependencies (unchanged from round 1):**

- `github.com/egoist/mygo v0.10.0` (`h1:HpLUrlh6WES+4VYbz5K0TvUSw/v8PSdjT/ttiUYrBFM=`), the same version `cmd/ctxferry-desktop` pins.
- `go-text/typesetting v0.3.5`, `purego v0.11.1`, `x/image v0.46.0`, `x/net v0.59.0`, `x/text v0.43.0`.
- `go.sum` is included.

**Vendored fonts** (OFL 1.1; licences in `fixture/fonts/OFL-*.txt`). Both are from `google/fonts` `main`, fetched 2026-11-20 via cdn.jsdelivr.net.

- `Inter-Variable.ttf`: SHA-256 `29160a80ff49ddcab2c97711247e08b1fab27a484a329ce8b813d820dc559031`
- `JetBrainsMono-Variable.ttf`: SHA-256 `48715a42ec242c21e9f02692891e147d022299a52e48d5e413e1a942193ffeda`

## Capture provenance and verification

- **Renderer:** MyGo's headless native renderer.
  - Each frame runs in `ui.NewTesterWith(view, w, h, …)` with `SetScale(2)`. The window is 1280×800, or 1024×680 for 31–34. Frame 24 also receives 7 Tab keys.
  - Then `Frame()` and `Image()` produce the bitmap, which Go's `image/png` encodes.
  - This is the same `ui` layout, text shaping and painter a MyGo window uses. No web technology is involved.
- **Native window smoke test (round 10):** under WSLg (Linux 5.15 WSL2), `-window 29` and `-window 34` each opened a MyGo native window and kept running until a 15 s `timeout` stopped it (exit 124). Mesa fell back to llvmpipe, and MyGo logged "native UI draws without the GPU". The PNGs come from the offscreen renderer, not screen capture.
- **Checks run on the delivered fixture:**
  - `gofmt -l .` reports nothing, and `go vet ./...` is clean.
  - `go run . -contrast` gives 45 pairs, all ≥ 4.5:1 (lowest: Subtle on the selected row, 5.11). New this round: Subtle group overlines on the exposure tint (5.30).
  - `go run . -check` reports 36 frames, 0 text boxes beyond their windows, and 0 lines sliced by the page edge or a pinned bar.
  - `go run . -audit` reports 36 frames and 0 one-line texts cut with an ellipsis.
- **Visual inspection:** these frames were inspected at native size: 18, 24, 29, 30, 33 and 34 (both compact frames at full 2048×1360 resolution). All 36 were also checked on the contact sheets. Issues found and fixed during round 10:
  - in 34, the exposure list read across rows and mixed shared and local-only items; it is now two labelled groups, and the two preflight lines became one to keep the fit
  - in 29, the local-only items were spread over three columns; they now use two
  - `TextInput`, `Select` and `Breadcrumbs`, which `-audit` cannot see into, were checked by eye; none is cut
- **Determinism:** a second render of all 36 frames into a separate directory reproduced every PNG byte for byte.
- **Focus:** frame 24 still receives 7 Tab keys, and its PNG shows the ring on "Keep local version".
- **Data:** synthetic, defined in `fixture/data.go`. No production code, real Workspace or data root was read or written; `internal/cli`, `internal/app/workspace_backup.go`, `workspace_artifacts.go`, `desktop_skills.go` and `internal/store/store.go` were only read, to verify command spellings and exactly what a backup contains. Nothing was committed.

## Limitations and open questions

- **The summary line has no next action.** Each page carries its own. Frames 18 and 19 have no confirmed commit, so they show no summary; their first-publication card is the next action. An uncertain publication is stated by the banner and the hold message, not counted, because it is a state of the Workspace, not of a record.
- **Backup read-back is not offered.** The CLI does not compare a backup with its source. If a verify step is added later, the receipt would add a READ BACK cell after WRITTEN.
- **Join stage 2 has one path.** 02b shows the bind after a successful Refresh. A Refresh that cannot resolve a slug (unknown or no longer subscribed) is not rendered; it would keep stage 2 locked and name the slug.
- **Light theme only.** The compact window is rendered for four workflows (31–34). The other views follow the same rules but are rendered only at 1280×800.
- **Readiness variants not rendered:** migrate an older catalog, update for a newer schema, and unreadable catalog. They reuse the frame 01 panel with one recovery action each.
- **Other states not rendered:**
  - the Profile-change preview on Project overview
  - the Skills rollback and interrupted-install flows
  - Resources for a remote environment
  - the unsaved-input prompt, the operation-running navigation lock, and empty states
- **The cache-auth resolution is shown in one state.** Frame 15 shows it before step 1. The mapped-slot state and the reference preview would reuse the preview and receipt panels of frames 08 and 09.
- **`map-resource` UUID:** the command uses the full Resource UUID because the CLI argument is `<resource-uuid>`. If the CLI later accepts a Resource name, the command should use it.
- **Task repair in frame 23** is a CLI handoff. The trust re-enable that follows a membership change, and the refreshed Queue, are not rendered.
- **Trust transitions:** frames 06 and 11 show the three trust states at rest. The Recheck flow after `task trust preview` and the state after re-enabling are not rendered.
- **Ellipsis audit scope:** `-audit` covers every `ui.Text` the fixture marks single-line. Text inside `TextInput`, `Select` and `Breadcrumbs` is laid out by MyGo and was checked by eye only.
- **Load older in frame 17** appends to the same list. The appended state (1–12 of 12, button gone) is not rendered.
- **Show all 14 in frames 07 and 32** reveals unset fields as "Not set" rows. That state is not rendered.
- **Join form parsing:** frame 02 shows comma-separated slugs mapping to repeated `--subscribe` flags. Validation messages for an unknown slug or a non-directory path are not rendered.
- **14b reuses frame 13's route and probe.** Only the origin differs. The return to 14 with db-password selected is described, not rendered as a separate frame.
- **Backup with local-only data cleared is not rendered.** The exposure keeps the "Every backup" group and labels the second group "Local-only data · not selected"; the fixture supports it through `backupLocal`.
- **Counts in the exposure are synthetic** but consistent with the other frames: 23 shared Context documents (18), 3 subscribed Projects (05), 3 Skills (16), 4 Checkouts (04), 1 tunnel route (13), 1.2 GB of artifacts (17).
- **Static frames:** widgets are not wired to behaviour. Icons approximate Lucide, and the diff in frame 16 is hand-built.
- **Sidebar selection colour** is MyGo's inactive grey while the sidebar is unfocused; this is native behaviour.
