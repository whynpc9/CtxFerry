# 文档

[English](README.md) · [简体中文](README.zh-CN.md)

[使用指南](content/zh-CN/index.md)涵盖当前源码树中可用的原生桌面和 CLI 工作流。Nimbus 文档站以 `docs/content/` 为内容来源。英文是默认语言并保留现有 URL；简体中文源文件位于 `docs/content/zh-CN/`，通过 `/zh-CN/` 路径提供。面向读者的指南及其资源需要在两个语言目录中同步添加。共享图片保留在 `docs/content/assets/`，中文页面通过 `../assets/` 引用。

## 本地开发

- [使用文档计划](development/documentation-plan.md)将每个公开页面映射到读者任务，并记录其覆盖的桌面版本。
- [Context 存储与查询约定](development/context-storage.md)介绍 schema 迁移、索引和响应限制。
- [同步协议与测试传输](development/sync-protocol.md)介绍 Git 格式、协调规则和故障注入场景。
- [原生重设计评审](development/desktop/review-native-redesign.md)记录当前桌面指南对应的运行应用、数据状态和截图来源。
- [桌面验收](development/desktop/acceptance-28.md)涵盖 Overview、同步队列、远程检查和确认备份。
- [初版桌面实现评审](development/desktop/review-28.md)记录测试版本、截图和发现。
- [Issue 跟踪](agents/issue-tracker.md)、[分类标签](agents/triage-labels.md)和[领域文档](agents/domain.md)介绍贡献者工作流。

实现说明和验证证据保存在 `docs/development/`，代理说明保存在 `docs/agents/`，架构决策在采用后保存在 `docs/adr/`。这些目录不属于站点内容来源。

## 本地预览站点

使用 Node.js 24。从仓库根目录构建并启动静态站点：

```sh
cd docs/site
npm ci
npm run build
npm run preview
```

在预览服务器上打开 `/CtxFerry/` 查看英文首页，打开 `/CtxFerry/docs/` 查看英文使用指南；中文首页和指南分别位于 `/CtxFerry/zh-CN/` 和 `/CtxFerry/zh-CN/docs/`。英文指南在 `docs/content/` 中编辑，中文指南在 `docs/content/zh-CN/` 中编辑；站点直接读取这些文件，并在构建时解析 Markdown 链接。

## 部署到 GitHub Pages

推送到 `main` 且改动涉及 `docs/` 或 `.github/workflows/docs.yml` 时，会构建静态站点并部署到 GitHub Pages。工作流也支持手动触发。它使用 Node.js 24、`npm ci` 和 `npm run build`，然后发布 `docs/site/dist`。公开站点为 `https://whynpc9.github.io/CtxFerry/`。

当前首页采用此前 CLI 首页设计评审第 7 轮的方案（归档 SHA-256 `38cc3497f59d66d6dcca246e63caccd7ccd1ec7f55ee976f07cc575d0f6d9b2b`）。在三个评审席位都提交结果的轮次中，该方案的最低分最高。评审在第 8 轮结束，未达到三个席位均至少 9.5 分的通过条件。

[与桌面一致的文档首页评审](../plans/documentation-landing/README.md)达到十轮上限，仍未达到三位评审者均至少 9/10 的通过条件。其提案和渲染设计已单独归档。
