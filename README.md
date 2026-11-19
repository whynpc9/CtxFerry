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

Run `ctxferry doctor` or `ctxferry doctor --format json` to inspect a device. Each check has its own status and evidence. The command succeeds when it can produce a report, including when individual capabilities are missing. A successful sync is marked stale after 24 hours, independently of queued changes and the recorded sync state.

Doctor works offline. Git authentication uses historical sync evidence and local configuration hints. Provider authentication uses scoped session metadata when the OS can expose it safely. Live authentication stays unknown when that evidence cannot establish it. A recorded TCP observation reports reachability only. This release supports foreground sync.

The CLI parses and renders; `internal/app` owns operations and validation, and `internal/store` owns persistence. Git is optional for discovery. Normal context queries run without a secret provider or desktop dependencies. Queries do not initialize a missing store.

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
Tasks can declare credential requirements; see Credentialed runs below.

### Credential references

Create a binding after registering a Task requirement and mapping its Resource slot in a Profile. The binding takes its Resource, purpose and type from that selection.

```sh
ctxferry secret bind integration-check login --profile development \
  --alias shared-db --provider infisical --provider-profile team --method universal-auth \
  --backend-project 00000000-0000-0000-0000-000000000000 --environment sandbox --path /app --key BACKEND_DB
ctxferry secret list --format json
ctxferry secret status integration-check login --profile development --format json
```

Use `--entry-id` in place of the four namespace and key flags when the backend provides a stable entry ID. The Task's delivery variable is independent of the backend key. To reuse a binding from another Project, map the Task's Resource slot to the same shared Resource UUID and pass `--binding <uuid>` in place of the new binding fields. To move a mapped slot to another Resource, run `secret unmap <task> <requirement> --profile <name>` for each credential mapped to that slot, change its Resource mapping, then bind the credentials again. Unmapping leaves the shared SecretBinding available to other Profiles.

`secret list` reads catalog metadata. `secret status` checks the exact reference for one Task requirement after validating its Profile mappings. Its JSON reports binding, authentication, entry retrievability and Resource reachability independently. Unknown facts are `null`. On failure the same facts are available in `error.details` when the Task and Project can be evaluated. Reachability uses the latest matching observation from `resource check` on this device.

### Infisical US Cloud

- **Personal** (`--method personal`). Sign in interactively with `infisical login` before running `ctxferry provider login infisical --provider-profile <name> --method personal`. Use the same named Infisical CLI profile, or explicitly select `default` to adopt its current profile. The adapter uses the existing CLI session to obtain an access token. Re-run CtxFerry login when the token expires.
- **Machine identity** (`--method universal-auth`). Supply `INFISICAL_CLIENT_ID` and `INFISICAL_CLIENT_SECRET` through your existing protected launcher. Set `CTXFERRY_INFISICAL_PROFILE` to the selected provider profile name, then run `ctxferry provider login infisical --provider-profile <name> --method universal-auth`. This flow needs network access and works without a browser or a terminal.

Both login methods store the access token in the OS credential store (Secret Service, Keychain or Credential Manager). Login reports `credential-storage-unavailable` if the store is inaccessible. On a headless host without a keyring, a protected launcher can instead supply a current machine token as `CTXFERRY_INFISICAL_ACCESS_TOKEN` for `secret status` and credentialed `run`. Set `CTXFERRY_INFISICAL_WORKSPACE_ID`, `CTXFERRY_INFISICAL_DEVICE_ID` and `CTXFERRY_INFISICAL_PROFILE` to the exact local workspace, device and provider profile. Find the workspace and device IDs with `ctxferry init --format json`. The matching token is used for that invocation and remains under the launcher's control. Without either source, status and run report `authentication-required`. These variables are never passed to a Task's child.

Session checks and reads require a connection. Both methods report token expiry, and Infisical can revoke or restrict access before that time. Reads use the stable secret ID when specified. A named read requires the explicit Infisical project ID, environment slug, folder and exact shared key. The Resource stage is independent, and the adapter disables imported secrets and reference expansion. The adapter reports no offline read, native execution or file delivery capability. Catalog sync works independently of provider login.

### Credentialed runs

`run` resolves only the requirements of the Task being run, through the
checkout's Project, the Task, the selected Profile, its explicit mappings, the
SecretBinding and its locator, and this device's own provider session: a
runtime session provisioned for this exact workspace, device and provider
profile, or the one saved by `provider login`. Before each run the requirement,
the selected Resource and the binding must agree on purpose, type and Resource;
otherwise the run stops with `incompatible`. A missing mapping reports
`binding-missing` and conflicting mapping edits report `binding-conflict`;
similar bindings are never substituted.

Credentialed execution also requires this device to have enabled the Task's
execution definition (see Execution trust below). Until then, credentialed runs
stop with `execution-definition-changed` before any provider call. The run opens
no connection itself: when this device's latest `resource check` of the
selected Resource's direct route found it unreachable, or the device has no
usable route, the run stops with `resource-unreachable`. A local Resource with
the same identity is never used in its place. Login is per device: a session
on another device does not count (`authentication-required`), and a missing or
denied entry reports `secret-unavailable` without its value. Interrupting the
run while it resolves values reports `interrupted`.

The child receives a minimal base environment (`PATH`, `HOME`, temporary
directories, locale and terminal variables) plus the Task's delivered
variables. Variables of other Tasks, Git and provider authentication material
and the rest of the caller's environment are not passed. Values never appear
in arguments, shell strings or files, and are held only in memory for the
invocation; there is no secret cache. CtxFerry builds the child environment
itself even when a provider reports a native execution capability. Delivered
values in forwarded stdout and stderr are replaced with `[REDACTED]`.

The target program can still read, encode, print or exfiltrate anything it is
given. Output replacement is not a guarantee and a Task is not OS isolation.

### Execution trust

Each device decides for itself which credentialed execution definitions it
runs. The reviewed definition covers the program and arguments, whether `exec`
may run custom commands, the Repository membership, the resolved local
checkout and working directory, and the selected Profile's Resource and
SecretBinding mapping with each binding's provider profile, locator and
Resource purpose. Preview it, then enable exactly what was previewed:

```sh
ctxferry task trust preview integration-check --profile development --format json
ctxferry task trust enable integration-check --profile development --preview <preview>
```

Pass the same `--profile` and `--checkout` as `run`. A preview reports `state`
(`not-enabled`, `enabled`, `execution-definition-changed` or `not-required` for
a Task without credentials), the current definition, and for a changed one the
enabled definition and the changed fields. `enable` refuses with
`preview-stale` when the definition no longer matches the preview, for
example after a sync landed in between; preview again. Once enabled, runs do
not ask again. A synchronized or local change to any reviewed field stops
credentialed `run` and `exec` with `execution-definition-changed`, whose
details name the changed fields, until this device enables the new version.
Editing a Task's purpose or inputs, aliases or other descriptive text keeps the
enabled state. Trust records stay in this device's local store and are never
synchronized; every device, including the one that made a change, reviews it.
Approval is per Git checkout or worktree and membership directory. When a
synchronized edit moves the Task's membership directory within the same
checkout, or to another Repository, the preview compares it with the latest
earlier approval. `enable` resolves the checkout first, then prepares and
stores the definition under the local store's write lock without running Git,
so nothing can change it between the check and the approval.

Change a Task at its current revision with `task update`; only the given fields
are replaced. An edit that would leave a Profile mapping pointing at a removed or
incompatible slot or requirement is refused with `invalid-task-dependency`;
change or unmap that mapping first.

```sh
ctxferry task update integration-check --if-revision <revision> --arg=check --arg=--verbose
```

### Custom commands

A Task registered or updated with `--allow-custom-command` lets `exec` run a
caller-chosen command with that Task's credentials:

```sh
ctxferry exec --task integration-check --profile development --format jsonl -- go test ./...
```

`exec` resolves the same checkout, working directory, Profile and credential
requirements as `run`, needs the same enabled definition, and follows the same
output, redaction and exit-code contract. The program is an absolute path, a
path relative to the Task's working directory, or a name on `PATH`. The path
reaches the operating system unchanged, so `..` follows symbolic links as the
filesystem resolves them. Windows drive-relative (`C:tool.exe`) and
drive-rooted (`\tool.exe`) paths are refused with `invalid-command`. Arguments
are passed as an array, never through a shell. Without the opt-in it stops with
`custom-command-not-allowed`. Enabling a definition that allows custom commands
approves any command run through `exec` for it, and the preview says so with
`customCommands: true`. Turning the opt-in on is itself an execution change.
The command can read and exfiltrate the delivered values.

Clients older than this release do not know the `allowCustomCommand` field.
Once a shared Task with the opt-in is published, they stop syncing with
`unsupported-schema` until they are upgraded; they never drop the field.

### Local artifacts

Register one explicit file for a new run, or append another artifact to an
existing run using its UUID. Supply provenance from the original execution.
Use `none` for an unused Profile and `unknown` for an unavailable source
revision. Repeat `--command` in argv order, and repeat `--tool`, `--input`,
`--completed` and `--uncompleted` as needed. For dirty source, `--diff-id`
can reference a diff you already have.

```sh
ctxferry artifact add report.json --project api --type application/json \
  --sensitivity internal --source-revision "$revision" --dirty=false \
  --profile development --tool 'go@1.27.4' --command go --command test \
  --command=./... --exit-code 0 --summary 'Unit tests passed' --completed unit \
  --format json
ctxferry artifact add summary.txt --project api --run <run-uuid> \
  --type text/plain --sensitivity internal
ctxferry artifact list --project api --run <run-uuid>
ctxferry artifact inspect <artifact-uuid> --project api --format json
ctxferry artifact fetch <artifact-uuid> --project api --output retrieved.json
```

Each manifest and its run provenance are immutable. Payloads are stored once per
SHA-256 in the workspace's local artifact cache. Fetch verifies the cached bytes
before publishing a new file. All sensitivity classes (`public`, `internal`,
`sensitive`) are local-only in this release. The local cache admits up to 1 GiB
of unique payload bytes by default. Set `CTXFERRY_ARTIFACT_CACHE_BYTES` to a
positive byte count to adjust the limit. At capacity, new content is rejected
and existing registrations remain retrievable.

### Skills

After `ctxferry init`, import one directory with a `SKILL.md` whose YAML front
matter declares its `name`. The name becomes the install directory name.

```sh
ctxferry skill add --local ./skills/example
ctxferry skill add --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example
ctxferry skill plan example --target codex --format json
ctxferry skill apply example --target codex
ctxferry skill plan another-example --target claude-code --symlink
ctxferry skill apply another-example --target claude-code --symlink
```

Git imports accept a full commit hash or a tag under `refs/tags/`. A tag records
both its name and resolved commit. Local imports capture a content digest and
retain a snapshot independent of subsequent source edits. Use `--license` to
override the front-matter license. Unknown licenses appear as `NOASSERTION`.
Use repeatable `--platform <os[/arch]>` and `--depends-on <executable>` to declare
requirements. Executable metadata is captured in the manifest and honored on
Unix. Import and install do not execute Skill scripts.

Codex uses `~/.agents/skills`. Claude Code uses `~/.claude/skills`, or
`$CLAUDE_CONFIG_DIR/skills` when configured. Copy is the default. `--symlink`
links to the verified local package and requires OS permission to create links.
The plan reports intended writes and blocking issues without changing the
target or local state. A blocked plan is a successful query and reports
`status: "blocked"`. Apply refuses unowned targets or externally edited
installs. If apply is interrupted, run the same command to resume a verified
staged install. Packages, receipts and recovery records are device-local in
this slice.
