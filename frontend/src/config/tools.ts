// Tool registry. Every tool call goes through the gateway, which checks the
// calling agent's allowlist (AgentDef.tools) before anything runs.

import type { ToolDef } from "@/lib/types";

export const tools: ToolDef[] = [
  { id: "serpapi.google_search", name: "Google Search", mark: "G", provider: "SerpAPI", sideEffect: "READ_ONLY", timeoutSeconds: 10 },
  { id: "serpapi.google_patents", name: "Google Patents", mark: "P", provider: "SerpAPI", sideEffect: "READ_ONLY", timeoutSeconds: 15 },
  { id: "serpapi.google_scholar", name: "Google Scholar", mark: "S", provider: "SerpAPI", sideEffect: "READ_ONLY", timeoutSeconds: 10 },
  { id: "catalog.query", name: "Vendor catalog", mark: "C", provider: "Internal", sideEffect: "READ_ONLY", timeoutSeconds: 5 },
  { id: "catalog.search", name: "Vendor catalog", mark: "C", provider: "Internal", sideEffect: "READ_ONLY", timeoutSeconds: 5 },
  { id: "web.fetch", name: "Fetch URL", mark: "W", provider: "HTTP", sideEffect: "READ_ONLY", timeoutSeconds: 10 },
  { id: "slack.post", name: "Post to Slack", mark: "#", provider: "Slack", sideEffect: "WRITE", timeoutSeconds: 5 },
];

export const toolById = (id: string) => tools.find((t) => t.id === id);
