---
name: ctxferry
description: Use the CtxFerry CLI to find context kept outside Git, look up project resources and registered Tasks, or run a Task that needs credentials without seeing their values. Use when asked about CtxFerry, a project handoff, or a command that needs a registered Task's credentials.
---

# CtxFerry

CtxFerry carries project context and credential references kept outside Git. Run from the current checkout. For queries with `--format json`, read `data` on success and `error.code`, `error.message`, and any `error.details` on failure. Execution uses `--format jsonl` as described below.

## Find context

1. `ctxferry context show --format json`: the Project summary, this environment, and an index of documents, Resources and Tasks. Use it first. If `data.truncated` is true, continue with `--cursor <data.nextCursor>`.
2. `ctxferry context search "<keywords>" --format json`: literal keyword search over agent-visible documents and Task names.
3. `ctxferry context get <document-uuid-or-title> --format json`: one document, subject to its disclosure policy. A `summary` or `reference` document deliberately omits its body; do not try to obtain it another way.

Retrieve only what the work needs. Add `--sync` to a query when the context may have just changed on another machine.

## Run commands that need credentials

- `ctxferry task list --format json` and `ctxferry task inspect <task> --format json` show registered Tasks, their command, and the environment variables they receive.
- `ctxferry run <task> --format jsonl` runs the registered command. In the final `result` event, read `exitCode` when `origin` is `child`, or `error.code` and `error.message` when `origin` is `control-plane`.
- `ctxferry exec --task <task> --format jsonl -- <program> [args...]` runs your own command with a Task's credentials. Inspect the Task first and use `exec` only when `allowCustomCommand` is true. A Task with no registered command must allow custom commands to be used for execution.
- `ctxferry secret status <task> <requirement> --format json` checks one credential on this device without revealing it. Use the requirement's name from `task inspect`. Read `bound`, `authenticated`, and `entryRetrievable` separately in `data` or `error.details`. A standalone credential has no Resource slot. Its `resourceReachable: null` is expected and needs no Resource mapping or probe.

Credential availability and local Task trust are separate checks. A successful `secret status` does not confirm that the Task is enabled to run on this device.

Credential values are delivered only to the child process as environment variables, and CtxFerry replaces them with `[REDACTED]` in the output it forwards. Never print, echo, encode, write to files, or copy credential values into your context, and never pass them as command arguments.

## When a human must act

Stop on a handoff error. Report its code and message and, when known, the Task, requirement and Profile. Ask the human to take the action below. Leave approval and configuration changes to them. A SecretBinding mapping belongs to the Project Profile. Provider authentication and execution trust are local to the device.

| `error.code` | Ask the human to |
| --- | --- |
| `project-unbound` | bind this checkout: `ctxferry project bind .`, or on a device that joined a synced workspace, attach it to the existing Project with `ctxferry project bind . --project <slug> --repository <repository-uuid>` so no duplicate Project is created |
| `authentication-required` | sign in to the provider: `ctxferry provider login <provider> --provider-profile <name> --method <method>` |
| `credential-storage-unavailable` | restore access to this device's secure credential store and retry `ctxferry secret status <task> <requirement> --format json` |
| `execution-definition-changed` (including `details.state: not-enabled`) | review and enable the Task on this device: `ctxferry task trust preview <task>`, then `ctxferry task trust enable <task> --preview <token>` |
| `profile-required`, `profile-not-found` | choose a Profile: `ctxferry profile select <name>` |
| `binding-missing` | map the named credential with `ctxferry secret bind <task> <requirement> ...`. If the error names a Resource slot, ask the human to map the correct Resource with `ctxferry profile map-resource <profile> <task> <slot> <resource-uuid>` first |
| `secret-unavailable` | check the exact provider entry and the provider account's access to it, then rerun `ctxferry secret status <task> <requirement> --format json` |
| `custom-command-not-allowed` | use `ctxferry run <task>`, or ask whether the Task should allow custom commands |
| `command-required` | the Task has no command of its own: use `ctxferry exec --task <task> -- <program> [args...]` |

For anything else unexpected, run `ctxferry doctor --format json` and include its failing checks in your report.
