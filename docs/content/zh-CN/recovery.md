---
title: 冲突解决与恢复
---

[English](../recovery.md) · 简体中文

恢复会保留本地工作和只追加的共享历史。请选择与问题范围匹配的操作；不要在设备之间复制活动 SQLite catalog，也不要重置同步分支。

| 情况 | 操作 |
| --- | --- |
| 两台设备修改了同一记录 | 检查并解决记录冲突 |
| 移除共享文档或恢复其较早内容 | 创建新的删除或恢复修订 |
| 重建已备份设备的未发布和本地状态 | 使用 `workspace recover-local` 恢复到新数据目录 |
| 将整个已发布 Workspace 回滚到备份 | 使用 `workspace restore` 并创建新 epoch |

## 解决记录冲突

从本地状态开始，此操作不访问网络：

```sh
ctxferry sync status --format json
```

`data.conflicts` 中每个条目都标识记录 `id`、授权解决所需的精确 `conflictId`，以及观察到的 `head`。选择前，先检查保留的版本：

```sh
ctxferry sync conflicts <record-uuid> --format json
```

结果包含 `base`、`local` 和 `remote` 外层，其中包括修订、来源设备、策略和披露允许的内容。只有 full 披露才显示 Context 正文；受限模式继续按策略隐藏正文和摘要。冲突证据和引用的不可变对象保留在本地数据库中，不会作为证据记录导出。

针对刚检查的精确冲突选择一方：

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --take local --format json
```

或选择远程版本：

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --take remote --format json
```

即使所选内容相同，每次选择也会创建新的修订和操作 ID。结果以 `state: "pending"` 在本地排队；使用以下命令发布：

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

冲突令牌发生变化时返回 `stale-conflict`；请检查最新冲突并重新选择。冲突解决前，对冲突记录的普通编辑会被拒绝。对已解决修订的后续远程编辑可能产生另一个冲突。

对于 Context 文档，也可以提供严格的替换 JSON 文件。此流程当前只支持 Context，不支持任意记录类型：

```sh
ctxferry sync resolve <record-uuid> --if-conflict <conflict-id> \
  --file resolution.json --format json
```

文件包含内容和策略，不包含身份或修订。例如：

```json
{
  "title": "Notes",
  "purpose": "notes",
  "source": "manual resolution",
  "body": "New Markdown content",
  "summary": "Approved summary",
  "policy": {"storage": "shared", "agentDisclosure": "full"},
  "deleted": false
}
```

解决方案必须保持 shared。安全相关引用、用途、执行参数、敏感级别和 Skill pin 始终需要显式选择；时钟和 Git 文本合并不会决定胜者。

不相关记录会继续同步。通过 Project、Repository、Task、Resource、凭据、配置或 Skill 依赖关联的待处理记录构成发布单元，因此发生冲突的成员可能阻止其依赖记录。同步状态中的 `data.blocked` 会指出记录和原因。Project slug 或 Project 摘要唯一性冲突同样会保留不兼容的待处理投影，同时不违反本地 catalog 约束。

## 删除或恢复一篇共享文档

这些操作要求已配置同步和共享 Context 文档。要按当前修订删除，请运行：

```sh
ctxferry context delete <document-uuid> --if-revision <current-revision> \
  --format json
ctxferry sync --format json
```

删除会创建保留在共享 catalog 中的墓碑。`get`、`show` 和 `search` 会省略它。未修改的离线设备会在下次同步时采用删除；编辑与删除冲突需要显式解决。明文正文仍保留在 Git 历史中。

要从已确认历史中经过验证的较早祖先恢复同一记录，请使用完整提交 SHA 和记录的**当前**修订；恢复删除时，当前修订就是当前墓碑修订：

```sh
ctxferry context restore <document-uuid> --if-revision <current-revision> \
  --from-commit <full-commit-sha> --format json
ctxferry sync --format json
```

使用经过验证的 Git 历史定位历史提交和当前墓碑修订。历史快照必须包含相同的活记录和范围。恢复通过常规同步将新修订加入队列，绝不会重置分支。此命令不涵盖 local-only 文档删除。

## 备份已发布和未发布状态

在受保护位置选择一个新目录。备份为明文。如果发布存在不确定的待处理结果，请先运行 `ctxferry sync`，让系统协调日志。

```sh
ctxferry workspace backup --to /protected/ctxferry-backup --format json
```

该命令可离线工作。它写入一致的独立 SQLite 快照，包含共享记录、未发布编辑及其原始基线、冲突证据和引用对象；存在已本地确认的发布历史时，还包含该历史的 Git bundle。无需凭据的同步配置以及 Workspace 和 Device ID 会保留。

要包含此设备仅限本地的内容和设置，请显式选择并使用另一个新目标：

```sh
ctxferry workspace backup --to /protected/ctxferry-device-backup \
  --include-local-only --format json
```

这会添加 local-only 记录、Checkout 绑定、本地配置、Profile 选择、订阅、Resource 路由和 Observation，以及本地 Artifact manifest 和已验证 Payload。两种模式都排除凭据存储、Provider 会话、Git 身份验证和执行信任。Secret 引用和用户编写的散文、来源信息或 Artifact Payload 仍可能包含敏感材料；请相应地检查并保护备份。

完成的备份包含带组件校验和的 `manifest.json`、`catalog.sqlite`、存在发布历史时的 `published.bundle`，以及包含所选本地 Payload 时的 `artifacts/` 目录。请保持整个目录完整。Manifest 是完成标记：失败的备份可能留下恢复会拒绝的不完整目录。

## 恢复一台设备的本地状态

本地恢复用于重建已保存设备的离线状态，不会发布 Workspace 回滚。选择一个新的应用数据目录并显式传递：

```sh
ctxferry --data-dir /protected/recovered-device workspace recover-local \
  --from /protected/ctxferry-device-backup --format json
```

目标目录不得已经存在。恢复在让新 catalog 可见前，会验证 manifest、校验和、catalog 完整性和任何已发布 bundle。它保留原始 Device ID 和待处理队列。通过 `--data-dir` 或 `CTXFERRY_DATA_DIR` 使用恢复目录，替代原设备的数据根目录。

备份的绝对 Checkout 路径和本地设置在新硬件上可能需要调整。请在恢复后的安装上重新进行 Git 和每个凭据 Provider 的身份验证；身份验证和执行信任未被备份。

## 在新 epoch 下恢复共享 Workspace

共享恢复刻意与本地恢复不同。它将较早备份作为新 head 和新历史代发布，所有同步设备都可见。只有整个共享 Workspace 都需要回滚时才使用。

前提条件是包含已发布历史的备份、同一 Workspace 中已配置的设备，以及当前 Git 和 GitHub 身份验证。先同步并检查当前已确认 head：

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

复制完整的当前 `data.head`，然后请求恢复：

```sh
ctxferry workspace restore --from /protected/ctxferry-backup \
  --new-epoch --if-head <current-full-commit-sha> --format json
ctxferry sync --format json
```

恢复会验证备份属于此 Workspace 且位于其已发布历史中，重新检查私有仓库身份，并要求远程 head 等于 `--if-head`。并发发布会返回 `stale-head`；请检查新 head 再决定。历史重写会停止操作，不会被覆盖。

成功恢复会在新 epoch 下追加一个提交。备份之后添加的记录会变为墓碑；旧历史仍保留。要在恢复视图中包含备份设备的未发布编辑或 local-only 状态，请先执行 `recover-local`，再使用恢复后的 `--data-dir` 运行 `workspace restore`。

其他设备保留其现有 outbox。下次同步时，每台设备使用原始基线比较排队编辑与恢复后的记录。兼容编辑可以在新 epoch 中发布；不兼容编辑会成为显式冲突，按上述流程处理。恢复响应丢失时，可以用 `sync` 协调，或在匹配的发布日志仍待处理时重试相同恢复。每台同步设备都要使用支持 epoch 的 CLI。

撤销丢失设备的 Git 或 Provider 访问，不会擦除它已经下载的明文或已经交付给进程的值。除撤销访问外，还应轮换实际暴露的凭据。
