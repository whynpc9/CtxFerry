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
it is never overwritten. Joining, routine sync, descendant-history recovery and
conflicts belong to subsequent slices. `synced` here acknowledges that first
snapshot; it does not yet track later local edits.

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
