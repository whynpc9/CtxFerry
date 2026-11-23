import { defineConfig } from "astro/config";
import nimbus from "@cloudflare/nimbus-docs";

const base = "/CtxFerry/";

// Keep the Markdown navigable on GitHub and use localized directory URLs on the site.
const guideLinks = ({ fileURL }) => {
  const sourcePrefix = fileURL?.pathname.includes("/zh-CN/") ? "zh-CN/" : "";
  return {
    name: "ctxferry-guide-links",
    element: {
      filter: ["a"],
      visit(node) {
        const match = /^(?:(\.\.\/)|(zh-CN\/)|(?:\.\/)?)([\w-]+)\.md([?#].*)?$/.exec(
          node.properties.href ?? "",
        );
        if (!match) return;
        const [, english, chinese, page, suffix = ""] = match;
        const localePrefix = english ? "" : chinese ?? sourcePrefix;
        const href =
          base + localePrefix + "docs/" + (page === "index" ? "" : page + "/") + suffix;
        return { ...node, properties: { ...node.properties, href } };
      },
    },
  };
};

export default defineConfig({
  base,
  output: "static",
  trailingSlash: "always",
  compressHTML: false,
  i18n: {
    locales: ["en", "zh-CN"],
    defaultLocale: "en",
    routing: { prefixDefaultLocale: false },
  },
  integrations: [
    nimbus(
      {
        site: "https://whynpc9.github.io",
        title: "CtxFerry",
        description: "Local-first development context",
        github: "https://github.com/whynpc9/CtxFerry",
        locale: "en",
        agents: { discovery: false },
      },
      {
        validateMdx: false,
        markdown: { hastPlugins: [guideLinks] },
      },
    ),
  ],
});
