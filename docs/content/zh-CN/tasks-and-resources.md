---
title: Resource 与本地 Task
---

[English](../tasks-and-resources.md) · 简体中文

Resource 描述服务及其非 Secret 的连接意图。Task 定义可执行入口、工作目录、参数、Resource 槽位和可选凭据要求。读取任一方都不会获取凭据或打开网络连接。

## 注册并检查 Resource

从[已绑定检出目录](getting-started.md)运行，或传递 `--project <uuid-or-slug>`。以下示例注册一个共享开发数据库端点：

```sh
ctxferry resource add --identity dev-db --kind database --stage dev \
  --purpose 'Integration tests' --storage shared \
  --endpoint db.example.com:5432 --format json
```

结果是 `data` 下的 Resource；将 `data.id` 保存为 `<resource-uuid>`。端点是主机和数字端口，不是 URL，也不是连接字符串。不要包含用户名、密码或令牌。

```sh
ctxferry resource list --format json
ctxferry resource inspect <resource-uuid> --format json
```

`list` 返回 Project 中配置的 Resource。`inspect` 额外显示为此执行环境解析的路由，以及此设备的 Observation 历史。两个命令都不会联系服务。

共享端点必须跨设备有意义；回环地址和设备本地地址应放在本地路由中。例如，如果此设备通过本地转发访问同一个逻辑数据库，请运行：

```sh
ctxferry resource route <resource-uuid> --endpoint localhost:15432 --format json
ctxferry resource inspect <resource-uuid> --format json
```

检查到的路由现在具有 `source: "device-local"`。路由事实保留在此设备上，不会覆盖共享端点。使用 `--proxy <host:port>` 添加本地代理，或在 Resource 声明网络条件时使用 `--network <label>`。不带任何路由参数运行 `resource route <resource-uuid>` 会清除全部三个本地事实。

路由还可能报告 `route-missing`、`route-ambiguous`、`network-mismatch` 或 `platform-unsupported`。在请求探测前，请解决所指示的问题。

## 显式检查 TCP 可达性

只有 `resource check` 会打开连接：

```sh
ctxferry resource check <resource-uuid> --format json
ctxferry resource inspect <resource-uuid> --format json
```

第一条命令以两秒超时尝试连接解析后的端点；配置代理时则连接代理。结果包含 `status: "reachable"` 或 `"unreachable"`、`tool: "tcp-connect"`、设备、时间、目标、平台、Resource 修订，以及 `serviceVerified: false`。

第二条命令显示追加到此设备历史的新 Observation。开放端口只能证明 TCP 可达，不能验证服务协议、身份、身份验证或 Task 是否成功。其他设备的 Observation 不会替代此设备的证据，检查也不会改变 Resource 意图。

## 注册本地 Task

Task 需要 Repository membership。使用 Git 支持的检出目录，从 `project bind` 结果或 `project inspect --format json` 的匹配条目中复制 `membershipId`。程序和每个参数都需要单独传递；CtxFerry 不会解析 shell 命令字符串。

```sh
ctxferry task add unit-tests --purpose 'Run the Go unit tests' \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared --format json
ctxferry task list --format json
ctxferry task inspect unit-tests --format json
```

添加结果包含 Task 的稳定 `id` 和 `revision`。检查结果显示其用途、输入、membership、工作目录、程序、参数数组、平台条件、Resource 槽位和凭据要求。

以短横线开头的参数使用 `--arg=VALUE`。重复 `--input` 描述输入，使用 `--platform <os>` 或 `--platform <os/arch>` 限制适用范围。存储策略是必需的：`shared` 使定义可以参与同步，`local-only` 将其保留在此设备上。

## 运行本地 Task

```sh
ctxferry run unit-tests --format jsonl
```

CtxFerry 根据 Task 的 Repository membership 解析工作目录，而不是使用调用方当前子目录。当多个已注册检出目录或 worktree 都能运行它时，请显式选择一个：

```sh
ctxferry run unit-tests --checkout /path/to/git-root --format jsonl
```

终端格式会流式转发子进程输出。JSONL 产生 `start`、`stdout`、`stderr` 和最终 `result` 事件。输出块位于 `data`；无效 UTF-8 块使用 `encoding: "base64"`。每个序列化事件最多 8 KiB，整个流最多 64 KiB。最终结果包含 `truncated` 和子进程 `exitCode`，因此测试失败仍可与 CtxFerry 控制面错误区分。`run --format json` 按设计是用法错误；请使用 `text` 或 `jsonl`。

执行发生在调用方 CLI 进程中，而不是桌面应用或同步服务中。无凭据 Task 不需要 Provider 登录或执行信任批准。

要修改 Task，请读取其当前修订，只替换计划修改的字段：

```sh
ctxferry task update unit-tests --if-revision <current-revision> \
  --arg=test --arg=-count=1 --arg=./... --format json
```

执行字段发生变化后，带凭据运行前需要重新进行信任评审。Resource 槽位、Profile 映射和凭据要求请参阅[凭据与执行信任](credentials.md)。共享定义通过[私有同步](sync.md)发布；执行信任仍按检出目录和设备保留在本地。
