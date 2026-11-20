---
title: Resources and local Tasks
---

A Resource describes a service and its non-secret connection intent. A Task
defines an executable entry point, its working directory, arguments, Resource
slots, and optional credential requirements. Reading either one does not fetch a
credential or open a network connection.

## Register and inspect a Resource

Run from a [bound checkout](getting-started.md), or pass
`--project <uuid-or-slug>`. This example registers a shared development
database endpoint:

```sh
ctxferry resource add --identity dev-db --kind database --stage dev \
  --purpose 'Integration tests' --storage shared \
  --endpoint db.example.com:5432 --format json
```

The result is the Resource under `data`; save `data.id` as `<resource-uuid>`.
The endpoint is a host and numeric port, not a URL and not a connection string.
Do not include usernames, passwords, or tokens.

```sh
ctxferry resource list --format json
ctxferry resource inspect <resource-uuid> --format json
```

`list` returns the Project's configured Resources. `inspect` adds the route as
resolved for this execution environment and this device's Observation history.
Neither command contacts the service.

Shared endpoints must be meaningful across devices; loopback and device-local
addresses belong in a local route. For example, if this device reaches the same
logical database through a local forward, run:

```sh
ctxferry resource route <resource-uuid> --endpoint localhost:15432 --format json
ctxferry resource inspect <resource-uuid> --format json
```

The inspected route now has `source: "device-local"`. Route facts stay on this
device and do not overwrite the shared endpoint. Add `--proxy <host:port>` for a
local proxy or `--network <label>` when a Resource declares a network condition.
Run `resource route <resource-uuid>` with no route flags to clear all three local
facts.

A route can also report `route-missing`, `route-ambiguous`, `network-mismatch`,
or `platform-unsupported`. Resolve the indicated condition before asking for a
probe.

## Check TCP reachability explicitly

Only `resource check` opens a connection:

```sh
ctxferry resource check <resource-uuid> --format json
ctxferry resource inspect <resource-uuid> --format json
```

The first command attempts a TCP connection to the resolved endpoint, or to the
proxy when one is configured, with a two-second timeout. Its result has
`status: "reachable"` or `"unreachable"`, `tool: "tcp-connect"`, the device,
time, target, platform, Resource revision, and `serviceVerified: false`.

The second command shows the new Observation appended to this device's history.
An open port proves TCP reachability only: it does not verify the service
protocol, identity, authentication, or a successful Task. Observations from
other devices do not replace this device's evidence, and checks do not change
Resource intent.

## Register a local Task

A Task requires a Repository membership. Use a Git-backed checkout and copy
`membershipId` from the `project bind` result or from the matching entry in
`project inspect --format json`. Pass the program and every argument separately;
CtxFerry does not evaluate a shell command string.

```sh
ctxferry task add unit-tests --purpose 'Run the Go unit tests' \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared --format json
ctxferry task list --format json
ctxferry task inspect unit-tests --format json
```

The add result contains the Task's stable `id` and `revision`. Inspection shows
its purpose, inputs, membership, working directory, program, argument array,
platform conditions, Resource slots, and credential requirements.

Use `--arg=VALUE` for an argument that begins with a dash. Repeat `--input` to
describe inputs and `--platform <os>` or `--platform <os/arch>` to restrict
applicability. Storage is required: `shared` makes the definition eligible for
sync, while `local-only` keeps it on this device.

## Run a local Task

```sh
ctxferry run unit-tests --format jsonl
```

CtxFerry resolves the working directory from the Task's Repository membership,
not from the caller's current subdirectory. When several registered checkouts or
worktrees can run it, choose one explicitly:

```sh
ctxferry run unit-tests --checkout /path/to/git-root --format jsonl
```

Terminal format streams the child output. JSONL emits `start`, `stdout`,
`stderr`, and a final `result` event. Output chunks are in `data`; chunks that
are not valid UTF-8 use `encoding: "base64"`. Each serialized event is limited
to 8 KiB and the stream to 64 KiB. The final result includes `truncated` and the
child `exitCode`, so a failing test remains distinguishable from a CtxFerry
control-plane error. `run --format json` is intentionally a usage error; use
`text` or `jsonl`.

Execution happens in the calling CLI process, not in the desktop or sync
service. A Task without credentials does not need provider login or execution
trust approval.

To change a Task, read its current revision and replace only the fields you
intend to change:

```sh
ctxferry task update unit-tests --if-revision <current-revision> \
  --arg=test --arg=-count=1 --arg=./... --format json
```

Execution-field changes require a fresh trust review before a credentialed run.
Resource slots, Profile mappings, and credential requirements are covered in
[Credentials and execution trust](credentials.md). Shared definitions are
published through [Private sync](sync.md); execution trust remains local to each
checkout and device.
