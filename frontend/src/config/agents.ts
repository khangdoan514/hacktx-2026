// Agent registry. Add an agent here and it appears on the Agents page, in the
// spec drawer, and in the execution graph of any orchestrator that delegates to it.
// The graph lays itself out from `delegates`, `tools` and `sim.steps`.

import type { AgentDef, Limits } from "@/lib/types";

const specialistLimits: Limits = { turns: 4, tools: 3, delegations: 0, parallel: 1, seconds: 20 };

export const agents: AgentDef[] = [
  {
    id: "idea-consultant", version: "1.0.0", name: "Idea Consultant", icon: "network", kind: "orchestrator",
    model: "claude-opus-5-5", role: "Finds original, feasible product ideas for a task and returns the top 3 with sources.",
    instructions: "Split the task into research, generation and critique. Delegate each part to a specialist. Only return ideas a critic has checked, and cite every source.",
    tools: [], delegates: ["market-scout", "idea-generator", "novelty-critic"],
    plan: [["market-scout"], ["idea-generator"], ["novelty-critic"]],
    limits: { turns: 20, tools: 8, delegations: 5, parallel: 2, seconds: 75 },
    input: "{ task: string }", output: "{ ideas: [{ title, why, novelty, source_ids[] }] }",
    sim: { result: "3 ideas" },
  },
  {
    id: "market-scout", version: "1.0.0", name: "Market Scout", icon: "compass", kind: "specialist",
    model: "claude-sonnet-5-5", role: "Maps what already exists: competitors, products and patents.",
    instructions: "Search the market and patents for the task. Return grouped sources with IDs. Do not propose ideas.",
    tools: ["serpapi.google_search", "serpapi.google_patents"], delegates: [],
    limits: specialistLimits, input: "{ task: string }", output: "{ clusters: [...], source_ids[] }",
    sim: {
      steps: [
        { tool: "serpapi.google_search", result: "10 results", ms: 1400 },
        { tool: "serpapi.google_patents", result: "6 patents", ms: 1400 },
      ],
      result: "16 sources",
    },
  },
  {
    id: "idea-generator", version: "1.0.0", name: "Idea Generator", icon: "sparkles", kind: "specialist",
    model: "claude-opus-5-5", role: "Writes candidate ideas that avoid what the scout found.",
    instructions: "Use only the market map you were given. Write 12 distinct ideas. You cannot call tools or other agents.",
    tools: [], delegates: ["trend-analyst"], limits: { ...specialistLimits, delegations: 1 }, input: "{ task, clusters[] }", output: "{ ideas: [{ title, why }] }",
    sim: { steps: [{ think: "Drafting ideas", ms: 1200 }, { delegate: "trend-analyst" }, { think: "Writing ideas", ms: 1400 }], result: "12 ideas" },
  },
  {
    id: "trend-analyst", version: "1.0.0", name: "Trend Analyst", icon: "activity", kind: "specialist",
    model: "claude-haiku-5-5", role: "Spots where the market is heading so ideas are not already stale.",
    instructions: "Find the three strongest trends for the task. Do not propose ideas. You cannot delegate any further.",
    tools: ["serpapi.google_search"], delegates: [], limits: specialistLimits, input: "{ task }", output: "{ trends: [...] }",
    sim: { steps: [{ tool: "serpapi.google_search", result: "8 results", ms: 1100 }, { blocked: "delegate", target: "market-scout" }], result: "3 trends" },
  },
  {
    id: "novelty-critic", version: "1.0.0", name: "Novelty Critic", icon: "scale", kind: "specialist",
    model: "claude-sonnet-5-5", role: "Checks each idea against published research and keeps only the original ones.",
    instructions: "Search research for each idea. Score novelty from 0 to 1. Keep ideas scoring 0.70 or higher.",
    tools: ["serpapi.google_scholar"], delegates: [], limits: specialistLimits,
    input: "{ ideas[] }", output: "{ kept: [{ title, novelty, source_ids[] }] }",
    sim: { steps: [{ tool: "serpapi.google_scholar", result: "22 papers", ms: 1500 }, { blocked: "tool", target: "web.fetch" }], result: "Kept 3 of 12" },
  },
  {
    id: "vendor-orchestrator", version: "1.0.0", name: "Vendor Analyst", icon: "boxes", kind: "orchestrator",
    model: "claude-sonnet-5-5", role: "Compares vendors against criteria and recommends one, with source IDs.",
    instructions: "Ask the catalog specialist for matching vendors and the pricing specialist for a scored comparison, in parallel. Recommend one vendor.",
    tools: [], delegates: ["catalog-specialist", "pricing-specialist"],
    plan: [["catalog-specialist", "pricing-specialist"]],
    limits: { turns: 8, tools: 4, delegations: 2, parallel: 2, seconds: 30 },
    input: "{ task, criteria[], budget_usd }", output: "{ recommendation, source_ids[] }",
    sim: { result: "Vendor A" },
  },
  {
    id: "catalog-specialist", version: "1.0.0", name: "Catalog Specialist", icon: "search", kind: "specialist",
    model: "claude-haiku-5-5", role: "Finds vendors in the catalog that match the criteria.",
    instructions: "Query the catalog with the given criteria and return matching vendor IDs.",
    tools: ["catalog.query"], delegates: [], limits: specialistLimits, input: "{ criteria[] }", output: "{ source_ids[] }",
    sim: { steps: [{ tool: "catalog.query", result: "3 vendors", ms: 900 }], result: "3 vendors" },
  },
  {
    id: "pricing-specialist", version: "1.0.0", name: "Pricing Specialist", icon: "calculator", kind: "specialist",
    model: "claude-sonnet-5-5", role: "Scores vendors on price and total cost.",
    instructions: "Score each vendor from 0 to 10 on price. Show your weights.",
    tools: ["catalog.query"], delegates: [], limits: specialistLimits, input: "{ vendors[], criteria[] }", output: "{ scores[] }",
    sim: { steps: [{ tool: "catalog.query", result: "price sheets", ms: 1100 }, { blocked: "tool", target: "slack.post" }], result: "Scored 3" },
  },
];

export const agentById = (id: string) => agents.find((a) => a.id === id);
/** How deep an agent sits: 0 for one nobody delegates to, otherwise one below its caller. */
export const depthOf = (id: string, seen: string[] = []): number => {
  const caller = agents.find((a) => a.delegates.includes(id) && !seen.includes(a.id));
  return caller ? 1 + depthOf(caller.id, [...seen, id]) : 0;
};
export const entryAgents = agents.filter((a) => a.kind === "orchestrator");
