---
title: Skills and run Artifacts
---

English · [简体中文](zh-CN/skills-and-artifacts.md)

Skills are pinned packages projected into an agent's user-level directory.
Artifacts are immutable files associated with a Project and run. Both require
explicit selection; neither one recursively watches a source directory.

## Import and preview a Skill

After `ctxferry init`, choose one local directory containing `SKILL.md`. Its
YAML frontmatter must declare a valid lowercase `name`, which becomes the
install directory name:

```yaml
---
name: example
license: MIT
---
```

Import the directory and preview the Codex installation:

```sh
ctxferry skill add --local ./skills/example --format json
ctxferry skill plan example --target codex --format json
```

The add result records the source, content digest, license, file manifest, and
platform or executable requirements. The local snapshot remains independent of
later edits to the source directory. Import and installation do not execute
Skill scripts or post-install hooks.

The plan shows the exact target path, copy or symlink mode, intended writes, and
blocking issues without changing the target or local package state. A blocked
plan is still a successful query with `status: "blocked"`; inspect `issues` for
name collisions, external edits, missing dependencies, platform mismatches, or
recovery conflicts.

Apply only a plan you have reviewed:

```sh
ctxferry skill apply example --target codex --format json
```

A successful result reports `status: "installed"`. Repeating apply is
idempotent. CtxFerry refuses unowned targets and externally edited installs,
and records ownership and content digests per device. If apply is interrupted,
run the same command again to resume the verified staged installation.

Copy is the default. Add `--symlink` to both plan and apply to link to the
verified local package; the operating system must permit link creation.
Targets are:

- Codex: `~/.agents/skills`.
- Claude Code: `~/.claude/skills`, or `$CLAUDE_CONFIG_DIR/skills` when set to an
  absolute path.

Use `--target claude-code` in place of `codex` for Claude Code.

## Pin a Git source and manage changes

Git imports require a full commit hash or a tag under `refs/tags/`; a moving
branch is not accepted:

```sh
ctxferry skill add --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example --format json
ctxferry skill plan another-example --target claude-code --symlink --format json
ctxferry skill apply another-example --target claude-code --symlink --format json
```

A tag records both its name and resolved commit. Use `--license` to override
frontmatter; an absent license becomes `NOASSERTION`. Repeat `--platform` and
`--depends-on <executable>` to declare compatibility requirements.

Preview an update or rollback before writing. Repeat the same target, mode, and
Git ref with the returned confirmation token:

```sh
ctxferry skill update example --target codex --format json
ctxferry skill update example --target codex --confirm <confirmation> --format json
ctxferry skill rollback example --target codex --format json
ctxferry skill rollback example --target codex --confirm <confirmation> --format json
```

The confirmation covers the exact pins and file additions, removals, digests,
and executable bits. If the source changes, preview again. The previous complete
version remains available locally for rollback. Updates and rollbacks preserve
copy or symlink mode and leave externally edited or unowned files in place.

### Share pins and selections

Sharing is a separate approval. Public Skills use a credential-free HTTPS Git
source and exact commit; private Skills package up to 1 MiB of selected files
for the private sync store:

```sh
ctxferry skill add --share public --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example
ctxferry skill add --share private --local ./skills/private-example
ctxferry skill group set global another-example
ctxferry skill group set project:my-project private-example
ctxferry skill group set capability:review another-example private-example
```

`group set` replaces that group's selection; pass no Skill names to clear it.
Groups can contain only shared pins. Publish or retrieve them through
[Private sync](sync.md), then preview a group on the receiving device:

```sh
ctxferry skill plan --group capability:review --target codex --format json
```

Review and apply each desired Skill separately. Installation directories,
links, receipts, and update recovery remain device-local. A changed shared pin
does not rewrite an existing installation until you preview and confirm an
update. Pin conflicts require an explicit choice through
[Recovery](recovery.md#resolve-a-record-conflict).

### Add the global CtxFerry agent entry

The built-in bootstrap previews the `ctxferry` Skill for the selected agent.

```sh
ctxferry skill bootstrap install --target codex --format json
ctxferry skill bootstrap install --target codex --preview <token-from-preview> --format json
```

| `--target` | User Skill location |
| --- | --- |
| `codex` | `~/.agents/skills/ctxferry/SKILL.md` |
| `claude-code` | `~/.claude/skills/ctxferry/SKILL.md` or `$CLAUDE_CONFIG_DIR/skills/ctxferry/SKILL.md` |
| `cursor` | `~/.cursor/skills/ctxferry/SKILL.md` |
| `pi` | `~/.pi/agent/skills/ctxferry/SKILL.md` or `$PI_CODING_AGENT_DIR/skills/ctxferry/SKILL.md` |

Cursor and Pi use their native Skill discovery. Open a new agent session after
installing. These targets install only the built-in Skill and are available to
`skill bootstrap`.

For Codex and Claude Code, bootstrap also adds a managed instruction block.
For Codex, the instruction file is `~/.codex/AGENTS.md` (or
`$CODEX_HOME/AGENTS.md`), using a nonempty `AGENTS.override.md` when present.
For Claude Code, use `--target claude-code`; the file is
`~/.claude/CLAUDE.md` or `$CLAUDE_CONFIG_DIR/CLAUDE.md`. The preview preserves
existing instructions and exposes only CtxFerry's proposed text. Use
`skill bootstrap uninstall` with the same preview-then-apply pattern to remove
only the managed entry. To refresh an older built-in Skill, uninstall and then
install it with a fresh preview for each action.

For another agent with shell access that reads repository instructions, add a
project `AGENTS.md` line telling it to run `ctxferry context show --format json`
from the current checkout when it needs project context. The CLI workflow needs
no MCP configuration.

## Register and retrieve an Artifact

Run from a bound checkout. Register one explicit result file with provenance
from the original execution. This example creates a new run; replace the source
revision and tool version with the real values, or use `unknown` when a source
revision is unavailable:

```sh
ctxferry artifact add report.json --project my-project \
  --type application/json --sensitivity internal \
  --source-revision <source-revision> --dirty=false --profile development \
  --tool go@1.27.4 --command go --command test --command=./... \
  --exit-code 0 --summary 'Unit tests passed' --completed unit --format json
```

A new run requires explicit `--dirty` and `--exit-code` values, plus a source
revision, Profile, command, and summary. Use `--profile none` when no Profile
was used. Repeat `--tool`, `--input`, `--command`, `--completed`, and
`--uncompleted` as needed. For dirty source, `--diff-id` can reference an
existing diff.

The manifest under `data` contains the new artifact `id`, immutable `run.id`,
payload size and SHA-256, source environment, and provenance. Save both IDs:

```sh
ctxferry artifact list --project my-project --run <run-uuid> --format json
ctxferry artifact inspect <artifact-uuid> --project my-project --format json
ctxferry artifact fetch <artifact-uuid> --project my-project \
  --output retrieved.json --format json
```

`list` filters by Project and optionally run. `fetch` verifies the cached bytes
before creating a new output file; it does not overwrite an existing output.
The receipt includes the output path, size, and verified hash.

Append another result to the same immutable run without supplying provenance
again:

```sh
ctxferry artifact add summary.txt --project my-project --run <run-uuid> \
  --type text/plain --sensitivity internal --format json
```

Payloads are stored once per SHA-256 in the local artifact cache. Registration
is local-only until sharing is approved. The default cache admits 1 GiB of
unique payload bytes; set `CTXFERRY_ARTIFACT_CACHE_BYTES` to a positive byte
count to change it. At capacity, new content is rejected and existing artifacts
remain retrievable.

## Share an artifact deliberately

Configure [Private sync](sync.md) first. Preview the manifest and payload mode,
inspect the actual file for private paths or pasted secrets, and approve the
exact preview ID:

```sh
ctxferry artifact share <artifact-uuid> --mode automatic --format json
ctxferry artifact share <artifact-uuid> --mode automatic \
  --preview <preview-id> --format json
ctxferry sync --format json
```

The second command queues the reviewed publication; the third publishes it.
After another device syncs and subscribes to the Project, it can use `artifact
list`, `inspect`, and `fetch` as above. Installing a verified shared snapshot
records the receiving device's integrity Observation separately from the source
run; `fetch` verifies the payload again before creating the output file.

Automatic mode admits up to 1 MiB of raw payload. `--mode explicit` allows a
reviewed sample up to 5 MiB. `--mode metadata` shares only its description.
Sensitive classification always omits the payload, and allow-lists exclude known
raw logs, credentials-related files, database dumps, model weights, archives,
and Git LFS pointers. The preview reports new payload bytes, expected omission,
and known history size when available. Approval does not scan or redact arbitrary
content.

Routine publication targets about 5 MiB of changed records and new payloads.
Over-limit or excluded content becomes metadata-only with a reason in `artifact
inspect`; the sender retains its local object. Around 100 MiB of Git history,
status warns about growth. Around 250 MiB, new automatic payloads pause while
ordinary records and eligible metadata continue. Removing a current artifact
does not erase older plaintext from Git history; rotate any accidentally
published credential.
