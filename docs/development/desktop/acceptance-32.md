# #32 Skills 与产物的 macOS 实机验收

初始 worktree 为 `cd0c95990e87eee21cd36945a719aa17c8245b5e`。#59 的 squash `463e0bc91efb04aeea2df7415d6db4429cb8adf4` 与初始基线的文件树一致。

## 场景与验证方式

在 macOS arm64 上使用原生 MyGo 窗口、隔离的 HOME、两个 catalog、两个合成项目、回环仓库元数据服务和私有裸 Git 仓库。接收端文件通过核心同步进入 catalog。实机操作由 `gpt-6.1-terra`（High）执行，采集期间持有共同 Git 目录下的 OS 排他文件锁。截图由运行中窗口采集，磁盘内容另用 SHA-256 核对。

- `dbark` 的 `db-check` 使用受管复制。首装 v1，外部修改 `references/checks.md` 后暂停 v2 更新。恢复原字节和重新扫描后，核对替换、删除和新增，再应用 v2。选择上一版本并回滚恢复 v1。共享目录继续记录独立批准的 v2。
- `windows-only` 在 macOS 上显示系统要求，目标目录保持缺失。
- 接收端的 `integration-018` 同时有 44 B 的 `summary.txt` 和 1,048,589 B 的 `trace.json`。摘要文件在原生保存窗口中选定路径，通过核心校验并导出。超额 trace 的接收端只有元数据。另一个本机 run 的完成范围为 `local-db`。
- 共享安装历史来自核心批准的 pin 和本机回执。同步运行记录提供摘要、来源设备和时间，本机运行记录还提供完成范围、源修订及其他追溯字段。

## 独立评审与改动

首轮实现 `a1fdc9384c346f498f80db903f13e94a15ce4b28`。Kimi K3.1 Pro 核对了十张实机图、三张冻结稿和原图哈希。七项等权均分为 8.0。T3 记录的模型为 `kimi-code/k3.1-pro`，提交给 Kimi Code provider 的选项为 `thinking=max`，provider 未返回独立的实际执行模型与推理档位回执。

| 初审发现 | 处理 |
| --- | --- |
| 顶部缺少候选 pin 选择 | 显式选择已批准的共享 pin 或可恢复的上次本机版本。本机 Git 来源接受完整 commit 或 release ref。共享和历史候选必须与核心计划匹配。 |
| 重扫跳回首行 | 以文件路径恢复选择，同一文件的候选 diff 与确认控件在默认窗口中同屏。 |
| 私有包来源留空 | 共享与本机安装各自显示私有快照来源和对应 pin。 |

中间实机复测 `2502d91d2eb14867fa8b71cd4abf72b828acecd1` 首次完成原生导出。输出 SHA-256 为 `3693e32a833b70e367a622569188bb3c69d9eba36515324acd39f75b9cab4d56`，与发布端登记文件一致。该轮验证了重新扫描和来源标签改动。

## 项目路由

本次基于 [#60](https://github.com/whynpc9/CtxFerry/pull/60) 的导航契约，两个页面消费 `desktopRoute.ProjectID`。#28 的关闭与合并提交为 `86780e2289aa69cee501876043c544897c5a252b`。最后移植到文档整理后的 main `757b619733d985c9b7d3d1ec3827e31d7cef1997`。

在临时目录组合 #29 的路由实现 `7eb6c15` 与 #32，用两个项目测试真实的“打开 Skills 管理”和“打开产物管理”按钮。两页保留指定项目，返回后保留原 Checkout 与项目分段。测试捕获了首次进入产物页时默认项目覆盖显式 `ProjectID` 的顺序问题，修复后两条路径均通过。

## 最终实机证据

实现 `4681f9394a3a5ccd3c772c82072b33ca9ffb8a46`。采集使用独立 Bundle ID 的测试应用和全新隔离目录。最后通过原生关闭按钮关闭窗口，测试进程和锁控制进程随后退出。

| 验收场景 | 原图 | 对应设计 |
| --- | --- | --- |
| 首装的手动确认与文件差异 | [安装预览](screenshots/issue-32/04-install-preview.png) | [Skills](../../../plans/desktop-design/07-skills.png) |
| 本地修改阻塞、候选文件差异 | [已阻塞](screenshots/issue-32/05-update-blocked.png)、[候选差异](screenshots/issue-32/06-candidate-diff.png) | [Skills](../../../plans/desktop-design/07-skills.png) |
| 重扫后保留所选文件，确认更新同屏 | [重新扫描](screenshots/issue-32/07-rescanned.png) | [重扫状态](../../../plans/desktop-design/states/07-skills-rescanned.png) |
| 明确选择上一版本、回滚和来源 | [候选选择](screenshots/issue-32/08-candidate-picker.png)、[回滚预览](screenshots/issue-32/09-rollback-preview.png)、[回滚回执](screenshots/issue-32/10-rollback-receipt.png) | [Skills](../../../plans/desktop-design/07-skills.png) |
| 平台条件 | [Windows-only Skill](screenshots/issue-32/11-windows-only.png) | [Skills](../../../plans/desktop-design/07-skills.png) |
| 本机范围、接收端仅元数据、项目无产物 | [本机产物](screenshots/issue-32/01-local-artifact.png)、[仅元数据](screenshots/issue-32/02-trace-metadata.png)、[切换项目](screenshots/issue-32/12-project-filter.png) | [产物](../../../plans/desktop-design/08-artifacts.png) |
| 原生保存及实际导出路径 | [导出回执](screenshots/issue-32/03-export-receipt.png) | [产物](../../../plans/desktop-design/08-artifacts.png) |

[原图尺寸与 SHA-256](screenshots/issue-32/manifest.json) · [场景数据](screenshots/issue-32/fixture.json) · [磁盘验证与回执](screenshots/issue-32/outcomes.json)

最终轮导出文件为 44 B，与登记 SHA-256 完全一致。此前的集成轮 `3a900ffc4734471a221a9d192e8efe358bfd0085` 验证了取消保存后目标目录为空。该轮一个隔离目录的系统保存按钮曾保持禁用，原因未确定。随后在另一隔离目录成功导出，最终轮在全新的测试包和目录中也成功导出。

## 自动化验证

- 桌面模块执行了 `go test -count=1 ./...` 和 `go vet ./...`。smoke 覆盖两页、项目过滤、安装确认、候选切换和回滚文件。
- 集成后的核心和 CLI 通过了 `go test -count=1 -timeout=25m ./...` 及 `go vet ./...`。覆盖过期预览、同大小但摘要不同的内容、接收端快照和仅元数据状态。
- CLI 依赖图无 MyGo。生产 Desktop 依赖图无实机测试使用的 Automation 插件。

## 最终实现复核

Kimi K3.1 Pro 对 `4681f9394a3a5ccd3c772c82072b33ca9ffb8a46` 的复核通过，初审三项均已关闭，剩余实质问题为 0。它逐张打开 12 张原图和原像素区域，核对完整 SHA-256，并独立重算 44 B 导出文件的 SHA-256。它还比对了安装回执中的 v1 与 v2，以及远端 Git 的产物对象。

| 排版 | 层级 | 间距密度 | 色彩对比 | 图标 | 一致性完成度 | 清晰度 | 等权均分 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 8.6 | 8.9 | 8.4 | 8.7 | 8.3 | 8.8 | 8.9 | 8.7 |

T3 记录使用 `kimi-code/k3.1-pro`，传入 `thinking=max`。Provider 的独立执行模型和实际推理档位回执仍不可用。复核覆盖归档图面、当轮磁盘证据和实现路径。Windows 与 Linux 验证覆盖编译，实机截图覆盖 macOS。
