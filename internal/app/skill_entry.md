---
name: ctxferry
description: Discover the current Project's context, resources, and registered Tasks with the CtxFerry CLI, and run Tasks that need credentials without seeing the values.
---

# CtxFerry

CtxFerry carries project context and credential references that do not belong in Git. Run every command from the current checkout and add `--format json` (one JSON object) so results are machine-readable. Each response has `ok`, `data`, and `error.code`.

## Find context

1. `ctxferry context show --format json`: the Project summary, this environment, and an index of documents, Resources and Tasks. Use it first. If `data.truncated` is true, continue with `--cursor <data.nextCursor>`.
2. `ctxferry context search "<keywords>" --format json`: literal keyword search over agent-visible documents and Task names.
3. `ctxferry context get <document-uuid-or-title> --format json`: one document, subject to its disclosure policy. A `summary` or `reference` document deliberately omits its body; do not try to obtain it another way.

Retrieve only what the work needs. Add `--sync` to a query when the context may have just changed on another machine.

## Run commands that need credentials

- `ctxferry task list --format json` and `ctxferry task inspect <task> --format json` show registered Tasks, their command, and the environment variables they receive.
- `ctxferry run <task> --format jsonl` runs a registered Task. It emits `start`, `stdout`/`stderr`, and a final `result` event with `exitCode`.
- `ctxferry exec --task <task> --format jsonl -- <program> [args...]` runs your own command with a Task's credentials. It works only if the Task allows custom commands (`allowCustomCommand`).
- `ctxferry secret status <task> <requirement> --format json` checks one credential on this device (bound, provider authenticated, entry retrievable) without revealing it.

Credential values are delivered only to the child process as environment variables, and CtxFerry replaces them with `[REDACTED]` in the output it forwards. Never print, echo, encode, write to files, or copy credential values into your context, and never pass them as command arguments.

## When a human must act

Some failures are device-local gates that only the person at this machine may clear. Do not work around them, and do not run the approval commands yourself. Stop, report the error code and message, and ask the human to run the command shown:

| `error.code` | Ask the human to |
| --- | --- |
| `project-unbound` | bind this checkout: `ctxferry project bind .` |
| `authentication-required`, `credential-storage-unavailable` | sign in to the provider: `ctxferry provider login <provider> --provider-profile <name> --method <method>` |
| `execution-definition-changed` (including `details.state: not-enabled`) | review and enable the Task on this device: `ctxferry task trust preview <task>`, then `ctxferry task trust enable <task> --preview <token>` |
| `profile-required`, `profile-not-found` | choose a Profile: `ctxferry profile select <name>` |
| `binding-missing`, `secret-unavailable` | map or fix the credential: `ctxferry secret bind <task> <requirement> ...` |
| `custom-command-not-allowed` | use `ctxferry run <task>`, or ask whether the Task should allow custom commands |

For anything else unexpected, run `ctxferry doctor --format json` and include its failing checks in your report.
