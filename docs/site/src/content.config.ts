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
        generateId: ({ entry }) =>
          ("docs/" + entry).replace(/\.md$/, "").replace(/(^|\/)index$/, ""),
      }),
    ),
    schema: defineDocSchema(),
  }),
};
