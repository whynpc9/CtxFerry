---
title: CtxFerry user guide
---

English · [简体中文](zh-CN/index.md)

CtxFerry gives coding agents on each machine approved project context and
registered Tasks for credentialed commands, without putting private notes or
secret values in source Git or agent command lines. A private Git repository
synchronizes shared context and credential references between machines; each
device keeps its own provider login and execution-trust decisions.

## Choose your goal

### Start here

- [Install and bind a checkout](getting-started.md): build the CLI from source,
  initialize a local workspace, and inspect a Project without network access.
- [Follow the coding-agent tutorial](tutorial.md): connect an agent on two
  machines, discover private Context and run a credentialed Task in five steps.
- [Open the native desktop](desktop.md) to follow a conflict from Home to the
  queue, inspect a Checkout’s effective configuration or find tools in Catalog.

### Work in a project

- [Write and retrieve Context](context.md): choose storage and agent disclosure,
  search local context, and explain Profile and device overrides.
- [Describe Resources and run Tasks](tasks-and-resources.md): register a service,
  check its TCP reachability, and run a pinned command from its checkout.
- [Use credential references](credentials.md): map a Task requirement, inspect
  provider state, and approve the exact execution definition on this device.
- [Install Skills and file Artifacts](skills-and-artifacts.md): preview a Skill
  projection, and register, retrieve, or explicitly share a run result.

### Work across devices

- [Publish and join private sync](sync.md): review plaintext scope, initialize a
  dedicated repository, join a second device, and choose foreground or
  background synchronization.
- [Resolve conflicts and recover data](recovery.md): compare record versions,
  restore one document, recover a local device, or restore shared history under
  a new epoch.

### Reference

- [CLI reference](cli-reference.md): look up global flags, output contracts,
  command syntax, and command-specific options.

### Troubleshooting

- [Diagnose local and sync state](diagnostics.md): interpret `doctor`, sync
  status, authentication evidence, and Resource observations without treating
  unknown state as success.

## Know the storage boundary

CtxFerry has three separate sources of truth:

- The **local catalog** holds device paths, unpublished work, cached data, and
  local-only records. It remains readable offline.
- The **private sync repository** holds explicitly shared records and eligible
  objects in plaintext. Repository access covers the whole workspace; a Project
  subscription is not an access-control boundary.
- The **external credential provider** remains the source of credential values.
  CtxFerry synchronizes references, not provider sessions or secret values.

Agent disclosure is independent of sync storage. A shared document can publish
its full body even when `context get` returns only its summary or reference.
Review shared source material before publication, and keep anything that must
not enter private Git history local-only.
