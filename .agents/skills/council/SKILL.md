---
name: council
description: 在 CtxFerry 项目中主持 MyGo Native UI 的多模型独立设计评审。用于用户要求按七项打分、冻结并比较多轮 UI 稿，或在达标、额度耗尽、轮数用尽时定稿。
---

# CtxFerry UI Council

在仓库根执行。先读 `plans/desktop-design/README.md` 确认设计范围和主流程。上次运行的分数、输入和定稿样例见 [评审记录](../../../plans/ui-council/README.md)。沿用用户本次指定的评委、轮数、门槛和提交范围。

## 准备可恢复的输入

1. 给每轮单独编号。以 `plans/ui-council/design-fixture.zip` 为起点，将出图用 Go 模块解压到临时目录。修改合成数据或视图后，从该目录运行 `go run -mod=mod . png`。渲染器和依赖留在设计夹具内。
2. 检查新图、文字和换行。沿场景核对项目、设备、时间、修订、权限、发布范围和回执。跨页查看同一记录时要能认出它。把本轮规格、所有主页面和状态 PNG 打包，另存完整渲染源及固定依赖文件，记录文件清单、轮次和 SHA-256。逐文件比对归档与评委将读到的工作副本。每个仍可能胜出的版本都要能整套恢复。
3. 为有图片载入限制的评委准备同一 PNG 的缩略拼图。此次按阅读顺序每两帧纵向拼接，图宽 1280，JPEG 质量 88，奇数末帧单独保留。列出每张输入并提供原分辨率 PNG 供放大。判读不了的帧记录为缺失并补读。

## 发起独立评审

先用 T3 的 `orchestrator_capabilities` 核对完整模型 ID 和可用参数。此次基线为：

| 席位 | providerInstanceId | model | target.options |
|---|---|---|---|
| Codex | codex | gpt-6.2-astra | reasoningEffort = high |
| Cursor | cursor | gemini-4-neon | reasoning_effort = high |
| Kimi | acpRegistry_kimi_code | kimi-code/k3.1-pro | thinking = max |

每轮每席发起新的 `delegate_task`，使用指定模型和参数、相同快照、只读范围。记录任务 ID 与绑定的模型设置。让三位各自判断；发给评委的输入只含设计、规格和问题，不附过线目标、历史分数或他人意见。可用下面的委派词：

> 独立审阅第 N 轮 CtxFerry MyGo Native UI。逐张查看清单中的全部主界面和状态图，对照本轮 README。整套按 Typography、Hierarchy、Spacing & density、Color & contrast、Iconography、Consistency & polish、Clarity 各给 0–10 分，保留一位小数。Overall 为七项等权未舍入均值。报告至多三处实质问题，给出画面位置和对用户的影响。说明未能检查的画面。只读评审。

收到异步任务的 next 后按它的完成通知推进。等待期间可在独立临时目录试改下一版。三份评审收齐前保持本轮图稿与规格冻结。启动失败、网络中断或图片过大时，核对原任务状态与错误，在原冻结材料上恢复同一席位。重试沿用本轮编号。额度不足须有提供方明确回执；记录原因并停止迭代。服务不可用、单次限流或通用错误须按实际原因报告。

## 算分、修订与收尾

- 逐项核对原始分。七项等权；只在展示时舍入。若只得到逐页七项矩阵，保留原矩阵并注明等权归并依据；无法确定口径时向该席补取整套评分。缺席留空。三份齐全后立即在聊天中展示本轮七行分数和 Overall，并更新历轮 Overall 汇总。
- 使用 [score.py](scripts/score.py) 复算，输入格式可参照 [本次记录](../../../plans/ui-council/scores.json)。每席可用七项，或每帧七项的矩阵；空值表示待评。运行 `python3 .agents/skills/council/scripts/score.py plans/ui-council/scores.json`。本次规则要求同一轮三个 Overall **各自严格大于 9.0**。63.0 ÷ 7 未通过，63.1 ÷ 7 通过。
- 首个全票通过的快照即为定稿。未通过时，把具体意见对应到用户操作，复查原图和核心约束，选取能解决问题的改动。同步更新图、状态、规格与夹具，生成新快照，再由全体独立复评。
- 本次上限为 20 轮。到上限仍未通过，从评分齐全的轮次先选最低席位得分最高者；平分取三席均值较高者，再平取较新轮。额度中止时报告已完成轮次、缺席与最佳候选，按任务对提前中止的授权处置。任何缺失席位都不能被历史分数补齐。
- 恢复选定轮的规格、PNG 和出图源码，按清单核对文件集合及字节，清理后续轮次独有的出图文件。将遗留问题另记在评审记录，保留已评设计。按用户授权提交并报告 commit 和归档摘要。静态通过只覆盖本轮提供的画面。平台实机、焦点、键盘、IME、读屏及真实写入分别验收。
