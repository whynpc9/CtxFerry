---
title: Credential references and execution trust
---

English · [简体中文](zh-CN/credentials.md)

CtxFerry stores and synchronizes credential **references**, not credential
values. Each device authenticates to its own provider profile, and each device
and checkout approves the execution definition before a Task receives values.

## Declare the requirement and Resource mapping

Start with the Resource and Git membership setup in
[Resources and Tasks](tasks-and-resources.md). The following Task declares a
Resource slot named `db` and a credential requirement named `login`. The backend
key is deliberately independent of the delivered variable `DATABASE_URL`.

```sh
ctxferry task add integration-check --purpose 'Run integration checks' \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared \
  --resource-slot '{"name":"db","kind":"database","stage":"dev"}' \
  --credential-requirement '{"name":"login","resourceSlot":"db","purpose":"integration-tests","type":"connection-string","env":"DATABASE_URL"}' \
  --format json
ctxferry profile create development
ctxferry profile map-resource development integration-check db <resource-uuid>
ctxferry profile select development
```

The Resource's kind and stage must match the slot. The requirement's purpose,
type, and selected Resource must also match the eventual SecretBinding; a
similarly named entry is never substituted.

## Bind an exact provider entry

Create a binding only after the Task requirement and Profile Resource mapping
exist. Replace the locator placeholders with the exact Infisical US Cloud
namespace and key:

```sh
ctxferry secret bind integration-check login --profile development \
  --alias integration-db --provider infisical --provider-profile team \
  --method universal-auth --backend-project <infisical-project-id> \
  --environment dev --path /api --key API_DB_URL --format json
```

The result contains the shared SecretBinding metadata and the new Profile
mapping, not a value. The backend environment is a provider namespace and is
not implicitly the Resource's business stage.

When a backend supplies a stable entry ID, use `--entry-id <entry-id>` instead
of `--backend-project`, `--environment`, `--path`, and `--key`. For Bitwarden,
create the binding instead with `--provider bitwarden`, provider profile
`default`, method `personal`, and the exact login item UUID as `--entry-id`; the
adapter resolves that item's password field.

To reuse an existing binding from another Project, map the Task's Resource slot
to the same shared Resource UUID and pass `--binding <binding-uuid>` instead of
all new-binding fields. To move a mapped slot to another Resource, first run
`secret unmap <task> <requirement> --profile <name>` for each credential mapped
to that slot, change the Resource mapping, and bind the credentials again.
Unmapping does not delete the reusable SecretBinding.

## Authenticate this device

Authentication is per Workspace, Device, and provider profile. Catalog sync
does not require provider login.

### Infisical US Cloud

For a personal session, first sign in with the official Infisical CLI using the
same named profile, then let CtxFerry adopt its current access token:

```sh
infisical login
ctxferry provider login infisical --provider-profile team --method personal
```

For a machine identity, supply `INFISICAL_CLIENT_ID` and
`INFISICAL_CLIENT_SECRET` through your existing protected launcher and run the
command below. If the launcher also sets `CTXFERRY_INFISICAL_PROFILE`, login
uses the client credentials only for that provider profile:

```sh
ctxferry provider login infisical --provider-profile team --method universal-auth
```

Both login methods require network access and store the resulting access token
in the operating system's credential store. CtxFerry reports
`credential-storage-unavailable` rather than writing a plaintext token file.
Re-run login when an Infisical token expires; server-side revocation or
restrictions can apply earlier.

On a headless host without a keyring, a protected launcher can instead supply a
current machine token as `CTXFERRY_INFISICAL_ACCESS_TOKEN` to `secret status`
and credentialed runs. It must also set `CTXFERRY_INFISICAL_WORKSPACE_ID`,
`CTXFERRY_INFISICAL_DEVICE_ID`, and `CTXFERRY_INFISICAL_PROFILE` to the exact
local IDs and profile. Find the IDs with `ctxferry init --format json`. The
scoped token remains under the launcher's control and is not passed to the
Task's child.

### Bitwarden Password Manager

Install the official `bw` executable, sign in and unlock Bitwarden on this
device, and supply the resulting `BW_SESSION` through a protected environment.
Use provider profile `default`, or set `CTXFERRY_BITWARDEN_PROFILE` to the
profile you selected. To adopt the unlock key into the OS credential store, run:

```sh
ctxferry provider login bitwarden --provider-profile default --method personal
```

For headless status checks and runs, a protected launcher can supply `BW_SESSION`
together with `CTXFERRY_BITWARDEN_WORKSPACE_ID`,
`CTXFERRY_BITWARDEN_DEVICE_ID`, and `CTXFERRY_BITWARDEN_PROFILE`, scoped to the
exact local installation. The launcher must establish the valid unlock session
independently; API-key sign-in alone does not normally decrypt the vault.

Bitwarden reports login, unlock, and session validity separately. A valid key
can read previously synced vault items offline, but that does not prove current
server access or freshness. Infisical does not report an offline read
capability. Neither provider's authentication material enters sync or the
Task's child environment.

## Inspect state without exposing a value

```sh
ctxferry secret list --format json
ctxferry secret status integration-check login --profile development --format json
```

`secret list` reads catalog metadata only and does not contact a provider.
`secret status` validates the selected Task, Profile, Resource mapping, exact
SecretBinding, and locator before checking this device.

Treat the returned facts independently:

| Field | What it establishes |
| --- | --- |
| `bound` | The requirement has an explicit compatible mapping |
| `authenticated` | The selected provider authentication check succeeded; `null` means unknown |
| `entryRetrievable` | The exact entry could be read; no value is returned |
| `resourceReachable` | This device's latest matching `resource check` observation, not a new probe |
| `authentication` and `capabilities` | Method-specific login details and offline or execution capabilities |

A failure can retain these facts in `error.details` when the Task and Project
can be evaluated. `binding-missing`, `binding-conflict`, `incompatible`,
`authentication-required`, and `secret-unavailable` identify different missing
conditions. See [Diagnostics](diagnostics.md) for the next check for each state.

## Review and enable execution trust

Credentialed execution is blocked until this device and checkout enable the
current definition. Preview it, inspect every field, and then enable exactly
that preview:

```sh
ctxferry task trust preview integration-check --profile development --format json
ctxferry task trust enable integration-check --profile development \
  --preview <preview> --format json
```

Use the token in `data.preview`. Pass the same `--profile` and, when needed, the
same `--checkout <git-root>` to preview, enable, and run.

The definition covers the program and arguments, custom-command permission,
Repository membership, resolved checkout and working directory, Resource
mapping, SecretBinding mapping, provider profile, locator, and Resource purpose.
The preview state is `not-enabled`, `enabled`,
`execution-definition-changed`, or `not-required` for a Task without
credentials. For a changed definition, compare `definition`, `enabled`, and the
named `changes`.

`enable` returns `preview-stale` if the definition changed after the preview;
preview it again. Trust records never sync. Every device, including the device
that made a synchronized edit, reviews the new execution-relevant fields.
Purpose and other descriptive-only edits do not invalidate an enabled
definition.

## Run with credentials

After checking the Resource and provider state and enabling trust, run:

```sh
ctxferry run integration-check --profile development --format jsonl
```

CtxFerry resolves only this Task's requirements. The child receives a minimal
base environment plus the Task's delivered variables, not other Tasks' values,
Git authentication, provider tokens, or the caller's whole environment. Values
stay in memory for the invocation, are not written to a general secret cache,
and are replaced with `[REDACTED]` in output CtxFerry forwards.

A missing or unusable route can stop the run with `resource-unreachable` before
provider resolution; CtxFerry does not silently substitute a local Resource with
the same name. An interruption during value resolution reports `interrupted`.

The target program can still read, encode, print, or exfiltrate every value it
receives. Output replacement is not a guarantee, and a Task is not OS isolation.

A Task registered or updated with `--allow-custom-command` also permits:

```sh
ctxferry exec --task integration-check --profile development --format jsonl \
  -- go test ./...
```

`exec` uses the same Profile, checkout, trust, environment, redaction, and exit
code rules. Enabling a definition with `customCommands: true` approves arbitrary
caller-chosen programs with those credentials. Without the explicit Task
opt-in, the command stops with `custom-command-not-allowed`.
