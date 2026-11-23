import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import {
  defineDocSchema,
  withNimbusMarkdown,
} from "@cloudflare/nimbus-docs/content";

export const collections = {
  docs: defineCollection({
    loader: withNimbusMarkdown(
      glob({
        base: "../content",
        pattern: "**/*.md",
        generateId: ({ entry }) => {
          const chinese = entry.startsWith("zh-CN/");
          const page = chinese ? entry.slice("zh-CN/".length) : entry;
          const prefix = chinese ? "zh-CN/docs/" : "docs/";
          return (prefix + page)
            .replace(/\.md$/, "")
            .replace(/(^|\/)index$/, "");
        },
      }),
    ),
    schema: defineDocSchema(),
  }),
};
