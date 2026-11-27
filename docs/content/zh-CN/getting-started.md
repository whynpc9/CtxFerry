---
title: 安装并绑定你的第一个检出目录
---

[English](../getting-started.md) · 简体中文

本指南将创建一个可用的本地 Workspace，无需账户、网络连接、桌面应用或凭据 Provider。绑定目录不要求 Git；但 Repository 发现、与 Repository membership 关联的 Task 和 Git 同步需要 Git。

## 从源码安装 CLI

你需要 CtxFerry 源码检出目录，以及与 `go.mod` 中版本兼容的 Go 工具链（此源码版本使用 Go 1.27.4）。

从 CtxFerry 源码目录运行：

```sh
go install ./cmd/ctxferry
ctxferry --help
```

`go install` 会将 `ctxferry` 放入 Go 二进制目录。如果第二条命令找不到它，请将该目录加入 `PATH`；`go env GOBIN` 和 `go env GOPATH` 可以显示相关 Go 设置。帮助输出可以确认可执行文件是否可用。

## 初始化本地 Workspace

运行：

```sh
ctxferry init --format json
```

成功结果包含 `ok: true`，以及 `data.workspace`、`data.device` 和 `data.created`。CtxFerry 在应用数据根目录中创建一个 Workspace 和一个 Device 身份。重复执行 `init` 会保留这些身份，并报告 `created: false`。

初始化不会联系 GitHub 或 Secret Provider。在决定是否配置同步之前，你可以继续使用所有本地工作流。

## 将检出目录绑定到 Project

进入一个现有项目目录，然后运行：

```sh
cd /path/to/your/checkout
ctxferry project bind . --slug my-project --format json
ctxferry project list
ctxferry project inspect --format json
```

将 `my-project` 替换为唯一的小写 slug，或省略 `--slug`，让系统根据目录名生成。绑定结果包含 `data.project` 和 `data.binding`，其中包括规范化本地路径。对于 Git 检出目录，还包含 Task 使用的 Repository 和 membership ID。

结果可以从两个位置观察：

- `project list` 输出 Project slug 和稳定 UUID。
- `project inspect` 输出解析到的 Project、本地绑定、解析来源、当前目录、可用时的 Git 检出信息，以及此 Device 的身份。

目录可以是 Git 检出目录、worktree 或非 Git 目录。绑定写入 CtxFerry 应用数据根目录，而不是被绑定目录。重复相同绑定会保留 Project 身份。具有相同默认名称的第二个目录会获得不同 slug，不会被隐式合并。

不带选择器时，检查使用当前目录最长已绑定祖先目录对应的 Project。要将另一个本地检出目录绑定到同一个逻辑 Project，请运行：

```sh
ctxferry project bind /path/to/another-checkout --project my-project
ctxferry project inspect my-project --format json
```

显式 `--project` 只选择 Project，并不表示当前目录属于它。远程 URL 可以产生候选项，但绝不会自动绑定。如果解析结果有歧义，CtxFerry 返回 `project-unbound` 并列出候选项，而不是猜测。

## 选择并保护数据目录

默认应用数据根目录如下：

- Linux 和 WSL：`$XDG_DATA_HOME/ctxferry`，或 `~/.local/share/ctxferry`。
- macOS：`~/Library/Application Support/ctxferry`。
- Windows：`%LOCALAPPDATA%\ctxferry`。

Windows 和 WSL 是独立执行环境，需要分别安装。要隔离测试安装或模拟另一台设备，请设置 `CTXFERRY_DATA_DIR`，或传入显式数据根目录：

```sh
ctxferry --data-dir /path/to/isolated-ctxferry init --format json
ctxferry --data-dir /path/to/isolated-ctxferry project list --format json
```

`--data-dir` 优先于环境变量。请将数据根目录放在本地文件系统上，并位于所有已绑定源码目录之外。活动 catalog 位于该根目录下的 `workspaces/default/catalog.sqlite`；它使用 WAL 模式，不是可以在运行中设备之间复制或放入同步文件夹的文件。查询不会创建缺失的存储，损坏或版本过新而不受支持的 catalog 也不会被自动替换。

## 使用机器可读接口约定

查询和管理命令接受 `--format json`。标准输出只包含一个对象，帮助和失败也是如此：

```json
{"schemaVersion":1,"ok":true,"data":{"projects":[]},"error":null}
```

失败时包含 `ok: false`、`data: null`，以及具有稳定 `code` 和人类可读 `message` 的 `error`；可选的 `details` 提供结构化证据。诊断信息写入标准错误。退出码 0 表示成功，2 表示用法错误，1 表示应用或存储错误。Task 执行改用 JSONL；请参阅 [Resource 和 Task](tasks-and-resources.md#运行本地-task)。

## 后续步骤

- [跟随编码代理教程](tutorial.md)，用五个步骤连接双设备代理、私有 Context 和带凭据 Task。
- [编写 Context 文档](context.md)，选择它仅保留本地，还是可以参与私有同步。
- 绑定或路径未按预期解析时，[使用 doctor 检查此设备](diagnostics.md)。
- 只有准备好检查并发布共享明文记录时，才[连接私有同步](sync.md)。
- [打开原生桌面应用](desktop.md)，检查此检出目录、预览配置变更或查看等待同步的记录。
