import { llmsFullRoute } from "@cloudflare/nimbus-docs/agent-endpoints";
import { withImageUrls } from "../markdown-images";

const route = llmsFullRoute();
export const GET = withImageUrls(route.GET);
