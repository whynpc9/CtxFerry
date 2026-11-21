---
title: Diagnostics and sync status
---

Start with local evidence, then choose only the network or provider check that
matches the failed capability. A local record, a successful TCP connection, a
provider login, and a successful Task are separate facts.

## Read doctor without changing the Workspace

From the checkout you want to diagnose, run:

```sh
ctxferry doctor --format json
```

`doctor` works offline and does not initialize a missing catalog, migrate a
catalog, probe Resources, or perform live Git or provider authentication. It
returns a report successfully even when individual capabilities are missing or
unknown, so inspect each check rather than treating exit code 0 as an overall
health verdict. Diagnostics never dump the full environment.

The report's `data.observedAt`, `os`, and `arch` identify this observation. Use
these fields to choose a next step:

| Field | What it reports | What it does not prove |
| --- | --- | --- |
| `cli` | This CLI is running | That any other capability works |
| `catalog` | The local catalog was read, is absent, needs migration, or uses an unsupported schema | That the catalog is synchronized |
| `directoryBinding` | Whether the current directory resolves through local binding evidence | That an explicit `--project` binds this directory |
| `pathResolution` | Current directory, Git checkout, local checkout, and effective working-directory resolution | That a Task has run successfully |
| `gitAuth` | Historical sync state and safe local configuration hints | Live Git authentication; its status commonly remains `unknown` |
| `providerAuth` | Each referenced provider profile, stored-session metadata, and scoped headless-session presence | Live provider authentication or entry retrievability |
| `lastSync` | Recorded state, pending count when available, and last successful timestamp | Current remote reachability |
| `credentialStorage` | Whether secure OS credential storage or its safe metadata inspection is available | Permission to read a particular business credential |
| `backgroundSync` | `running`, `installed`, `foreground-only`, or `unavailable` for this store | That a sync has just succeeded |
| `dependencies` | Whether `git`, `gh`, `infisical`, and `bw` resolve on `PATH` | That an optional tool is authenticated |
| `resources` | Local route state and latest recorded TCP reachability | Service protocol or identity; `serviceIdentity` can remain `unknown` |

A successful sync becomes `stale` in `lastSync` after 24 hours, independently of
queued changes and the recorded sync state. A future timestamp is reported as
unknown rather than accepted as fresh. Git mechanism hints report only facts
such as an SSH agent socket or metadata-token environment being configured;
they do not print their values.

For a different Project, `ctxferry doctor --project <uuid-or-slug> --format json`
changes Project-specific evaluation, but `directoryBinding` still describes the
actual current directory. This helps distinguish an explicit selection from a
local checkout binding.

## Read sync status locally

After sync is configured, run:

```sh
ctxferry sync status --format json
```

This command reads local state without contacting the remote. Before
configuration it returns `sync-disabled`; use `doctor` for general local health
and [Private sync](sync.md) to configure a destination.

The important fields are:

- `state`: the last observed outcome, including `enabled`, `synced`, `pending`,
  `conflict`, `offline`, `auth-required`, `unlock-required`, `incomplete`,
  `unsupported-schema`, or `history-rewritten`.
- `pending`: the number of records with queued changes. It is not a count of
  successful remote publications.
- `head` and `epoch`: the last locally confirmed published commit and history
  generation, not a live remote reading.
- `lastSyncAt`: the last successful sync timestamp, distinct from the latest
  failed attempt.
- `conflicts`: active record IDs, conflict tokens, and observed heads.
- `blocked`: records held by a dependency or uniqueness reason.
- `history.rawBytes`: the recorded estimate of distinct reachable raw Git object
  bytes when measured.

`enabled` means a destination was configured but no confirmed head exists yet.
`pending` can mean local edits are queued or a publication result needs
reconciliation. Active conflicts remain visible even when a later attempt is
offline; a more fundamental authentication, history, or catalog failure can
remain the top-level state.

## Choose an active check

Use the narrowest check that supplies new evidence:

| Need | Command | Side effect and result |
| --- | --- | --- |
| Recheck configured Git reachability and authenticated GitHub privacy | `ctxferry sync check --format json` | Network reads only; returns `check.gitReachable`, `check.privacy`, stable identity when available, and `failure` |
| Fetch and publish shared changes | `ctxferry sync --format json` | A bounded sync round; local work is retained on failure |
| Check one exact credential requirement | `ctxferry secret status <task> <requirement> --profile <name> --format json` | Validates mappings and checks this device's provider session and exact entry; returns no value |
| Measure one Resource route | `ctxferry resource check <resource-uuid> --format json` | Opens one TCP connection and appends a device-local Observation |
| Inspect the route without a connection | `ctxferry resource inspect <resource-uuid> --format json` | Reads configured intent, local route, and prior Observations only |

For `sync check`, `gitReachable: true` and `privacy: "private"` are independent
requirements. A successful SSH or HTTPS check alone does not prove privacy.
`privacy: "unknown"` or `failure: "auth-required"` calls for authenticated
GitHub metadata access; do not initialize over an unverified destination.

## Match the state to the next action

| Error or evidence | Meaning | Specific next check or action |
| --- | --- | --- |
| `not-initialized` or unavailable catalog on a new installation | No local Workspace exists at the selected data root | Verify `--data-dir` / `CTXFERRY_DATA_DIR`; run `ctxferry init` only for a genuinely new root |
| Catalog needs migration or has a newer schema | This viewer or CLI cannot safely use the current catalog version | Use a matching current CLI; do not delete or replace the catalog |
| `project-unbound` or unavailable `directoryBinding` | No unique local binding matched | Run `project inspect --format json` for candidates, then `project bind . --project <slug>` deliberately |
| Unavailable `pathResolution.workingDirectory` | The effective path is absent or cannot resolve here | Run `context explain --format json`; correct or unset the named device/checkout override as described in [Context](context.md) |
| `sync-disabled` | No sync destination is configured | Continue locally, or follow [Private sync](sync.md) |
| `enabled` with no confirmed head | First publication has not completed | Review `sync preview`, then explicitly run `sync init --preview <id>` |
| `pending` | Local work or an uncertain publication awaits reconciliation | Run `ctxferry sync`; retrying checks whether an accepted candidate already exists |
| `offline` or `git-error` | Transport or Git operation failed; local work remains | Verify network and system Git access, run `sync check`, then retry `sync` |
| `auth-required` or `unlock-required` for sync | Git/helper access needs authentication or unlock | Restore this device's Git, SSH agent, credential helper, or GitHub metadata authentication, then run `sync check` |
| `authentication-required` for a credential | This exact device/profile has no usable provider session | Follow the provider-specific login in [Credentials](credentials.md), then rerun `secret status` |
| `secret-unavailable` | The exact backend item is missing, denied, or has no usable value | Verify the locator and provider permissions; do not substitute a similar item |
| `binding-missing`, `binding-conflict`, or `incompatible` | The explicit Task/Profile/Resource/SecretBinding chain is absent or mismatched | Inspect the Task and `secret status` details; repair the named mapping in [Credentials](credentials.md) |
| `resource-unreachable` or an unreachable Observation | The current route's latest TCP result is not usable | Inspect `route`, fix this device's endpoint/proxy/network, and rerun `resource check`; no local same-name Resource is substituted |
| `platform-unsupported` | The Resource or Task conditions exclude this OS/architecture | Check the declared platform conditions on the actual Device; Windows and WSL are separate |
| `execution-definition-changed` | An execution-relevant local or synced field changed | Run `task trust preview`, compare the named changes, and enable the exact new preview on this device |
| `conflict` | One or more records have preserved base/local/remote versions | Follow [Resolve a record conflict](recovery.md#resolve-a-record-conflict) |
| `branch-exists` | First initialization found an existing sync branch | Inspect and validate that Workspace; to join it, use a fresh data root rather than overwriting the branch |
| `missing-branch` | Routine sync or join cannot find the configured branch | Verify the repository and branch and restore access; routine sync and join never initialize a missing branch |
| `incomplete` | Remote records or objects are missing, partial, corrupt, or fail validation | Preserve the last confirmed local data; inspect and repair the remote catalog before retrying |
| `unsupported-schema` | This client does not understand the local or remote record schema | Upgrade to a compatible CLI; do not force publication that could drop unknown fields |
| `history-rewritten` | The remote head no longer extends confirmed history | Stop automatic publication, preserve local work, and inspect remote history; do not force publication or assume a restore can bypass the ancestry check |
| `store-busy` or `sync-busy` | Another local writer or sync owner is active | Retry, or use explicit `sync` to wait for the existing driver; do not run a second service for the same store |

The [native desktop](desktop.md) shows local evidence on Home. Choose **Sync → Repository → Check remote** to check Git reachability and repository privacy.
Its refresh button does not perform any of the active checks above. When its
snapshot is unavailable, `doctor` is the next local diagnostic.
