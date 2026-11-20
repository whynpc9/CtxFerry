# #28 桌面总览与同步验收

起点为 #59 的 `cd0c95990e87eee21cd36945a719aa17c8245b5e`。#59 squash 合入 `463e0bc91efb04aeea2df7415d6db4429cb8adf4` 后，只移植本分支的实现提交。MyGo Native UI 采用冻结设计规定的路线，直接使用共享 Go 应用核心。

| #28 验收项 | #59 已交付 | 本次补齐 |
| --- | --- | --- |
| 原生窗口、共享核心 | 独立 MyGo Go 窗口、总览和同步只读入口 | 共用路由、筛选和列表状态；慢操作在后台调用业务层 |
| 总览 | 本机、绑定路径、本机队列和冲突计数 | 项目汇总和目录检查器、队列下钻、最后确认时间、Git 和 provider 的本机证据、后台服务状态 |
| 同步 | 仓库、分支、明文边界、head、历史体积的基础呈现 | 已配置仓库、非空队列、冲突与离线状态；显式远端检查；备份范围确认、写入和实机回执 |
| 错误与空状态 | 缺少、过期或不可用 catalog 的基础提示 | 读取失败保留已读快照，局部查询失败显示未知；区分空队列与筛选无匹配 |
| 业务与外链 | 只读快照在共享核心中查询；原生视图不加载页面 | 检查、备份走共享核心；仓库外链只从已验证的 GitHub URL 交给系统默认浏览器 |
| 后台同步 | 桌面未启动后台任务 | 验证用户服务与窗口的生命周期独立 |
| 界面范围 | 总览与同步，无聊天、订阅、模型面板 | 增加后续页面的稳定导航 ID，当前可用页面由各自 renderer 注册 |
| CLI 与 smoke | CLI 独立构建；两页的 MyGo 视图测试 | 保持 CLI 无 MyGo 依赖；macOS 自动化从真实进程验证两页和相关状态 |

本机 Git 读取、GitHub 私有性和认证机制分别报告。身份来源不能确认时显示未知。Provider 只观察绑定及本机安全存储的元数据或作用域匹配的运行时会话配置。登录有效性与条目读取由各自的诊断流程确认。备份回执是此窗口已完成的操作，备份目录可由 CLI 验证和恢复。冻结稿中的常规发布预览需要可绑定到后续发布的核心预览合同，当前发布仍由 CLI 或独立同步服务执行。

## 后续页面接入

使用 `cmd/ctxferry-desktop/navigation.go` 中的 `desktopRoute` 和 `desktopPages`。各页面可在自己的文件里注册对应常量的渲染函数。路由参数 `ProjectID` 和 `RecordID` 使用核心稳定 ID。冲突页注册 `pageConflict` 后，同步队列显示比较入口，并将此记录及项目 ID 交给比较页。详情可调用 `d.back()` 返回先前的队列筛选和选中项。

## 复现

以下命令用于 Apple Silicon 桌面会话。

```sh
python3 cmd/ctxferry-desktop/smoke_macos.py /tmp/ctxferry-desktop-smoke-28
```

路径应为新的短目录。运行终端需能通过 System Events 操作本次测试进程的 macOS 辅助功能节点。脚本用临时 checkout、SQLite catalog、裸 Git 仓库、环回 GitHub 元数据服务和合成 provider 配置。自动化插件只在测试构建启用。截图取自运行窗口的 MyGo 原生内容面。脚本通过共同 Git 目录中的排他锁串行使用 macOS 窗口，并将窗口退出和用户服务清理纳入验收。

## macOS 窗口证据

复核画面的实现版本为 `083b88ba1a2ec5522562d7faa361b652498d9d90`。同一隔离窗口的原生内容面为 2360 × 1436 像素。两个项目 demo 和 tools 已绑定。CLI 有三条 outbox 记录，其中一条同时有活跃冲突。桌面待发布队列显示其余两条，冲突单独计数。已确认 head 为 `3c4ae810f83fc4ddb1a928b1202e6a42703db223`，历史体积 5416 B。

| 截图 | 对照冻结帧 | SHA-256 |
| --- | --- | --- |
| [已配置总览](screenshots/issue-28/01-overview-configured.png) | [01 总览](../../plans/desktop-design/01-overview.png) | `fb1ed9e6bed64aaaf32ec70ead290e018d550d219819813c93228c567d518311` |
| [同步与队列](screenshots/issue-28/02-sync-configured.png) | [02 同步](../../plans/desktop-design/02-sync.png) | `a67bb570db55828de601b343fa54bf7c48e40ca3a3678e27fca75b234659930f` |
| [备份确认](screenshots/issue-28/03-backup-confirm.png) | [02 同步](../../plans/desktop-design/02-sync.png)，参照[明文操作确认](../../plans/desktop-design/states/02-sync-preview.png) | `aac5c89537f94bf3bd2bc95b033494256b2e573af75161ede9735e1361082498` |
| [备份回执](screenshots/issue-28/04-backup-receipt.png) | [02 同步](../../plans/desktop-design/02-sync.png) | `17d5261fbd344d009da336dfff1d90f9563a62e70ad3b36411f469b154815835` |
| [离线后保留的本机视图](screenshots/issue-28/05-sync-offline.png) | [02 同步](../../plans/desktop-design/02-sync.png) | `408be7c65f9ab4be2dc5f78b8bbb386cd2466b6672a5ba7faefb907c3a14e7ae` |

备份输出的 manifest 表明目标未包含仅本机数据，head 与窗口一致。独立核对 `catalog.sqlite` 的 SHA-256 为 `b24349daeee55e782a71584afb74ce9830006b149298728266224192c5be6f09`，`published.bundle` 为 `f29b1b5e3fd0a86cdfc9647ad3f000a1534b90e4fc0ef239b20bd5fa43cea241`，两项与 manifest 一致。

### Kimi 独立评审

首轮实现为 `e5291b344864a8f75418906c92d783b269ade6ab`。T3 委派请求为 `kimi-code/k3.1-pro`、Thinking Max，评审运行时标识为同一模型。Thinking Max 由任务配置记录，ACP 无独立运行值回读。评委通过 ReadMediaFile 打开全部五张原始 PNG，并逐一核对 SHA-256。首轮排版 9.1、层级 8.8、间距与密度 8.7、色彩与对比 8.9、图标 8.5、一致性与完成度 9.1、清晰度 8.8，等权结果为 61.9 ÷ 7，即 8.8/10。五帧无阻断问题。

实现 `083b88ba1a2ec5522562d7faa361b652498d9d90` 对两项建议作了调整。备份回执直接指出校验清单 `manifest.json`。同步检查器可以复制带有选中记录完整 UUID 的 CLI 比较命令。

独立复核重新打开并逐一校验上表五张原图。评委确认同步和离线页均显示复制比较命令的入口，备份回执可直接定位校验清单。复核通过，未见回归或阻断。复核以同一模型及 Thinking Max 配置运行，未重新打分。截图验证覆盖可见入口，剪贴板的实际内容未由评委执行验证。

### 关闭窗口与后台同步

Terra `gpt-6.1-terra` 以 High 执行实机验证。受版本控制的 smoke 脚本在 `278002e0f8ee5ee631e5f57e6d5cecf175a875c6` 运行通过。测试先启动独立的 macOS launchd 用户服务，暂缓其读取远端。用目标进程的系统关闭按钮关闭可见窗口，窗口列表随后为空。同一个服务进程继续运行。解除测试门闩后，服务确认新的 Git head，第二份 catalog 同步后读到了待发布正文。验证结束后清理该窗口、测试服务和锁。

这些场景使用裸 Git 仓库、环回元数据和合成 provider 会话，覆盖 macOS 原生桌面与本机核心操作。真实 GitHub、provider 登录、其他平台桌面、读屏和输入法另行验收。
