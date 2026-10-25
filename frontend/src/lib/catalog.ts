// What the agent builder offers as tool tiles and model choices. The agent
// service enforces the real allowlists; this only decides what can be picked.

import type { IconName } from "@/components/ui/Icon";

export type ToolTile = { id: string; name: string; provider: string; icon: IconName };

export const toolTiles: ToolTile[] = [
  { id: "serpapi.google_search", name: "Google Search", provider: "SerpAPI", icon: "search" },
  { id: "serpapi.google_patents", name: "Google Patents", provider: "SerpAPI", icon: "scroll-text" },
  { id: "serpapi.google_scholar", name: "Google Scholar", provider: "SerpAPI", icon: "graduation-cap" },
  { id: "catalog.query", name: "Vendor catalog", provider: "Internal", icon: "boxes" },
  { id: "web.fetch", name: "Fetch URL", provider: "HTTP", icon: "globe" },
  { id: "slack.post", name: "Post to Slack", provider: "Slack", icon: "message" },
];

export const models: Record<string, { label: string }> = {
  "meta/muse-spark-1.3-contributor": { label: "Muse Spark 1.3 Contributor" },
  "meta/muse-wonder-1.0": { label: "Muse Wonder" },
  "opencode/flint-40b-free": { label: "Flint 40B Free" },
};
