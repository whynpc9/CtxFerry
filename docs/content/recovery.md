---
title: Conflict resolution and recovery
---

Recovery preserves local work and append-only shared history. Choose the
operation that matches the scope of the problem; do not copy an active SQLite
catalog between devices or reset the sync branch.

| Situation | Operation |
| --- | --- |
| Two devices changed the same record | Inspect and resolve the record conflict |
| Remove a shared document or restore its earlier content | Create a new deletion or restoration revision |
| Recreate the backed-up device's unpublished and local state | `workspace recover-local` into a new data directory |
| Roll back the whole published Workspace to a backup | `workspace restore` with a new epoch |

## Resolve a record conflict

Start with local status. It does not contact the network:

```sh
ctxferry sync status --format json
```

Each entry in `data.conflicts` identifies the record `id`, the exact
`conflictId` to authorize a resolution, and the observed `head`. Inspect the
preserved versions before choosing one:

```sh
ctxferry sync conflicts <record-uuid> --format json
```

The result contains `base`, `local`, and `remote` envelopes, including revisions,
provenance devices, policies, and content allowed by disclosure. Context bodies
appear only for full disclosure; restricted modes continue to hide bodies and
summaries as their policies require. Conflict evidence and referenced immutable
objects stay in the local database and are not exported as evidence records.

Choose one side against exactly the conflict you inspected:

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --take local --format json
```

Or choose the remote version:

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --take remote --format json
```

Every choice creates a fresh revision and operation ID, even when the chosen
content is identical. The result is queued locally with `state: "pending"`;
publish it with:

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

A changed conflict token returns `stale-conflict`; inspect the latest conflict
and choose again. Ordinary edits to the conflicted record are refused until it
is resolved. A later remote edit to the resolved revision can create another
conflict.

For a Context document, you can instead supply a strict replacement JSON file.
This flow currently supports Context only, not arbitrary record types:

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --file resolution.json --format json
```

The file contains content and policy, never identity or revision. For example:

```json
{
  "title": "Notes",
  "purpose": "notes",
  "source": "manual resolution",
  "body": "New Markdown content",
  "summary": "Approved summary",
  "policy": {"storage": "shared", "agentDisclosure": "full"},
  "deleted": false
}
```

The resolution must remain shared. Security-relevant references, purposes,
execution arguments, sensitivity, and Skill pins always require an explicit
choice; clocks and Git text merges do not choose a winner.

Unrelated records continue to sync. Pending records connected by Project,
Repository, Task, Resource, credential, configuration, or Skill dependencies
form publication units, so a conflicted member can hold its dependent records.
`data.blocked` in sync status names the record and reason. A Project slug or
Project-summary uniqueness collision likewise keeps the incompatible pending
projection visible without violating local catalog constraints.

## Delete or restore one shared document

These operations require configured sync and a shared Context document. To
delete at the current revision, run:

```sh
ctxferry context delete <document-uuid> --if-revision <current-revision> \
  --format json
ctxferry sync --format json
```

Deletion creates a tombstone retained in the shared catalog. `get`, `show`, and
`search` omit it. An unchanged offline device adopts the deletion on its next
sync; an edit-versus-delete conflict requires explicit resolution. The plaintext
body remains in Git history.

To restore the same record from an earlier validated ancestor of confirmed
history, use a full commit SHA and the record's **current** revision, including
the current tombstone revision when restoring a deletion:

```sh
ctxferry context restore <document-uuid> --if-revision <current-revision> \
  --from-commit <full-commit-sha> --format json
ctxferry sync --format json
```

Use validated Git history to locate the historical commit and current tombstone
revision. The historical snapshot must contain the same live record and scope.
Restoration queues a fresh revision through routine sync; it never resets the
branch. Local-only document deletion is outside this command.

## Back up published and unpublished state

Choose a new directory in a protected location. Backups are plaintext. If a
publication has an uncertain pending result, first run `ctxferry sync` so the
journal can be reconciled.

```sh
ctxferry workspace backup --to /protected/ctxferry-backup --format json
```

The command works offline. It writes a consistent standalone SQLite snapshot of
shared records, unpublished edits and their original bases, conflict evidence,
and referenced objects, plus a Git bundle of the last locally confirmed
published history when one exists. Credential-free sync configuration and
Workspace and Device IDs are retained.

To include this device's local-only content and settings, select them
explicitly with a different new destination:

```sh
ctxferry workspace backup --to /protected/ctxferry-device-backup \
  --include-local-only --format json
```

This adds local-only records, checkout bindings, local configuration, Profile
selections, subscriptions, Resource routes and Observations, and local artifact
manifests and verified payloads. Both modes exclude credential stores, provider
sessions, Git authentication, and execution trust. Secret references and
user-authored prose, provenance, or artifact payloads can still contain sensitive
material; review and protect the backup accordingly.

A completed backup contains `manifest.json` with component checksums,
`catalog.sqlite`, `published.bundle` when published history exists, and an
`artifacts/` directory when selected local payloads are included. Keep the
directory together. The manifest is the completion marker: a failed backup may
leave an incomplete directory that recovery rejects.

## Recover one device's local state

Use local recovery to recreate the saved device's offline state without
publishing a Workspace rollback. Choose a new application data directory and
pass it explicitly:

```sh
ctxferry --data-dir /protected/recovered-device workspace recover-local \
  --from /protected/ctxferry-device-backup --format json
```

The destination must not already exist. Recovery verifies the manifest,
checksums, catalog integrity, and any published bundle before making the new
catalog visible. It preserves the original Device ID and pending queue. Use the
recovered directory through `--data-dir` or `CTXFERRY_DATA_DIR` in place of the
original device's data root.

Backed-up absolute checkout paths and local settings may need adjustment on new
hardware. Authenticate Git and each credential provider again on the recovered
installation; authentication and execution trust were not backed up.

## Restore the shared Workspace under a new epoch

Shared restore is intentionally different from local recovery. It publishes an
older backup as a new head and a new history generation, visible to every
syncing device. Use it only when the whole shared Workspace should roll back.

Prerequisites are a backup containing published history, a configured device in
the same Workspace, and current Git and GitHub authentication. First catch up
and inspect the current confirmed head:

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

Copy the full current `data.head`, then request the restore:

```sh
ctxferry workspace restore --from /protected/ctxferry-backup \
  --new-epoch --if-head <current-full-commit-sha> --format json
ctxferry sync --format json
```

The restore verifies that the backup belongs to this Workspace and lies in its
published history, rechecks private repository identity, and requires the remote
head to equal `--if-head`. A concurrent publication returns `stale-head`;
inspect the new head before deciding again. History rewriting stops the
operation rather than being overwritten.

A successful restore appends a commit under a new epoch. Records added after
the backup become tombstones; old history remains present. To include the
backup device's unpublished edits or local-only state in the restoring view,
perform `recover-local` first and run `workspace restore` with that recovered
`--data-dir`.

Other devices retain their existing outboxes. On the next sync, each compares
queued edits with restored records using the original bases. Compatible edits
can publish in the new epoch; incompatible ones become explicit conflicts for
the procedure above. A lost restore response can be reconciled with `sync`, or
by retrying the same restore while its matching publication journal is pending.
Use an epoch-aware CLI on every syncing device.

Revoking a lost device's Git or provider access does not erase plaintext it
already downloaded or values already delivered to processes. Rotate credentials
that were actually exposed in addition to revoking access.
