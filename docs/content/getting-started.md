---
title: Install and bind your first checkout
---

English · [简体中文](zh-CN/getting-started.md)

This guide creates a usable local workspace with no account, network
connection, desktop application, or credential provider. Git is optional for a
directory binding; it is needed for Repository discovery, Tasks tied to a
Repository membership, and Git synchronization.

## Install the CLI from source

You need a CtxFerry source checkout and a Go toolchain compatible with the
version in `go.mod` (Go 1.27.4 for this source revision).

From the CtxFerry source directory, run:

```sh
go install ./cmd/ctxferry
ctxferry --help
```

`go install` places `ctxferry` in your Go binary directory. Add that directory
to `PATH` if the second command cannot find it; `go env GOBIN` and
`go env GOPATH` show the relevant Go settings. The help output confirms that
the executable is available.

## Initialize a local Workspace

Run:

```sh
ctxferry init --format json
```

A successful result has `ok: true` and `data.workspace`, `data.device`, and
`data.created`. CtxFerry creates one Workspace and one Device identity in its
application data root. Repeating `init` preserves those identities and reports
`created: false`.

Initialization does not contact GitHub or a secret provider. You can continue
using every local workflow before deciding whether to configure sync.

## Bind a checkout to a Project

Change to an existing project directory, then run:

```sh
cd /path/to/your/checkout
ctxferry project bind . --slug my-project --format json
ctxferry project list
ctxferry project inspect --format json
```

Replace `my-project` with a unique lowercase slug, or omit `--slug` to derive
one from the directory name. The bind result contains `data.project` and
`data.binding`, including the canonical local path. For a Git checkout it also
contains the Repository and membership IDs used by Tasks.

The result is observable in two places:

- `project list` prints the Project slug and stable UUID.
- `project inspect` prints the resolved Project, local bindings, resolution
  source, current directory, Git checkout information when available, and this
  Device's identity.

The directory may be a Git checkout, a worktree, or a non-Git directory. Binding
writes to the CtxFerry application data root, not to the bound directory.
Repeating the same bind preserves the Project identity. A second directory with
the same default name receives a distinct slug rather than being merged
implicitly.

Without a selector, inspection uses the longest bound ancestor of the current
directory. To bind another local checkout to the same logical Project, run:

```sh
ctxferry project bind /path/to/another-checkout --project my-project
ctxferry project inspect my-project --format json
```

An explicit `--project` selects a Project but does not assert that the current
directory belongs to it. Remote URLs can produce candidates, never an automatic
binding. If resolution is ambiguous, CtxFerry returns `project-unbound` and
lists the candidates instead of guessing.

## Choose and protect the data directory

By default, the application data root is:

- Linux and WSL: `$XDG_DATA_HOME/ctxferry`, or `~/.local/share/ctxferry`.
- macOS: `~/Library/Application Support/ctxferry`.
- Windows: `%LOCALAPPDATA%\ctxferry`.

Windows and WSL are separate execution environments and need separate
installations. To isolate a test installation or simulate another device, set
`CTXFERRY_DATA_DIR`, or pass an explicit root:

```sh
ctxferry --data-dir /path/to/isolated-ctxferry init --format json
ctxferry --data-dir /path/to/isolated-ctxferry project list --format json
```

`--data-dir` takes precedence over the environment variable. Keep the root on a
local filesystem and outside every bound source directory. The active catalog
is `workspaces/default/catalog.sqlite` beneath the root; it uses WAL mode and
is not a file to copy between running devices or place in a sync folder.
Queries do not create a missing store, and a damaged or newer unsupported
catalog is not replaced automatically.

## Use the machine-readable contract

Query and management commands accept `--format json`. Standard output contains
exactly one object, including for help and failures:

```json
{"schemaVersion":1,"ok":true,"data":{"projects":[]},"error":null}
```

Failures have `ok: false`, `data: null`, and an `error` with a stable `code` and
human-readable `message`; optional `details` add structured evidence.
Diagnostics go to standard error. Exit code 0 means success, 2 means a usage
error, and 1 means an application or storage error. Task execution uses JSONL
instead; see [Resources and Tasks](tasks-and-resources.md#run-a-local-task).

## Next steps

- [Follow the end-to-end tutorial](tutorial.md) to connect Context, Tasks,
  credentials and a second device in one proof-of-concept workflow.
- [Write a Context document](context.md) and choose whether it stays local or
  becomes eligible for private sync.
- [Check this device with doctor](diagnostics.md) when a binding or path does
  not resolve as expected.
- [Connect private sync](sync.md) only when you are ready to review and publish
  shared plaintext records.
- [Open the native desktop](desktop.md) to inspect this checkout, preview a
  configuration change or review records waiting to sync.
