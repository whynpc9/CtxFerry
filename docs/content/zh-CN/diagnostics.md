---
title: 诊断与同步状态
---

[English](../diagnostics.md) · 简体中文

先查看本地证据，再只选择与失败能力对应的网络或 Provider 检查。本地记录、成功 TCP 连接、Provider 登录和成功 Task 是相互独立的事实。

## 读取 doctor 而不更改 Workspace

从需要诊断的检出目录运行：

```sh
ctxferry doctor --format json
```

`doctor` 可离线工作，不会初始化缺失的 catalog、迁移 catalog、探测 Resource，或执行实时 Git/Provider 身份验证。即使个别能力缺失或未知，它也会成功返回报告。因此请检查每一项，不要把退出码 0 当作整体健康结论。诊断绝不会输出完整环境。

报告中的 `data.observedAt`、`os` 和 `arch` 标识此次观察。使用以下字段选择后续步骤：

| 字段 | 报告内容 | 无法证明的内容 |
| --- | --- | --- |
| `cli` | 此 CLI 正在运行 | 任何其他能力都正常 |
| `catalog` | 本地 catalog 已读取、缺失、需要迁移，或使用不支持的 schema | Catalog 已同步 |
| `directoryBinding` | 当前目录是否能通过本地绑定证据解析 | 显式 `--project` 会绑定此目录 |
| `pathResolution` | 当前目录、Git Checkout、本地 Checkout 和有效工作目录解析 | Task 已成功运行 |
| `gitAuth` | 历史同步状态和安全的本地配置线索 | 实时 Git 身份验证；其状态通常保持 `unknown` |
| `providerAuth` | 每个引用的 Provider Profile、已存会话元数据和限定范围的无头会话是否存在 | 实时 Provider 身份验证或条目可检索性 |
| `lastSync` | 已记录状态、可用时的待处理数量和最后成功时间戳 | 当前远程可达性 |
| `credentialStorage` | 安全操作系统凭据存储或其安全元数据检查是否可用 | 读取特定业务凭据的权限 |
| `backgroundSync` | 此存储的状态为 `running`、`installed`、`foreground-only` 或 `unavailable` | 刚刚完成了一次成功同步 |
| `dependencies` | `git`、`gh`、`infisical` 和 `bw` 是否能在 `PATH` 中解析 | 可选工具已完成身份验证 |
| `resources` | 本地路由状态和最近记录的 TCP 可达性 | 服务协议或身份；`serviceIdentity` 可以保持 `unknown` |

一次成功同步在 24 小时后会在 `lastSync` 中变为 `stale`，与排队变更和已记录同步状态无关。未来时间戳会报告为未知，不会被接受为新鲜。Git 机制线索只报告 SSH agent socket 或元数据令牌环境已配置等事实，不会输出其值。

对于不同 Project，`ctxferry doctor --project <uuid-or-slug> --format json` 会改变 Project 相关评估，但 `directoryBinding` 仍描述实际当前目录。这有助于区分显式选择和本地 Checkout 绑定。

## 在本地读取同步状态

配置同步后，运行：

```sh
ctxferry sync status --format json
```

此命令读取本地状态，不联系远程。配置前返回 `sync-disabled`；一般本地健康检查使用 `doctor`，配置目标请参阅[私有同步](sync.md)。

重要字段包括：

- `state`：最后观察到的结果，包括 `enabled`、`synced`、`pending`、`conflict`、`offline`、`auth-required`、`unlock-required`、`incomplete`、`unsupported-schema` 或 `history-rewritten`。
- `pending`：具有排队变更的记录数，不是成功远程发布数。
- `head` 和 `epoch`：最后在本地确认的已发布提交和历史代，不是实时远程读取。
- `lastSyncAt`：最后成功同步时间戳，与最近失败尝试不同。
- `conflicts`：活动记录 ID、冲突令牌和观察到的 head。
- `blocked`：因依赖或唯一性原因被阻止的记录。
- `history.rawBytes`：测量时记录的不同可达原始 Git 对象字节估计值。

`enabled` 表示已配置目标，但还没有已确认 head。`pending` 可能表示本地编辑已排队，或发布结果需要协调。即使后续尝试离线，活动冲突仍保持可见；更基础的身份验证、历史或 catalog 失败可能仍是顶层状态。

## 选择主动检查

使用能够提供新证据的最小范围检查：

| 需要 | 命令 | 副作用和结果 |
| --- | --- | --- |
| 重新检查配置的 Git 可达性和经过身份验证的 GitHub 隐私 | `ctxferry sync check --format json` | 只进行网络读取；返回 `check.gitReachable`、`check.privacy`、可用时的稳定身份和 `failure` |
| 获取并发布共享变更 | `ctxferry sync --format json` | 一个有界同步周期；失败时保留本地工作 |
| 检查一个精确凭据要求 | `ctxferry secret status <task> <requirement> --profile <name> --format json` | 验证映射，并检查此设备的 Provider 会话和精确条目；不返回值 |
| 测量一个 Resource 路由 | `ctxferry resource check <resource-uuid> --format json` | 打开一个 TCP 连接，并追加设备本地 Observation |
| 在不建立连接的情况下检查路由 | `ctxferry resource inspect <resource-uuid> --format json` | 只读取配置意图、本地路由和先前 Observation |

对于 `sync check`，`gitReachable: true` 和 `privacy: "private"` 是独立要求。仅 SSH 或 HTTPS 检查成功不能证明隐私。`privacy: "unknown"` 或 `failure: "auth-required"` 表示需要经过身份验证的 GitHub 元数据访问；不要在未经验证的目标上初始化。

## 将状态对应到下一步操作

| 错误或证据 | 含义 | 具体的下一项检查或操作 |
| --- | --- | --- |
| 新安装上的 `not-initialized` 或 catalog 不可用 | 选定数据根目录没有本地 Workspace | 验证 `--data-dir` / `CTXFERRY_DATA_DIR`；只对真正的新数据根目录运行 `ctxferry init` |
| Catalog 需要迁移或具有较新 schema | 此查看器或 CLI 无法安全使用当前 catalog 版本 | 使用匹配的当前 CLI；不要删除或替换 catalog |
| `project-unbound` 或 `directoryBinding` 不可用 | 没有唯一的本地绑定匹配 | 运行 `project inspect --format json` 查看候选项，再有意运行 `project bind . --project <slug>` |
| `pathResolution.workingDirectory` 不可用 | 有效路径不存在或无法在此解析 | 运行 `context explain --format json`；按照 [Context](context.md) 修正或取消指定的 device/checkout 覆盖 |
| `sync-disabled` | 未配置同步目标 | 继续本地使用，或按照[私有同步](sync.md)配置 |
| `enabled` 但没有已确认 head | 首次发布尚未完成 | 检查 `sync preview`，再显式运行 `sync init --preview <id>` |
| `pending` | 本地工作或不确定的发布等待协调 | 运行 `ctxferry sync`；重试会检查已接受候选是否已存在 |
| `offline` 或 `git-error` | 传输或 Git 操作失败；本地工作仍保留 | 验证网络和系统 Git 访问，运行 `sync check`，再重试 `sync` |
| 同步的 `auth-required` 或 `unlock-required` | Git/助手访问需要身份验证或解锁 | 恢复此设备的 Git、SSH agent、凭据助手或 GitHub 元数据身份验证，再运行 `sync check` |
| 凭据的 `authentication-required` | 此精确设备/Profile 没有可用 Provider 会话 | 按照[凭据](credentials.md)中 Provider 特定流程登录，再重新运行 `secret status` |
| `secret-unavailable` | 精确后端条目缺失、访问被拒绝或没有可用值 | 验证定位器和 Provider 权限；不要替换为相似条目 |
| `binding-missing`、`binding-conflict` 或 `incompatible` | 显式 Task/Profile/Resource/SecretBinding 链缺失或不匹配 | 检查 Task 和 `secret status` 详情；按照[凭据](credentials.md)修复指定映射 |
| `resource-unreachable` 或不可达 Observation | 当前路由最近的 TCP 结果不可用 | 检查 `route`，修复此设备的端点/代理/网络，再运行 `resource check`；不会替换为同名本地 Resource |
| `platform-unsupported` | Resource 或 Task 条件排除了此操作系统/架构 | 检查实际 Device 上声明的平台条件；Windows 和 WSL 相互独立 |
| `execution-definition-changed` | 执行相关的本地或同步字段发生变化 | 运行 `task trust preview`，比较指定变化，并在此设备上启用精确的新预览 |
| `conflict` | 一条或多条记录具有保留的 base/local/remote 版本 | 按照[解决记录冲突](recovery.md#解决记录冲突)处理 |
| `branch-exists` | 首次初始化发现同步分支已存在 | 检查并验证该 Workspace；要加入它，使用全新数据根目录，不要覆盖分支 |
| `missing-branch` | 常规同步或加入找不到配置的分支 | 验证仓库和分支并恢复访问；常规同步和加入绝不会初始化缺失分支 |
| `incomplete` | 远程记录或对象缺失、不完整、损坏或未通过验证 | 保留最后确认的本地数据；检查并修复远程 catalog 后重试 |
| `unsupported-schema` | 此客户端不理解本地或远程记录 schema | 升级到兼容 CLI；不要强制发布，以免丢弃未知字段 |
| `history-rewritten` | 远程 head 不再延续已确认历史 | 停止自动发布，保留本地工作并检查远程历史；不要强制发布，也不要认为恢复可以绕过祖先检查 |
| `store-busy` 或 `sync-busy` | 另一个本地写入方或同步所有者正在活动 | 重试，或使用显式 `sync` 等待现有驱动；不要为同一存储运行第二个服务 |

[原生桌面应用](desktop.md)在 Home 显示本地证据。选择 **Sync → Repository → Check remote** 检查 Git 可达性和仓库隐私。
其刷新按钮不会执行上述任何主动检查。快照不可用时，`doctor` 是下一项本地诊断。
