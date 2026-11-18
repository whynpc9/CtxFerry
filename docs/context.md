# Context documents

`context put` is the explicit write command chosen for issue #4. It creates a
Project-scoped ContextDoc without an ID, or replaces the complete document when
an ID and its current `--if-revision` are provided. A stale replacement fails
with `revision-conflict`. Each successful write preserves the document ID and
creates UUID revision and operation IDs; clocks do not determine revisions.

```sh
ctxferry context put --title 'Development workflow' --purpose notes \
  --source 'Team-maintained notes' --summary 'Build and test conventions' \
  --body-file notes.md --storage local-only --disclosure summary --format json
ctxferry context show --format json
ctxferry context get <document-uuid> --format json
ctxferry context search 'build test' --format json
ctxferry context put <document-uuid> --if-revision <revision> ...complete-fields...
```

All commands accept `--project <uuid-or-slug>`; otherwise they use the shared
Project resolver used by `project inspect`. Unbound directories report
`project-unbound` with `error.details.candidates:[]` when no candidates are detected. Document
IDs cannot bypass project resolution or retrieve another Project's document.
Git common-directory and remote candidate detection come from that shared resolver.
Only Project scope is supported in this slice. Scope is ownership, not an ACL.

Storage (`local-only` or `shared`) and disclosure must both be explicit. Shared
means eligible for future publication, not already uploaded. These commands
perform no sync, network access, environment enumeration or credential retrieval.

| Disclosure | `get` | Searchable content |
| --- | --- | --- |
| `full` | Metadata, explicit summary, body | Metadata, summary, body |
| `summary` | Metadata and explicit summary | Metadata and summary |
| `reference` | Metadata and UUID reference | Metadata |
| `local-tool` | Metadata and UUID reference | Metadata |

Title, purpose and source are agent-visible metadata in every mode. Source is
explicit, never inferred from a body file path. `local-tool` records a policy;
there is no body-revealing local-tool execution command in this slice. Disclosure
controls CLI responses, not filesystem access by processes running as the same
OS user. Context notes are not a secret store.

`put` returns a metadata receipt without summary or body. `show` never returns
bodies; `search` returns a metadata index with approved summaries, not bodies or
body snippets. Restricted bodies are never placed in the agent-facing FTS5 index,
so even match presence cannot reveal a hidden term. Policy changes and index
replacement commit together. Search words are literal AND terms, not raw MATCH
syntax; input is limited to 1024 bytes and results to 100 documents.

A single document with `purpose=project-summary` supplies the Project's explicit
summary. Reference/local-tool disclosure suppresses it. No summary is synthesized
from a body. `source`, `updatedAt`, nullable `observedAt`, `revision` and `freshness`
accompany documents. Freshness is `unknown` unless an explicit RFC3339
`--valid-until` has expired, when it is `stale`. Aggregate provenance uses
`source:local-store`, the Project creation timestamp, and null revision,
observation and last-sync timestamps: an aggregate has no persisted revision and
no claim of verified or synced state. The environment reports current device
ID, OS, architecture and calling directory.

`show` is limited to 8192 actual emitted bytes, including the JSON envelope and
newline, or indented text rendering. Index fields and summaries are shortened
at UTF-8 boundaries; complete entries are removed until the budget fits.
`truncated:true` and `nextCursor` indicate more entries. Continue with
`context show --cursor <nextCursor>`; `get` returns the complete approved fields.
Ordering is by UUID. Pagination is a live view, not a snapshot across writes.

Size decisions: structured metadata (the ContextDoc JSON excluding separately
stored Markdown) is limited to 262144 bytes; raw UTF-8 Markdown is independently
limited to 1048576 bytes. Oversize or invalid UTF-8 input fails before mutation.
`--body-file -` reads stdin; `--summary-file` supports large explicit summaries
without command-line length limits and is mutually exclusive with `--summary`.
Updates must supply the complete replacement, including policy and body.

Local schema v3 migrates v1 and v2 transactionally, preserving existing identities and
bindings. `records` stores structured document metadata and `context_bodies`
stores Markdown separately in the same database transaction. FTS is a derived
agent-visible index. Unknown newer database schemas fail without replacement.
No source-checkout files are written.
