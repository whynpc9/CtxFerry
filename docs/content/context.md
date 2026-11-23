---
title: Context documents and Profiles
---

English · [简体中文](zh-CN/context.md)

Context documents hold Project notes, conventions, and summaries. Reads use the
local catalog by default, so they work offline and do not require a credential
provider.

## Write a note

First [initialize and bind a checkout](getting-started.md). Run these commands
from that checkout, or add `--project <uuid-or-slug>` to select a subscribed
Project explicitly.

Create a UTF-8 Markdown file such as `notes.md`, then choose its storage and
disclosure explicitly:

```sh
ctxferry context put --title 'Development workflow' --purpose notes \
  --source 'Team-maintained notes' --summary 'Build and test conventions' \
  --body-file notes.md --storage local-only --disclosure summary --format json
```

The successful receipt is under `data.document` and includes the new document
`id` and `revision`. It contains metadata, not the body. Keep the ID for `get`
and the revision for a later replacement.

Storage and disclosure answer different questions:

- `local-only` keeps the document on this device.
- `shared` makes the complete body eligible for publication to the private sync
  repository. The write itself does not upload it.
- Disclosure controls what agent-facing CLI queries return; it does not limit
  publication and is not filesystem or operating-system isolation.

| Disclosure | `context get` returns | Searchable fields |
| --- | --- | --- |
| `full` | Metadata, explicit summary, and body | Metadata, summary, and body |
| `summary` | Metadata and explicit summary | Metadata and summary |
| `reference` | Metadata and UUID reference | Metadata |
| `local-tool` | Metadata and UUID reference | Metadata |

Title, purpose, and source are visible in every mode. Supply `--source`
explicitly; CtxFerry does not infer it from the body path. `local-tool` records
the policy but does not provide a body-revealing tool-execution command.
Processes running as your OS user may still access local application files, so
Context is not a secret store.

## Read, search, and paginate

Use the ID returned by `put`:

```sh
ctxferry context show --format json
ctxferry context get <document-uuid> --format json
ctxferry context search 'build test' --format json
```

`show` returns the Project summary when one is configured, the current execution
environment, the selected Profile, and indexes of documents, Resource purposes,
and Task entry points. It never returns document bodies. `get` returns the
fields allowed by that document's disclosure policy.

`search` treats all words as literal terms that must match. It searches only
fields allowed by disclosure and can also return matching Task names. Search
input is limited to 1024 bytes and results to 100 entries. Results contain
metadata and approved summaries, not body snippets.

A `show` response is limited to 8192 emitted bytes, including its JSON envelope
or text rendering. If it reports `truncated: true`, continue with the returned
cursor:

```sh
ctxferry context show --cursor <nextCursor> --format json
```

Pagination is a live view; writes between requests can affect later pages.
After configuring sync, add `--sync` to `show`, `get`, or `search` to wait for a
successful sync before reading. Without that flag, the response includes the
last locally known sync time when available.

A document with `--purpose project-summary` supplies the Project summary.
Reference and local-tool disclosure suppress that summary. CtxFerry never
synthesizes a summary from the body. Set `--valid-until` to an RFC3339 timestamp
when readers need a freshness expiry; otherwise freshness remains `unknown`.

## Replace a document safely

Replacement requires the document ID, its current revision, and the complete
new document, including policy and body:

```sh
ctxferry context put <document-uuid> --if-revision <current-revision> \
  --title 'Development workflow' --purpose notes \
  --source 'Team-maintained notes' --summary 'Updated build conventions' \
  --body-file notes.md --storage local-only --disclosure summary --format json
```

If another writer changed the document first, the command returns
`revision-conflict` instead of overwriting that work. Read the current version,
review the change, and retry with its revision. A successful replacement keeps
the document ID and creates fresh revision and operation IDs.

Markdown bodies must be valid UTF-8 and at most 1 MiB. Structured metadata has
a separate 256 KiB limit. Use `--body-file -` to read the body from standard
input. For a large explicit summary, use `--summary-file` instead of
`--summary`; the two flags are mutually exclusive.

For deletion, tombstones, and restoring an earlier shared version, see
[Recovery](recovery.md#delete-or-restore-one-shared-document). For concurrent
shared edits, see [Resolve a record conflict](recovery.md#resolve-a-record-conflict).

## Resolve ordinary configuration and Profile overrides

Profiles select ordinary configuration and explicit Task mappings. They do not
override agent instruction-file precedence or grant provider permissions.

This example sets each supported kind of value and then explains the result:

```sh
ctxferry config set parallelism 2 --layer global
ctxferry config set timeoutSeconds 60 --layer project
ctxferry profile create development
ctxferry config set parallelism 4 --layer profile --profile development
ctxferry profile select development
ctxferry config set workingDirectory '"tools"' --layer checkout
ctxferry context explain --format json
```

The field names are exact, and values passed to `config set` are JSON. The
quotes around `"tools"` are therefore required for a string value.

| Field | Accepted value |
| --- | --- |
| `parallelism` | Integer from 1 through 1024 |
| `timeoutSeconds` | Integer from 1 through 86400 |
| `workingDirectory` | Checkout-relative slash path, or a device-local absolute path at the device or checkout layer |

Each field inherits independently in this order:

**global → Project → explicitly selected Profile → device → current checkout.**

`context explain` reports each effective value and its source layer, and resolves
the working directory on this device. The directory must exist; a shared path
must remain inside the current checkout. Use `context explain --profile <name>`
to select a Profile for one invocation instead of changing the saved selection.

Remove an override to restore inheritance, or clear the local Profile selection:

```sh
ctxferry config unset workingDirectory --layer checkout
ctxferry profile clear
ctxferry context explain --format json
```

Global, Project, and Profile configuration participate in the shared catalog.
Device and checkout overrides, and the saved Profile selection, remain in the
local store. The effective explanation is computed on demand rather than
synchronized as another configuration record.

## Next steps

Continue to [Resources and Tasks](tasks-and-resources.md) to turn the indexed
service purposes and Task entry points into explicit local actions. Use
[Private sync](sync.md) when shared Context should reach another device.
