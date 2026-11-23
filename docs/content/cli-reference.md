---
title: CLI reference
---

English · [简体中文](zh-CN/cli-reference.md)

This page lists the commands and command-specific flags in the current CLI. Use
the linked guides for complete workflows and safety checks. Run
`ctxferry <command> --help` or `ctxferry help <command>` for the help text of a
particular command.

## Invocation, output, and selection

```sh
ctxferry [global flags] <command> [arguments] [command flags]
```

In the tables below, angle brackets denote required positional arguments and
square brackets denote optional ones. **Repeatable** flags accept multiple
values by repeating the flag. Every command also accepts these global flags:

| Flag | Meaning |
| --- | --- |
| `--data-dir <path>` | Use this application data root instead of the platform default or `CTXFERRY_DATA_DIR`. An explicit flag takes precedence over the environment variable. |
| `--format <text\|json\|jsonl>` | Select the output format. The default is `text`; `jsonl` is valid only for `run` and `exec`. |
| `--project <uuid-or-slug>` | Explicitly select a Project. This does not bind the current directory to that Project. |
| `-h`, `--help` | Print help for the selected command. |

Most query and management commands accept `--format json`. Standard output then
contains exactly one envelope, including for help and failures:

```json
{"schemaVersion":1,"ok":true,"data":{},"error":null}
```

A failure has `ok: false`, `data: null`, and an `error` containing a stable
`code`, a human-readable `message`, and optional structured `details`.
Diagnostics go to standard error. `run` and `exec` accept only `text` or
`jsonl`; requesting `json` is a usage error. Their JSONL streams contain
execution events rather than this envelope.

The general exit codes are 0 for success, 1 for an application, storage, or
execution control-plane error, and 2 for a usage error. After a Task or custom
command starts successfully, `run` or `exec` returns the child process's exit
code.

## Workspace and Projects

### Initialize a Workspace

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry init` | — | Create or reuse the local Workspace and Device identity without logging in. |

See [Install and bind your first checkout](getting-started.md) for data-directory
and binding examples.

### `project`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry project bind <path>` | `--slug <slug>`, `--repository <uuid>`, `--remote <url>` (repeatable) | Bind a directory to a new or explicitly selected Project; optionally select a Repository or accept its remote aliases. |
| `ctxferry project list` | `--all` | List subscribed local Projects; `--all` also includes unsubscribed Projects. |
| `ctxferry project inspect [uuid-or-slug]` | — | Inspect the selected Project, or resolve the Project from the current directory when no selector is given. |
| `ctxferry project remote add <repository-uuid> <url>` | — | Explicitly accept a remote URL as an alias of an existing Repository. |
| `ctxferry project subscribe <project>` | — | Subscribe this Device to a Project. |
| `ctxferry project unsubscribe <project>` | — | Remove this Device's Project subscription without deleting the shared Project. |

### `onboard`

The onboarding commands preserve progress in the selected application data
root. Start with `onboard start`, inspect `onboard status`, and confirm the exact
preview with `onboard bind`.

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry onboard start <directory>` | `--repository <uuid>` | Save a directory and preview its Project resolution. |
| `ctxferry onboard status` | `--repository <uuid>` | Resume saved progress and review the current Project preview. |
| `ctxferry onboard bind` | `--preview <id>`, `--repository <uuid>` | Confirm the preview and bind the saved directory. |
| `ctxferry onboard scan <note>` | — | Report credential-like findings in one selected note. |
| `ctxferry onboard import <note>` | `--title <title>`, `--summary <text>`, `--storage <local-only\|shared>`, `--disclosure <full\|summary\|reference\|local-tool>` | Import one explicitly classified note. Title, storage, and disclosure are required. |
| `ctxferry onboard scan-env <file>` | — | List env field names and matching Task credential requirements without importing values. |
| `ctxferry onboard import-env <file>` | `--field <name>`, `--task <name-or-uuid>`, `--requirement <name>`, `--profile <name>`, `--resource <uuid>`, `--alias <alias>`, `--provider <name>`, `--provider-profile <name>`, `--method <method>`, `--backend-project <id>`, `--environment <name>`, `--path <path>`, `--key <key>`, `--recover`, `--confirm <preview-id>` | Preview one env field's provider import and Profile mapping; use the returned preview ID with `--confirm` to apply it. `--recover` verifies an existing backend entry against the source first. |
| `ctxferry onboard finish` | — | Run `doctor` and complete the saved onboarding flow. |

## Context and configuration

### `context`

Shared Context publishes the complete document body even when agent disclosure
is restricted. Review [Write and retrieve Context](context.md) before choosing
`--storage shared`.

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry context put [document-uuid]` | `--body-file <path\|->`, `--title <title>`, `--purpose <purpose>`, `--source <description>`, `--summary <text>`, `--summary-file <path>`, `--storage <local-only\|shared>`, `--disclosure <full\|summary\|reference\|local-tool>`, `--valid-until <rfc3339>`, `--if-revision <revision>` | Create a document, or replace the identified document at its current revision. Body file, storage, and disclosure are required; `-` reads the body from standard input. Use `--purpose project-summary` for the Project summary. |
| `ctxferry context show` | `--cursor <uuid>`, `--sync` | Read the Project summary, execution environment, and a bounded document, Resource, and Task index. Use the returned cursor for another page; `--sync` waits for a successful sync first. |
| `ctxferry context get <document-uuid>` | `--sync` | Retrieve a document through its agent-disclosure policy, optionally after a successful sync. |
| `ctxferry context search <keywords>` | `--sync` | Search agent-visible content with literal keywords, returning at most 100 results. |
| `ctxferry context delete <document-uuid>` | `--if-revision <revision>` | Delete a document at the required current revision. |
| `ctxferry context restore <document-uuid>` | `--from-commit <sha>`, `--if-revision <revision>` | Restore a document from the required ancestor commit at its current revision. |
| `ctxferry context explain` | `--profile <name>` | Show effective ordinary configuration and the origin of each field. |

`--summary` and `--summary-file` are mutually exclusive. The title, purpose, and
source of a document are agent-visible metadata.

### `config`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry config set <field> <json-value>` | `--layer <global\|project\|profile\|device\|checkout>`, `--profile <name>` | Set one ordinary field to a JSON value at exactly one layer. |
| `ctxferry config unset <field>` | `--layer <global\|project\|profile\|device\|checkout>`, `--profile <name>` | Remove one field from exactly one layer. |

For the `profile` layer, supply its Profile name with `--profile`.

### `profile`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry profile create <name>` | — | Create a Project Profile. |
| `ctxferry profile select <name>` | — | Select a Profile for the current checkout. |
| `ctxferry profile clear` | — | Clear the current checkout's Profile selection. |
| `ctxferry profile map-resource <profile> <task> <slot> <resource-uuid>` | — | Set an explicit Resource mapping for a Task slot in a Profile. |

## Resources, Tasks, and credentials

### `resource`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry resource add` | `--identity <identity>`, `--kind <kind>`, `--purpose <purpose>`, `--stage <stage>`, `--storage <local-only\|shared>`, `--network <label>`, `--endpoint <host:port>` (repeatable), `--platform <os[/arch]>` (repeatable) | Register a service and its non-secret configuration. Storage is required; supply a network label when applicable. `resource register` is an alias. |
| `ctxferry resource list` | — | List configured Resources for the Project. |
| `ctxferry resource inspect <resource-uuid>` | — | Read service configuration, this Device's resolved route, and prior probe history without connecting. |
| `ctxferry resource route <resource-uuid>` | `--endpoint <host:port>`, `--network <label>`, `--proxy <host:port>` | Replace or clear this Device's route facts. |
| `ctxferry resource check <resource-uuid>` | — | Open one TCP connection to the resolved endpoint and record an Observation. |

### `task`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry task add <name>` | `--purpose <purpose>`, `--input <description>` (repeatable), `--resource-slot <json>` (repeatable), `--credential-requirement <json>` (repeatable), `--membership <uuid>`, `--working-directory <path>`, `--program <executable>`, `--arg <argument>` (repeatable), `--platform <os[/arch]>` (repeatable), `--storage <local-only\|shared>`, `--allow-custom-command` | Register a Task and its execution definition. The working directory defaults to `.`; storage is required. Use `--arg=VALUE` for an argument beginning with a dash. |
| `ctxferry task list` | — | List Tasks and their requirements. |
| `ctxferry task inspect <name-or-uuid>` | — | Inspect one Task and its requirements. |
| `ctxferry task update <name-or-uuid>` | `--if-revision <revision>`, `--purpose <purpose>`, `--input <description>` (repeatable), `--resource-slot <json>` (repeatable), `--credential-requirement <json>` (repeatable), `--membership <uuid>`, `--working-directory <path>`, `--program <executable>`, `--arg <argument>` (repeatable), `--no-args`, `--platform <os[/arch]>` (repeatable), `--allow-custom-command[=<true\|false>]` | Replace only the supplied fields at the required current revision. Repeated list flags replace the whole list; `--no-args` clears arguments and cannot be combined with `--arg`. |
| `ctxferry task trust preview <task>` | `--checkout <path>`, `--profile <name>` | Show the execution definition and changes since it was enabled on this Device. |
| `ctxferry task trust enable <task>` | `--checkout <path>`, `--profile <name>`, `--preview <token>` | Enable the exact previewed execution definition on this Device. |

Resource-slot JSON contains `name`, `kind`, and `stage`. Credential-requirement
JSON contains `name`, `resourceSlot`, `purpose`, `type`, and `env`. Execution
changes require a new trust preview and enablement before credentialed execution;
trust records remain local and are not synchronized.

### `run` and `exec`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry run <task>` | `--checkout <path>`, `--profile <name>` | Run the registered Task from its Repository membership in a local checkout. |
| `ctxferry exec --task <task> -- <program> [args...]` | `--task <name-or-uuid>`, `--checkout <path>`, `--profile <name>` | Run a caller-chosen program with a Task's working directory and credentials when that Task allows custom commands. Arguments after `--` are passed as an array, never through a shell. |

Both commands accept `--format text` or `--format jsonl`. Text streams child
output; JSONL emits `start`, `stdout`, `stderr`, and a final `result` event.
Delivered credential values are replaced with `[REDACTED]` in forwarded output,
but the target program can still read, print, encode, or exfiltrate them. A Task
is not operating-system isolation. See
[Resources and Tasks](tasks-and-resources.md#run-a-local-task) and
[Credentials](credentials.md) before enabling credentialed execution.

### `secret` and `provider`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry secret bind <task> <requirement>` | `--binding <uuid>`, `--alias <alias>`, `--profile <name>`, `--provider <name>`, `--provider-profile <name>`, `--method <method>`, `--entry-id <id>`, `--backend-project <id>`, `--environment <name>`, `--path <path>`, `--key <key>` | Create and map a SecretBinding, or reuse an existing shared binding by UUID. The Profile defaults to the current checkout's selection. |
| `ctxferry secret list` | — | List SecretBinding metadata without contacting a provider. |
| `ctxferry secret status <task> <requirement>` | `--profile <name>` | Check one explicitly mapped credential on this Device without returning its value. |
| `ctxferry secret unmap <task> <requirement>` | `--profile <name>` | Remove an explicit credential mapping from a Profile. |
| `ctxferry provider login <provider>` | `--provider-profile <name>`, `--method <method>` | Authenticate a provider profile on this Device. |

For a named backend entry, use the provider, project, environment, path, and key
locator fields appropriate to that provider; `--entry-id` selects an exact stable
backend entry instead. Secret values and provider sessions are not synchronized.

## Skills and artifacts

### `skill`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry skill add` | `--local <directory>`, `--git <url-or-path>`, `--ref <commit-or-tag>`, `--subdir <path>`, `--license <identifier>`, `--depends-on <executable>` (repeatable), `--platform <os[/arch]>` (repeatable), `--share <public\|private>` | Snapshot one explicit local directory or pinned Git tree as a Skill package. |
| `ctxferry skill plan [name]` | `--group <group>`, `--target <codex\|claude-code>`, `--symlink` | Preview projected files, collisions, and requirements without writing. Plan one Skill or a named group selection. |
| `ctxferry skill apply <name>` | `--target <codex\|claude-code>`, `--symlink` | Install or resume installation of a pinned Skill at the user level. |
| `ctxferry skill update <name>` | `--ref <commit-or-tag>`, `--license <identifier>`, `--target <codex\|claude-code>`, `--confirm <token>` | Preview an explicit pin change; apply it only with the confirmation from that exact preview. |
| `ctxferry skill rollback <name>` | `--target <codex\|claude-code>`, `--confirm <token>` | Preview or confirm restoration of the previously installed Skill. |
| `ctxferry skill bootstrap install` | `--target <codex\|claude-code>`, `--preview <token>` | Preview changes to the CtxFerry Skill and global instructions; apply the current preview with its token. |
| `ctxferry skill bootstrap uninstall` | `--target <codex\|claude-code>`, `--preview <token>` | Preview or confirm removal of the CtxFerry bootstrap entry. |
| `ctxferry skill group set <group> [name...]` | — | Replace a `global`, `project:<project>`, or `capability:<name>` Skill selection. |

`--local` and `--git` identify alternative package sources. Git references must
be a full commit hash or `refs/tags/<release>`. The default Git subdirectory is
the repository root. See [Skills and artifacts](skills-and-artifacts.md) for
projection and confirmation examples.

### `artifact`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry artifact add <file>` | `--run <uuid>`, `--command <argv-element>` (repeatable), `--source-revision <revision>`, `--diff-id <id>`, `--dirty=<true\|false>`, `--exit-code <code>`, `--profile <name\|none>`, `--input <identity>` (repeatable), `--tool <tool-and-version>` (repeatable), `--completed <scope>` (repeatable), `--uncompleted <scope>` (repeatable), `--summary <text>`, `--sensitivity <public\|internal\|sensitive>`, `--type <media-type>` | Register one file for a new run or an existing run, including its provenance and payload metadata. |
| `ctxferry artifact list` | `--run <uuid>` | List artifacts for the Project, optionally restricted to one run. |
| `ctxferry artifact inspect <artifact-uuid>` | — | Show the artifact manifest. |
| `ctxferry artifact fetch <artifact-uuid>` | `-o <path>`, `--output <path>` | Verify the payload and copy it to a new destination file. The output flag is required. |
| `ctxferry artifact share <artifact-uuid>` | `--mode <automatic\|explicit\|metadata>`, `--preview <id>` | Preview an artifact publication, or approve the inspected preview ID. The default mode is `automatic`. |

Artifact sharing is explicit even when the containing Project is shared. Inspect
the original payload before approving publication; sensitivity labels do not
remove secrets automatically.

## Synchronization and recovery

### `sync` and `join`

Sync publishes eligible shared records in plaintext to a dedicated private Git
repository. Review [Private sync](sync.md) before the first publication.

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry sync` | — | Run one bounded foreground synchronization round. |
| `ctxferry sync enable` | `--remote <url>`, `--branch <name>` | Verify Git access, repository identity, and private GitHub visibility without uploading. The default branch is `ctxferry-v1`. |
| `ctxferry sync preview` | — | List the exact plaintext files, sizes, and hashes proposed for the first upload and return a preview ID. |
| `ctxferry sync init` | `--preview <id>` | Create the sync branch from the required current preview using an expected-absent lease; never overwrite an existing branch. |
| `ctxferry sync status` | — | Read the last observed sync state, pending records, conflicts, and confirmed head without contacting the remote. |
| `ctxferry sync check` | — | Recheck configured Git reachability and authenticated GitHub privacy without publishing. |
| `ctxferry sync conflicts <record-uuid>` | — | Show the preserved versions and details of one record conflict. |
| `ctxferry sync resolve <record-uuid>` | `--if-conflict <id>`, `--take <local\|remote>`, `--file <path>` | Resolve a conflict using the required conflict ID and either an explicit side or strict replacement JSON with an inline body. |
| `ctxferry sync serve` | — | Run the synchronization service in the foreground. |
| `ctxferry sync install-service` | — | Install background synchronization for the current user session. |
| `ctxferry join <git-remote>` | `--branch <name>`, `--subscribe <uuid-or-slug>` (repeatable), `--all-projects` | Join an existing Workspace from its sync branch with a new local Device identity. The default branch is `ctxferry-v1`; subscribe to selected Projects, all Projects, or none by default. |

### `workspace`

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry workspace backup` | `--to <directory>`, `--include-local-only` | Save a plaintext catalog snapshot and confirmed Git history in a new backup directory; optionally include local-only content and Device settings. |
| `ctxferry workspace recover-local` | `--from <directory>` | Recover the saved Device into a new application data directory. |
| `ctxferry workspace restore` | `--from <directory>`, `--if-head <commit>`, `--new-epoch` | Publish an older Workspace snapshot under a new epoch at the required current published Git head. |

Backups can contain sensitive plaintext. Local recovery and shared restoration
are different operations; see [Recovery](recovery.md) before restoring shared
history.

## Diagnostics

| Command | Command-specific flags | Purpose |
| --- | --- | --- |
| `ctxferry doctor` | — | Report local Device capabilities, catalog and binding evidence, recorded sync state, optional dependencies, and Resource observations. |

`doctor` works offline and does not initialize or migrate a missing catalog,
probe Resources, or perform live Git or provider authentication. A successful
command does not mean every reported capability is healthy; inspect the
individual checks in [Diagnostics](diagnostics.md).
