import { agentApi, AgentRequestError } from "@/lib/api";
import { readToken } from "@/product/api";
import type { AgentDef, Limits } from "@/lib/types";

type WireLimits = { max_delegations: number; max_concurrent_agents: number; max_tool_calls: number; max_model_turns: number; max_tokens: number; max_cost_usd: number; timeout_seconds: number };
type WireSpec = {
  id: string; version: string; name: string; role?: string; instructions?: string;
  input_schema?: unknown; output_schema?: unknown; allowed_tools?: string[]; allowed_delegates?: string[]; limits?: WireLimits;
};
type WireDefinition = { spec: WireSpec; state?: string };

export const limitsFromWire = (l: WireLimits): Limits => ({
  turns: l.max_model_turns, tools: l.max_tool_calls, delegations: l.max_delegations, parallel: l.max_concurrent_agents, seconds: l.timeout_seconds,
});

export const limitsToWire = (l: Limits, base?: Partial<WireLimits>): WireLimits => ({
  max_delegations: l.delegations, max_concurrent_agents: l.parallel, max_tool_calls: l.tools, max_model_turns: l.turns,
  max_tokens: base?.max_tokens ?? 12000, max_cost_usd: base?.max_cost_usd ?? 2, timeout_seconds: l.seconds,
});

const NO_LIMITS: Limits = { turns: 0, tools: 0, delegations: 0, parallel: 0, seconds: 0 };

function versionParts(version: string): number[] {
  return version.split(".").map((part) => Number(part) || 0);
}

function newerVersion(left: string, right: string): boolean {
  const a = versionParts(left);
  const b = versionParts(right);
  const n = Math.max(a.length, b.length);
  for (let i = 0; i < n; i++) {
    const diff = (a[i] ?? 0) - (b[i] ?? 0);
    if (diff !== 0) return diff > 0;
  }
  return false;
}

/** One card per agent. A republish keeps the older version for past runs. */
function latestVersions(definitions: WireDefinition[]): WireDefinition[] {
  const byID = new Map<string, WireDefinition>();
  for (const definition of definitions) {
    const id = definition.spec?.id;
    if (!id) continue;
    const current = byID.get(id);
    if (!current || newerVersion(definition.spec.version, current.spec.version)) byID.set(id, definition);
  }
  return [...byID.values()];
}

function fromSpec(spec: WireSpec, state: string): AgentDef {
  const kind = spec.role === "orchestrator" || (spec.allowed_delegates?.length ?? 0) > 0 ? "orchestrator" : "specialist";
  return {
    id: spec.id, version: spec.version, name: spec.name || spec.id,
    icon: kind === "orchestrator" ? "network" : "bot", kind,
    role: spec.role ?? kind, instructions: spec.instructions ?? "",
    tools: spec.allowed_tools ?? [], delegates: spec.allowed_delegates ?? [],
    limits: spec.limits ? limitsFromWire(spec.limits) : NO_LIMITS,
    maxCostUsd: spec.limits?.max_cost_usd ?? 0, maxTokens: spec.limits?.max_tokens ?? 0,
    input: spec.input_schema ? JSON.stringify(spec.input_schema) : "", output: spec.output_schema ? JSON.stringify(spec.output_schema) : "",
    state,
  };
}

/** True when the agent's input schema accepts a free-text `task`. */
export const takesTask = (a: AgentDef) => a.input.includes('"task"');

/**
 * Agents known to the service. Published definitions carry the full spec; when
 * the definitions API is not mounted, only the roster (id, version, name) exists.
 */
export async function loadAgents(): Promise<AgentDef[]> {
  const headers = new Headers();
  const token = readToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch("/agent-api/definitions", { cache: "no-store", headers });
  if (res.ok) {
    const body = (await res.json()) as { definitions?: WireDefinition[] };
    const published = (body.definitions ?? []).filter((d) => d.state === "published" || d.state === undefined);
    const latest = latestVersions(published);
    if (latest.length) return latest.map((d) => fromSpec(d.spec, d.state ?? "published"));
  }
  if (res.status === 401) throw new AgentRequestError("Session expired", 401);
  const { agents } = await agentApi.agents();
  return agents.map((a) => fromSpec({ id: a.id, version: a.version, name: a.name }, "roster"));
}
