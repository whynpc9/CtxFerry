---
title: 凭据引用与执行信任
---

[English](../credentials.md) · 简体中文

CtxFerry 存储和同步凭据**引用**，而不是凭据值。每台设备分别向自己的 Provider Profile 进行身份验证，每台设备和每个 Checkout 都要在 Task 获得值之前批准执行定义。

## 声明要求和 Resource 映射

从 [Resource 和 Task](tasks-and-resources.md) 中的 Resource 与 Git membership 设置开始。以下 Task 声明名为 `db` 的 Resource 槽位和名为 `login` 的凭据要求。后端键刻意独立于交付变量 `DATABASE_URL`。

```sh
ctxferry task add integration-check --purpose 'Run integration checks' \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared \
  --resource-slot '{"name":"db","kind":"database","stage":"dev"}' \
  --credential-requirement '{"name":"login","resourceSlot":"db","purpose":"integration-tests","type":"connection-string","env":"DATABASE_URL"}' \
  --format json
ctxferry profile create development
ctxferry profile map-resource development integration-check db <resource-uuid>
ctxferry profile select development
```

Resource 的 kind 和 stage 必须与槽位匹配。要求的用途、类型和选定 Resource 也必须与最终 SecretBinding 匹配；绝不会用名称相似的条目替代。

## 绑定精确的 Provider 条目

只有在 Task 要求和 Profile Resource 映射存在后才能创建绑定。将定位器占位符替换为精确的 Infisical US Cloud 命名空间和键：

```sh
ctxferry secret bind integration-check login --profile development \
  --alias integration-db --provider infisical --provider-profile team \
  --method universal-auth --backend-project <infisical-project-id> \
  --environment dev --path /api --key API_DB_URL --format json
```

结果包含共享 SecretBinding 元数据和新 Profile 映射，不包含值。后端环境是 Provider 命名空间，不会隐式等同于 Resource 的业务 stage。

后端提供稳定条目 ID 时，使用 `--entry-id <entry-id>` 替代 `--backend-project`、`--environment`、`--path` 和 `--key`。对于 Bitwarden，改用 `--provider bitwarden`、Provider Profile `default`、方法 `personal`，并将精确登录条目 UUID 作为 `--entry-id` 创建绑定；适配器解析该条目的密码字段。

要复用另一个 Project 的现有绑定，请将 Task 的 Resource 槽位映射到同一个共享 Resource UUID，并传递 `--binding <binding-uuid>` 替代所有新绑定字段。要将已映射槽位移动到另一个 Resource，请先对该槽位映射的每个凭据运行 `secret unmap <task> <requirement> --profile <name>`，更改 Resource 映射，再重新绑定凭据。取消映射不会删除可复用的 SecretBinding。

## 在此设备上进行身份验证

身份验证按 Workspace、Device 和 Provider Profile 区分。Catalog 同步不需要 Provider 登录。

### Infisical US Cloud

对于个人会话，先使用官方 Infisical CLI 以同一个命名 Profile 登录，再让 CtxFerry 采用其当前访问令牌：

```sh
infisical login
ctxferry provider login infisical --provider-profile team --method personal
```

对于机器身份，通过现有受保护启动器提供 `INFISICAL_CLIENT_ID` 和 `INFISICAL_CLIENT_SECRET`，然后运行下面的命令。如果启动器同时设置了 `CTXFERRY_INFISICAL_PROFILE`，登录只会将客户端凭据用于该 provider profile：

```sh
ctxferry provider login infisical --provider-profile team --method universal-auth
```

两种登录方法都需要网络，并将生成的访问令牌存入操作系统凭据存储。CtxFerry 报告 `credential-storage-unavailable`，不会写入明文令牌文件。Infisical 令牌过期后重新登录；服务端撤销或限制可能更早生效。

在没有 keyring 的无头主机上，受保护启动器可以改为通过 `CTXFERRY_INFISICAL_ACCESS_TOKEN` 向 `secret status` 和带凭据运行提供当前机器令牌。还必须将 `CTXFERRY_INFISICAL_WORKSPACE_ID`、`CTXFERRY_INFISICAL_DEVICE_ID` 和 `CTXFERRY_INFISICAL_PROFILE` 设置为精确的本地 ID 和 Profile。使用 `ctxferry init --format json` 查询 ID。限定范围的令牌仍由启动器控制，不会传给 Task 子进程。

### Bitwarden Password Manager

安装官方 `bw` 可执行文件，在此设备上登录并解锁 Bitwarden，通过受保护环境提供生成的 `BW_SESSION`。使用 Provider Profile `default`，或将 `CTXFERRY_BITWARDEN_PROFILE` 设置为所选 Profile。要将解锁密钥纳入操作系统凭据存储，请运行：

```sh
ctxferry provider login bitwarden --provider-profile default --method personal
```

对于无头状态检查和运行，受保护启动器可以同时提供 `BW_SESSION`、`CTXFERRY_BITWARDEN_WORKSPACE_ID`、`CTXFERRY_BITWARDEN_DEVICE_ID` 和 `CTXFERRY_BITWARDEN_PROFILE`，并限定到精确的本地安装。启动器必须独立建立有效解锁会话；仅使用 API key 登录通常不能解密 vault。

Bitwarden 分别报告登录、解锁和会话有效性。有效密钥可以离线读取先前同步的 vault 条目，但不能证明当前服务端访问或内容新鲜度。Infisical 不报告离线读取能力。两个 Provider 的身份验证材料都不会进入同步或 Task 子进程环境。

## 在不暴露值的情况下检查状态

```sh
ctxferry secret list --format json
ctxferry secret status integration-check login --profile development --format json
```

`secret list` 只读取 catalog 元数据，不联系 Provider。`secret status` 先验证选定 Task、Profile、Resource 映射、精确 SecretBinding 和定位器，再检查此设备。

独立看待返回的事实：

| 字段 | 能够确认的内容 |
| --- | --- |
| `bound` | 要求具有显式且兼容的映射 |
| `authenticated` | 选定 Provider 身份验证检查成功；`null` 表示未知 |
| `entryRetrievable` | 精确条目可以读取；不返回值 |
| `resourceReachable` | 此设备最近一次匹配的 `resource check` 观察，而不是新探测 |
| `authentication` 和 `capabilities` | 方法特定的登录详情，以及离线或执行能力 |

只要能够评估 Task 和 Project，失败仍可能在 `error.details` 中保留这些事实。`binding-missing`、`binding-conflict`、`incompatible`、`authentication-required` 和 `secret-unavailable` 表示不同的缺失条件。每种状态的下一项检查请参阅[诊断](diagnostics.md)。

## 评审并启用执行信任

在此设备和 Checkout 启用当前定义之前，带凭据执行会被阻止。先预览并检查每个字段，再启用与该预览完全一致的定义：

```sh
ctxferry task trust preview integration-check --profile development --format json
ctxferry task trust enable integration-check --profile development \
  --preview <preview> --format json
```

使用 `data.preview` 中的令牌。预览、启用和运行使用相同的 `--profile`，并在需要时使用相同的 `--checkout <git-root>`。

定义涵盖程序和参数、自定义命令权限、Repository membership、解析后的 Checkout 和工作目录、Resource 映射、SecretBinding 映射、Provider Profile、定位器和 Resource 用途。预览状态为 `not-enabled`、`enabled`、`execution-definition-changed`，或无凭据 Task 的 `not-required`。对于已变化的定义，请比较 `definition`、`enabled` 和列出的 `changes`。

预览后定义发生变化时，`enable` 返回 `preview-stale`；请重新预览。信任记录绝不同步。每台设备都要评审新的执行相关字段，包括做出同步编辑的设备。用途和其他纯描述性编辑不会使已启用定义失效。

## 带凭据运行

检查 Resource 和 Provider 状态并启用信任后，运行：

```sh
ctxferry run integration-check --profile development --format jsonl
```

CtxFerry 只解析此 Task 的要求。子进程获得最小基础环境和 Task 的交付变量，不会获得其他 Task 的值、Git 身份验证、Provider 令牌或调用方的完整环境。值在调用期间只保存在内存中，不写入通用 Secret 缓存，并在 CtxFerry 转发的输出中替换为 `[REDACTED]`。

缺失或不可用的路由可能在 Provider 解析前以 `resource-unreachable` 停止运行；CtxFerry 不会静默替换为同名本地 Resource。解析值期间中断会报告 `interrupted`。

目标程序仍可读取、编码、打印或外传它收到的每个值。输出替换不是安全保证，Task 也不是操作系统隔离。

注册或更新 Task 时指定 `--allow-custom-command`，还允许：

```sh
ctxferry exec --task integration-check --profile development --format jsonl \
  -- go test ./...
```

`exec` 使用相同的 Profile、Checkout、信任、环境、脱敏和退出码规则。启用带有 `customCommands: true` 的定义，意味着批准调用方使用这些凭据选择任意程序。Task 未显式启用该选项时，命令以 `custom-command-not-allowed` 停止。
