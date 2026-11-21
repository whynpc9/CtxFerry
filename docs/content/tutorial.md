---
title: 'Tutorial: from one checkout to two devices'
---

This walkthrough builds a complete, typical CtxFerry workflow around a Go
service named `atlas-api`. You will register Project instructions, run a local
test Task, connect an integration Task to a database credential, publish the
shared catalog, and reproduce the Task on a second device.

The example uses Infisical US Cloud for the credential reference. The local
steps need no account or network. Sync and credential steps are explicit and
require your own private GitHub repository, database, and provider entry.

![Two device-local CtxFerry workspaces synchronize approved plaintext records through a private Git repository. Checkout paths, provider sessions, and execution trust stay local; credential values come directly from the external provider on each device.](assets/tutorial-flow.png)

The important distinction is that CtxFerry synchronizes **development context
and credential references**. It does not copy a checkout, synchronize a SQLite
file, share a provider login, or place credential values in Git.

## What you need

- Go 1.27.4 or a later compatible version, and Git.
- An existing Git checkout of a Go project. The example assumes ordinary tests
  run with `go test ./...` and integration tests use the `integration` build
  tag; adjust the arguments for your project.
- For the credentialed steps, a **disposable development database**, its
  non-secret host and port, and an Infisical Universal Auth identity allowed to
  read one connection-string entry. Do not start with production data.
- For the two-device steps, an empty, dedicated **private GitHub repository**
  and working Git and GitHub metadata authentication on both devices.

The shell examples use POSIX syntax. Replace every `<angle-bracket>` value with
a value from your own output or service. Output snapshots are abridged; UUIDs,
revisions, timestamps, paths, and hashes will differ.

## 1. Install and initialize the first device

From the CtxFerry source checkout, install the CLI:

```sh
go install ./cmd/ctxferry
ctxferry --help
```

If your Go binary directory is not on `PATH`, use `go env GOBIN` and
`go env GOPATH` to locate it. Then initialize a local Workspace:

```sh
ctxferry init --format json
```

The response contains `data.workspace`, `data.device`, and `created: true`.
This creates a local catalog and stable identities. It does not contact GitHub
or a credential provider. Repeating `init` preserves the identities and returns
`created: false`.

## 2. Bind the checkout and record its IDs

Move to the project checkout and bind it to a logical Project:

```sh
cd /path/to/atlas-api
ctxferry project bind . --slug atlas-api --format json
ctxferry project inspect --format json
```

A successful bind has this shape:

```json
{
  "schemaVersion": 1,
  "ok": true,
  "data": {
    "project": { "id": "<project-uuid>", "slug": "atlas-api" },
    "binding": {
      "path": "/path/to/atlas-api",
      "membershipId": "<membership-uuid>",
      "repositoryId": "<repository-uuid>"
    },
    "created": true
  },
  "error": null
}
```

Copy the membership and Repository IDs for later steps. The membership connects
the Project to a scope in a source Repository; Tasks use it to find their
working directory. The binding itself and its absolute path stay on this
device. CtxFerry does not add files to your source checkout.

`project inspect` should report `resolution.source: "path-binding"` and the
current checkout. Later commands run from this checkout resolve `atlas-api`
without an explicit `--project` flag.

## 3. Add shared instructions and a local-only note

Create a UTF-8 Markdown file named `development.md` with content suitable for a
private GitHub repository:

```md
# Development workflow

Run `go test ./...` before publishing a change.
The integration database contains disposable test data only.
Use the registered integration-check Task instead of pasting its connection
string into a shell or agent session.
```

Register it as shared Context with full agent disclosure:

```sh
ctxferry context put \
  --title 'Development workflow' \
  --purpose project-summary \
  --source 'Atlas maintainers' \
  --summary 'Build, test, and integration database conventions' \
  --body-file /path/to/development.md \
  --storage shared --disclosure full --format json
```

Save `data.document.id` as `<shared-document-uuid>`. The receipt contains
metadata and a revision, not the document body.

Now create a second Markdown file, `private-notes.md`, and register it with a
different storage policy:

```sh
ctxferry context put \
  --title 'Private launch notes' \
  --purpose notes \
  --source 'Local maintainer' \
  --summary 'Notes that must not leave this device' \
  --body-file /path/to/private-notes.md \
  --storage local-only --disclosure full --format json
```

Save this ID as `<local-document-uuid>`. Then inspect the Project as an agent
would:

```sh
ctxferry context show --format json
ctxferry context get <shared-document-uuid> --format json
ctxferry context search 'integration database' --format json
```

The `show` snapshot should include both document indexes and the shared Project
summary, but no document body:

```text
summary: Build, test, and integration database conventions
documents:
  - Development workflow    storage: shared      disclosure: full
  - Private launch notes    storage: local-only  disclosure: full
truncated: false
```

`get` returns the shared body because its disclosure is `full`; `show` still
returns only the index. Storage and disclosure are independent: a `shared`
document's complete body is eligible for plaintext publication even when its
agent disclosure is only `summary` or `reference`.

## 4. Register and run a credential-free Task

Register a Task using the membership ID from step 2. Each argument is a separate
`--arg`; CtxFerry does not evaluate a shell command string.

```sh
ctxferry task add unit-tests \
  --purpose 'Run the Go unit tests' \
  --membership <membership-uuid> \
  --working-directory . \
  --program go --arg=test --arg=./... \
  --storage shared --format json
```

Inspect and run it:

```sh
ctxferry task inspect unit-tests --format json
ctxferry run unit-tests --format jsonl
```

A passing run emits a start event, forwarded output, and a final child result:

```jsonl
{"type":"start","taskId":"<task-uuid>","task":"unit-tests"}
{"type":"stdout","data":"ok  \texample.com/atlas-api\t0.457s\n"}
{"type":"result","origin":"child","exitCode":0,"truncated":false}
```

This Task needs neither provider authentication nor execution-trust approval.
Its working directory comes from the bound Repository membership, not from an
arbitrary caller directory. A failing child preserves its own exit code;
CtxFerry control-plane failures use `origin: "control-plane"` instead.

## 5. Describe the database and integration Task

Register the database's non-secret endpoint. Replace the example host with a
real development endpoint; do not put a username, password, or connection
string in `--endpoint`.

```sh
ctxferry resource add \
  --identity dev-db --kind database --stage dev \
  --purpose 'Disposable integration-test database' \
  --storage shared --endpoint db.example.com:5432 --format json
```

Save `data.id` as `<resource-uuid>`. If this device reaches that logical service
through a local forward instead, add a device-local route:

```sh
ctxferry resource route <resource-uuid> \
  --endpoint localhost:15432 --format json
```

The route replaces neither the shared endpoint nor another device's route.
Next, register the integration Task with one Resource slot and one credential
requirement:

```sh
ctxferry task add integration-check \
  --purpose 'Run integration checks against disposable data' \
  --membership <membership-uuid> \
  --working-directory . \
  --program go --arg=test --arg=-tags=integration --arg=./... \
  --storage shared \
  --resource-slot '{"name":"db","kind":"database","stage":"dev"}' \
  --credential-requirement '{"name":"login","resourceSlot":"db","purpose":"integration-tests","type":"connection-string","env":"DATABASE_URL"}' \
  --format json
```

This says that `integration-check` needs a development database and one
connection string delivered as `DATABASE_URL`. It does not name a backend entry
or contain a credential value.

## 6. Connect the requirement to an exact provider entry

Create a Profile, map the Task's `db` slot to the registered Resource, and
select the Profile for this checkout:

```sh
ctxferry profile create development --format json
ctxferry profile map-resource development integration-check db \
  <resource-uuid> --format json
ctxferry profile select development --format json
```

Create the shared credential reference. Replace the backend project,
environment, folder, and key with the exact Infisical entry that contains the
development connection string:

```sh
ctxferry secret bind integration-check login --profile development \
  --alias integration-db \
  --provider infisical --provider-profile team --method universal-auth \
  --backend-project <infisical-project-id> \
  --environment dev --path /atlas-api --key ATLAS_DB_URL \
  --format json
```

The backend key `ATLAS_DB_URL` is deliberately independent of the child
variable `DATABASE_URL`. The command creates SecretBinding metadata and an
explicit Profile mapping; it neither reads nor stores the entry's value.

```sh
ctxferry secret list --format json
ctxferry task inspect integration-check --format json
```

The list snapshot includes the provider profile and locator, but no value:

```text
alias: integration-db
providerProfile: infisical / team / universal-auth
locator: <infisical-project-id> / dev / /atlas-api / ATLAS_DB_URL
resourceId: <resource-uuid>
purpose: integration-tests
type: connection-string
```

## 7. Authenticate, review trust, and run on device A

Supply `INFISICAL_CLIENT_ID` and `INFISICAL_CLIENT_SECRET` through your existing
protected launcher. With those variables present for this invocation, run:

```sh
CTXFERRY_INFISICAL_PROFILE=team \
  ctxferry provider login infisical \
  --provider-profile team --method universal-auth --format json
```

CtxFerry stores the resulting access token in this device's operating-system
credential store. It does not write a plaintext token file or synchronize the
session. For personal authentication, headless token injection, or Bitwarden,
use the provider-specific procedure in [Credentials](credentials.md).

Check the Resource and exact provider entry independently:

```sh
ctxferry resource check <resource-uuid> --format json
ctxferry secret status integration-check login \
  --profile development --format json
```

`resource check` performs a two-second TCP probe; it does not prove the database
protocol or login works. After a reachable probe and successful provider read,
the `data` facts should include:

```json
{
  "bound": true,
  "authenticated": true,
  "entryRetrievable": true,
  "resourceReachable": true
}
```

No credential value appears in this report. A `null` fact is unknown, not a
successful check.

Before the first credentialed run, preview the exact execution definition:

```sh
ctxferry task trust preview integration-check \
  --profile development --format json
```

Review at least `definition.program`, `definition.argv`, `checkoutRoot`,
`workingDirectory`, the Resource mapping, and every credential locator. The
initial `data.state` is `not-enabled`. Enable exactly the preview you inspected:

```sh
ctxferry task trust enable integration-check --profile development \
  --preview <preview-token> --format json
```

Copy `<preview-token>` from `data.preview`, not from the Task revision. Then run:

```sh
ctxferry run integration-check --profile development --format jsonl
```

The child receives a minimal base environment plus `DATABASE_URL`, not your
entire shell environment, Git authentication, or provider token. CtxFerry
replaces delivered values it recognizes in forwarded output with `[REDACTED]`.
The child can still read, encode, print, or transmit the value: a Task is not an
operating-system security boundary.

## 8. Review and publish the shared catalog

Prepare Git authentication and GitHub metadata authentication as described in
[Private sync](sync.md#enable-and-verify-a-private-destination). Then verify the
dedicated destination without uploading anything:

```sh
ctxferry sync enable \
  --remote git@github.com:<owner>/atlas-context.git --format json
```

Continue only when `data.state` is `"enabled"` and `data.check` reports both
`gitReachable: true` and `privacy: "private"`. An unverified result does not
authorize publication.

Preview the exact export:

```sh
ctxferry sync preview --format json
```

Inspect every `data.files` path and the original shared source material. Save
`data.previewId`. The export includes shared Context bodies, the Resource and
Task definitions, Profile mappings, and the SecretBinding's locator. It excludes
the local-only note, checkout bindings and paths, local routes and observations,
Profile selection, execution trust, provider sessions, and credential values.
Repository history retains published plaintext, including later-deleted
content.

When that exact snapshot is approved, initialize the sync branch:

```sh
ctxferry sync init --preview <preview-id> --format json
ctxferry sync status --format json
```

A successful first publication returns `data.state: "synced"`, a confirmed
`data.head`, and eventually `pending: 0` in status. Any shared change after the
preview makes it stale; preview again instead of forcing publication.

## 9. Join and run from device B

On the second device, install the same CLI and prepare Git/GitHub
authentication. Start with a fresh CtxFerry data root. Do **not** copy device
A's SQLite catalog, WAL files, locks, managed Git repository, or credential
store, and do not run `init` to create a separate Workspace first.

Join the existing Workspace and subscribe to the Project:

```sh
ctxferry join git@github.com:<owner>/atlas-context.git \
  --subscribe atlas-api --format json
ctxferry project list --format json
```

Move to device B's own source checkout and bind it to the synchronized Project
and Repository:

```sh
cd /path/to/device-b/atlas-api
ctxferry project bind . --project atlas-api \
  --repository <repository-uuid> --format json
ctxferry project inspect --format json
ctxferry profile select development --format json
```

The Project, Repository, membership, Task, Resource, and SecretBinding IDs now
match device A. The Device ID and checkout path are different and local.
Inspect the synchronized context without logging in to Infisical:

```sh
ctxferry context show --format json
ctxferry context get <shared-document-uuid> --format json
ctxferry task inspect integration-check --format json
ctxferry secret list --format json
```

The shared document and credential **reference** are available offline after
join. The local-only note is not. This expected failure demonstrates that
storage policy was enforced:

```sh
ctxferry context get <local-document-uuid> --format json
# error.code: document-not-found
```

Device B must establish its own network evidence, provider authentication, and
execution trust. If it uses a local forward, set its own route first:

```sh
ctxferry resource route <resource-uuid> \
  --endpoint localhost:25432 --format json
```

Then authenticate and inspect the current state:

```sh
CTXFERRY_INFISICAL_PROFILE=team \
  ctxferry provider login infisical \
  --provider-profile team --method universal-auth --format json
ctxferry resource check <resource-uuid> --format json
ctxferry secret status integration-check login \
  --profile development --format json
ctxferry task trust preview integration-check \
  --profile development --format json
```

The trust preview must again start as `not-enabled`: device A's approval did
not synchronize. Review device B's resolved paths and the same execution
fields, then enable and run:

```sh
ctxferry task trust enable integration-check --profile development \
  --preview <device-b-preview-token> --format json
ctxferry run integration-check --profile development --format jsonl
```

For later shared edits, run `ctxferry sync --format json` on the editing device,
then on the other device. Local reads remain available when sync is offline;
`sync status --format json` distinguishes pending, offline, conflict, and synced
state.

## Proof-of-concept checklist

You have exercised the core product promise when all of these are true:

| Expectation | Evidence from this tutorial |
| --- | --- |
| Local-first context | Initialization, binding, Context reads, and the unit Task work before sync or provider login. |
| Progressive disclosure | `context show` indexes documents without bodies; `context get` returns only what disclosure permits. |
| Repeatable execution | Both devices run the same Task and argument array from their own bound checkouts. |
| Explicit credential resolution | The Task requirement, Profile mapping, Resource, and exact provider locator agree; no similar entry is guessed. |
| Device-local security decisions | Each device performs its own provider login, Resource observation, and execution-trust approval. |
| Deliberate plaintext sync | First upload requires repository verification and an exact preview; the local-only note never reaches device B. |
| No secret-value transport | `secret list`, `secret status`, sync records, and output snapshots contain references and facts, not the connection string. |

To check the corresponding implementation paths from a CtxFerry source
checkout, run:

```sh
go test -count=1 ./tests \
  -run '^(TestFirstPrivatePublish|TestTwoDeviceSync|TestCredentialedRun)$'
```

These integration tests use an isolated local Git transport and a fake provider;
they do not replace the live GitHub, database, and Infisical checks above. Run
the full suite before changing the implementation:

```sh
go test -count=1 ./...
go vet ./...
```

For deeper reference, continue to [Context](context.md), [Resources and
Tasks](tasks-and-resources.md), [Credentials](credentials.md), and [Private
sync](sync.md).
