---
title: CLI 参考
---

[English](../cli-reference.md) · 简体中文

本页列出当前 CLI 的命令和命令专用参数。完整工作流和安全检查请参阅链接的指南。运行 `ctxferry <command> --help` 或 `ctxferry help <command>` 可查看指定命令的帮助文本。

## 调用、输出与选择

```sh
ctxferry [全局参数] <command> [arguments] [命令参数]
```

在下表中，尖括号表示必需的位置参数，方括号表示可选参数。**可重复**参数可以通过重复传入该参数来提供多个值。每个命令还接受以下全局参数：

| 参数 | 含义 |
| --- | --- |
| `--data-dir <path>` | 使用指定的应用数据根目录，替代平台默认值或 `CTXFERRY_DATA_DIR`。显式参数优先于环境变量。 |
| `--format <text\|json\|jsonl>` | 选择输出格式。默认值为 `text`；`jsonl` 仅对 `run` 和 `exec` 有效。 |
| `--project <uuid-or-slug>` | 显式选择 Project。这不会将当前目录绑定到该 Project。 |
| `-h`、`--help` | 输出所选命令的帮助。 |

大多数查询和管理命令接受 `--format json`。此时标准输出只包含一个 envelope，帮助和失败结果也是如此：

```json
{"schemaVersion":1,"ok":true,"data":{},"error":null}
```

失败时包含 `ok: false`、`data: null`，以及具有稳定 `code`、人类可读 `message` 和可选结构化 `details` 的 `error`。诊断信息写入标准错误。`run` 和 `exec` 只接受 `text` 或 `jsonl`；请求 `json` 属于用法错误。它们的 JSONL 流包含执行事件，而不是上述 envelope。

一般退出码为：0 表示成功，1 表示应用、存储或执行控制面错误，2 表示用法错误。Task 或自定义命令成功启动后，`run` 或 `exec` 返回子进程的退出码。

## Workspace 和 Project

### 初始化 Workspace

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry init` | — | 无需登录，创建或复用本地 Workspace 和 Device 身份。 |

数据目录和绑定示例请参阅[安装并绑定你的第一个检出目录](getting-started.md)。

### `project`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry project bind <path>` | `--slug <slug>`、`--repository <uuid>`、`--remote <url>`（可重复） | 将目录绑定到新的或显式选择的 Project；可选择 Repository 或接受其远程别名。 |
| `ctxferry project list` | `--all` | 列出本地已订阅的 Project；`--all` 还包含未订阅的 Project。 |
| `ctxferry project inspect [uuid-or-slug]` | — | 检查所选 Project；未提供选择器时，从当前目录解析 Project。 |
| `ctxferry project remote add <repository-uuid> <url>` | — | 显式接受一个远程 URL，作为现有 Repository 的别名。 |
| `ctxferry project subscribe <project>` | — | 让此 Device 订阅一个 Project。 |
| `ctxferry project unsubscribe <project>` | — | 移除此 Device 的 Project 订阅，但不删除共享 Project。 |

### `onboard`

onboarding 命令会在所选应用数据根目录中保存进度。先运行 `onboard start`，再检查 `onboard status`，最后用 `onboard bind` 确认精确的预览结果。

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry onboard start <directory>` | `--repository <uuid>` | 保存目录并预览其 Project 解析结果。 |
| `ctxferry onboard status` | `--repository <uuid>` | 恢复已保存的进度并检查当前 Project 预览。 |
| `ctxferry onboard bind` | `--preview <id>`、`--repository <uuid>` | 确认预览并绑定已保存的目录。 |
| `ctxferry onboard scan <note>` | — | 报告所选笔记中疑似凭据的发现。 |
| `ctxferry onboard import <note>` | `--title <title>`、`--summary <text>`、`--storage <local-only\|shared>`、`--disclosure <full\|summary\|reference\|local-tool>` | 导入一篇显式分类的笔记。标题、存储策略和披露策略为必需项。 |
| `ctxferry onboard scan-env <file>` | — | 列出 env 字段名及匹配的 Task 凭据要求，不导入值。 |
| `ctxferry onboard import-env <file>` | `--field <name>`、`--task <name-or-uuid>`、`--requirement <name>`、`--profile <name>`、`--resource <uuid>`、`--alias <alias>`、`--provider <name>`、`--provider-profile <name>`、`--method <method>`、`--backend-project <id>`、`--environment <name>`、`--path <path>`、`--key <key>`、`--recover`、`--confirm <preview-id>` | 预览一个 env 字段的 Provider 导入和 Profile 映射；使用返回的预览 ID 和 `--confirm` 应用变更。`--recover` 会先根据来源验证现有后端条目。 |
| `ctxferry onboard finish` | — | 运行 `doctor` 并完成已保存的 onboarding 流程。 |

## Context 和配置

### `context`

即使代理披露策略受限，共享 Context 仍会发布完整文档正文。选择 `--storage shared` 前，请检查[编写和检索 Context](context.md)。

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry context put [document-uuid]` | `--body-file <path\|->`、`--title <title>`、`--purpose <purpose>`、`--source <description>`、`--summary <text>`、`--summary-file <path>`、`--storage <local-only\|shared>`、`--disclosure <full\|summary\|reference\|local-tool>`、`--valid-until <rfc3339>`、`--if-revision <revision>` | 创建文档，或按当前 revision 替换指定文档。正文文件、存储策略和披露策略为必需项；`-` 从标准输入读取正文。使用 `--purpose project-summary` 指定 Project 摘要。 |
| `ctxferry context show` | `--cursor <uuid>`、`--sync` | 读取 Project 摘要、执行环境，以及大小受限的文档、Resource 和 Task 索引。使用返回的 cursor 获取下一页；`--sync` 会先等待一次成功同步。 |
| `ctxferry context get <document-uuid>` | `--sync` | 按代理披露策略检索文档，也可先等待一次成功同步。 |
| `ctxferry context search <keywords>` | `--sync` | 使用字面关键词搜索代理可见内容，最多返回 100 个结果。 |
| `ctxferry context delete <document-uuid>` | `--if-revision <revision>` | 按必需的当前 revision 删除文档。 |
| `ctxferry context restore <document-uuid>` | `--from-commit <sha>`、`--if-revision <revision>` | 按当前 revision，从必需的祖先 commit 恢复文档。 |
| `ctxferry context explain` | `--profile <name>` | 显示有效的普通配置及每个字段的来源。 |

`--summary` 和 `--summary-file` 互斥。文档的标题、用途和来源属于代理可见的元数据。

### `config`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry config set <field> <json-value>` | `--layer <global\|project\|profile\|device\|checkout>`、`--profile <name>` | 在且仅在一个层级，将一个普通字段设置为 JSON 值。 |
| `ctxferry config unset <field>` | `--layer <global\|project\|profile\|device\|checkout>`、`--profile <name>` | 从且仅从一个层级移除一个字段。 |

对于 `profile` 层级，使用 `--profile` 提供 Profile 名称。

### `profile`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry profile create <name>` | — | 创建 Project Profile。 |
| `ctxferry profile select <name>` | — | 为当前检出目录选择 Profile。 |
| `ctxferry profile clear` | — | 清除当前检出目录的 Profile 选择。 |
| `ctxferry profile map-resource <profile> <task> <slot> <resource-uuid>` | — | 在 Profile 中为 Task 的槽位设置显式 Resource 映射。 |

## Resource、Task 和凭据

### `resource`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry resource add` | `--identity <identity>`、`--kind <kind>`、`--purpose <purpose>`、`--stage <stage>`、`--storage <local-only\|shared>`、`--network <label>`、`--endpoint <host:port>`（可重复）、`--platform <os[/arch]>`（可重复） | 注册服务及其非密配置。存储策略为必需项；适用时提供网络标签。`resource register` 是别名。 |
| `ctxferry resource list` | — | 列出 Project 中配置的 Resource。 |
| `ctxferry resource inspect <resource-uuid>` | — | 读取服务配置、此 Device 解析到的路由和以往的探测历史，不建立连接。 |
| `ctxferry resource route <resource-uuid>` | `--endpoint <host:port>`、`--network <label>`、`--proxy <host:port>` | 替换或清除此 Device 的路由信息。 |
| `ctxferry resource check <resource-uuid>` | — | 与解析到的端点建立一个 TCP 连接，并记录一条 Observation。 |

### `task`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry task add <name>` | `--purpose <purpose>`、`--input <description>`（可重复）、`--resource-slot <json>`（可重复）、`--credential-requirement <json>`（可重复）、`--membership <uuid>`、`--working-directory <path>`、`--program <executable>`、`--arg <argument>`（可重复）、`--platform <os[/arch]>`（可重复）、`--storage <local-only\|shared>`、`--allow-custom-command` | 注册 Task 及其执行定义。工作目录默认为 `.`；存储策略为必需项。对于以短横线开头的参数，使用 `--arg=VALUE`。 |
| `ctxferry task list` | — | 列出 Task 及其要求。 |
| `ctxferry task inspect <name-or-uuid>` | — | 检查一个 Task 及其要求。 |
| `ctxferry task update <name-or-uuid>` | `--if-revision <revision>`、`--purpose <purpose>`、`--input <description>`（可重复）、`--resource-slot <json>`（可重复）、`--credential-requirement <json>`（可重复）、`--membership <uuid>`、`--working-directory <path>`、`--program <executable>`、`--arg <argument>`（可重复）、`--no-args`、`--platform <os[/arch]>`（可重复）、`--allow-custom-command[=<true\|false>]` | 在必需的当前 revision 上，仅替换提供的字段。重复的列表参数会替换整个列表；`--no-args` 清空参数，且不能与 `--arg` 组合。 |
| `ctxferry task trust preview <task>` | `--checkout <path>`、`--profile <name>` | 显示执行定义，以及自此 Device 启用该定义以来发生的变更。 |
| `ctxferry task trust enable <task>` | `--checkout <path>`、`--profile <name>`、`--preview <token>` | 在此 Device 上启用精确的已预览执行定义。 |

Resource 槽位 JSON 包含 `name`、`kind` 和 `stage`。凭据要求 JSON 包含 `name`、`resourceSlot`、`purpose`、`type` 和 `env`。执行定义变更后，必须重新预览并启用信任，才能进行带凭据执行；信任记录仅保留在本地，不会同步。

### `run` 和 `exec`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry run <task>` | `--checkout <path>`、`--profile <name>` | 从本地检出目录中 Task 的 Repository membership 运行已注册 Task。 |
| `ctxferry exec --task <task> -- <program> [args...]` | `--task <name-or-uuid>`、`--checkout <path>`、`--profile <name>` | 当 Task 允许自定义命令时，使用其工作目录和凭据运行调用方选择的程序。`--` 后的参数以数组形式传递，绝不经过 shell。 |

两个命令都接受 `--format text` 或 `--format jsonl`。文本格式流式转发子进程输出；JSONL 发出 `start`、`stdout`、`stderr` 和最终的 `result` 事件。转发输出中的已投递凭据值会替换为 `[REDACTED]`，但目标程序仍能读取、打印、编码或外泄这些值。Task 不是操作系统隔离机制。启用带凭据执行前，请参阅 [Resource 和 Task](tasks-and-resources.md#运行本地-task)及[凭据](credentials.md)。

### `secret` 和 `provider`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry secret bind <task> <requirement>` | `--binding <uuid>`、`--alias <alias>`、`--profile <name>`、`--provider <name>`、`--provider-profile <name>`、`--method <method>`、`--entry-id <id>`、`--backend-project <id>`、`--environment <name>`、`--path <path>`、`--key <key>` | 创建并映射 SecretBinding，或按 UUID 复用现有共享绑定。Profile 默认为当前检出目录选择的 Profile。 |
| `ctxferry secret list` | — | 列出 SecretBinding 元数据，不联系 Provider。 |
| `ctxferry secret status <task> <requirement>` | `--profile <name>` | 检查此 Device 上一个显式映射的凭据，不返回其值。 |
| `ctxferry secret unmap <task> <requirement>` | `--profile <name>` | 从 Profile 中移除显式凭据映射。 |
| `ctxferry provider login <provider>` | `--provider-profile <name>`、`--method <method>` | 在此 Device 上验证一个 Provider profile 的身份。 |

对于按名称定位的后端条目，使用该 Provider 对应的 Provider、项目、环境、路径和键字段；`--entry-id` 则选择精确稳定的后端条目。Secret 值和 Provider 会话不会同步。

## Skill 和 Artifact

### `skill`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry skill add` | `--local <directory>`、`--git <url-or-path>`、`--ref <commit-or-tag>`、`--subdir <path>`、`--license <identifier>`、`--depends-on <executable>`（可重复）、`--platform <os[/arch]>`（可重复）、`--share <public\|private>` | 将一个显式本地目录或固定 Git 树快照为 Skill 包。 |
| `ctxferry skill plan [name]` | `--group <group>`、`--target <codex\|claude-code>`、`--symlink` | 预览投影文件、冲突和要求，不写入文件。可规划单个 Skill 或指定的组选择。 |
| `ctxferry skill apply <name>` | `--target <codex\|claude-code>`、`--symlink` | 在用户级安装或继续安装固定的 Skill。 |
| `ctxferry skill update <name>` | `--ref <commit-or-tag>`、`--license <identifier>`、`--target <codex\|claude-code>`、`--confirm <token>` | 预览显式 pin 变更；只有提供该精确预览的确认信息才会应用。 |
| `ctxferry skill rollback <name>` | `--target <codex\|claude-code>`、`--confirm <token>` | 预览或确认恢复先前安装的 Skill。 |
| `ctxferry skill bootstrap install` | `--target <codex\|claude-code>`、`--preview <token>` | 预览对 CtxFerry Skill 和全局指令的变更；使用当前预览的 token 应用。 |
| `ctxferry skill bootstrap uninstall` | `--target <codex\|claude-code>`、`--preview <token>` | 预览或确认移除 CtxFerry bootstrap 条目。 |
| `ctxferry skill group set <group> [name...]` | — | 替换 `global`、`project:<project>` 或 `capability:<name>` Skill 选择。 |

`--local` 和 `--git` 表示可替代的包来源。Git 引用必须是完整 commit 哈希或 `refs/tags/<release>`。默认 Git 子目录是仓库根目录。投影和确认示例请参阅 [Skill 和 Artifact](skills-and-artifacts.md)。

### `artifact`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry artifact add <file>` | `--run <uuid>`、`--command <argv-element>`（可重复）、`--source-revision <revision>`、`--diff-id <id>`、`--dirty=<true\|false>`、`--exit-code <code>`、`--profile <name\|none>`、`--input <identity>`（可重复）、`--tool <tool-and-version>`（可重复）、`--completed <scope>`（可重复）、`--uncompleted <scope>`（可重复）、`--summary <text>`、`--sensitivity <public\|internal\|sensitive>`、`--type <media-type>` | 为新运行或现有运行注册一个文件，包括其来源和载荷元数据。 |
| `ctxferry artifact list` | `--run <uuid>` | 列出 Project 的 Artifact，也可只列出一个运行的 Artifact。 |
| `ctxferry artifact inspect <artifact-uuid>` | — | 显示 Artifact manifest。 |
| `ctxferry artifact fetch <artifact-uuid>` | `-o <path>`、`--output <path>` | 验证载荷并复制到一个新的目标文件。输出参数为必需项。 |
| `ctxferry artifact share <artifact-uuid>` | `--mode <automatic\|explicit\|metadata>`、`--preview <id>` | 预览 Artifact 发布，或批准已检查的预览 ID。默认模式为 `automatic`。 |

即使所属 Project 已共享，Artifact 发布仍需显式操作。批准发布前请检查原始载荷；敏感性标签不会自动移除 Secret。

## 同步与恢复

### `sync` 和 `join`

同步会以明文将符合条件的共享记录发布到专用私有 Git 仓库。首次发布前请检查[私有同步](sync.md)。

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry sync` | — | 在前台运行一轮有界同步。 |
| `ctxferry sync enable` | `--remote <url>`、`--branch <name>` | 验证 Git 访问、仓库身份和 GitHub 私有可见性，不上传内容。默认分支为 `ctxferry-v1`。 |
| `ctxferry sync preview` | — | 列出首次上传拟包含的精确明文文件、大小和哈希，并返回预览 ID。 |
| `ctxferry sync init` | `--preview <id>` | 根据必需的当前预览，使用 expected-absent lease 创建同步分支；绝不覆盖现有分支。 |
| `ctxferry sync status` | — | 读取最近观察到的同步状态、待处理记录、冲突和已确认 head，不联系远程仓库。 |
| `ctxferry sync check` | — | 重新检查已配置的 Git 可达性和经过身份验证的 GitHub 私有性，不发布内容。 |
| `ctxferry sync conflicts <record-uuid>` | — | 显示一条记录冲突的保留版本和详细信息。 |
| `ctxferry sync resolve <record-uuid>` | `--if-conflict <id>`、`--take <local\|remote>`、`--file <path>` | 使用必需的冲突 ID，并选择显式一方或包含内联正文的严格替换 JSON 来解决冲突。 |
| `ctxferry sync serve` | — | 在前台运行同步服务。 |
| `ctxferry sync install-service` | — | 为当前用户会话安装后台同步。 |
| `ctxferry join <git-remote>` | `--branch <name>`、`--subscribe <uuid-or-slug>`（可重复）、`--all-projects` | 通过现有同步分支加入 Workspace，并创建新的本地 Device 身份。默认分支为 `ctxferry-v1`；可订阅所选 Project、全部 Project，或默认不订阅。 |

### `workspace`

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry workspace backup` | `--to <directory>`、`--include-local-only` | 在新的备份目录中保存明文 catalog 快照和已确认 Git 历史；可选择包含 local-only 内容和 Device 设置。 |
| `ctxferry workspace recover-local` | `--from <directory>` | 将已保存的 Device 恢复到新的应用数据目录。 |
| `ctxferry workspace restore` | `--from <directory>`、`--if-head <commit>`、`--new-epoch` | 在必需的当前已发布 Git head 上，以新 epoch 发布较旧的 Workspace 快照。 |

备份可能包含敏感明文。本地恢复和共享恢复是不同操作；恢复共享历史前请参阅[恢复](recovery.md)。

## 诊断

| 命令 | 命令专用参数 | 用途 |
| --- | --- | --- |
| `ctxferry doctor` | — | 报告本地 Device 能力、catalog 和绑定证据、已记录同步状态、可选依赖及 Resource 观察。 |

`doctor` 可以离线工作，不会初始化或迁移缺失的 catalog、探测 Resource，也不会执行实时 Git 或 Provider 身份验证。命令成功不代表报告的每项能力都健康；请检查[诊断](diagnostics.md)中的各项检查结果。
