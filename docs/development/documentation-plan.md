# User documentation plan

The product landing page links to the guide at `/CtxFerry/docs/`. The guide
serves a developer starting with one checkout, then adding tools or a second
device. A coding agent can follow the same examples and use the JSON output.

| Navigation group | Page under `docs/content/` | What the reader should be able to do |
| --- | --- | --- |
| Start here | [index.md](../content/index.md) | Choose the next guide from a goal. |
| Start here | [getting-started.md](../content/getting-started.md) | Install the CLI from source, bind a checkout and inspect the result offline. |
| Work in a project | [context.md](../content/context.md) | Write and retrieve a note, choose its storage and disclosure, and explain Profile overrides. |
| Work in a project | [tasks-and-resources.md](../content/tasks-and-resources.md) | Describe a resource, check reachability and run a local Task. |
| Work in a project | [credentials.md](../content/credentials.md) | Bind a credential requirement, inspect provider state and review execution trust. |
| Work in a project | [skills-and-artifacts.md](../content/skills-and-artifacts.md) | Preview a Skill installation and register or retrieve an artifact. |
| Work across devices | [sync.md](../content/sync.md) | Review plaintext scope, publish to a private repository and join from a second device. |
| Work across devices | [recovery.md](../content/recovery.md) | Inspect a conflict, choose a resolution and distinguish local recovery from shared restore. |
| Desktop preview | [desktop.md](../content/desktop.md) | Start the native viewer, read Overview and Sync, and find the CLI action behind each view. |
| Troubleshooting | [diagnostics.md](../content/diagnostics.md) | Interpret doctor and sync status, then choose a specific next check. |

Keep prerequisites next to the first command that needs them. Give examples an
observable result and link to the next task. Keep one home for each procedure.
Retain the existing English documentation language and verbatim desktop labels.

Use issues #1–32 for user intent, the README and existing guides for workflow
details, and the CLI, application code and tests for implemented behavior. Use
the desktop design frames to interpret the intended experience. Publish walkthroughs
of controls present in the documented revision and caption implementation
screenshots with their scenario. Record the revision and relevant issues in
this maintainer plan so the desktop guide can be revisited as features land.

The [v1 specification](https://github.com/whynpc9/CtxFerry/issues/1)
describes the intended audience. The [context read workflow](https://github.com/whynpc9/CtxFerry/issues/4),
[second-device workflow](https://github.com/whynpc9/CtxFerry/issues/8),
[execution trust](https://github.com/whynpc9/CtxFerry/issues/16),
[Skill installation](https://github.com/whynpc9/CtxFerry/issues/19) and
[artifact sharing](https://github.com/whynpc9/CtxFerry/issues/24) explain
why the guides call out disclosure, local trust and explicit publication.

Initial CLI revision: `463e0bc`. Desktop coverage includes `86780e2`. Issue descriptions were checked against GitHub on November 20, 2026. The desktop viewer and its review evidence
cover the [Overview and Sync scope](https://github.com/whynpc9/CtxFerry/issues/28).
Follow the desktop work in issues [#29](https://github.com/whynpc9/CtxFerry/issues/29),
[#30](https://github.com/whynpc9/CtxFerry/issues/30),
[#31](https://github.com/whynpc9/CtxFerry/issues/31) and
[#32](https://github.com/whynpc9/CtxFerry/issues/32) when updating this guide.

## Mounting the guide in Nimbus

Mount these Markdown files under the collection's `docs/` directory and set
Astro's base to `/CtxFerry`. Then `index.md` becomes `/CtxFerry/docs/` and
`sync.md` becomes `/CtxFerry/docs/sync/`. Keep the product landing page at
the collection root. Register the five groups in the table above in sidebar order.

Use relative Markdown filenames in authored links so they open in a repository
checkout. In the site build, map links between sibling guide pages to their
published routes, including `index.md` to the guide root, and preserve fragments.
For example, `recovery.md#recover-one-devices-local-state` should keep its
fragment when it becomes a directory URL. Nimbus 0.19.2 leaves these filenames
in the rendered article by default. Its Markdown `hastPlugins` option supports
rewriting anchor destinations. Keep local image references relative to the
source page so Astro can bundle the images. Verify the links, fragments and
image sources in the generated HTML under the project base before publishing.
