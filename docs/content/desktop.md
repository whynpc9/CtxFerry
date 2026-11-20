---
title: Native desktop preview
---

The desktop opens the same local Workspace as the CLI. At revision `86780e2`,
**设备总览** (Overview) shows this device's Projects and sync health. **同步**
(Sync) lets you inspect queued records, check a configured remote and create
a local backup. Publishing shared records runs through the CLI or sync service.

## Start the viewer

Use a desktop session supported by the native UI and a Go toolchain compatible
with `cmd/ctxferry-desktop/go.mod`. From the CtxFerry repository root, run:

```sh
cd cmd/ctxferry-desktop
go run .
```

The desktop is its own Go module and refers to the surrounding checkout's
application core. The window opens with **设备总览** selected.

To inspect an isolated Workspace, set the same data root for both CLI and
viewer. For example, in separate shells on macOS or Linux:

```sh
export CTXFERRY_DATA_DIR=/path/to/isolated-ctxferry
ctxferry init
```

Then start the desktop from `cmd/ctxferry-desktop` with that same environment:

```sh
go run .
```

The desktop honors `CTXFERRY_DATA_DIR`; passing `--data-dir` only to a CLI
command does not change the viewer's location. Initialize and bind through the
CLI first if the viewer reports **尚未初始化工作区**.

## Read Overview

![macOS Overview showing two bound Projects and their sync counts](assets/desktop-overview-macos.png)

This macOS example has two bound Projects, two records waiting to publish and
one unresolved conflict. Select a Project to inspect its bound directories.
Choose **查看队列** or **查看冲突** to open the corresponding records in Sync.
The desktop counts conflicts separately from the pending queue.

Choose **刷新本地视图** after a CLI operation to reread local state. The last
confirmed sync time, service state and locally available authentication evidence
help you decide whether to run an active check. Use [Diagnostics](diagnostics.md)
to check one credential mapping or diagnose a service.

## Read Sync

![macOS Sync showing a configured private repository and the local queue](assets/desktop-sync-macos.png)

This is the same test Workspace. For a configured repository, inspect the
branch, confirmed commit and measured Git history size. Select **检查远端** to
check Git reachability and repository privacy. Filter the queue and select a
record for its details. For a conflict, copy the CLI comparison command and
follow [Conflict resolution](recovery.md#resolve-a-record-conflict).

For a new Workspace, follow the on-screen terminal commands to configure a
private repository, preview the shared plaintext and confirm first publication.
See [Private sync](sync.md) for the prerequisites and how to review the preview.

## Create a local backup

In **同步**, choose **创建备份…**. Enter an absolute path to a new directory,
review whether to include local-only data, and confirm the write. Backups are
plaintext. The receipt identifies the destination and its `manifest.json`.
[Recovery](recovery.md) explains the contents and how to restore them.

## Handle viewer states

| Desktop message | Next step |
| --- | --- |
| **尚未初始化工作区** | Run `ctxferry init` with the intended data root, then refresh. |
| **本机目录需要升级** | Use a matching CLI to open and migrate the catalog, then refresh; the viewer does not migrate it. |
| **本机快照不可用** | Run `ctxferry doctor --format json` and inspect the catalog evidence. |

## Planned desktop scope

The desktop roadmap includes a Projects configuration editor, Fleet and
Resources, Secrets, conflict resolution, Skills, and Artifacts. Those screens and synthetic states in the design frames are
not controls in this checkout. Follow issues
[#29](https://github.com/whynpc9/CtxFerry/issues/29),
[#30](https://github.com/whynpc9/CtxFerry/issues/30),
[#31](https://github.com/whynpc9/CtxFerry/issues/31), and
[#32](https://github.com/whynpc9/CtxFerry/issues/32) for that planned work.
Until then, use the linked CLI guides for those operations.
