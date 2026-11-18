# First private publish

Local `ctxferry init` remains offline. First publication is explicit:

```sh
ctxferry sync enable --remote git@github.com:owner/private-context.git
ctxferry sync check --format json
ctxferry sync preview --format json
ctxferry sync init --preview <previewId> --format json
ctxferry sync status --format json
```

Use a dedicated private GitHub repository and an unused branch (default
`ctxferry-v1`). Git must already have SSH or credential-helper access. Metadata
reads use `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth token --hostname github.com`.
CtxFerry does not save a token. A fine-grained token limited to the dedicated
repository is preferable to broad personal credentials.

`enable` uploads nothing. It reports Git reachability and authenticated privacy
separately. Only a positive private response with a matching repository name and
stable numeric identity creates access configuration. Public, unauthenticated,
missing or malformed metadata never authorize upload. Local commands remain
available. `check` repeats the independent checks. `init` repeats them again and
refuses an identity change. Verification cannot atomically prevent somebody from
changing repository visibility during a push.

`preview` lists every upload path, size and SHA-256, together with the destination
and plaintext boundary. Inspect the original shared documents before publishing.
The preview contains hashes and identities rather than restricted document bodies.
Its ID binds the exact export bytes and destination; changes to records, policies,
objects or project dependencies require a fresh preview. Agent disclosure controls
CLI visibility, independently of storage: a **shared** summary/reference/local-tool
document still publishes its complete body. Keep material that must not be uploaded
**local-only**. Credential stores, authentication, device paths, checkout bindings,
SQLite and indexes never enter the allow-listed export. This does not detect every
secret a user might paste into shared prose.

The application-managed bare repository is `workspaces/default/sync.git` in the
data root, which must remain outside source checkouts. Git plumbing constructs
blobs with `--no-filters`, trees from explicit paths and a zero-parent root commit.
There is no checkout or recursive staging. Hooks and signing are disabled. Git
location/configuration environment overrides are removed; system/user credential
helpers and SSH-agent authentication remain available. URL rewrite rules, config
includes and alternate push URLs are rejected because they could disconnect the
verified GitHub identity from the push destination. Git errors are deliberately
redacted; troubleshoot authentication using Git separately.

The root is pinned locally and the pending push (expected absent head, candidate,
epoch and operation IDs) is committed durably before publication. The push uses an
exact expected-absent lease; a competing root is preserved. A failed or ambiguous
push stays pending. Retrying the same preview reuses the same candidate and can
acknowledge an already accepted candidate when it is still the remote tip. A
changed snapshot or a different existing remote history requires reconciliation;
it is never overwritten. Routine sync and join use the same validated record format. Descendant-history
recovery and explicit conflict resolution are tracked by issues #10 and #9.

## Format decisions

- `workspace.json`: format `1`, stable workspace UUID, random UUID epoch and
  `plaintext` storage mode. Epoch is a history generation, not an encryption key.
- `records/<prefix>/<uuid>.json`: prefix is the first two UUID hex characters.
  Envelopes have schemaVersion, id, kind, workspaceId, scope, revision,
  previousRevision, operationId, deleted, provenance, policy and payload.
- UUID v4 revisions and operation IDs are independent of wall-clock order.
  Existing context identities are preserved. Project, Repository and membership
  export revisions are persisted and change only when their payload changes.
- Existing Projects, Repositories and memberships are workspace-shared structural
  records. Their local bindings and discovery evidence remain local. ContextDocs
  retain their explicit storage policy. Scope/reference closure must be valid;
  unknown kinds/fields/policies and missing or invalid references block export.
- Markdown bodies are immutable `objects/<sha256>` bytes, deduplicated by hash.
  Payload manifests carry type, size and SHA-256. Shared summary/reference bodies
  are exported without passing through the CLI disclosure projection.
- README describes plaintext, repository-wide access, history and recovery limits.
- Schema 4 adds local access configuration, immutable preview snapshots, stable
  structural export identities and pending/confirmed publication state. These
  tables are not exported. A separate SQLite owner lock serializes publishers and
  shared-data writers and is automatically released on process death.

CLI scenarios compile with `ctxferry_test`, which substitutes a loopback metadata
endpoint and local bare Git transport. These environment overrides are absent from
production builds. Tests never push context to an external repository.

## Joining and two-way sync

```sh
ctxferry join git@github.com:owner/private-context.git --subscribe my-project
ctxferry project list --all --format json
ctxferry project subscribe my-project
ctxferry project bind . --project my-project
ctxferry sync --format json
ctxferry context show --sync --format json
```

Join requires a fresh application data directory and an existing sync branch.
It verifies authenticated repository identity/privacy, stages the full catalog,
and adopts its workspace identity in one transaction with a new local Device ID.
Use repeated `--subscribe` selectors or `--all-projects`. The default subscribes
to no Projects. `project unsubscribe` hides a Project from ordinary listing and
context queries and removes its search entries. `project list --all` discovers
all Projects. The full shared baseline stays available for synchronization.
Subscriptions and checkout paths remain device-local.

Shared edits queue durably with their original base envelope, epoch, operation
ID and desired envelope in the same transaction as the local edit. Reads use
local SQLite immediately and include `lastSyncAt`. `--sync` on context reads
waits for a successful round. `sync status` reports the last observed outcome,
last successful time and pending operation count without contacting the network.
Offline/authentication failures preserve local records and queued edits.

A round fetches and validates the complete snapshot, checks ancestry and record
baselines, and publishes compatible operations with an exact expected-head lease.
The candidate has the fetched commit as its sole parent. Unchanged record files
and content-addressed bodies retain their Git blob identities. Empty rounds
advance the local observation without publishing an empty commit. Hashes, paths,
schema, scopes, references and application uniqueness constraints are verified
before downloaded records become visible in a database transaction.

Concurrent edits retain the original base, desired local envelope and current
remote baseline. Independent records can proceed. A conflict reports `conflict`;
explicit resolution follows in #9. A failed/ambiguous push retains its pinned
candidate and operation IDs. The next round checks fetched history to acknowledge
an accepted candidate or reconcile its operations against the current head. Issue
#10 extends fault injection and retry scheduling.
Published records cannot disappear or change kind. Tombstones and changing an
already-published record to local-only require the explicit deletion flow in #9.

Schema 5 adds baseline, outbox and subscription tables plus sync timestamps and
outcomes. An existing schema-4 publisher bootstraps its actual confirmed Git
snapshot from the locally retained commit before its next edit or routine sync, preserving later unpublished edits. A staged
catalog is limited to 64 MiB, each Markdown object to 1 MiB, and each envelope to
256 KiB. Existing local-only data and bindings survive sync.
