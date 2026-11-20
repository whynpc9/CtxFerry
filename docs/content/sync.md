---
title: Private sync across devices
---

Local initialization and editing remain offline. Sync is an explicit additional
step that publishes shared records to a dedicated private GitHub repository in
plaintext, then retrieves and validates the same catalog on another device.

## Review the plaintext boundary

Use a dedicated private repository, not a business source repository. Anyone or
any device that can read the sync repository can read the whole published
workspace, regardless of Project subscriptions. Private repository access is
not end-to-end content encryption.

The typed export includes shared Projects, Repository metadata and memberships,
ordinary shared configuration, Context documents, Resources, Tasks, credential
references, shared Skill pins and groups, and explicitly approved artifact
records and objects. In particular:

- A shared Context document publishes its **complete body**, even when its agent
  disclosure is summary, reference, or local-tool.
- SecretBindings contain provider and locator metadata, never the credential
  value or provider session.
- Local-only records, checkout paths and bindings, device routes, TCP
  Observations, Profile selections, execution trust, SQLite files, indexes,
  credential stores, and authentication material are excluded.
- Git history retains published plaintext, including content later deleted from
  the current snapshot.

Scanning and allow-lists cannot detect every credential pasted into prose or an
artifact. Inspect the original shared material before publishing. Content that
cannot be hosted by GitHub must remain local-only.

## Enable and verify a private destination

Prerequisites are an initialized first device, an empty dedicated private
GitHub repository, and working system Git authentication through SSH or a
credential helper. The authenticated GitHub metadata read uses `GH_TOKEN`,
`GITHUB_TOKEN`, or `gh auth token --hostname github.com`. A fine-grained token
limited to the sync repository is preferable to a broad credential. CtxFerry
uses the token for the check but does not save it.

Run:

```sh
ctxferry sync enable \
  --remote git@github.com:owner/private-context.git --format json
```

The default sync branch is `ctxferry-v1`; choose another unused branch with
`--branch <name>`. `enable` uploads nothing. It checks Git reachability and
authenticated repository privacy separately in `data.check`.

Inspect the result before continuing. Only `data.state: "enabled"` with a saved
`data.config` means that Git access, a matching repository name, stable numeric
identity, and a positive private response were verified. `state: "unverified"`
does not authorize configuration or upload. Public, missing, malformed, or
unauthenticated metadata never authorizes the first publication.

Repeat the independent checks later with:

```sh
ctxferry sync check --format json
```

This is a network operation, not a publication. Verification cannot prevent a
repository owner from changing visibility or identity after the check, so each
first-publish attempt repeats it. CtxFerry rejects credential-bearing URLs, URL
rewrites, configuration includes, and alternate push URLs that could disconnect
the verified identity from the actual destination.

## Preview and initialize the first publication

After `enable` succeeds, run:

```sh
ctxferry sync preview --format json
```

The response lists every destination path, byte size, and SHA-256 under
`data.files`, plus `data.config`, `data.boundary`, and `data.previewId`. The
preview deliberately contains hashes and identities rather than restricted
document bodies; inspect the original shared documents and payload files
separately.

The preview ID binds the exact export bytes and destination. Changes to records,
policies, dependencies, or objects require a fresh preview. When the list and
source material are approved, initialize the branch explicitly:

```sh
ctxferry sync init --preview <previewId> --format json
ctxferry sync status --format json
```

`sync init` repeats Git and privacy verification, builds a root commit from the
allow-listed snapshot, and creates the branch with an expected-absent lease. It
does not overwrite an existing branch. A competing or invalid existing workspace
is preserved and reported rather than replaced.

A successful result has `state: "synced"` and the confirmed `head`; `status`
then reads the local confirmation without contacting the network. If the first
push response is lost or interrupted, retry with the same preview ID. CtxFerry
checks whether the candidate already entered valid remote history before
publishing again. A changed snapshot still needs a new preview.

## Join from a second device

Install the CLI and prepare Git and GitHub metadata authentication on the second
device. Its application data directory must be fresh: do not copy the first
device's SQLite catalog, WAL, locks, or managed Git directory.

```sh
ctxferry join git@github.com:owner/private-context.git \
  --subscribe my-project --format json
ctxferry project list --all --format json
```

Use a credential-free HTTPS GitHub URL instead if that is how this device
authenticates. The branch defaults to `ctxferry-v1`; use the same custom branch
with `--branch` when necessary. Repeat `--subscribe <uuid-or-slug>` for several
Projects, or use `--all-projects`. The default subscribes to none.

`join` verifies repository identity and privacy, requires the existing sync
branch, stages and validates the shared catalog, and adopts its Workspace
identity in one transaction with a new local Device ID. A missing branch,
permission failure, 404, or incomplete response is not treated as an empty
workspace.

Subscriptions control local indexing, display, Skill projection, and artifact
access, not repository access. The full validated baseline remains available
for synchronization. Discover and change local subscriptions with:

```sh
ctxferry project list --all
ctxferry project subscribe my-project
ctxferry project unsubscribe my-project
```

Bind this device's actual checkout only after joining:

```sh
cd /path/to/second-device/checkout
ctxferry project bind . --project my-project --format json
ctxferry context show --format json
```

Checkout paths and subscriptions stay local. Provider authentication is a
separate, optional step needed only when a Task must resolve a business
credential; see [Credentials](credentials.md).

## Run routine two-way sync

Shared edits are visible in the local working view immediately and queued for
publication. Run one sync cycle explicitly:

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

A round fetches and validates the remote snapshot, compares record baselines,
applies compatible operations, and appends one commit with an exact expected-head
lease. Unrelated records can proceed while another record is conflicted;
dependent pending records remain in one publication unit. CtxFerry never uses a
Git text merge or history rewrite to settle record conflicts.

`sync status` works offline. It reports the last observed `state`, confirmed
`head`, last successful `lastSyncAt`, queued `pending` count, active `conflicts`,
and `blocked` records with reasons. Access failures preserve local records and
queued edits. A stale timestamp does not make local content unreadable.

Context reads remain immediate by default. To wait for a successful round first,
use:

```sh
ctxferry context show --sync --format json
```

For artifact publication, use the preview-and-approve procedure in
[Skills and Artifacts](skills-and-artifacts.md#share-an-artifact-deliberately).
For conflict resolution, uncertain push outcomes, and failure-specific recovery,
continue to [Recovery](recovery.md) and [Diagnostics](diagnostics.md).

## Optional background sync

Without a service, `ctxferry sync` performs a one-shot cycle. To run a foreground
loop, use:

```sh
ctxferry sync serve
```

To install sync for the current user session, first install the CLI at a stable
executable path, then run:

```sh
ctxferry sync install-service --format json
ctxferry doctor --format json
```

The installer uses a systemd user unit on Linux, including WSL when its user
manager is usable, a LaunchAgent on macOS, or an interactive Task Scheduler job
on Windows. The result and `doctor` distinguish a running service, an installed
but stopped service, foreground-only operation, and an unavailable service
manager. No background capability is claimed when the platform cannot provide
it.

The service catches up at startup, coalesces shared edits after one second of
quiet and at most five seconds from the start of a burst, and checks for remote
changes every 30 seconds while healthy. Transient errors back off up to one
minute. After repairing authentication or catalog state, run `ctxferry sync` to
resume immediately.

Each local store has one sync driver. An explicit sync or a Context query with
`--sync` waits for the existing driver rather than starting a competing network
sync. The service recovers the existing outbox and pending publication journal,
uses the session's noninteractive Git and GitHub authentication, and does not
copy authentication environment variables into its service definition.

Closing the desktop window does not stop an independently installed service.
The service accepts no remote commands and executes no Tasks. First publication
still requires the explicit `sync init --preview` approval above.
