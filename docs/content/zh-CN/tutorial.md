---
title: 教程：双设备编码代理工作流
---

[English](../tutorial.md) · 简体中文

你在两台机器上使用 Claude Code、Codex、Cursor 或 Pi 处理同一个源码 Project。某些私有环境说明和代理指令不应进入源码 Git，API 凭据也不应粘贴到代理对话中。CtxFerry 让每个代理发现你批准的上下文并运行已注册的带凭据 Task，同时由你控制发布、Provider 登录和执行信任。

在本演练中，人工在设备 A 上创建一条共享笔记和一个独立 API 凭据引用。随后，编码代理发现该笔记并运行 Task，无需接触原始令牌。私有同步把笔记、Task 和凭据引用带到设备 B；人工在设备 B 上分别完成本地登录和信任审批，然后由代理重复整套工作流。

![两个设备本地的 CtxFerry Workspace 通过私有 Git 仓库同步经过批准的明文记录。Checkout 路径、Provider 会话和执行信任保留在本地；凭据值在每台设备上直接来自外部 Provider。](../assets/tutorial-flow.png)

CtxFerry 同步的是**开发上下文和凭据引用**，而不是凭据值、Provider 会话、Checkout 路径或执行信任。共享 Context 在专用私有同步仓库及其历史中以明文保存。

## 准备工作

- 两台 macOS 或 Linux 设备，各自安装 Go 1.27.4 或更新的兼容版本、Git、Python 3 和 POSIX shell。Shell 示例使用 `python3`。
- 一个用于安装 CLI 的 CtxFerry 源码检出目录，以及 `skill bootstrap` 支持的一种已安装编码代理：Codex、Claude Code、Cursor 或 Pi。Bootstrap 安装的是 CtxFerry Skill，而不是代理本身。
- 一个名为 `atlas-api` 的一次性 Git 源码 Project，每台设备各有一个检出目录。你将添加下述安全 Python fixture，并通过 Project 的普通源码 Git 工作流分发。该源码仓库与 CtxFerry 同步仓库相互独立。
- 一个 Infisical US Cloud Project，其中包含 `dev` 环境和路径 `/` 下名为 `DEMO_API_TOKEN` 的条目。为它设置非空、URL 安全的**一次性测试值**，不要使用生产凭据。回环 fixture 接受任意此类值。Universal Auth 身份需要读取权限，每台设备还需要可访问的操作系统凭据存储。
- 一个名为 `atlas-context` 的空专用**私有 GitHub 仓库**，以及两台设备上均可用的 Git 和 GitHub 元数据身份验证。身份验证详情见[私有同步](sync.md#启用并验证私有目标)。
- 两台设备上的 TCP 端口 `18080` 均空闲，供回环 fixture 使用。

第 1–2 步和本地注册步骤不需要 Provider 登录。Provider 身份验证、首次发布和设备 B 加入需要网络。将每个 `<angle-bracket>` 值替换为你自己的输出或服务元数据。人工命令使用默认的可读输出；JSON 和 JSONL 只出现在代理的底层调用中，用作结构化工具约定。

## 1. 初始化设备 A 并连接编码代理

**人工：**从 CtxFerry 源码检出目录安装 CLI。然后初始化一个本地 Workspace 并绑定源码检出目录：

```sh
go install ./cmd/ctxferry
ctxferry init
cd /path/to/atlas-api
ctxferry project bind . --slug atlas-api
```

如果 `PATH` 中找不到 `ctxferry`，请参阅[安装并绑定你的第一个检出目录](getting-started.md)。将 `project bind` 输出的 `Repository:` UUID 保存为 `<repository-uuid>`，供设备 B 使用。绑定和绝对 Checkout 路径保留在设备 A 上。

接下来，预览内置 CtxFerry Skill 的安装：

```sh
ctxferry skill bootstrap install --target codex
```

检查计划写入的用户级文件，然后使用输出中的确切令牌运行打印的 `To apply:` 命令：

```sh
ctxferry skill bootstrap install --target codex --preview <skill-preview-token>
```

如果使用其他支持的目标，请替换这两条命令中的 `codex`：

| 目标 | 安装内容 |
| --- | --- |
| `codex` | `~/.agents/skills` 中的 Skill，以及 `~/.codex/AGENTS.md` 中的受管理指令块 |
| `claude-code` | `~/.claude/skills` 中的 Skill，以及 `~/.claude/CLAUDE.md` 中的受管理指令块 |
| `cursor` | 仅安装 `~/.cursor/skills` 中的 Skill |
| `pi` | 仅安装 `~/.pi/agent/skills` 中的 Skill |

`CODEX_HOME`、`CLAUDE_CONFIG_DIR` 和 `PI_CODING_AGENT_DIR` 分别选择对应的自定义配置目录。预览只显示 CtxFerry 自己的变更，不会暴露你已有的指令文本。安装后启动新的代理会话，使其能够发现 Skill。

**代理：**在 `atlas-api` 检出目录中提问：

```text
使用 CtxFerry 检查这个检出目录。告诉我有哪些上下文文档和已注册 Task。
```

Skill 会指导代理先运行：

```sh
ctxferry context show --format json
```

此时 Project 已存在，但还没有文档或 Task。如果代理找不到 `ctxferry`，应修正其环境中的 `PATH`，而不是把上下文粘贴到对话中。

## 2. 添加私有代理上下文并让代理发现它

公开的构建命令应放在源码 Git 中。本例改为记录私有环境说明和个人代理指令：它们适合放入你的专用私有同步仓库，但不适合放入源码 Project。

**人工：**在已绑定检出目录中注册一篇共享文档：

```sh
ctxferry context put \
  --title 'Atlas agent handoff' \
  --purpose project-summary \
  --source 'Atlas 维护者' \
  --summary '回环演示和带凭据 Task 约定' \
  --body '# Atlas 代理交接

一次性 API fixture 只监听 127.0.0.1:18080。
带凭据检查必须使用已注册的 demo-api-check Task。
不得打印、编码、保存或复制 DEMO_API_TOKEN；只能通过该 Task 使用它。' \
  --storage shared --disclosure full
```

`shared` 使完整正文可以参与明文私有同步；`full` 还允许代理检索该正文。绝不能进入任何 Git 历史的内容应保持为 `local-only`。独立的存储和披露策略见 [Context](context.md)。

**代理：**在检出目录中启动会话并提问：

```text
使用 CtxFerry 阅读 Atlas agent handoff。总结本地演示和带凭据检查的规则。
```

在底层，Skill 会告诉代理先发现索引，再只检索所需文档：

```sh
ctxferry context show --format json
ctxferry context get 'Atlas agent handoff' --format json
```

`context show` 返回 Project 摘要和文档索引，不返回文档正文。由于此文档的披露策略为 `full`，`context get` 会返回正文。代理可以使用索引中的标题，无需复制 UUID。

## 3. 让代理使用 API 凭据而不暴露它

本例使用独立凭据：API 令牌不需要 CtxFerry Resource、Resource 映射或 TCP 探测。示例还使用本地 fixture 而不是真实第三方 API，因此演练不会修改生产数据。

### 创建安全测试 fixture

**人工：**将 `tools/demo_api.py` 添加到一次性源码检出目录，并通过普通源码 Git 工作流提交：

```python
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.request import Request, urlopen

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        authorization = self.headers.get("Authorization", "")
        body = json.dumps({"authorization": authorization}).encode()
        self.send_response(200 if authorization.startswith("Bearer ") and authorization[7:] else 401)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

if sys.argv[1:] == ["serve"]:
    HTTPServer(("127.0.0.1", 18080), Handler).serve_forever()
elif sys.argv[1:] == ["check"]:
    request = Request(
        "http://127.0.0.1:18080/check",
        headers={"Authorization": "Bearer " + os.environ["DEMO_API_TOKEN"]},
    )
    with urlopen(request) as response:
        print(response.read().decode())
else:
    raise SystemExit("usage: demo_api.py serve|check")
```

该文件不包含令牌。服务器只监听回环地址，接受任意非空 Bearer 令牌，并刻意回显 Authorization 请求头，以便验证输出脱敏。不要将 fixture 指向外部服务。

在设备 A 的另一个终端中启动服务器并保持运行：

```sh
cd /path/to/atlas-api
python3 tools/demo_api.py serve
```

### 注册 Task 和凭据引用

**人工：**在原始终端中注册固定命令，并将其独立凭据需求绑定到 Provider 条目：

```sh
ctxferry task add demo-api-check \
  --purpose '使用一次性 API 令牌检查回环演示' \
  --credential demo-token::demo-api:api-key:DEMO_API_TOKEN \
  --storage shared -- python3 tools/demo_api.py check
ctxferry secret bind demo-api-check demo-token \
  --provider infisical --provider-profile team --method universal-auth \
  --backend-project <infisical-project-id> --environment dev
```

`demo-token::demo-api:...` 中的空槽位使其成为独立凭据需求。绑定默认使用别名 `demo-token`、路径 `/` 和键 `DEMO_API_TOKEN`。由于 Project 没有 Profile，`secret bind` 会创建名为 `default` 的 Profile，无需执行 Profile 设置命令。这些命令只记录引用，不读取条目的值。

**人工：**在此设备上进行身份验证并检查状态，而不暴露该值。通过受保护启动器提供 `INFISICAL_CLIENT_ID` 和 `INFISICAL_CLIENT_SECRET`，绝不能把它们放入代理对话或命令行：

```sh
CTXFERRY_INFISICAL_PROFILE=team \
  ctxferry provider login infisical --provider-profile team --method universal-auth
ctxferry secret status demo-api-check demo-token
```

状态应显示已绑定的映射、身份验证成功以及条目可检索。独立凭据的 Resource 可达性为**不适用**。登录将 Provider 会话存入设备 A 的操作系统凭据存储，不会同步该会话。

首次带凭据运行之前，人工必须检查并启用精确的执行定义：

```sh
ctxferry task trust preview demo-api-check
ctxferry task trust enable demo-api-check --preview <task-preview-token>
```

检查程序和参数、Checkout 和工作目录、`default` Profile、Provider 定位器及独立映射。`Custom commands` 必须为 `false`。使用此设备输出的预览令牌。Provider 身份验证和执行信任刻意由人工负责；Skill 会告诉代理在遇到这些交接时停止并请求人工处理，而不是自行执行。

**代理：**保持 fixture 服务器运行，然后提问：

```text
使用 CtxFerry 运行已注册的 demo-api-check Task。报告回环 API 是否接受请求。不得检查、打印、编码、保存或复制 DEMO_API_TOKEN。
```

代理检查已注册定义并运行它：

```sh
ctxferry task inspect demo-api-check --format json
ctxferry run demo-api-check --format jsonl
```

fixture 返回其请求头，转发到代理的子进程输出包含：

```text
{"authorization": "Bearer [REDACTED]"}
```

CtxFerry 自行解析 Provider 条目，只将 `DEMO_API_TOKEN` 交付到子进程环境。代理无需读取该值，也无需把它放进命令参数。CtxFerry 将转发 stdout 和 stderr 中识别到的交付值替换为 `[REDACTED]`。

脱敏不是操作系统安全边界：子进程仍可读取、转换、打印或传输收到的值。因此，Skill 禁止故意读取 Secret、打印、编码、写入文件、复制以及将值作为命令参数传递。只批准你信任的代码，不要要求代理通过 `env`、`printenv` 或自定义脚本展示原始值。

## 4. 私有同步并在设备 B 上完成本地设置

**设备 A 上的人工操作：**准备 Git 和 GitHub 元数据身份验证，然后验证专用私有目标。`sync enable` 不会上传任何内容：

```sh
ctxferry sync enable --remote git@github.com:<owner>/atlas-context.git
```

只有结果显示已启用，且检查同时确认 Git 可访问和仓库为私有时才继续。检查共享源材料和计划导出的精确内容，然后批准当前预览：

```sh
ctxferry sync preview
ctxferry sync init --preview <sync-preview-id>
ctxferry sync status
```

导出包括共享 Context 正文、Task、`default` Profile 和 SecretBinding 定位器。它排除令牌值、Provider 会话、Checkout 绑定和路径、Profile 选择及执行信任。已发布 Context 为明文，即使后来删除，也仍保留在仓库历史中。

**设备 B 上的人工操作：**安装相同 CLI，并准备此设备自己的源码检出目录。从全新的 CtxFerry 数据根目录开始：不要复制设备 A 的 SQLite catalog、WAL、锁、受管理 Git 仓库或凭据存储，也不要先运行 `init`。加入现有 Workspace 并订阅 Project：

```sh
ctxferry join git@github.com:<owner>/atlas-context.git --subscribe atlas-api
cd /path/to/device-b/atlas-api
ctxferry project bind . --project atlas-api --repository <repository-uuid>
ctxferry project list
ctxferry project inspect
```

使用第 1 步保存的 Repository UUID。`--project atlas-api` 将此检出目录附加到已同步 Project，而不是创建重复 Project；`--repository` 复用已同步 Task 的 membership 所需的 Repository 身份。`project list` 应只显示一个 `atlas-api`，检查结果应显示设备 B 的本地路径以及与设备 A 相同的 Repository 和 membership ID。

为此设备上的代理安装 Skill，检查新的本地写入，并使用设备 B 预览中的令牌：

```sh
ctxferry skill bootstrap install --target codex
ctxferry skill bootstrap install --target codex --preview <device-b-skill-preview-token>
```

使用第 1 步选择的目标。然后建立此设备自己的 Provider 会话和 Task 信任。已同步的 `default` Profile 无需在本地创建或选择：

```sh
CTXFERRY_INFISICAL_PROFILE=team \
  ctxferry provider login infisical --provider-profile team --method universal-auth
ctxferry secret status demo-api-check demo-token
ctxferry task trust preview demo-api-check
ctxferry task trust enable demo-api-check --preview <device-b-task-preview-token>
```

即使设备 A 已启用同一个 Task，此处的信任预览仍从 `not-enabled` 开始。除了命令和凭据定位器，还要检查设备 B 解析出的 Checkout 和工作目录。登录和信任不会同步；每台设备和每个 Checkout 都在本地作出这些决定。

## 5. 让设备 B 上的代理重复整套工作流

**人工：**确认第 3 步的 fixture 已存在于设备 B 的源码检出目录，然后在另一个终端启动其回环服务器：

```sh
cd /path/to/device-b/atlas-api
python3 tools/demo_api.py serve
```

**代理：**在设备 B 的检出目录中打开新会话并提问：

```text
使用 CtxFerry 阅读 Atlas agent handoff，然后运行 demo-api-check。报告结果，不得检查或打印 DEMO_API_TOKEN。
```

代理现在具有与设备 A 相同的发现和执行路径：

```sh
ctxferry context show --format json
ctxferry context get 'Atlas agent handoff' --format json
ctxferry task inspect demo-api-check --format json
ctxferry run demo-api-check --format jsonl
```

你应看到相同的私有笔记，以及另一个包含 `Bearer [REDACTED]` 的成功响应。Project、Task 和凭据引用身份与设备 A 相同；Device ID、Checkout 路径、Provider 会话和执行信任则为设备 B 本地所有。如果代理收到 `authentication-required`、`execution-definition-changed` 或其他交接错误，它必须停止并请求人工处理，而不是读取令牌或自行更改信任。

后续共享编辑先在编辑设备上运行 `ctxferry sync`，再在另一台设备上运行。同步离线时，本地读取仍然可用。

继续阅读 [Context](context.md)、[凭据与执行信任](credentials.md)、[Resource 和 Task](tasks-and-resources.md)、[私有同步](sync.md)和 [CLI 参考](cli-reference.md)，了解高级策略、JSON 自动化、设备本地路由和恢复流程。
