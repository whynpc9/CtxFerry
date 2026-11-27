export const locales = ["en", "zh-CN"] as const;
export type Locale = (typeof locales)[number];
export const defaultLocale: Locale = "en";

export function otherLocale(locale: Locale): Locale {
  return locale === "en" ? "zh-CN" : "en";
}

export function localeFromDocId(id: string): Locale {
  return id.startsWith("zh-CN/") ? "zh-CN" : "en";
}

export function localizedDocId(id: string, locale: Locale): string {
  if (locale === "zh-CN") {
    return id === "docs" ? "zh-CN/docs" : id.replace(/^docs\//, "zh-CN/docs/");
  }
  return id === "zh-CN/docs" ? "docs" : id.replace(/^zh-CN\/docs\//, "docs/");
}

export function localeUrl(base: string, locale: Locale, path = ""): string {
  return base + (locale === defaultLocale ? "" : locale + "/") + path;
}

export function docUrl(base: string, id: string): string {
  return base + id + "/";
}

export const ui = {
  en: {
    docs: "Docs",
    mainNavigation: "Main",
    footerNavigation: "Footer",
    documentation: "Documentation",
    homeLabel: "CtxFerry home",
    skipToGuide: "Skip to guide",
    browseGuides: "Browse guides",
    useDarkTheme: "Use dark theme",
    useLightTheme: "Use light theme",
    languageLabel: "中文",
    languageAriaLabel: "Read this page in Simplified Chinese",
  },
  "zh-CN": {
    docs: "文档",
    mainNavigation: "主导航",
    footerNavigation: "页脚导航",
    documentation: "文档导航",
    homeLabel: "CtxFerry 首页",
    skipToGuide: "跳转到指南正文",
    browseGuides: "浏览指南",
    useDarkTheme: "使用深色主题",
    useLightTheme: "使用浅色主题",
    languageLabel: "English",
    languageAriaLabel: "阅读此页的英文版本",
  },
} as const;

export const landingCopy = {
  en: {
    title: "CtxFerry — Coding-agent context across machines",
    description:
      "Give coding agents on every machine approved project context and credentialed Tasks. Private Git sync carries references, not credential values.",
    eyebrow: "Local-first · CLI, agent Skill and native desktop",
    heroTitle: "Coding agents,",
    heroAccent: "one private workflow.",
    lead: "Give agents on each machine project context kept outside source Git and registered Tasks for credentialed commands. CtxFerry synchronizes context and credential references through your private GitHub repo—not secret values, provider sessions or execution trust.",
    getStarted: "Get started",
    browseGuides: "Browse the guides",
    policyTitle: "One note, two decisions.",
    policyDescription:
      "These settings apply together. An agent can read a summary while sync sends the same note’s full body to your dedicated private GitHub repository.",
    guidesTitle: "Find your guide.",
    guidesDescription:
      "Connect your agent, add private context and a credentialed Task, then repeat the workflow on another device.",
    firstRun: {
      imageAlt:
        "A ferry carries three file folders across a teal harbor toward a lighthouse.",
      title: "First run",
      note: "Run the installed CLI in your project folder.",
      commandsLabel: "Shell commands",
      resultLabel: "Expected result",
      result: "Folder bound. Your Project is ready for private agent context.",
    },
    notePolicy: {
      example: "Example note",
      title: "Private agent handoff",
      summaryTitle: "Agents read approved context",
      summary: "“Use the registered API check.”",
      summaryNote: "The CLI also returns the title and metadata.",
      sharedTitle: "Sync publishes the full note in plaintext",
      body: "Use demo-api-check for the disposable API. Never print its token.",
      sharedNote: "Your private repo carries this note and the credential reference—not the token.",
      localTitle: "Keep other notes on this device",
      localNote: "Local-only notes, provider sessions and execution trust never sync.",
    },
    guideGroups: [
      {
        label: "Start here",
        links: [
          ["getting-started", "Install and bind a checkout"],
          ["tutorial", "Tutorial: agents on two machines"],
          ["desktop", "Open the native desktop"],
        ],
      },
      {
        label: "Work in a project",
        links: [
          ["context", "Write and retrieve Context"],
          ["tasks-and-resources", "Describe Resources and run Tasks"],
          ["credentials", "Use credential references"],
          ["skills-and-artifacts", "Install Skills and file Artifacts"],
        ],
      },
      {
        label: "Work across devices",
        links: [
          ["sync", "Publish and join private sync"],
          ["recovery", "Resolve conflicts and recover data"],
        ],
      },
      {
        label: "Troubleshooting",
        links: [["diagnostics", "Diagnose local and sync state"]],
      },
    ],
  },
  "zh-CN": {
    title: "CtxFerry — 跨机器编码代理上下文",
    description: "为每台机器上的编码代理提供经过批准的 Project 上下文和带凭据 Task。私有 Git 同步携带引用，而不是凭据值。",
    eyebrow: "本地优先 · CLI、代理 Skill 与原生桌面应用",
    heroTitle: "编码代理，",
    heroAccent: "共用一套私有工作流。",
    lead: "让每台机器上的代理使用保存在源码 Git 之外的 Project 上下文，以及用于带凭据命令的已注册 Task。CtxFerry 通过你的私有 GitHub 仓库同步上下文和凭据引用，而不是 Secret 值、Provider 会话或执行信任。",
    getStarted: "快速开始",
    browseGuides: "浏览使用指南",
    policyTitle: "一条笔记，两个决定。",
    policyDescription: "这些设置会同时生效。代理可以只读取摘要，而同步会将同一条笔记的完整正文发送到你专用的私有 GitHub 仓库。",
    guidesTitle: "找到所需指南。",
    guidesDescription: "连接代理，添加私有上下文和带凭据 Task，然后在另一台设备上重复整套工作流。",
    firstRun: {
      imageAlt: "一艘渡轮载着三个文件夹，穿过青绿色的港湾驶向灯塔。",
      title: "首次运行",
      note: "在项目文件夹中运行已安装的 CLI。",
      commandsLabel: "Shell 命令",
      resultLabel: "预期结果",
      result: "文件夹已绑定。Project 已准备好接入私有代理上下文。",
    },
    notePolicy: {
      example: "示例笔记",
      title: "私有代理交接",
      summaryTitle: "代理读取批准的上下文",
      summary: "“使用已注册的 API 检查。”",
      summaryNote: "CLI 还会返回标题和元数据。",
      sharedTitle: "同步以明文发布完整笔记",
      body: "一次性 API 使用 demo-api-check。不得打印其令牌。",
      sharedNote: "私有仓库携带此笔记和凭据引用，而不是令牌。",
      localTitle: "将其他笔记保留在此设备上",
      localNote: "Local-only 笔记、Provider 会话和执行信任绝不同步。",
    },
    guideGroups: [
      {
        label: "从这里开始",
        links: [
          ["getting-started", "安装并绑定一个检出目录"],
          ["tutorial", "教程：双设备编码代理工作流"],
          ["desktop", "打开原生桌面应用"],
        ],
      },
      {
        label: "在项目中工作",
        links: [
          ["context", "编写和检索 Context"],
          ["tasks-and-resources", "描述 Resource 并运行 Task"],
          ["credentials", "使用凭据引用"],
          ["skills-and-artifacts", "安装 Skill 和归档 Artifact"],
        ],
      },
      {
        label: "跨设备工作",
        links: [
          ["sync", "发布并加入私有同步"],
          ["recovery", "解决冲突并恢复数据"],
        ],
      },
      {
        label: "故障排查",
        links: [["diagnostics", "诊断本地和同步状态"]],
      },
    ],
  },
} as const;
