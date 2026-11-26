---
title: Skill 与运行 Artifact
---

[English](../skills-and-artifacts.md) · 简体中文

Skill 是固定版本的包，会投影到代理的用户级目录。Artifact 是与 Project 和运行关联的不可变文件。两者都需要显式选择，也都不会递归监视源目录。

## 导入并预览 Skill

执行 `ctxferry init` 后，选择一个包含 `SKILL.md` 的本地目录。其 YAML frontmatter 必须声明有效的小写 `name`，该名称会成为安装目录名称：

```yaml
---
name: example
license: MIT
---
```

导入目录并预览 Codex 安装：

```sh
ctxferry skill add --local ./skills/example --format json
ctxferry skill plan example --target codex --format json
```

添加结果记录来源、内容摘要、许可证、文件 manifest，以及平台或可执行文件要求。本地快照独立于源目录的后续编辑。导入和安装不会执行 Skill 脚本或安装后钩子。

Plan 显示精确目标路径、复制或符号链接模式、预期写入和阻塞问题，不更改目标或本地包状态。被阻止的 Plan 仍是成功查询，状态为 `status: "blocked"`；检查 `issues` 中的名称冲突、外部编辑、缺失依赖、平台不匹配或恢复冲突。

只应用已经检查过的 Plan：

```sh
ctxferry skill apply example --target codex --format json
```

成功结果报告 `status: "installed"`。重复 Apply 是幂等的。CtxFerry 拒绝不属于它的目标和被外部编辑的安装，并按设备记录所有权和内容摘要。如果 Apply 中断，再次运行相同命令可恢复经过验证的暂存安装。

默认使用复制。在 Plan 和 Apply 中都添加 `--symlink`，可链接到经过验证的本地包；操作系统必须允许创建链接。目标包括：

- Codex：`~/.agents/skills`。
- Claude Code：`~/.claude/skills`；当 `$CLAUDE_CONFIG_DIR` 设置为绝对路径时，使用 `$CLAUDE_CONFIG_DIR/skills`。

Claude Code 使用 `--target claude-code` 替代 `codex`。

## 固定 Git 来源并管理变更

Git 导入要求完整提交哈希或 `refs/tags/` 下的标签；不接受会移动的分支：

```sh
ctxferry skill add --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example --format json
ctxferry skill plan another-example --target claude-code --symlink --format json
ctxferry skill apply another-example --target claude-code --symlink --format json
```

标签会同时记录名称和解析后的提交。使用 `--license` 覆盖 frontmatter；缺少许可证时记为 `NOASSERTION`。重复 `--platform` 和 `--depends-on <executable>` 声明兼容性要求。

写入前预览更新或回滚。使用返回的确认令牌，重复相同目标、模式和 Git ref：

```sh
ctxferry skill update example --target codex --format json
ctxferry skill update example --target codex --confirm <confirmation> --format json
ctxferry skill rollback example --target codex --format json
ctxferry skill rollback example --target codex --confirm <confirmation> --format json
```

确认涵盖精确 pin，以及文件新增、移除、摘要和可执行位。源发生变化时重新预览。先前完整版本保留在本地供回滚。更新和回滚保留复制或符号链接模式，不会改动被外部编辑或不属于它的文件。

### 共享 pin 和选择

共享是独立的批准操作。公共 Skill 使用无需凭据的 HTTPS Git 来源和精确提交；私有 Skill 将最多 1 MiB 的选定文件打包到私有同步存储：

```sh
ctxferry skill add --share public --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example
ctxferry skill add --share private --local ./skills/private-example
ctxferry skill group set global another-example
ctxferry skill group set project:my-project private-example
ctxferry skill group set capability:review another-example private-example
```

`group set` 替换该组的选择；不传 Skill 名称即可清空。组只能包含共享 pin。通过[私有同步](sync.md)发布或获取后，在接收设备上预览组：

```sh
ctxferry skill plan --group capability:review --target codex --format json
```

分别检查并应用每个需要的 Skill。安装目录、链接、回执和更新恢复保留在设备本地。共享 pin 变化不会改写现有安装，直到你预览并确认更新。Pin 冲突需要通过[恢复](recovery.md#解决记录冲突)显式选择。

### 添加全局 CtxFerry 代理入口

内置 bootstrap 会为选定的代理预览 `ctxferry` Skill。

```sh
ctxferry skill bootstrap install --target codex --format json
ctxferry skill bootstrap install --target codex --preview <token-from-preview> --format json
```

| `--target` | 用户级 Skill 位置 |
| --- | --- |
| `codex` | `~/.agents/skills/ctxferry/SKILL.md` |
| `claude-code` | `~/.claude/skills/ctxferry/SKILL.md` 或 `$CLAUDE_CONFIG_DIR/skills/ctxferry/SKILL.md` |
| `cursor` | `~/.cursor/skills/ctxferry/SKILL.md` |
| `pi` | `~/.pi/agent/skills/ctxferry/SKILL.md` 或 `$PI_CODING_AGENT_DIR/skills/ctxferry/SKILL.md` |

Cursor 和 Pi 会自行发现 Skill，安装后请启动新的代理会话。这两个 `skill bootstrap` 目标只安装内置 Skill。

Codex 和 Claude Code 还会添加一段受管理指令。对于 Codex，指令文件为 `~/.codex/AGENTS.md`（或 `$CODEX_HOME/AGENTS.md`），存在非空 `AGENTS.override.md` 时使用后者。对于 Claude Code，文件为 `~/.claude/CLAUDE.md` 或 `$CLAUDE_CONFIG_DIR/CLAUDE.md`。预览仅展示 CtxFerry 拟添加的文本。使用 `skill bootstrap uninstall` 按相同的先预览再应用流程移除受管理入口。要更新旧版内置 Skill，请先卸载再安装，分别使用各自的新预览。

如果其他代理支持读取仓库指令和运行 CLI，可以在项目的 `AGENTS.md` 中说明，需要项目上下文时，从当前检出目录运行 `ctxferry context show --format json`。此方式无需配置 MCP。

## 注册并检索 Artifact

从已绑定检出目录运行。注册一个显式结果文件，并提供来自原始执行的来源信息。此示例创建一个新运行；将源修订和工具版本替换为真实值，无法取得源修订时使用 `unknown`：

```sh
ctxferry artifact add report.json --project my-project \
  --type application/json --sensitivity internal \
  --source-revision <source-revision> --dirty=false --profile development \
  --tool go@1.27.4 --command go --command test --command=./... \
  --exit-code 0 --summary 'Unit tests passed' --completed unit --format json
```

新运行要求显式 `--dirty` 和 `--exit-code` 值，以及源修订、Profile、命令和摘要。未使用 Profile 时指定 `--profile none`。根据需要重复 `--tool`、`--input`、`--command`、`--completed` 和 `--uncompleted`。对于有未提交更改的源，`--diff-id` 可以引用现有 diff。

`data` 下的 manifest 包含新 Artifact 的 `id`、不可变的 `run.id`、Payload 大小和 SHA-256、源环境以及来源信息。保存两个 ID：

```sh
ctxferry artifact list --project my-project --run <run-uuid> --format json
ctxferry artifact inspect <artifact-uuid> --project my-project --format json
ctxferry artifact fetch <artifact-uuid> --project my-project \
  --output retrieved.json --format json
```

`list` 按 Project 筛选，也可按运行筛选。`fetch` 在创建新输出文件前验证缓存字节，不会覆盖现有输出。回执包含输出路径、大小和已验证哈希。

无需再次提供来源信息，即可向同一个不可变运行追加另一个结果：

```sh
ctxferry artifact add summary.txt --project my-project --run <run-uuid> \
  --type text/plain --sensitivity internal --format json
```

Payload 按 SHA-256 在本地 Artifact 缓存中只存储一次。注册在批准共享前仅限本地。默认缓存最多接受 1 GiB 唯一 Payload 字节；将 `CTXFERRY_ARTIFACT_CACHE_BYTES` 设置为正字节数可以调整。达到容量后，新内容会被拒绝，现有 Artifact 仍可检索。

## 有意共享 Artifact

先配置[私有同步](sync.md)。预览 manifest 和 Payload 模式，检查实际文件是否包含私有路径或粘贴的 Secret，再批准精确的预览 ID：

```sh
ctxferry artifact share <artifact-uuid> --mode automatic --format json
ctxferry artifact share <artifact-uuid> --mode automatic \
  --preview <preview-id> --format json
ctxferry sync --format json
```

第二条命令将经过评审的发布加入队列，第三条执行发布。另一台设备同步并订阅 Project 后，可以按上述方式使用 `artifact list`、`inspect` 和 `fetch`。安装经过验证的共享快照时，接收设备的完整性 Observation 会与源运行分开记录；`fetch` 在创建输出文件前再次验证 Payload。

Automatic 模式最多接受 1 MiB 原始 Payload。`--mode explicit` 允许经过评审的最多 5 MiB 样本。`--mode metadata` 只共享描述。Sensitive 分类始终省略 Payload，允许列表会排除已知原始日志、凭据相关文件、数据库转储、模型权重、归档和 Git LFS 指针。预览报告新 Payload 字节、预期省略和可用时的已知历史大小。批准不会扫描或脱敏任意内容。

常规发布以约 5 MiB 的变化记录和新 Payload 为目标。超限或被排除的内容会变为仅元数据，并在 `artifact inspect` 中说明原因；发送方保留其本地对象。Git 历史约达到 100 MiB 时，状态会提示增长。约达到 250 MiB 时，新的自动 Payload 会暂停，普通记录和符合条件的元数据仍可继续。移除当前 Artifact 不会清除 Git 历史中较早的明文；如果意外发布了凭据，请轮换该凭据。
