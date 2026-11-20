# 项目与普通配置的 macOS 实机验收

实机截图对应实现提交 `7eb6c154bba8a504e3a5873a6fd2cee4ddf601bd`。专属工作树起点为 `cd0c95990e87eee21cd36945a719aa17c8245b5e`。PR #59 squash 后的基底为 `463e0bc91efb04aeea2df7415d6db4429cb8adf4`。#28 随 PR #60 交付后，仅移植 #29 的提交到 `86780e2289aa69cee501876043c544897c5a252b`，保留该版的公共导航、总览与同步页面。
随后移植到文档整理提交 `757b619733d985c9b7d3d1ec3827e31d7cef1997`。新实现提交 `8e8f59dc6b42c26a28f24bca5294fde6038395db` 的应用、测试、Go 模块和 CI 文件与截图构建版本逐文件一致。

## 场景和读回

隔离目录中有独立 SQLite catalog、项目 `dbark`、两个显式绑定的 Checkout、两个 Profile，以及合成的 Context、Task、资源、provider 引用、Skill 和产物。通过 MyGo Automation 操作实际 macOS 窗口。构建信息中的 `vcs.revision` 与实现提交一致。采集过程持共享 GUI 排他锁，测试窗口关闭后释放锁。

| 操作 | GUI 和核心读回 |
| --- | --- |
| 从总览打开项目 | 稳定项目 ID 进入详情，两条 Checkout 需要明确选择。 |
| 恢复继承 | 主 Checkout 的设备超时从 90 秒恢复为项目层 60 秒。取消和 Escape 均保留 90 秒。确认后 `context explain` 回读 60 秒。 |
| 编辑项目共享层 | 预览列出主 Checkout 的 60 → 65 和备用 Checkout 保留 120。确认后主 Checkout 回读项目层 65 秒，备用 Checkout 回读 Checkout 层 120 秒。 |
| 不适用与 Profile | `missing-tools` 的原因在字段检查器显示。备用 Checkout 显式选为 `diagnostics`，主 Checkout 保留 `development`。 |
| 关联项目记录 | Context 显示来源和披露规则，Task 显示 `mac-mini` 的执行信任状态。项目内资源、Skill 和产物也均可查看。 |

管理入口以 #28 的 `desktopPages` 注册表为准。Skills 和产物的路由键分别是 `skills` 和 `artifacts`，通过 `desktopRoute{Page: page, ProjectID: id}` 传递稳定项目 ID。测试覆盖入口随注册显隐、项目范围以及返回后的分段与 Checkout。此版本的实机截图覆盖 #29 的项目关联记录。管理页的实际集成由对应工单验收。

## 首轮独立评审

T3 以 Kimi Code 的 `kimi-code/k3.1-pro` 和 `thinking=max` 启动首轮评审。评审逐张打开实现 `0a0f63499e0c86f16ab0c73389050efe4cdcebc5` 的 13 张原图，复算清单中的 SHA-256，并对照 [冻结设计图 04](../../../plans/desktop-design/04-project.png)。七项原始分依次为 8.2、8.1、7.4、7.6、6.8、7.6、7.4，均值 **7.6/10**。

| 评审发现 | 修订 |
| --- | --- |
| 共享层预览只描述继承者类别 | 预览列出本机 Checkout 的实际变化，保留更高层覆盖，提醒其他设备的覆盖需各自核对。 |
| Task 详情缺少当前设备的执行信任状态 | 打开详情时重读核心执行定义的信任状态，并在同一区域显示本机身份与状态。 |

## 实机原图与证据

13 张原图均为 2360 × 1436 像素，采集时窗口具有系统焦点，独立测试实例退出码为 0。清单保留每个场景的 SHA-256 和二进制构建信息。CLI 摘要逐字段保留配置值和来源，并记录原始响应的 SHA-256。[原图清单](screenshots/project-29/manifest.json) · [CLI 读回](screenshots/project-29/cli-readback.json)

| 场景 | 原图 |
| --- | --- |
| 打开项目和配置 | [两个 Checkout](screenshots/project-29/01-project-two-checkouts.png) · [配置来源](screenshots/project-29/02-configuration.png) |
| 恢复继承 | [确认前](screenshots/project-29/03-reset-preview.png) · [写入回执](screenshots/project-29/04-reset-receipt.png) |
| 共享项目层 | [两个 Checkout 的影响](screenshots/project-29/05-shared-edit-preview.png) · [写入回执](screenshots/project-29/06-shared-edit-receipt.png) |
| 路径与方案 | [工作目录不适用](screenshots/project-29/07-working-directory-unavailable.png) · [本机方案切换](screenshots/project-29/08-project-profile-switch.png) |
| 关联记录 | [Context](screenshots/project-29/09-context.png) · [资源](screenshots/project-29/10-resource.png) · [Task 信任](screenshots/project-29/11-task.png) · [Skills](screenshots/project-29/12-skills.png) · [产物](screenshots/project-29/13-artifacts.png) |

实机验证由 `gpt-6.1-terra`（High）执行，覆盖前台窗口、控件输入和 Escape 取消。完整键盘遍历、中文 IME、读屏及 Windows 和 Linux 的实机界面另有各自的验证范围。合成 provider 引用用于核对本机定义，源数据和证据仅含引用元数据。
