---
title: Native desktop
---

CtxFerry desktop opens the same local Workspace as the CLI, using native MyGo widgets only. Home answers what needs attention, Projects explain what a Project uses on this device, Catalog holds cross-Project tools, and Sync shows what can travel to another device.

## Start the app

Use a desktop session supported by MyGo Native UI and a Go toolchain compatible with `cmd/ctxferry-desktop/go.mod`:

```sh
cd cmd/ctxferry-desktop
go run .
```

The window starts at 1280×800 and supports a compact 1024×680 layout. The compact layout uses an icon rail and keeps Configuration, Conflict and Backup confirmations in a pinned action bar outside the scrolling page.

To inspect an isolated Workspace, set the same data root for the CLI and desktop:

```sh
export CTXFERRY_DATA_DIR=/path/to/isolated-ctxferry
ctxferry init
cd /path/to/CtxFerry/cmd/ctxferry-desktop
go run .
```

The desktop honors `CTXFERRY_DATA_DIR`; passing `--data-dir` only to a CLI command does not change the desktop location. Every CLI handoff shown by the app includes the current root explicitly.

## Open or create a Workspace

The readiness gate distinguishes a fresh root, a catalog needing migration, a newer schema and an unreadable catalog.

- **Create Workspace** previews the target and initializes the local catalog only after confirmation. No remote is configured and nothing is uploaded.
- **Join a Workspace…** generates a `ctxferry join` command with optional Project subscriptions. Run it in a terminal and choose Refresh. Bind a Checkout afterward from Projects, using the resolved Project ID.
- For migration or diagnostics, use the displayed CLI handoff and then Refresh. The desktop does not migrate a catalog implicitly or expose raw database diagnostics.

After a failed refresh, the last successful snapshot remains visible but stale. Writes, Backup and remote checks stay locked until a successful reread.

## Home

Home separates **Waiting**, **Conflicts**, and **Blocked** counts. A conflict is not also counted as blocked-only or waiting. Each tile opens Sync → Queue with the matching filter.

The Bound Projects card shows real local directories and flags unavailable paths. This device shows catalog compatibility, local read time, sync service state and the last confirmed commit separately from provider or publication claims. An empty Workspace includes a real `ctxferry project bind` handoff.

## Projects and Configuration

Projects are grouped by subscription and then name. An unsubscribed Project retains its identity and offers `ctxferry project subscribe`; the app never substitutes another writable target.

A subscribed Project overview lists its local Checkouts. Select the exact Checkout before changing its saved Profile. A Profile change shows the affected Checkout and resulting scope before saving, then reloads Configuration, Context and Task trust.

Configuration provides:

- effective values and their origins;
- ordered Workspace-default, Project, Profile, Device and Checkout contributions;
- an explicit writable layer and its shared or local-only scope;
- previews of affected local Checkouts, retained or inherited values, and publication effects;
- a read-only preview ID bound to the core's stale-write check;
- a Written / Read back / Published receipt.

The first three layers are shared. Device and Checkout layers stay on each device, so another device can override a shared value locally in ways this device cannot see. A shared write is queued locally until a confirmed commit includes it.

Context keeps the same Checkout and resolved Profile for listing, opening and pagination. Tasks resolve Resource slots through that Profile. A Task shows the mapped Resource, exact TCP target and latest target-specific observation time separately from provider authentication and execution trust. Task → Resource and Task → Credential links preserve the calling scope, and Back restores it.

## Catalog

Catalog links four tools with an explicit subscribed-Project scope:

- **Resources & environments:** configured endpoints, local overrides, per-environment routes and observations, and confirmed TCP probes. Unknown evidence remains unknown, especially for another device.
- **Credential bindings:** Project, Profile, Task and requirement scope, compatible shared references, provider locators, and separate authentication, entry-readability and reachability checks. Secret values never enter drafts, results or copied commands.
- **Skills:** approved pins, installed receipts, candidate selection, install/update/rollback/recovery previews, and per-file diffs. A drifted file offers both installed-to-local drift and installed-to-candidate comparisons. Drift blocks confirmation until reconciled and rescanned.
- **Run artifacts:** run/source/content filters, provenance, full digests, content availability and metadata-only explanations. Available bytes can be verified and exported to a new file.

The device-wide Skill catalog remains available without a Project. Related links from a Project supply its ID automatically, and Back restores the originating Project section and selection.

## Sync and publication recovery

Sync has three sections:

- **Repository** shows the remote, branch, confirmed commit, history size, service and latest reachability/privacy check. Routine synchronization is an explicit `ctxferry sync` handoff.
- **Queue** filters All, Conflicts, Blocked and Waiting records by stable record ID. Inspect revision evidence, blocking reasons, and Written / Read back / Published state. Conflicts open comparison.
- **Backup** reviews source/preflight evidence, a new absolute destination, fixed data scope, optional local-only data, plaintext exposure and acknowledgement before writing.

Before first publication, check that the remote is reachable and private, then choose **Review export…**. The app calls the core's real preview operation and displays its paths, sizes and SHA-256 digests. Inspect original shared documents and artifact payloads separately for pasted secrets. The final `ctxferry sync init --preview <id>` handoff creates the configured branch only if absent; pushed history cannot be recalled.

If first publication loses its acknowledgement, Repository reuses the original preview ID. For an uncertain routine push, it offers one-shot reconciliation with `ctxferry sync`. Backup and Conflict stay locked until Refresh confirms the journal cleared and rereads the confirmed commit.

Conflict resolution compares base, local and remote evidence. Keep a whole side, apply a deletion, or replace eligible Context content. Confirmation names the consequence and checks the current conflict token. A stale comparison must be reloaded and chosen again. The receipt identifies the new local revision, not a published result.

## Backup contents and recovery

Every backup includes the Workspace and shared catalog records, shared Context and Configuration, Skill pins and sources, queue state, and confirmed Git history when present. Local-only bindings, overrides, routes, observations, subscriptions, run records and available artifact bytes are included only with the single local-only option.

Provider-held credential values, provider sessions, Git authentication and execution trust are never included. Skill installation receipts and installed Skill files are also excluded.

Backups are plaintext. Context, Configuration, Skill sources and Git history can contain embedded secrets, so review the original content rather than relying on a manifest. The receipt identifies the destination, `manifest.json`, catalog SHA-256, included scope and source snapshot. [Recovery](recovery.md) provides the restore commands and required flags.

## Validation and native capture

Run the repository and desktop suites:

```sh
go test ./...
cd cmd/ctxferry-desktop
go test ./...
go test -tags ctxferry_test ./...
```

The native capture scenario creates isolated data through real CLI/core operations and drives running MyGo windows at both supported sizes:

```sh
python3 capture_redesign.py /tmp/ctxferry-redesign-<revision> --revision <full-sha>
```

The final implementation review, data state, 22 production frames and independent K3.1 Pro findings are recorded in [Native desktop redesign implementation review](../development/desktop/review-native-redesign.md).
