# CtxFerry

Local-first development context. This first slice supports local workspaces and Project bindings without any login, network connection, GUI, DBus or WebKitGTK.

```sh
go build -o ctxferry ./cmd/ctxferry
ctxferry init
ctxferry project bind .
ctxferry project list
ctxferry project inspect
ctxferry project inspect <uuid-or-slug> --format json
```

`project bind` accepts any existing directory, including non-Git directories. It writes only to the application data root. Repeated init and repeated binding preserve identities. `--slug` selects a unique lowercase slug; otherwise a slug derives from the directory name. A second directory with the same name gets a distinct Project. Inspection without a selector uses the longest bound ancestor of the current directory.

Set `--data-dir <root>` or `CTXFERRY_DATA_DIR` to isolate an installation. The flag takes precedence. Defaults are `$XDG_DATA_HOME/ctxferry` (or `~/.local/share/ctxferry`) on Linux/WSL, `~/Library/Application Support/ctxferry` on macOS, and `%LOCALAPPDATA%\ctxferry` on Windows. Windows and WSL have separate installations. Keep the root outside bound directories and on a local filesystem: it contains an active WAL-mode SQLite database, not a shareable sync folder.

Each application root currently holds one Workspace and one Device at `workspaces/default/catalog.sqlite`. UUIDs are stable across process restarts. Local schema version 2 uses short immediate write transactions, foreign keys, FULL synchronous writes, and a 5-second busy timeout on every connection. Unsupported newer schemas and damaged databases fail without replacement. Future workspace switching must preserve the Device identity.

Every command supports `--format json`, emitting one object (including failures and help):

```json
{"schemaVersion":1,"ok":true,"data":{"projects":[]},"error":null}
```

Failures return `ok:false`, `data:null`, and an error with `code` and `message`; diagnostics go to stderr. Exit codes are 0 for success, 2 for usage errors and 1 for application/storage errors. JSON field names and schema version form the scripting contract. Text output is intended for humans.

The CLI parses and renders; `internal/app` owns operations and validation, and `internal/store` owns persistence. Git is optional for discovery; there are no provider, credential or desktop dependencies at runtime. Queries do not initialize a missing store.

Run `go test ./...` and `go vet ./...`. The reusable `internal/testcli` harness exercises the real binary with isolated data/home directories, sanitized environment, deadlines, separate streams and exact JSON decoding. CI checks Linux, macOS and Windows with CGO disabled and runs an offline Linux container smoke test.

Project identification uses this order: explicit `--project`, longest local path binding,
locally bound Git common directory and membership scope, then remote candidates.
A new worktree inherits the deepest containing repository-relative scope. Equal
scopes naming different Projects stay unbound. A direct path binding always wins,
even over a narrower inferred scope. A remote match always stays unbound, including
a single candidate. Unbound inspection exits 1 with `project-unbound`; JSON includes
the resolution and candidates in `error.details`, and text lists candidates on stderr.

```sh
ctxferry project bind . --slug api --remote git@github.com:owner/repo.git
ctxferry project bind ../other-repo --project api
ctxferry project inspect --project api --format json
ctxferry project remote add <repository-uuid> https://github.com/owner/renamed.git
ctxferry project bind ../another-clone --project api --repository <repository-uuid>
```

Git bindings report their Repository UUID and membership UUID. A Project can span
Repositories; a Repository can contain several Projects bound at different scopes.
Common-directory identity is reused only from explicit local bindings. Another
clone requires `--repository` to reuse a Repository; remotes never establish identity.
Use repeatable `--remote` at bind time or `project remote add` to accept an alias
explicitly. Adding a rename alias retains the old one. Only conventional GitHub
SSH/HTTPS forms normalize; owner identity is retained. Forks with upstream remotes
can produce upstream candidates but never inherit a Project automatically.

`project inspect` reports resolution source, current directory, current checkout root,
worktree Git directory, common directory, and Device identity/OS/architecture. Explicit
Project selection does not claim that the current checkout belongs to that Project.
Git discovery uses local read-only subprocesses with a five-second total deadline,
ignores inherited `GIT_*` overrides, and never fetches. Missing Git or failed discovery
is reported as unavailable; explicit path bindings and non-Git directories still work.
Raw remote URLs are not echoed or stored. Schema 2 migrates old bindings transactionally,
keeping identities and paths; repeat bind explicitly to enrich old Git bindings.
