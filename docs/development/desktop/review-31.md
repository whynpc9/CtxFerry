# 桌面同步冲突的 macOS 实机验证

开发 worktree 从 `cd0c95990e87eee21cd36945a719aa17c8245b5e` 起步。PR #59 squash 合并后，只将 #31 的提交移植到 `463e0bc91efb04aeea2df7415d6db4429cb8adf4`。以下窗口由实现 `e48e903fbfdf58ee43109788a7dbf8acfb1cf926` 生成。测试 .app 的构建 overlay 仅将 MyGo 窗口位置文件导向临时目录。应用视图与 Go 用例来自该提交。

`gpt-6.1-terra`（High）在持有共享 GUI 文件锁期间操作测试窗口。夹具使用两个隔离的 SQLite 目录、两个 checkout、一个本机裸 Git 仓库和 loopback GitHub 仓库元数据响应。窗口通过正常入口读取这些记录。初始队列共三条，其中「测试环境说明」是正文冲突，「过期排查记录」是摘要披露的编辑与删除冲突，第三条独立记录等待发布。

## 窗口截图

原图是 CUA 返回的 2360 × 1500 JPEG，原样保存。[SHA-256 清单](screenshots/issue-31/sha256.txt)

| 截图 | 窗口数据与操作 |
| --- | --- |
| [01 同步队列](screenshots/issue-31/01-queue.jpg) | 从总览点击「查看冲突队列」。两个有标题的冲突行可进入比较，独立待发布记录单列。 |
| [02 三方比较](screenshots/issue-31/02-compare.jpg) | 查看共同基线、本机及远端的正文、设备 ID、时间和修订。初次进入时未选方案，生成动作禁用。 |
| [03 采用远端](screenshots/issue-31/03-side.jpg) | 已选择远端，显示来源和结果预览。 |
| [04 编辑草稿](screenshots/issue-31/04-edit.jpg) | 新版正文含两行，第二行以两个空格开头，结尾保留换行。 |
| [05 显式确认](screenshots/issue-31/05-confirm.jpg) | 确认对话框说明方案、本机目标、待发布与 Git 明文边界。用 Tab 和 Space 操作确认区，取消后仍是原修订。 |
| [06 过期保护](screenshots/issue-31/06-stale.jpg) | 对端在比较后更新记录。确认旧 token 被拒，编辑与生成动作禁用。刷新后保留草稿，重新选择编辑与披露范围后才可确认。 |
| [07 本机回执](screenshots/issue-31/07-saved.jpg) | 核心生成本机修订，回执说明后续同步负责发布。 |
| [08 更新后的队列](screenshots/issue-31/08-queue-after.jpg) | 已解决的冲突作为待发布修订显示，另一条冲突仍活动。 |
| [09 删除冲突](screenshots/issue-31/09-deletion.jpg) | 三侧正文按披露范围隐藏，远端侧标明删除，提供显式「保留远端删除」选择。 |

冻结稿为 [同步](../../../plans/desktop-design/02-sync.png) 与 [三方冲突](../../../plans/desktop-design/03-conflict.png)，SHA-256 分别为 `2f63f07d95641115848f63ac6e1356f83c39f1518bbc05292c9b8d91d396ab6c` 和 `9ae6daf4d845341ab49e6684d0ae2be398c999e713b6bdde54671a4e81a0a5e9`。

## 写入和读回

- CLI 读到新修订 `f707c669-f93d-48b7-b926-290cce57a2a6`，其父修订是刷新后查看的远端 `01d67425-5fbc-41e2-b4e7-faf47b9fad00`。设备 ID 与桌面当前设备一致，正文 SHA-256 为 `4f47302b847763f77a603ed2510c097792204161ba9b3379cfd7987447e1db0f`。
- 保存后本机队列有两条记录，裸 Git 远端 head 仍为 `0b3e7d7005cd28deab94e07675f49fb148f46ee2`。关闭测试窗口后，通过 CLI 显式同步，对端读到同一修订和正文，裸 Git head 更新为 `61255ced1b7d5b2754495c67ed0b78028235fbc3`。
- Terra 实测 Tab 和 Space 进入确认及取消。编辑态生成按钮贴近窗口底部，过期提示出现时可滚动查看余下内容。此轮覆盖 macOS Apple Silicon、合成 provider 和本机裸 Git。读屏、中文输入法组词及远程 GitHub 双机验收有各自的测试范围。

## 第一轮 Kimi 独立评审

Kimi Code 的运行记录绑定 `kimi-code/k3.1-pro`，委派参数为 `thinking=max`。提供方运行记录未单独返回 executedModel 或实际推理档位。评审回复确认模型 ID，逐一用 ReadMediaFile 打开九张 CUA 原始 JPEG 及两张冻结稿，九项 SHA-256 均通过。

结论为需要修改。首轮确认显式选择、版本来源、草稿保留、本机保存和待发布、远端删除以及披露受限的语义。需要修正两处：三方卡内应标出具体增删，成功回执与队列应保留对象名并展示实际生成的修订。评审也建议在来源环境未知时做明确标记。

## 修订版的实机复核

实现 `1951aa4b2da761259ffc5cf55f5848d41b4d495c`。Terra 使用新的隔离夹具重跑同一条桌面旅程。其中共同基线含「旧端口」，本机与远端分别改写不同片段。版本卡保留原文，分别以绿色底色和删除线标记实际增删，并显示设备 ID。没有环境元数据的设备标明环境未知。返回总览后二次点击「查看冲突队列」可回到队列，桌面烟测也验证了通过「返回同步队列」回到触发记录的焦点。

| 原始截图 | 复核内容 |
| --- | --- |
| [01 同步队列](screenshots/issue-31-recheck/01-queue.jpg) | 两条具名冲突和一条独立待发布记录。 |
| [02 三方比较](screenshots/issue-31-recheck/02-compare.jpg) | 基线保留原文，本机与远端标出具体新增和移除片段。产生修订前须选择方案。 |
| [03 选定远端](screenshots/issue-31-recheck/03-side.jpg) | 远端结果原文与选中状态一致。 |
| [04 编辑](screenshots/issue-31-recheck/04-edit.jpg) | 编辑精确多行正文。向下滚动可到达生成动作。 |
| [05 确认](screenshots/issue-31-recheck/05-confirm.jpg) | 显示操作范围、本机设备、待发布和 Git 明文边界。 |
| [06 过期](screenshots/issue-31-recheck/06-stale.jpg) | 禁止使用旧比较生成修订。刷新保留草稿，要求重新确认披露范围。 |
| [07 本机回执](screenshots/issue-31-recheck/07-saved.jpg) | 记录标题、记录 ID 和新修订号独立显示。 |
| [08 返回队列](screenshots/issue-31-recheck/08-queue-after.jpg) | 待发布项保留与回执相同的标题、记录 ID 和新修订号。另一条冲突仍活动。 |
| [09 删除冲突](screenshots/issue-31-recheck/09-deletion.jpg) | 远端标为已删除，按策略隐藏正文，保留删除须显式选择。 |

[修订版原始截图 SHA-256](screenshots/issue-31-recheck/sha256.txt)

记录 `197c3717-9b38-4e8d-8269-cf38b4eb1acb` 的新本机修订为 `9f6528d4-c334-4194-966e-df76d64ff274`，父修订为刷新后查看的 `43ca9945-e2d4-424e-95e3-216285855aac`。CLI 读回相同设备、修订与原样换行的正文，正文 SHA-256 为 `4f47302b847763f77a603ed2510c097792204161ba9b3379cfd7987447e1db0f`。生成修订时观察到的裸 Git head 保持 `8df54742dbb975eaa277ba4ddfe729d3a97f5662`。关闭窗口后显式同步，第二个目录读到相同修订和正文，裸 Git head 更新为 `d62706d814bd60e2d00c6c5e512c5717f370c856`。

自动化通过了桌面冲突烟测、桌面与根模块静态检查、核心的本机快照测试、四组 CLI 冲突和双目录同步回归。生产 CLI 以 CGO 禁用的方式构建并启动。首轮全仓本机测试达到 15 分钟的包级总时限，当时正在启动 `TestTwoDeviceSync`。该用例通过单独重跑及修订版回归。

修订版的 `go test ./internal/...` 和带竞态检测的桌面冲突烟测均通过。

## 修订版 Kimi 独立评审

**通过。** Kimi K3.1 Pro 逐张打开了修订版的九张 CUA 原图及两张冻结稿，九项 SHA-256 均通过。复核确认卡片只高亮新增的文本片段，移除片段单独标出，且回执与队列的标题、记录 ID 和修订号均与 CLI 回读一致。评审也检查了过期时的草稿、明文发布边界、设备来源和删除状态。两处初审问题均已关闭。

T3 为两轮均绑定 `kimi-code/k3.1-pro` 并提交 `thinking=max`，评审回复报告了相同模型 ID。提供方运行记录没有独立的 executedModel 或实际推理档位回执。复核建议给未解决的冲突行增加同名记录辨识信息，以及改善过期提示出现时的滚动体验。两项均被评为非阻塞。
