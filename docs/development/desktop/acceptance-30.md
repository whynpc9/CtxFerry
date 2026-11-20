# #30 Desktop 实机验收

## 实现范围

MyGo Native UI 使用与 CLI 相同的 Go 核心和本机 provider 适配器。凭据页的引用、认证、条目读取与资源可达性分别显示。Fleet 按执行环境保存 TCP 观察，确认框显示本机目标和范围。

## 现场数据

- 独立临时工作区、SQLite、Git checkout 和回环 TCP listener。
- 内存 Infisical 替身和安全凭据存储。替身只有合成条目，应用窗口使用实际 #30 视图、事件与数据库写入。Automation 仅用于验证构建。
- 本机 mac-lab，项目 synthetic-lab，配置方案 development。check-db 持有 reader。verify-audit 先复用 reader，再创建并映射 auditor。
- test-database 的本机路线指向隔离回环端口。witness-database 存放两个分离的 Windows 和 WSL 合成历史观察。远端认证未知。
- CLI 和 SQLite 回读检查两个映射 ID、本机观察设备 ID 和目标、TCP reachable 及 serviceVerified=false。CLI 输出、应用日志与抽查的原生无障碍树没有合成条目值或会话标记。

## 首轮独立评审

实现 `4a7da01ceeb75149d5adb6cb749bb1bc1fe8f3c5`。T3 记录的 provider 为 Kimi Code，模型为 `kimi-code/k3.1-pro`，任务设置为 Thinking Max。评委打开两个冻结原图和九张实机原图并核对 SHA-256。依次评出排版 8.3、层级 8.2、间距 7.7、色彩 8.5、图标 7.8、一致性 7.9、清晰度 8.8，均值 57.2 ÷ 7 = 8.2（显示取一位）。

评委发现回执与选中任务或最近操作可能不一致，Fleet 表格末行部分裁切。独立实机还发现较长设备 ID 挤占列宽，以及认证主操作需要滚动才能看到。修订缩短列表展示 ID，检查器保留完整 ID。固定表格行高并让本机认证和条目操作进入首屏。回执按槽位显示，显式条目检查后更新为本次结果。

`4e85846bf1a57fa12afab746695537f20d607bdc` 的中间实机复验中，三行执行环境完整、表格列无重叠，凭据标题与认证动作同屏。两次实机运行均正常关闭本会话窗口并释放共同文件锁。

## 最终集成验收

独立分支起点为 `cd0c95990e87eee21cd36945a719aa17c8245b5e`。PR #59 squash 为 `463e0bc91efb04aeea2df7415d6db4429cb8adf4`，PR #60 squash 为 `86780e2289aa69cee501876043c544897c5a252b`。#28 关闭后，将 #30 的提交移植到包含两项变更的 `757b619733d985c9b7d3d1ec3827e31d7cef1997`。最终实机实现为 `7c49e55ba3f44f30b499f3ee11934592374d0aa1`。

| 实机场景 | 原图和回读 |
| --- | --- |
| 未登录及独立条件 | [绑定已存在、认证需登录、条目未知](screenshots/issue-30/signed-out.png) |
| 复用共享引用 | [确认](screenshots/issue-30/reuse-confirm.png) · [本机保存回执](screenshots/issue-30/reuse-receipt.png) |
| 创建引用并替换当前映射 | [确认和元数据](screenshots/issue-30/new-confirm.png) · [本机保存回执](screenshots/issue-30/new-receipt.png) |
| 验证认证，随后显式验证条目 | [已认证、条目仍未知](screenshots/issue-30/authenticated-unknown.png) · [可读取及本次检查范围](screenshots/issue-30/entry-result.png) · [切换任务后的范围](screenshots/issue-30/receipt-scope-switch.png) |
| 本机 TCP 探测 | [确认设备及环回目标](screenshots/issue-30/tcp-confirm.png) · [观察已写入](screenshots/issue-30/tcp-receipt.png) |
| 远端环境分别显示 | [Windows](screenshots/issue-30/witness-windows.png) · [WSL](screenshots/issue-30/witness-wsl.png) |
| 最终导航集成 | [设备总览](screenshots/issue-30/overview-navigation.png) · [同步](screenshots/issue-30/sync-navigation.png) |

实机由 GPT-6.1 Terra（High）操作隔离窗口取得。应用正常退出并释放共同 Git 目录下的 OS 排他文件锁。最终图为 MyGo Automation 从运行窗口采集的 1180 × 718 原 PNG。附 [原图 SHA-256](screenshots/issue-30/SHA256SUMS)、[CLI / SQLite 回读](screenshots/issue-30/verify.json) 及对应冻结稿 05 凭据、06 Fleet。

集成后，桌面模块的 vet、测试和独立生产构建通过。核心凭据与资源、探测作用域和首次私有发布的定向 CLI 测试通过。此前全量 Go 测试首轮达到 15 分钟上限；延长测试超时后，完整用例通过。验收在 macOS 实机使用合成 provider，与 Windows 和 WSL 的独立历史观察夹具配合。

## 最终 Kimi 复核

独立评委报告其运行模型为 `kimi-code/k3.1-pro`。T3 完成记录确认 Kimi Code provider、该模型与 Thinking Max 请求，另行提供的 `executedModel` 字段为空。评委直接打开 11 张实机原图与两张冻结原图。实机图计算哈希与清单一致，原图均成功载入。

| 排版 | 层级 | 间距 | 色彩 | 图标 | 一致性 | 清晰度 | Overall |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 9.1 | 8.7 | 9.1 | 8.9 | 8.6 | 8.8 | 9.0 | 62.2 ÷ 7 ≈ 8.9 |

复核通过，无阻断问题。确认保存、条目检查回执与所选槽位一致，切换任务后旧回执消失。本机 TCP 与两套远端环境的观察清楚分开，三行均完整，设备 ID 可在检查器核对。评委记录了两项非阻断观察。实机描边主按钮和灰色选中状态的视觉强调弱于冻结稿。完整设备 ID 在详情可见，当前交互没有专用复制按钮。
