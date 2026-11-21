import type { APIRoute } from "astro";

const images = import.meta.glob<string>("../../content/assets/*.png", {
  eager: true,
  query: "?url",
  import: "default",
});

// Nimbus serves authored Markdown from several directories, including the site
// root. Give image references a stable URL in each standalone text document.
export function withImageUrls(get: APIRoute): APIRoute {
  return async (context) => {
    const response = await get(context);
    if (!response.ok) return response;
    const source = await response.text();
    const body = source.replace(
      /(!\[[^\]\n]*\]\()assets\/([\w-]+\.png)\)/g,
      (match, prefix, file) => {
        const url = images["../../content/assets/" + file];
        return url
          ? prefix + new URL(url, context.site ?? context.url).href + ")"
          : match;
      },
    );
    return new Response(body, {
      status: response.status,
      headers: response.headers,
    });
  };
}
