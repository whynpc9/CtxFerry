import { markdownRoute } from "@cloudflare/nimbus-docs/agent-endpoints";
import { withImageUrls } from "../../markdown-images";

const route = markdownRoute();
export const { getStaticPaths } = route;
export const GET = withImageUrls(route.GET);
