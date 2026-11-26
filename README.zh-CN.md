# CtxFerry

[English](README.md) · [简体中文](README.zh-CN.md)

本地优先的开发上下文。当前首个阶段支持本地 Workspace 和 Project 绑定，无需登录、网络连接、GUI、DBus 或 WebKitGTK。

[使用指南](docs/content/zh-CN/index.md) · [开发者文档](docs/README.zh-CN.md)

```sh
go build -o ctxferry ./cmd/ctxferry
ctxferry init
ctxferry project bind .
ctxferry project list
ctxferry project inspect
ctxferry project inspect <uuid-or-slug> --format json
```

`project bind` 接受任何现有目录，包括非 Git 目录。它只写入应用数据根目录。重复初始化和重复绑定会保留原有身份。`--slug` 用于指定唯一的小写 slug；否则根据目录名生成 slug。同名第二个目录会获得不同的 Project。不带选择器执行检查时，会使用当前目录最长已绑定祖先目录对应的 Project。

设置 `--data-dir <root>` 或 `CTXFERRY_DATA_DIR` 可以隔离一套安装。命令行参数优先。Linux/WSL 默认使用 `$XDG_DATA_HOME/ctxferry`（或 `~/.local/share/ctxferry`），macOS 默认使用 `~/Library/Application Support/ctxferry`，Windows 默认使用 `%LOCALAPPDATA%\ctxferry`。Windows 和 WSL 使用相互独立的安装。请将数据根目录放在已绑定目录之外，并位于本地文件系统上：其中包含正在使用的 WAL 模式 SQLite 数据库，不是可共享的同步文件夹。

每个应用数据根目录目前保存一个 Workspace 和一个 Device，数据库位于 `workspaces/default/catalog.sqlite`。UUID 在进程重启后保持稳定。本地 catalog 使用短暂的立即写事务、外键、FULL 同步写入，并为每个连接设置 5 秒忙碌超时。不支持的新 schema 和损坏的数据库会直接报错，不会被替换。未来切换 Workspace 时必须保留 Device 身份。

每条命令都支持 `--format json`，输出一个对象（失败和帮助信息也是如此）：

```json
{"schemaVersion":1,"ok":true,"data":{"projects":[]},"error":null}
```

失败时返回 `ok:false`、`data:null`，以及包含 `code` 和 `message` 的错误；诊断信息写入 stderr。退出码 0 表示成功，2 表示用法错误，1 表示应用或存储错误。JSON 字段名和 schema 版本构成脚本接口约定。文本输出供人阅读。

运行 `ctxferry doctor` 或 `ctxferry doctor --format json` 可以检查设备。每项检查都有独立状态和证据。只要能够生成报告，命令就会成功，即使部分能力缺失也是如此。一次成功同步会在 24 小时后标记为过期，与排队变更和已记录同步状态无关。

Doctor 可以离线工作。Git 身份验证依据历史同步证据和安全的本地配置线索。Provider 身份验证在操作系统能够安全暴露信息时使用限定范围的会话元数据。如果这些证据无法确认实时身份验证状态，结果会保持未知。已记录的 TCP 观察只说明可达性。本版本支持前台同步。

CLI 负责解析和渲染；`internal/app` 负责操作与验证，`internal/store` 负责持久化。Git 仅用于发现，且是可选依赖。普通 Context 查询不需要 Secret Provider 或桌面依赖。查询不会初始化缺失的存储。

运行 `go test -count=1 ./...` 和 `go vet ./...`。可复用的 `internal/testcli` 测试工具使用隔离的数据目录和 home 目录、经过清理的环境、截止时间、独立输出流以及严格的 JSON 解码来执行真实二进制。CI 在禁用 CGO 的情况下检查 Linux、macOS 和 Windows，并运行离线 Linux 容器冒烟测试。

## 原生桌面应用

从仓库根目录进入独立 Go 模块，运行 MyGo 原生桌面应用。

```sh
cd cmd/ctxferry-desktop
go run .
```

Home、Projects、Catalog 和 Sync 打开与 CLI 相同的本地 Workspace。
Home 显示此设备上需要处理的事项。Projects 用于检查
Checkout 并预览配置变更。Catalog 保存 Resource、
凭据、Skill 和运行 Artifact。Sync 显示仓库、队列和
备份范围。在两个 shell 中都设置 `CTXFERRY_DATA_DIR` 可以选择另一个 Workspace。

设置和完整操作演示请参阅[桌面应用图文指南](docs/content/zh-CN/desktop.md)，
已捕获场景请参阅[原生实现评审](docs/development/desktop/review-native-redesign.md)。

## 首设备引导

```sh
ctxferry onboard start .
ctxferry onboard status
ctxferry onboard bind --preview <preview-id>
ctxferry onboard scan ./private-notes.md
ctxferry onboard import ./private-notes.md --title "Private notes" \
  --storage local-only --disclosure reference
ctxferry onboard finish
```

Start 将选定目录保存在设备的本地 catalog 中。Status 使用本地目录、Git 元数据和 catalog 预览 Project。如果预览列出候选项，请使用 `onboard status --project <uuid-or-slug>`，并将相同选择器传给 `onboard bind`。绑定需要当前预览 ID。关闭 CLI 后，可以运行 `onboard status` 继续。

每篇 Markdown 或 UTF-8 文本笔记都需要明确选择。扫描结果显示字段名和发现类型。导入会重新扫描实际保存的字节，并使用你选择的存储与披露模式。共享笔记可以进行明文同步。使用 `--summary` 提供摘要披露所需的文本。不导入任何内容也可以完成引导。Finish 会对已保存目录运行常规 doctor 检查。

### 导入选定的 env 文件

运行 `ctxferry onboard scan-env ./.env --project api` 可以列出变量名和该 Project 中候选的 Task。候选项是具有凭据要求的共享 Task，且其交付变量与文件中的字段匹配。对于尚无要求的 Task，使用 `task add` 或 `task update` 声明要求。每个字段都只是提示，你需要独立选择要求和后端键。

选择一个共享 Task。附属于 Resource 槽位的需求还需要兼容的共享 Resource：使用 Profile 中该槽位的现有映射，或传入 `--resource <uuid>`。API 密钥这类独立需求不需要 Resource。Profile 的默认规则与 `secret bind` 相同。确认前，请登录选定设备的 Provider Profile。运行以下命令预览精确映射，然后带上 `--confirm <preview-id>` 再次运行。

```sh
ctxferry onboard import-env ./.env --project api --profile dev --task integration \
  --requirement login --field DATABASE_URL --resource <resource-uuid> --alias db \
  --provider infisical --provider-profile team --method universal-auth \
  --backend-project <infisical-project-id> --environment dev --path /api --key API_DB_URL
```

重复 `--field` 可一次导入多个字段。每个字段映射到交付变量同名的需求，并获得各自的绑定：别名为需求名称，键为字段名。`--like <alias>` 会从现有绑定复制 Provider 和后端命名空间：

```sh
ctxferry onboard import-env ./.env --project api --task agent \
  --field OPENAI_API_KEY --field ANTHROPIC_API_KEY --like openai
```

解析器接受 UTF-8 单行 `NAME=value` 赋值、可选的 `export`、空行、注释以及成对的单引号或双引号。它拒绝重复名称（包括仅大小写不同的名称）、多行值、shell 语法和展开。单引号内可以将 shell 字符作为字面数据。命令只读取指定的普通文件，不会修改它。预览 ID 确认文件路径和映射元数据。导入在确认时会重新读取并验证选定文件。

导入支持 Infisical US Cloud 的 Universal Auth、精确文件夹和键，以及在其中创建共享 Secret 的权限。它使用 [v4 create endpoint](https://infisical.com/docs/api-reference/endpoints/secrets/create)。带有首尾空白或 Infisical 引用语法的值会被拒绝，因为端点可能转换它们。其他 Provider 和身份验证方法需要声明支持创建导入的适配器。

一个预览 ID 覆盖整批字段。确认时会依次创建每个 Provider 条目，然后在一个 catalog 事务中提交所有 SecretBinding 和 Profile 映射。已存在的后端键或凭据映射会阻止普通导入。如果某个 Provider 步骤中途失败，错误会在 `error.details.created` 中列出已创建条目的字段；请对每个字段使用 `--recover=<field>` 重新预览。Catalog、同步记录和回执只携带经过批准的元数据与引用。

如果 Provider 结果不确定、批准待处理，或者创建后本地 catalog 提交失败，请用 `secret status` 检查映射。对于缺失的映射，使用 `--recover`（所有字段）或 `--recover=<field>`（可重复）再次运行相同导入命令以获得新预览，然后确认。恢复需要 read-value 权限，并在内存中将精确的现有条目与源字段比较后才保存映射。条目缺失或不同则不会改动 catalog。普通导入和恢复都不会改变现有 Provider 条目。

Project 按以下顺序识别：显式 `--project`、最长本地路径绑定、本地绑定的 Git common directory 与 membership 范围，最后是远程候选项。新 worktree 继承包含它的最深仓库相对范围。相同范围指向不同 Project 时保持未绑定。直接路径绑定始终优先，即使存在更窄的推断范围。远程匹配始终保持未绑定，即使只有一个候选项。未绑定检查以退出码 1 和 `project-unbound` 结束；JSON 在 `error.details` 中包含解析结果和候选项，文本输出在 stderr 中列出候选项。

```sh
ctxferry project bind . --slug api --remote git@github.com:owner/repo.git
ctxferry project bind ../other-repo --project api
ctxferry project inspect --project api --format json
ctxferry project remote add <repository-uuid> https://github.com/owner/renamed.git
ctxferry project bind ../another-clone --project api --repository <repository-uuid>
```

Git 绑定会报告 Repository UUID 和 membership UUID。一个 Project 可以跨越多个 Repository；一个 Repository 可以在不同范围包含多个 Project。只有显式本地绑定才能复用 common-directory 身份。另一个 clone 需要 `--repository` 才能复用 Repository；远程地址不会建立身份。绑定时使用可重复的 `--remote`，或之后使用 `project remote add`，可以显式接受别名。添加重命名别名会保留旧别名。只有常规 GitHub SSH/HTTPS 形式会被规范化，owner 身份会保留。带有 upstream 远程地址的 fork 可能产生 upstream 候选项，但绝不会自动继承 Project。

`project inspect` 会报告解析来源、当前目录、当前 Checkout 根目录、worktree Git 目录、common directory，以及 Device 身份、操作系统和架构。显式选择 Project 并不表示当前 Checkout 属于该 Project。Git 发现使用本地只读子进程，总截止时间为 5 秒，忽略继承的 `GIT_*` 覆盖，并且绝不 fetch。缺少 Git 或发现失败会报告为不可用；显式路径绑定和非 Git 目录仍可工作。原始远程 URL 不会被回显或存储。Schema 2 以事务方式迁移旧绑定并保留身份和路径；请重复执行显式绑定以补全旧 Git 绑定。

Context 命令添加了离线 ContextDoc、相互独立的存储与披露策略、带修订检查的 `context put`，以及 SQLite FTS5 搜索。请参阅 [Context 命令指南](docs/content/zh-CN/context.md)。Schema 3 在 Schema 2 的 Project 识别迁移之后添加文档。

首次私有发布支持独立的 Git/隐私检查、精确的明文预览和显式分支初始化。请参阅[私有同步指南](docs/content/zh-CN/sync.md)。

注册 Project 服务时必须指定存储策略。共享端点使用非 Secret 的主机与端口组合。localhost 端点和代理地址通过设备本地路由配置。

```sh
ctxferry resource add --identity dev-db --kind database --stage dev \
  --purpose "Integration tests" --storage shared --endpoint db.example.com:5432
ctxferry resource list
ctxferry resource route <resource-uuid> --endpoint localhost:15432
ctxferry resource inspect <resource-uuid> --format json
ctxferry resource check <resource-uuid> --format json
```

只有 `resource check` 会打开 TCP 连接。它记录解析后端点的可达性；配置了本地代理时，则记录代理本身的可达性。每个结果都会追加到此设备的 Observation 历史。检查不会验证服务协议，也不会使用凭据。

使用 `project bind` 或 `project inspect` 显示的 membership UUID 注册 Task。每个程序参数都需要单独传递。

```sh
ctxferry task add unit-tests --purpose "Run the Go unit tests" \
  --membership <membership-uuid> --working-directory . --program go \
  --arg=test --arg=./... --storage shared
ctxferry task list --format json
ctxferry task inspect unit-tests --format json
ctxferry run unit-tests --format jsonl
```

`run` 根据 membership 根目录解析 Task 的工作目录。当多个本地已注册 Checkout 或 worktree 都能运行 Task 时，传递 `--checkout <git-root>`。默认文本格式会流式转发子进程输出。JSONL 将每个序列化事件限制为 8 KiB，整个流限制为 64 KiB。输出事件携带 `data`，包含无效 UTF-8 的块使用 `encoding: "base64"`。最终结果包含 `truncated`，并区分子进程退出码和控制面错误。

使用 `--resource-slot '{"name":"db","kind":"database","stage":"dev"}'` 声明 Resource 槽位。使用 `ctxferry profile map-resource development <task> db <resource-uuid>` 显式映射。共享 Profile 将共享 Task 映射到符合槽位 kind 和 stage 的共享 Resource。使用 `profile select` 在本地选择 Profile，或在运行时使用 `run --profile`。Task 可以声明凭据要求，请参阅下文的“带凭据运行”。

### 凭据引用

不对应 CtxFerry 可路由或探测服务的 API 密钥属于**独立**凭据。将槽位留空声明它并直接绑定，无需 Resource、槽位映射或 `resource check`：

```sh
ctxferry task add agent --purpose "Coding agent" --storage shared \
  --credential openai::model-api:api-key:OPENAI_API_KEY \
  --credential anthropic::model-api:api-key:ANTHROPIC_API_KEY --allow-custom-command
ctxferry secret bind agent openai --provider infisical --provider-profile team \
  --method universal-auth --backend-project <infisical-project-id> --environment dev
ctxferry secret bind agent anthropic --like openai
```

新绑定的 `--alias` 默认为需求名称，`--path` 默认为 `/`，`--key` 默认为需求的交付变量。`--like <alias-or-uuid>` 从 Project 可见的绑定复制 Provider、Provider profile、方法、后端项目、环境和路径；显式参数优先。未指定 `--profile` 时，凭据命令先使用检出目录选择的 Profile，其次使用 Project 中名为 `default` 的 Profile；两者都不存在且 Project 没有任何 Profile 时，`secret bind` 和 `onboard import-env` 会创建 `default`。带 `--allow-custom-command` 注册的 Task 可以省略命令；此时 `run` 会拒绝它，它只把凭据提供给 `exec`。

独立需求只接受没有 Resource 的绑定，带槽位的需求只接受其映射 Resource 的绑定。独立凭据的 `secret status` 中 `resourceReachable` 为 `null`。参见 [ADR 0001](docs/adr/0001-standalone-credentials.md)。

对于对应某个服务的凭据，注册 Task 要求并在 Profile 中映射其 Resource 槽位后，再创建绑定。绑定从该选择中取得 Resource、用途和类型。

```sh
ctxferry secret bind integration-check login --profile development \
  --alias shared-db --provider infisical --provider-profile team --method universal-auth \
  --backend-project 00000000-0000-0000-0000-000000000000 --environment sandbox --path /app --key BACKEND_DB
ctxferry secret list --format json
ctxferry secret status integration-check login --profile development --format json
```

后端提供稳定条目 ID 时，可以使用 `--entry-id` 替代四个命名空间与键参数。Task 的交付变量独立于后端键。要复用另一个 Project 的绑定，请将 Task 的 Resource 槽位映射到同一个共享 Resource UUID，并传递 `--binding <uuid>` 替代新绑定字段。要将已映射槽位移动到另一个 Resource，请先对该槽位对应的每个凭据运行 `secret unmap <task> <requirement> --profile <name>`，再更改 Resource 映射并重新绑定凭据。取消映射后，共享 SecretBinding 仍可供其他 Profile 使用。

`secret list` 读取 catalog 元数据。`secret status` 在验证 Profile 映射后，检查一个 Task 要求的精确引用。其 JSON 独立报告绑定、身份验证、条目可检索性和 Resource 可达性。未知事实为 `null`。失败时，只要能够评估 Task 和 Project，同样的事实也会出现在 `error.details` 中。可达性使用此设备上最近一次匹配的 `resource check` 观察结果。

### Infisical US Cloud

- **个人身份**（`--method personal`）。在运行 `ctxferry provider login infisical --provider-profile <name> --method personal` 前，先通过 `infisical login` 交互登录。使用相同的 Infisical CLI 命名 Profile，或显式选择 `default` 来采用其当前 Profile。适配器使用现有 CLI 会话获取访问令牌。令牌过期后，重新运行 CtxFerry 登录。
- **机器身份**（`--method universal-auth`）。通过现有受保护启动器提供 `INFISICAL_CLIENT_ID` 和 `INFISICAL_CLIENT_SECRET`。将 `CTXFERRY_INFISICAL_PROFILE` 设置为选定 Provider Profile 名称，然后运行 `ctxferry provider login infisical --provider-profile <name> --method universal-auth`。此流程需要网络，但不需要浏览器或终端。

两种登录方法都将访问令牌存入操作系统凭据存储（Secret Service、Keychain 或 Credential Manager）。无法访问存储时，登录报告 `credential-storage-unavailable`。在没有 keyring 的无头主机上，受保护启动器可以改为通过 `CTXFERRY_INFISICAL_ACCESS_TOKEN` 为 `secret status` 和带凭据 `run` 提供当前机器令牌。将 `CTXFERRY_INFISICAL_WORKSPACE_ID`、`CTXFERRY_INFISICAL_DEVICE_ID` 和 `CTXFERRY_INFISICAL_PROFILE` 设置为精确的本地 Workspace、Device 和 Provider Profile。使用 `ctxferry init --format json` 查询 Workspace 和 Device ID。匹配令牌只用于该次调用，并始终由启动器控制。两种来源都没有时，状态和运行报告 `authentication-required`。这些变量绝不会传给 Task 子进程。

会话检查和读取需要网络连接。两种方法都会报告令牌过期，Infisical 也可能提前撤销或限制访问。指定稳定 Secret ID 时，读取会使用该 ID。按名称读取需要显式的 Infisical Project ID、环境 slug、文件夹和精确共享键。Resource stage 与这些设置独立，适配器会禁用导入 Secret 和引用展开。适配器不声明离线读取、原生执行或文件交付能力。Catalog 同步与 Provider 登录互不影响。

### Bitwarden Password Manager

在 PATH 中安装官方 `bw` 可执行文件。在此设备上登录并解锁 Bitwarden，通过受保护环境提供生成的 `BW_SESSION`。在 `secret bind` 中使用 `--method personal` 和 `--entry-id <item-uuid>`。绑定解析该精确登录条目的密码。从 Bitwarden 复制其 UUID。条目缺失、访问被拒绝或没有密码时返回 `secret-unavailable`。
[Bitwarden CLI 文档](https://bitwarden.com/help/cli/)

使用 Provider Profile `default`，或将 `CTXFERRY_BITWARDEN_PROFILE` 设置为正在使用的命名 Profile。如果使用独立的 Bitwarden CLI 账户目录，请在解锁和运行 CtxFerry 命令时都将 `BITWARDENCLI_APPDATA_DIR` 设置为同一个绝对路径。要将密钥纳入此设备的操作系统凭据存储，请运行：

```sh
ctxferry provider login bitwarden --provider-profile default --method personal
```

无头使用时，受保护启动器可以为 `secret status` 和带凭据运行提供 `BW_SESSION`。还必须将 `CTXFERRY_BITWARDEN_WORKSPACE_ID`、`CTXFERRY_BITWARDEN_DEVICE_ID` 和 `CTXFERRY_BITWARDEN_PROFILE` 设置为精确的本地 ID 和 Provider Profile。使用 `ctxferry init --format json` 获取 ID。这条路径无需操作系统 keyring。启动器必须独立建立有效的解锁会话。Bitwarden 个人 API key 可以无头登录，但 vault 解密通常仍需单独解锁。没有可用密钥时，即使 vault 已登录，也会报告 `authentication-required` 并附带解锁诊断。
[Bitwarden API key 身份验证](https://bitwarden.com/help/personal-api-key/)

`secret status` 分别报告 `loggedIn`、`unlocked` 和 `sessionValidity`。有效性反映选定的本地 `bw` 缓存和限定范围的密钥。Bitwarden 在锁定或注销时使密钥失效，CtxFerry 不报告定时过期。有效密钥允许从本地 vault 离线读取先前同步的条目。内容新鲜度、当前服务端访问权和特定条目的可用性仍是独立事实。登录和获取更新需要网络连接。
[Bitwarden 离线行为](https://bitwarden.com/help/using-bitwarden-offline/)

### 带凭据运行

`run` 只解析正在运行的 Task 的要求，解析链包括 Checkout 的 Project、Task、选定 Profile、其显式映射、SecretBinding 及其定位器，以及此设备自己的 Provider 会话：既可以是针对这个精确 Workspace、Device 和 Provider Profile 配置的运行时会话，也可以是 `provider login` 保存的会话。每次运行前，要求、选定 Resource 和绑定必须在用途、类型和 Resource 上一致；否则运行以 `incompatible` 停止。缺失映射报告 `binding-missing`，冲突的映射编辑报告 `binding-conflict`；绝不会用相似绑定替代。

带凭据执行还要求此设备已启用 Task 的执行定义（见下文“执行信任”）。在此之前，带凭据运行会在调用任何 Provider 前以 `execution-definition-changed` 停止。运行本身不会主动建立连接：如果此设备最近一次通过选定 Resource 的直接路由执行 `resource check` 时不可达，或者设备没有可用路由，运行会以 `resource-unreachable` 停止。绝不会改用同名的本地 Resource。登录按设备区分：另一台设备上的会话不算数（`authentication-required`）；条目缺失或访问被拒绝会报告 `secret-unavailable`，且不包含其值。解析值期间中断运行会报告 `interrupted`。

子进程获得最小基础环境（`PATH`、`HOME`、临时目录、区域设置和终端变量）以及 Task 的交付变量。其他 Task 的变量、Git 和 Provider 身份验证材料以及调用方环境的其余部分都不会传入。值不会出现在参数、shell 字符串或文件中，只在调用期间保存在内存中；没有 Secret 缓存。即使 Provider 声明原生执行能力，CtxFerry 也会自行构建子进程环境。转发 stdout 和 stderr 时，交付值会替换为 `[REDACTED]`。

目标程序仍然可以读取、编码、打印或外传它获得的一切。输出替换不是安全保证，Task 也不是操作系统隔离。

### 执行信任

每台设备独立决定运行哪些带凭据执行定义。评审定义涵盖程序和参数、`exec` 是否可以运行自定义命令、Repository membership、解析后的本地 Checkout 和工作目录，以及选定 Profile 的 Resource 与 SecretBinding 映射，包括每个绑定的 Provider Profile、定位器和 Resource 用途。先预览，再启用与预览完全一致的定义：

```sh
ctxferry task trust preview integration-check --profile development --format json
ctxferry task trust enable integration-check --profile development --preview <preview>
```

传递与 `run` 相同的 `--profile` 和 `--checkout`。预览会报告 `state`（`not-enabled`、`enabled`、`execution-definition-changed`，或无凭据 Task 的 `not-required`）、当前定义；对于已变化的定义，还会报告已启用定义和变化字段。如果定义不再匹配预览，`enable` 会以 `preview-stale` 拒绝，例如预览后又有同步变更落地；此时需要重新预览。启用后，运行不会再次询问。任何已评审字段发生同步或本地变更，都会使带凭据 `run` 和 `exec` 以 `execution-definition-changed` 停止，详细信息会列出变化字段，直到此设备启用新版本。编辑 Task 的用途、输入、别名或其他描述性文本不会影响启用状态。信任记录保留在此设备的本地存储中，绝不同步；每台设备都要评审，包括做出变更的设备。

批准按 Git Checkout 或 worktree 以及 membership 目录区分。当同步编辑将 Task 的 membership 目录移动到同一 Checkout 内或另一个 Repository 时，预览会与最近一次先前的批准比较。`enable` 先解析 Checkout，再在本地存储写锁下准备并保存定义，期间不运行 Git，因此检查和批准之间不会发生变更。

使用 `task update` 在当前修订上修改 Task；只有给定字段会被替换。如果编辑会使 Profile 映射指向已移除或不兼容的槽位或要求，命令以 `invalid-task-dependency` 拒绝；请先更改或取消该映射。

```sh
ctxferry task update integration-check --if-revision <revision> --arg=check --arg=--verbose
```

### 自定义命令

注册或更新 Task 时指定 `--allow-custom-command`，可让 `exec` 使用该 Task 的凭据运行调用方选择的命令：

```sh
ctxferry exec --task integration-check --profile development --format jsonl -- go test ./...
```

`exec` 使用与 `run` 相同的 Checkout、工作目录、Profile 和凭据要求，需要相同的已启用定义，并遵循相同的输出、脱敏和退出码约定。程序可以是绝对路径、相对于 Task 工作目录的路径，或 PATH 中的名称。路径原样传递给操作系统，因此 `..` 会按文件系统解析符号链接的方式处理。Windows 驱动器相对路径（`C:tool.exe`）和驱动器根路径（`\tool.exe`）会以 `invalid-command` 拒绝。参数以数组传递，绝不经过 shell。未显式启用时以 `custom-command-not-allowed` 停止。启用允许自定义命令的定义，意味着批准通过它的 `exec` 运行任何命令，预览也会通过 `customCommands: true` 说明这一点。开启此选项本身就是执行变更。命令可以读取并外传交付值。

早于此版本的客户端不认识 `allowCustomCommand` 字段。带有此选项的共享 Task 发布后，旧客户端会以 `unsupported-schema` 停止同步，直到升级；它们不会丢弃该字段。

### 本地 Artifact

为新运行显式注册一个文件，或使用运行 UUID 向现有运行追加另一个 Artifact。提供来自原始执行的来源信息。未使用 Profile 时使用 `none`，无法取得源修订时使用 `unknown`。按 argv 顺序重复 `--command`，并根据需要重复 `--tool`、`--input`、`--completed` 和 `--uncompleted`。对于有未提交更改的源，`--diff-id` 可以引用已有 diff。

```sh
ctxferry artifact add report.json --project api --type application/json \
  --sensitivity internal --source-revision "$revision" --dirty=false \
  --profile development --tool 'go@1.27.4' --command go --command test \
  --command=./... --exit-code 0 --summary 'Unit tests passed' --completed unit \
  --format json
ctxferry artifact add summary.txt --project api --run <run-uuid> \
  --type text/plain --sensitivity internal
ctxferry artifact list --project api --run <run-uuid>
ctxferry artifact inspect <artifact-uuid> --project api --format json
ctxferry artifact fetch <artifact-uuid> --project api --output retrieved.json
```

每个 manifest 及其运行来源信息都不可变。Payload 按 SHA-256 在 Workspace 的本地 Artifact 缓存中只存储一次。Fetch 在发布新文件前验证缓存字节。本版本中，所有敏感级别（`public`、`internal`、`sensitive`）都仅限本地。本地缓存默认最多接受 1 GiB 唯一 Payload 字节。将 `CTXFERRY_ARTIFACT_CACHE_BYTES` 设置为正字节数可调整限制。达到容量后，新内容会被拒绝，现有注册仍可检索。

### Skill

执行 `ctxferry init` 后，可以导入一个包含 `SKILL.md` 的目录，其 YAML front matter 必须声明 `name`。该名称会成为安装目录名称。

```sh
ctxferry skill add --local ./skills/example
ctxferry skill add --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example
ctxferry skill plan example --target codex --format json
ctxferry skill apply example --target codex
ctxferry skill plan another-example --target claude-code --symlink
ctxferry skill apply another-example --target claude-code --symlink
```

Git 导入接受完整提交哈希或 `refs/tags/` 下的标签。标签会同时记录其名称和解析后的提交。本地导入捕获内容摘要，并保留独立于后续源编辑的快照。使用 `--license` 覆盖 front matter 中的许可证。未知许可证显示为 `NOASSERTION`。使用可重复的 `--platform <os[/arch]>` 和 `--depends-on <executable>` 声明要求。可执行文件元数据会捕获到 manifest 中，并在 Unix 上生效。导入和安装不会执行 Skill 脚本。

Codex 使用 `~/.agents/skills`。Claude Code 使用 `~/.claude/skills`，配置后则使用 `$CLAUDE_CONFIG_DIR/skills`。默认使用复制。`--symlink` 链接到经过验证的本地包，并要求操作系统允许创建链接。Plan 在不更改目标或本地状态的情况下报告预期写入和阻塞问题。被阻止的 Plan 仍是成功查询，并报告 `status: "blocked"`。Apply 拒绝不属于它的目标或被外部编辑的安装。如果 Apply 中断，再次运行相同命令即可恢复经过验证的暂存安装。安装文件、本地包缓存、回执和恢复记录都留在各自设备上。

要共享 Skill，请显式批准 pin 并将其名称加入组：

```sh
ctxferry skill add --share public --git https://github.com/example/skills.git \
  --ref refs/tags/v1.2.0 --subdir skills/another-example
ctxferry skill add --share private --local ./skills/private-example
ctxferry skill group set global another-example
ctxferry skill group set project:my-project private-example
ctxferry skill group set capability:review another-example private-example
ctxferry sync
```

`group set` 替换指定组的选择。不传 Skill 名称即可清空。组只能包含共享 pin。Project 组可以在订阅其 Project 的设备上规划。Capability 组需要在每台设备上显式选择。例如，加入同一 Workspace 后：

```sh
ctxferry skill plan --group capability:review --target codex --format json
ctxferry skill apply another-example --target codex
```

组 Plan 列出每个成员的本地投影。检查后，逐个 Apply 需要安装的 Skill。公共 Skill 从经过批准的 HTTPS 来源按记录提交获取，并验证文件 manifest。私有 Skill 通过 Workspace 的私有同步存储分发最多 1 MiB 的选定文件，同步后可以离线安装。共享前请检查这些文件。并发 pin 编辑需要通过 `sync conflicts` 和 `sync resolve` 显式选择。共享 pin 变更不会改动现有安装，直到你预览并确认本地更新。对于共享安装，使用带目标的 `skill update` 预览已批准 pin。使用 `skill add --share` 批准不同的共享 pin。

对于已安装 Skill，请先预览更新再应用。本地更新读取记录的源目录。Git 更新需要完整提交哈希或标签，并显示精确解析后的提交。

```sh
ctxferry skill update example --target codex
ctxferry skill update example --target codex --confirm <confirmation-from-preview>
ctxferry skill rollback example --target codex
ctxferry skill rollback example --target codex --confirm <confirmation-from-preview>
```

确认时重复相同参数。Git 更新两次都要包含相同的 `--ref`。确认涵盖预览中的精确 pin、安装条目，以及文件新增、移除、内容摘要和可执行位。如果源在此期间变化，请重新预览。更新和回滚保留目标的复制或符号链接模式，并操作该目标的本地回执。存在外部编辑或不属于它的文件时，操作会被阻止且文件保持原样。两个命令都不会执行 Skill 脚本。

先前完整版本仍可离线用于回滚。如果替换中断，使用目标及其安装模式重新运行 `skill apply`，即可恢复最近提交的版本。恢复暂存目录和本地版本缓存会保留被替换的数据。

### 全局代理入口

执行 `ctxferry init` 后，可以预览并安装内置的 `ctxferry` Skill。

```sh
ctxferry skill bootstrap install --target codex
ctxferry skill bootstrap install --target codex --preview <token-from-preview>
```

Cursor 使用 `--target cursor`，从 `~/.cursor/skills/ctxferry/SKILL.md` 加载 Skill。Pi 使用 `--target pi`，从 `~/.pi/agent/skills/ctxferry/SKILL.md` 加载。配置 `PI_CODING_AGENT_DIR` 时，Pi 使用 `$PI_CODING_AGENT_DIR/skills/ctxferry/SKILL.md`。这两个 `skill bootstrap` 目标只安装 Skill。

Claude Code 使用 `--target claude-code`。Codex 和 Claude Code 还会安装一段受管理指令。Codex 的指令块写入 `~/.codex/AGENTS.md`（或 `$CODEX_HOME/AGENTS.md`），存在非空 `AGENTS.override.md` 时使用后者。Claude Code 的指令块写入 `~/.claude/CLAUDE.md`（或 `$CLAUDE_CONFIG_DIR/CLAUDE.md`）。预览仅显示拟议变更和 CtxFerry 的文本。使用 `--format json` 从 `data.preview` 读取令牌。

如果其他代理支持读取仓库指令，可以在项目的 `AGENTS.md` 中说明，需要项目上下文时，从当前检出目录运行 `ctxferry context show --format json`。代理只需能通过终端运行 CLI，无需配置 MCP 服务。

重复安装可以验证入口或再次预览变更。完整安装会报告 `unchanged`。若要更新已安装的内置 Skill，请先预览并确认卸载，再预览并确认安装。如果文件在预览和应用之间发生变化，请重新预览。要移除入口，使用 `skill bootstrap uninstall` 按同样方式预览并应用。卸载保留其他指令文本和文件。受管理块被编辑、Skill 被编辑或名称冲突且目标不属于它时，会报告为需要手动解决。所有权和恢复记录保留在 CtxFerry 数据根目录中。
