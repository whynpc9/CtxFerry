---
title: Context 文档与 Profile
---

[English](../context.md) · 简体中文

Context 文档保存 Project 笔记、约定和摘要。读取默认使用本地 catalog，因此可以离线工作，也不需要凭据 Provider。

## 编写笔记

首先[初始化并绑定一个检出目录](getting-started.md)。从该检出目录运行这些命令，或添加 `--project <uuid-or-slug>` 显式选择已订阅 Project。

创建一个 UTF-8 Markdown 文件，例如 `notes.md`，然后显式选择存储和披露策略：

```sh
ctxferry context put --title 'Development workflow' --purpose notes \
  --source 'Team-maintained notes' --summary 'Build and test conventions' \
  --body-file notes.md --storage local-only --disclosure summary --format json
```

记录简短笔记时只需要标题和正文。新文档默认使用 `--purpose note`、`--source 'ctxferry CLI'`、`--storage local-only` 和 `--disclosure full`，`--body` 可直接传入 Markdown：

```sh
ctxferry context put --title 'Release checklist' --body 'Tag from main after CI passes.'
```

替换已有文档会替换所有字段，因此替换时不使用默认值：需要再次传入用途、来源、存储和披露策略。

成功回执位于 `data.document` 下，包含新文档的 `id` 和 `revision`。它包含元数据，不包含正文。保留 ID 供 `get` 使用，保留修订供后续替换使用。

存储和披露回答的是不同问题：

- `local-only` 将文档保留在此设备上。
- `shared` 使完整正文可以发布到私有同步仓库。写入本身不会上传。
- 披露控制面向代理的 CLI 查询返回什么；它不限制发布，也不是文件系统或操作系统隔离。

| 披露 | `context get` 返回 | 可搜索字段 |
| --- | --- | --- |
| `full` | 元数据、显式摘要和正文 | 元数据、摘要和正文 |
| `summary` | 元数据和显式摘要 | 元数据和摘要 |
| `reference` | 元数据和 UUID 引用 | 元数据 |
| `local-tool` | 元数据和 UUID 引用 | 元数据 |

标题、用途和来源在每种模式下都可见。请显式提供 `--source`；CtxFerry 不会根据正文路径推断。`local-tool` 会记录策略，但不提供会揭示正文的工具执行命令。以你的操作系统用户身份运行的进程仍可能访问本地应用文件，因此 Context 不是 Secret 存储。

## 读取、搜索和分页

使用 `put` 返回的 ID；如果没有其他文档同名，也可以使用文档的完整标题：

```sh
ctxferry context show --format json
ctxferry context get <document-uuid> --format json
ctxferry context get 'Development workflow' --format json
ctxferry context search 'build test' --format json
```

`show` 返回已配置时的 Project 摘要、当前执行环境、选定 Profile，以及文档、Resource 用途和 Task 入口的索引。它绝不返回文档正文。`get` 返回该文档披露策略允许的字段。

`search` 将所有单词视为必须匹配的字面搜索词。它只搜索披露允许的字段，也可以返回匹配的 Task 名称。搜索输入最多 1024 字节，结果最多 100 条。结果包含元数据和获准披露的摘要，不包含正文片段。

一个 `show` 响应最多输出 8192 字节，包括 JSON 外层或文本渲染。如果报告 `truncated: true`，请使用返回的游标继续：

```sh
ctxferry context show --cursor <nextCursor> --format json
```

分页是实时视图；请求之间的写入可能影响后续页面。配置同步后，为 `show`、`get` 或 `search` 添加 `--sync`，可以在读取前等待一次成功同步。不带该参数时，响应会在可用时包含本地最后已知的同步时间。

具有 `--purpose project-summary` 的文档提供 Project 摘要。Reference 和 local-tool 披露会隐藏该摘要。CtxFerry 绝不会根据正文生成摘要。读者需要新鲜度到期时间时，将 `--valid-until` 设置为 RFC3339 时间戳；否则新鲜度保持 `unknown`。

## 安全替换文档

替换需要文档 ID、当前修订和完整的新文档，包括策略和正文：

```sh
ctxferry context put <document-uuid> --if-revision <current-revision> \
  --title 'Development workflow' --purpose notes \
  --source 'Team-maintained notes' --summary 'Updated build conventions' \
  --body-file notes.md --storage local-only --disclosure summary --format json
```

如果其他写入方先修改了文档，命令返回 `revision-conflict`，不会覆盖其工作。请读取当前版本、检查变更，并使用其修订重试。成功替换会保留文档 ID，并创建新的修订和操作 ID。

Markdown 正文必须是有效 UTF-8，且最多 1 MiB。结构化元数据另有 256 KiB 限制。使用 `--body-file -` 从标准输入读取正文。较大的显式摘要使用 `--summary-file` 替代 `--summary`；两个参数互斥。

删除、墓碑和恢复较早共享版本，请参阅[恢复](recovery.md#删除或恢复一篇共享文档)。并发共享编辑请参阅[解决记录冲突](recovery.md#解决记录冲突)。

## 解析普通配置和 Profile 覆盖

Profile 选择普通配置和显式 Task 映射。它们不会覆盖代理指令文件优先级，也不会授予 Provider 权限。

以下示例设置每种支持的值，然后解释结果：

```sh
ctxferry config set parallelism 2 --layer global
ctxferry config set timeoutSeconds 60 --layer project
ctxferry profile create development
ctxferry config set parallelism 4 --layer profile --profile development
ctxferry profile select development
ctxferry config set workingDirectory '"tools"' --layer checkout
ctxferry context explain --format json
```

字段名必须精确，传给 `config set` 的值是 JSON。因此字符串值 `"tools"` 两侧的引号必不可少。

| 字段 | 接受的值 |
| --- | --- |
| `parallelism` | 1 到 1024 的整数 |
| `timeoutSeconds` | 1 到 86400 的整数 |
| `workingDirectory` | 相对于 Checkout 的斜杠路径，或在 device 或 checkout 层使用的设备本地绝对路径 |

每个字段独立按以下顺序继承：

**global → Project → 显式选择的 Profile → device → 当前 checkout。**

`context explain` 报告每个有效值及其来源层，并在此设备上解析工作目录。目录必须存在；共享路径必须位于当前 Checkout 内。使用 `context explain --profile <name>` 可以为单次调用选择 Profile，而不更改已保存选择。

移除覆盖可以恢复继承，也可以清除本地 Profile 选择：

```sh
ctxferry config unset workingDirectory --layer checkout
ctxferry profile clear
ctxferry context explain --format json
```

Global、Project 和 Profile 配置参与共享 catalog。Device 和 Checkout 覆盖以及已保存的 Profile 选择保留在本地存储中。有效配置解释按需计算，不会作为另一条配置记录同步。

## 后续步骤

继续阅读 [Resource 和 Task](tasks-and-resources.md)，将索引中的服务用途和 Task 入口转化为显式本地操作。需要将共享 Context 发送到另一台设备时，使用[私有同步](sync.md)。
