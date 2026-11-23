---
title: 跨设备私有同步
---

[English](../sync.md) · 简体中文

本地初始化和编辑始终保持离线可用。同步是显式的附加步骤：它将共享记录以明文发布到专用的私有 GitHub 仓库，然后在另一台设备上检索并验证同一 catalog。

## 检查明文边界

使用专用的私有仓库，不要使用业务源码仓库。任何能够读取同步仓库的人或设备，都可以读取整个已发布 Workspace，与 Project 订阅无关。私有仓库访问权限不等于端到端内容加密。

类型化导出包括共享 Project、Repository 元数据和 membership、普通共享配置、Context 文档、Resource、Task、凭据引用、共享 Skill pin 和组，以及显式批准的 Artifact 记录和对象。尤其要注意：

- 共享 Context 文档会发布其**完整正文**，即使代理披露为 summary、reference 或 local-tool。
- SecretBinding 包含 Provider 和定位器元数据，绝不包含凭据值或 Provider 会话。
- 仅限本地的记录、Checkout 路径和绑定、设备路由、TCP Observation、Profile 选择、执行信任、SQLite 文件、索引、凭据存储和身份验证材料都会被排除。
- Git 历史保留已发布明文，包括后来从当前快照删除的内容。

扫描和允许列表无法发现粘贴到散文或 Artifact 中的所有凭据。发布前请检查原始共享材料。不能托管在 GitHub 上的内容必须保持 local-only。

## 启用并验证私有目标

前提条件是已初始化的第一台设备、空的专用私有 GitHub 仓库，以及可通过 SSH 或凭据助手工作的系统 Git 身份验证。经过身份验证的 GitHub 元数据读取使用 `GH_TOKEN`、`GITHUB_TOKEN` 或 `gh auth token --hostname github.com`。相比权限广泛的凭据，优先使用仅限同步仓库的细粒度令牌。CtxFerry 使用令牌进行检查，但不会保存它。

运行：

```sh
ctxferry sync enable \
  --remote git@github.com:owner/private-context.git --format json
```

默认同步分支为 `ctxferry-v1`；使用 `--branch <name>` 选择另一个未使用分支。`enable` 不上传任何内容。它在 `data.check` 中分别检查 Git 可达性和经过身份验证的仓库隐私。

继续前检查结果。只有 `data.state: "enabled"` 和已保存的 `data.config` 才表示已验证 Git 访问、匹配的仓库名称、稳定数字身份和明确的私有响应。`state: "unverified"` 不授权配置或上传。公开、缺失、格式错误或未经身份验证的元数据绝不会授权首次发布。

之后可以重复独立检查：

```sh
ctxferry sync check --format json
```

这是网络操作，不是发布。验证无法阻止仓库所有者在检查后更改可见性或身份，因此每次首次发布尝试都会重新检查。CtxFerry 拒绝包含凭据的 URL、URL 重写、配置 include 和备用推送 URL，避免已验证身份与实际目标脱节。

## 预览并初始化首次发布

`enable` 成功后，运行：

```sh
ctxferry sync preview --format json
```

响应在 `data.files` 中列出每个目标路径、字节大小和 SHA-256，并包含 `data.config`、`data.boundary` 和 `data.previewId`。预览刻意只包含哈希和身份，不包含受限文档正文；请另外检查原始共享文档和 Payload 文件。

预览 ID 绑定精确的导出字节和目标。记录、策略、依赖或对象发生变化后，都需要重新预览。当列表和源材料都获得批准后，显式初始化分支：

```sh
ctxferry sync init --preview <previewId> --format json
ctxferry sync status --format json
```

`sync init` 再次执行 Git 和隐私验证，根据允许列表中的快照构建根提交，并以预期不存在的租约创建分支。它不会覆盖现有分支。存在冲突或无效的现有 Workspace 时，会保留并报告，而不是替换。

成功结果包含 `state: "synced"` 和已确认的 `head`；随后 `status` 读取本地确认，不访问网络。如果首次推送响应丢失或中断，使用相同预览 ID 重试。CtxFerry 会先检查候选是否已经进入有效的远程历史，再决定是否重新发布。快照发生变化仍需要新预览。

## 从第二台设备加入

在第二台设备上安装 CLI，并准备 Git 和 GitHub 元数据身份验证。其应用数据目录必须是全新的：不要复制第一台设备的 SQLite catalog、WAL、锁或受管理 Git 目录。

```sh
ctxferry join git@github.com:owner/private-context.git \
  --subscribe my-project --format json
ctxferry project list --all --format json
```

如果此设备通过无需凭据的 HTTPS GitHub URL 进行身份验证，可以改用该 URL。分支默认为 `ctxferry-v1`；必要时用 `--branch` 指定同一个自定义分支。重复 `--subscribe <uuid-or-slug>` 订阅多个 Project，或使用 `--all-projects`。默认不订阅任何 Project。

`join` 验证仓库身份和隐私，要求同步分支已存在，暂存并验证共享 catalog，然后在一个事务中采用其 Workspace 身份并创建新的本地 Device ID。分支缺失、权限失败、404 或不完整响应都不会被视为空 Workspace。

订阅控制本地索引、显示、Skill 投影和 Artifact 访问，不控制仓库访问。完整且经过验证的基线仍可用于同步。发现和更改本地订阅：

```sh
ctxferry project list --all
ctxferry project subscribe my-project
ctxferry project unsubscribe my-project
```

加入后，再绑定此设备的实际检出目录：

```sh
cd /path/to/second-device/checkout
ctxferry project bind . --project my-project --format json
ctxferry context show --format json
```

Checkout 路径和订阅保留在本地。Provider 身份验证是独立的可选步骤，只有 Task 必须解析业务凭据时才需要；请参阅[凭据](credentials.md)。

## 运行常规双向同步

共享编辑会立即出现在本地工作视图中，并排队等待发布。显式运行一个同步周期：

```sh
ctxferry sync --format json
ctxferry sync status --format json
```

每轮会获取并验证远程快照，比较记录基线，应用兼容操作，并使用精确 expected-head 租约追加一个提交。一条记录发生冲突时，不相关的记录仍可继续；有依赖关系的待处理记录保留在同一个发布单元中。CtxFerry 绝不会使用 Git 文本合并或历史重写来解决记录冲突。

`sync status` 可以离线工作。它报告最后观察到的 `state`、已确认 `head`、最后成功的 `lastSyncAt`、排队 `pending` 数量、活动 `conflicts`，以及带原因的 `blocked` 记录。访问失败会保留本地记录和排队编辑。时间戳过期不会使本地内容无法读取。

Context 读取默认仍然立即执行。要先等待一次成功的同步周期，请使用：

```sh
ctxferry context show --sync --format json
```

Artifact 发布使用 [Skill 和 Artifact](skills-and-artifacts.md#有意共享-artifact) 中的预览并批准流程。冲突解决、不确定推送结果和针对失败的恢复，请继续阅读[恢复](recovery.md)和[诊断](diagnostics.md)。

## 可选后台同步

没有服务时，`ctxferry sync` 执行一次性同步周期。要运行前台循环，请使用：

```sh
ctxferry sync serve
```

要为当前用户会话安装同步服务，先将 CLI 安装到稳定的可执行文件路径，再运行：

```sh
ctxferry sync install-service --format json
ctxferry doctor --format json
```

安装器在 Linux 上使用 systemd 用户单元（包括用户管理器可用时的 WSL），在 macOS 上使用 LaunchAgent，在 Windows 上使用交互式 Task Scheduler 任务。结果和 `doctor` 会区分正在运行的服务、已安装但停止的服务、仅前台运行和不可用的服务管理器。平台无法提供后台能力时，不会声称支持。

服务启动时会补做同步；共享编辑在安静一秒后合并，从连续变更开始最多等待五秒；健康时每 30 秒检查一次远程变化。暂时性错误采用最长一分钟的退避。修复身份验证或 catalog 状态后，运行 `ctxferry sync` 立即恢复。

每个本地存储只有一个同步驱动。显式同步或带 `--sync` 的 Context 查询会等待现有驱动，而不是启动竞争的网络同步。服务恢复现有 outbox 和待处理发布日志，使用会话的非交互式 Git 和 GitHub 身份验证，不会将身份验证环境变量复制到服务定义中。

关闭桌面窗口不会停止独立安装的服务。服务不接受远程命令，也不执行 Task。首次发布仍需要上述显式 `sync init --preview` 批准。
