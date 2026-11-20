import { defineConfig } from "astro/config";
import nimbus from "@cloudflare/nimbus-docs";

const base = "/CtxFerry/";

// Keep the Markdown navigable on GitHub and use directory URLs on the site.
const guideLinks = {
  name: "ctxferry-guide-links",
  element: {
    filter: ["a"],
    visit(node) {
      const match = /^(?:\.\/)?([\w-]+)\.md([?#].*)?$/.exec(
        node.properties.href ?? "",
      );
      if (!match) return;
      const [, page, suffix = ""] = match;
      const href =
        base + "docs/" + (page === "index" ? "" : page + "/") + suffix;
      return { ...node, properties: { ...node.properties, href } };
    },
  },
};

export default defineConfig({
  base,
  output: "static",
  trailingSlash: "always",
  compressHTML: false,
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
