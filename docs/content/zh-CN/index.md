---
title: CtxFerry 使用指南
---

[English](../index.md) · 简体中文

CtxFerry 将一个逻辑 Project 的开发上下文保存在你的设备上。从一个检出目录开始，无需登录；需要时再添加共享 Resource、Task、凭据引用、Skill 和 Artifact。只有当你希望在另一台设备上使用相同的共享上下文时，才需要私有 Git 仓库。

## 选择你的目标

### 从这里开始

- [安装并绑定检出目录](getting-started.md)：从源码构建 CLI，初始化本地 Workspace，并在无网络的情况下检查 Project。
- [跟随端到端教程](tutorial.md)：让一个 Go Project 从本地 Context 和 Task，逐步走到带凭据执行和双设备同步。
- [打开原生桌面应用](desktop.md)：从 Home 跟踪冲突到队列，检查 Checkout 的有效配置，或在 Catalog 中查找工具。

### 在项目中工作

- [编写和检索 Context](context.md)：选择存储和代理披露策略，搜索本地上下文，并理解 Profile 与设备覆盖。
- [描述 Resource 并运行 Task](tasks-and-resources.md)：注册服务，检查其 TCP 可达性，并从对应 Checkout 运行固定命令。
- [使用凭据引用](credentials.md)：映射 Task 要求，检查 Provider 状态，并在此设备上批准精确的执行定义。
- [安装 Skill 和归档 Artifact](skills-and-artifacts.md)：预览 Skill 投影，注册、检索或显式共享运行结果。

### 跨设备工作

- [发布并加入私有同步](sync.md)：检查明文范围，初始化专用仓库，加入第二台设备，并选择前台或后台同步。
- [解决冲突并恢复数据](recovery.md)：比较记录版本，恢复一篇文档，恢复本地设备，或在新 epoch 下恢复共享历史。

### 参考

- [CLI 参考](cli-reference.md)：查询全局参数、输出约定、命令语法和命令专用参数。

### 故障排查

- [诊断本地和同步状态](diagnostics.md)：解读 `doctor`、同步状态、身份验证证据和 Resource 观察，不将未知状态视为成功。

## 了解存储边界

CtxFerry 有三个相互独立的事实来源：

- **本地 catalog** 保存设备路径、未发布工作、缓存数据和仅限本地的记录。它可以离线读取。
- **私有同步仓库**以明文保存显式共享的记录和符合条件的对象。仓库访问权限覆盖整个 Workspace；Project 订阅不是访问控制边界。
- **外部凭据 Provider** 仍是凭据值的事实来源。CtxFerry 同步的是引用，而不是 Provider 会话或 Secret 值。

代理披露与同步存储相互独立。即使 `context get` 只返回摘要或引用，共享文档仍可发布其完整正文。发布前请检查共享源材料；绝不能进入私有 Git 历史的内容必须保持为 local-only。
