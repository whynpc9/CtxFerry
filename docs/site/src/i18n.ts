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
    title: "CtxFerry — Local-first development context",
    description:
      "Bind local project context, choose what agents can read, and sync selected notes to your private GitHub repository.",
    eyebrow: "Local-first · CLI and native desktop",
    heroTitle: "Project context,",
    heroAccent: "ready to travel.",
    lead: "CtxFerry keeps development context for each project on your device. Give agents concise summaries, and sync only the notes you choose to your private GitHub repo.",
    getStarted: "Get started",
    browseGuides: "Browse the guides",
    policyTitle: "One note, two decisions.",
    policyDescription:
      "These settings apply together. An agent can read a summary while sync sends the same note’s full body to your dedicated private GitHub repository.",
    guidesTitle: "Find your guide.",
    guidesDescription:
      "Start with one checkout and no login. Add Tasks, credential references, Skills and private sync when you need them.",
    firstRun: {
      imageAlt:
        "A ferry carries three file folders across a teal harbor toward a lighthouse.",
      title: "First run",
      note: "Run the installed CLI in your project folder.",
      commandsLabel: "Shell commands",
      resultLabel: "Expected result",
      result: "Folder bound. Your new project lists zero documents.",
    },
    notePolicy: {
      example: "Example note",
      title: "Build instructions",
      summaryTitle: "Agents read the summary",
      summary: "“Build the CLI locally.”",
      summaryNote: "The CLI also returns the title and metadata.",
      sharedTitle: "Sync publishes the full note in plaintext",
      body: "From the repository root, build the CLI.",
      sharedNote: "Sync targets your dedicated private GitHub repository.",
      localTitle: "Keep a note on this device",
      localNote: "Excluded from sync.",
    },
    guideGroups: [
      {
        label: "Start here",
        links: [
          ["getting-started", "Install and bind a checkout"],
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
    title: "CtxFerry — 本地优先的开发上下文",
    description: "绑定本地项目上下文，选择代理可以读取的内容，并将选定笔记同步到你的私有 GitHub 仓库。",
    eyebrow: "本地优先 · CLI 与原生桌面应用",
    heroTitle: "项目上下文，",
    heroAccent: "随时随行。",
    lead: "CtxFerry 将每个项目的开发上下文保存在你的设备上。向代理提供简明摘要，并只把选定笔记同步到你的私有 GitHub 仓库。",
    getStarted: "快速开始",
    browseGuides: "浏览使用指南",
    policyTitle: "一条笔记，两个决定。",
    policyDescription: "这些设置会同时生效。代理可以只读取摘要，而同步会将同一条笔记的完整正文发送到你专用的私有 GitHub 仓库。",
    guidesTitle: "找到所需指南。",
    guidesDescription: "从一个检出目录开始，无需登录。需要时，再添加 Task、凭据引用、Skill 和私有同步。",
    firstRun: {
      imageAlt: "一艘渡轮载着三个文件夹，穿过青绿色的港湾驶向灯塔。",
      title: "首次运行",
      note: "在项目文件夹中运行已安装的 CLI。",
      commandsLabel: "Shell 命令",
      resultLabel: "预期结果",
      result: "文件夹已绑定。新项目当前包含零篇文档。",
    },
    notePolicy: {
      example: "示例笔记",
      title: "构建说明",
      summaryTitle: "代理读取摘要",
      summary: "“在本地构建 CLI。”",
      summaryNote: "CLI 还会返回标题和元数据。",
      sharedTitle: "同步以明文发布完整笔记",
      body: "从仓库根目录构建 CLI。",
      sharedNote: "同步目标是你专用的私有 GitHub 仓库。",
      localTitle: "将笔记保留在此设备上",
      localNote: "不参与同步。",
    },
    guideGroups: [
      {
        label: "从这里开始",
        links: [
          ["getting-started", "安装并绑定一个检出目录"],
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
