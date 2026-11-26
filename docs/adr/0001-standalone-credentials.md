# 0001: Standalone credentials and a shorter credential setup

- Status: accepted
- Date: 2026-11-24
- Issue: #71 (part of #68)

## Context

The most common coding-agent credential is an environment-variable API key such as `OPENAI_API_KEY`. It is not tied to a service CtxFerry can route to or probe. Before this decision every credential requirement had to name a Task Resource slot, and every SecretBinding had to name a shared Resource. Delivering one API key therefore needed a synthetic Resource, a slot, a Profile created and selected by hand, `profile map-resource`, a `secret bind` call with nine provider and locator flags, `provider login`, `resource check`, and the two trust commands.

We considered three options:

1. Let credentials exist without a Resource.
2. Keep the model and have a high-level command create a hidden synthetic Resource, slot and mapping.
3. Reduce the flags of `secret bind` and `onboard import-env`, and import several fields per call.

Option 2 leaks synthetic Resources into `resource list`, `context show`, `task trust preview` and the Desktop Resources view, and splits one API key across four synced records whose revisions, dependencies and conflicts users would later meet. CtxFerry is pre-v1, so we fix the model now instead of locking tutorials, Desktop views and sync histories into a workaround.

We chose options 1 and 3.

## Decision

### Standalone credential requirements

A `CredentialRequirement` may leave `resourceSlot` empty. Such a requirement is **standalone**: its credential is not attached to a routable service. On the CLI it is written with an empty slot, `--credential openai::model-api:api-key:OPENAI_API_KEY`, or `"resourceSlot": ""` in JSON.

A SecretBinding may leave `resourceId` empty. Such a binding is **standalone**. The record shape is unchanged: the field is present and empty, so `decodeSecretBinding` still sees exactly the same fields.

Compatibility between a requirement and a binding (`compatibleSecret`) keeps the two kinds strictly apart:

| Requirement | Binding must have |
| --- | --- |
| slot-attached | the `resourceId` the Profile maps to the slot, the requirement's `purpose` and `type` (unchanged) |
| standalone | an empty `resourceId`, the requirement's `purpose` and `type` |

A slot-attached binding never satisfies a standalone requirement and the reverse. Reusing an existing binding across Tasks or Projects still needs an explicit `--binding <uuid>`; CtxFerry never guesses one.

Sync validation follows the same rule. A standalone binding has no Resource dependency, so `secret-binding` records list no dependency for it and `invalidTaskDependencies` checks the Resource only when `resourceId` is set.

### Reachability and status

`requireReachable` and `secret status` read Resource reachability only for slot-attached credentials. A standalone credential's `resourceReachable` stays `null`. There is nothing for `resource check` to probe.

### Execution trust

`ReviewedCredential.resourceSlot`, `resourceId` and `resourcePurpose` are serialized with `omitempty`. These fields are always set for slot-attached credentials, so their reviewed JSON, and therefore every existing device trust digest, is byte-for-byte unchanged. The trust format stays `ctxferry-execution-trust-v1`. A standalone credential is still covered by the digest through its requirement, delivery variable, purpose, type, binding ID, provider profile and locator.

### Desktop

`DesktopCredentialSlot` reports `Standalone` and leaves `Resource` empty for a standalone requirement. The Desktop skips the "resolve the Resource slot first" handoff and the Resource match card. It offers reusable standalone bindings, those with an empty `resourceId` and a matching purpose and type, and shows each one's alias, provider, locator and source Project before the user confirms.

### `exec`-only Tasks

A Task with `allowCustomCommand: true` may omit `program` (with an empty `argv`). Such a Task exists to hand credentials to `ctxferry exec --task <name> -- <cmd>`. `ctxferry run` refuses it with `command-required`. Every other Task still needs a program. `program` stays in the reviewed definition, so adding one later is an execution change that devices review again.

### The `default` Profile

When a command that resolves credentials or Resource slots gets no `--profile` and the checkout has no selected Profile, it uses the Project's Profile named `default` if one exists. This applies to `run`, `exec`, `task trust`, `secret bind`, `secret status`, `secret unmap` and `onboard import-env`. Other commands, such as `context show` and `config explain`, report the selection unchanged.

`secret bind` and `onboard import-env` create the `default` Profile when they get no `--profile`, the checkout has no selection and the Project has no Profiles at all. Users of a single-environment Project never run `profile create` or `profile select`. A Project that already has Profiles never gets one created implicitly: the user picks one.

The fallback is by name, never by counting Profiles, so adding a second Profile later does not silently change which one a run uses.

### Fewer `secret bind` flags

For a new binding:

- `--alias` defaults to the requirement name.
- `--path` defaults to `/` for a named entry.
- `--key` defaults to the requirement's delivery variable, so `OPENAI_API_KEY` is looked up as `OPENAI_API_KEY`.
- `--like <alias-or-uuid>` copies the provider, provider profile, method, backend project, environment and path from a binding the Project can see. Any flag given explicitly overrides the copied value. The key, or Bitwarden entry ID, is never copied: it names a different entry. The new binding is an independent record; nothing links it to the template.

After the first binding, a second API key in the same backend namespace needs only `secret bind <task> <requirement> --like <alias>`.

### Batch `onboard import-env`

`--field` may be given several times. With more than one field, each field is matched to the Task's requirement whose delivery variable has the same name; `--requirement` is accepted only with a single field. Every field must match a requirement that has no mapping yet. The same alias, key and `--like` defaults as `secret bind` apply. Each field gets its own binding, with alias and key from its requirement.

`--resource` is required for slot-attached requirements whose slot the Profile does not map yet, and it is refused when no requirement in the batch needs it.

One preview ID covers the whole batch: the file path, every field and target, and the revisions involved. Confirmation then:

1. transfers each field to the provider in order, with the existing create-only semantics;
2. commits every binding and mapping in one catalog transaction.

`--recover` verifies the provider entry against the source instead of creating it. A bare `--recover` applies to every field; `--recover=FIELD` (repeatable) applies to the named fields only. If a transfer fails part-way, the error lists the fields whose provider entries were already created, so the user previews again with `--recover=<those fields>`.

### Forward compatibility

Older clients reject synced records that use the new shapes (a standalone requirement or binding, or a Task without a program) because they fail validation. This is the same contract as `allowCustomCommand`: upgrade every device before relying on the new shapes.

## Consequences

- One API key now needs: a Task requirement with an empty slot, `secret bind` (which can create the `default` Profile), `provider login`, and `task trust preview` / `enable`. No Resource, slot mapping or `resource check` is involved.
- `resource list`, `context show` and the Desktop Resources view no longer contain synthetic Resources.
- The batch import is not atomic across the provider: provider entries created before a failure stay, and `--recover` maps them.
- The `default` Profile is a convention by name. A Project can still name and select Profiles explicitly, and an explicit selection always wins.
