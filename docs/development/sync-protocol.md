# Sync protocol and test transport

## Initial publication

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
acknowledge an already accepted candidate anywhere in validated remote history. A
changed snapshot or a different existing remote history requires reconciliation;
it is never overwritten. Routine sync and join use the same validated record format. Recovery also works when another device has appended descendants. Conflicts are resolved explicitly with `sync resolve`.

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
  and Resources retain their explicit storage policy. A shared Resource depends on
  its Project and carries non-secret configuration in its v1 envelope. Device
  route facts and TCP Observations stay in the local SQLite store, in tables
  added by migration 8.
  Scope/reference closure must be valid;
  unknown kinds/fields/policies and missing or invalid references block export.
- Markdown bodies are immutable `objects/<sha256>` bytes, deduplicated by hash.
  Payload manifests carry type, size and SHA-256. Shared summary/reference bodies
  are exported without passing through the CLI disclosure projection.
- Skill pins and groups are published as `skill-pin` and `skill-group`
  records. A group depends on each of its shared pins and, for a Project
  group, its Project. Public pins carry a credential-free HTTPS source,
  an exact Git commit and a file manifest. Private pins reference selected
  files by hash under `objects/<sha256>`, up to 1 MiB per package.
  These bytes count toward the ordinary staging and routine publication
  budgets, including when an artifact references the same object.
  Install directories, link targets and ownership receipts are device-local.
- README describes plaintext, repository-wide access, history and recovery limits.
- Schema 4 adds local access configuration, immutable preview snapshots, stable
  structural export identities and pending/confirmed publication state. These
  tables are not exported. A separate SQLite owner lock serializes publishers and
  shared-data writers and is automatically released on process death.

CLI scenarios compile with `ctxferry_test`, which substitutes a loopback metadata
endpoint and local bare Git transport. These environment overrides are absent from
production builds. Tests never push context to an external repository.

## Round and migration invariants

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
explicit resolution uses `sync resolve`. A failed/ambiguous push retains its pinned
candidate and operation IDs. The next round checks fetched history to acknowledge
an accepted candidate or reconcile its operations against the current head. Lease rejection (including a server-side atomic ref race after advertisement) starts a fresh fetch and business-baseline comparison within the same bounded invocation.
Published records cannot disappear or change kind. Deletion uses retained tombstones; changing a published record to local-only still requires an explicit deletion flow.

Schema 5 adds baseline, outbox and subscription tables plus sync timestamps and
outcomes. An existing schema-4 publisher bootstraps its actual confirmed Git
snapshot from the locally retained commit before its next edit or routine sync, preserving later unpublished edits. A staged
catalog is limited to 64 MiB, each Markdown object to 1 MiB, and each envelope to
256 KiB. Existing local-only data and bindings survive sync.

## Recovery and failure states

The Git backend separates ancestry false (exit 1) from an operational failure.
An ancestry failure never acknowledges or discards a pending candidate. Fetch uses
`FETCH_HEAD`, so rewritten remote history can be inspected without modifying a
remote branch. A head that no longer extends confirmed history reports
`history-rewritten` and stops publication while preserving local reads and work.

Initialization reads and validates an existing winner after an expected-absent
race. A different root reports `branch-exists` and retains this device's workspace,
preview and candidate; it never silently adopts or replaces the winner. Invalid
winners report the validation failure. Missing branches in routine sync or join,
404s, permissions and failed listings never authorize initialization.

Malformed JSON, missing or corrupt payloads, partial Git listings, damaged Git objects and short blob
reads report `incomplete`. Future record versions, kinds and unknown fields report
`unsupported-schema` and refuse publication of the unfamiliar catalog. Neither
failure advances the confirmed head, last successful sync time or visible data.
Git helper/SSH-agent lock signals report `unlock-required`; ordinary access denial
reports `auth-required`. Authentication diagnostics are redacted.

Routine sync and initialization allow at most four rounds in a two-minute command
budget, with cancellable waits of 250 ms, 500 ms and 1 s. Transient transport errors
and exact-lease contention retry; authentication, unlock, missing branches and
catalog incompatibility/damage stop immediately. A failed push also reads the exact remote ref: a changed head starts a fresh
reconciliation round, independently of Git diagnostic wording. If its outcome
remains uncertain it stays journaled as `pending`; the next invocation checks
candidate ancestry before any further publication. Permanent push rejection also keeps the journal for explicit retry.

## Fault injection

The `ctxferry_test` binary wraps the real backend to lose push responses, publish a
competing candidate immediately before the lease, fail ancestry/fetch, or truncate
Git reads. These controls are absent from production builds. Scenarios use only a
loopback metadata endpoint, temporary application directories and local bare Git.
