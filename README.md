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

Each application root currently holds one Workspace and one Device at `workspaces/default/catalog.sqlite`. UUIDs are stable across process restarts. Local schema version 4 uses short immediate write transactions, foreign keys, FULL synchronous writes, and a 5-second busy timeout on every connection. Unsupported newer schemas and damaged databases fail without replacement. Future workspace switching must preserve the Device identity.

Every command supports `--format json`, emitting one object (including failures and help):

```json
{"schemaVersion":1,"ok":true,"data":{"projects":[]},"error":null}
```

Failures return `ok:false`, `data:null`, and an error with `code` and `message`; diagnostics go to stderr. Exit codes are 0 for success, 2 for usage errors and 1 for application/storage errors. JSON field names and schema version form the scripting contract. Text output is intended for humans.

The CLI parses and renders; `internal/app` owns operations and validation, and `internal/store` owns persistence. Git is optional for discovery; there are no provider, credential or desktop dependencies at runtime. Queries do not initialize a missing store.

Run `go test -count=1 ./...` and `go vet ./...`. The reusable `internal/testcli` harness exercises the real binary with isolated data/home directories, sanitized environment, deadlines, separate streams and exact JSON decoding. CI checks Linux, macOS and Windows with CGO disabled and runs an offline Linux container smoke test.

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

Context commands add offline ContextDocs with independent storage and disclosure policies, revision-checked `context put`, and SQLite FTS5 search. See [context commands and decisions](docs/context.md). Schema 3 adds documents after the schema 2 Project identification migration.

First private publication supports separate Git/privacy checks, an exact plaintext preview and explicit branch initialization. See [sync workflow and format](docs/sync.md).

Register a Project service with an explicit storage policy. Shared endpoints use
non-secret host and port pairs. Configure localhost endpoints and proxy addresses
with a device-local route.

```sh
ctxferry resource add --identity dev-db --kind database --stage dev \
  --purpose "Integration tests" --storage shared --endpoint db.example.com:5432
ctxferry resource list
ctxferry resource route <resource-uuid> --endpoint localhost:15432
ctxferry resource inspect <resource-uuid> --format json
ctxferry resource check <resource-uuid> --format json
```

Only `resource check` opens a TCP connection. It records reachability of the
resolved endpoint, or the proxy itself when a local proxy is configured. Each
result is appended to this device's Observation history. The check does not
verify a service protocol or use credentials.

Register a Task with the membership UUID shown by `project bind` or
`project inspect`. Pass each program argument separately.

```sh
ctxferry task add unit-tests --purpose "Run the Go unit tests" \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared
ctxferry task list --format json
ctxferry task inspect unit-tests --format json
ctxferry run unit-tests --format jsonl
```

`run` resolves the Task's working directory from the membership root.
When several locally registered checkouts or worktrees can run the Task, pass
`--checkout <git-root>`. The default text format streams the child output.
JSONL limits each serialized event to 8 KiB and the stream to 64 KiB. Output
events carry `data`, with `encoding: "base64"` for chunks containing invalid
UTF-8. The final result includes `truncated` and distinguishes child exit codes
from control-plane errors.

Declare a Resource slot with
`--resource-slot '{"name":"db","kind":"database","stage":"dev"}'`. Map it
explicitly with `ctxferry profile map-resource development <task> db <resource-uuid>`.
The shared Profile maps shared Tasks to shared Resources of the slot's kind and
stage. Select the Profile locally with `profile select` or use `run --profile`.
Task definitions can declare credential requirements for discovery. Runs of
those Tasks return `credentials-required` before starting a child process.

### Credential references

Create a binding after registering a Task requirement and mapping its Resource slot in a Profile. The binding takes its Resource, purpose and type from that selection.

```sh
ctxferry secret bind integration-check login --profile development \
  --alias shared-db --provider infisical --provider-profile team --method machine \
  --backend-project team-vault --environment sandbox --path /app --key BACKEND_DB
ctxferry secret list --format json
ctxferry secret status integration-check login --profile development --format json
```

Use `--entry-id` in place of the four namespace and key flags when the backend provides a stable entry ID. The Task's delivery variable is independent of the backend key. To reuse a binding from another Project, map the Task's Resource slot to the same shared Resource UUID and pass `--binding <uuid>` in place of the new binding fields.

`secret list` reads catalog metadata. `secret status` checks the exact reference for one Task requirement after validating its Profile mappings. Its JSON reports binding, authentication, entry retrievability and Resource reachability independently. Unknown facts are `null`. On failure the same facts are available in `error.details` when the Task and Project can be evaluated. Reachability uses the latest matching observation from `resource check` on this device.

`provider login <provider> --provider-profile <name> --method <method>` uses the configured adapter and secure device credential store. This release defines the adapter and storage interfaces. Production login reports unavailable secure storage until a concrete integration is configured. Catalog sync works independently of provider login.
