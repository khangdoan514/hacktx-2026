import { AgentRequestError, agentApi, type AgentRun, type RunEvent } from "@/lib/api";
import type { AgentDef, Call, ErrorCode, EventType, Limits, Op, RunPlan, State, Turn } from "@/lib/types";

const knownErrors = new Set<ErrorCode>([
  "DELEGATION_DEPTH_EXCEEDED", "TOOL_NOT_ALLOWED", "BUDGET_EXCEEDED", "TIMEOUT", "OUTPUT_SCHEMA_INVALID",
]);
const EVENT_TYPES = new Set<EventType>([
  "RUN_STARTED", "AGENT_STARTED", "TOOL_STARTED", "TOOL_SUCCEEDED", "TOOL_FAILED", "DELEGATION_REJECTED",
  "AGENT_SUCCEEDED", "AGENT_FAILED", "RUN_SUCCEEDED", "RUN_FAILED", "RUN_CANCELLED",
]);
const TERMINAL = new Set<State>(["SUCCEEDED", "FAILED", "CANCELLED"]);
const NO_LIMITS: Limits = { turns: 0, tools: 0, delegations: 0, parallel: 0, seconds: 0 };

export const isTerminal = (state: State) => TERMINAL.has(state);

const ms = (iso?: string) => {
  const t = iso ? Date.parse(iso) : NaN;
  return Number.isFinite(t) ? t : undefined;
};

export function taskOf(input: unknown): string {
  if (input && typeof input === "object" && "task" in input && typeof (input as { task: unknown }).task === "string") {
    return (input as { task: string }).task;
  }
  return typeof input === "string" ? input : "Run";
}

const text = (value: unknown) => (value === undefined || value === null ? "" : typeof value === "string" ? value : JSON.stringify(value, null, 2));

/**
 * A persisted run laid out on its real timeline. Agent and tool spans use the
 * start and finish times the service stored; model turns come from
 * MODEL_SUCCEEDED events when `events` is given, else usage is one lump.
 */
export function planFromRun(run: AgentRun, agents: AgentDef[], events?: RunEvent[]): RunPlan {
  const start = ms(run.created_at) ?? Date.now();
  const terminal = isTerminal(run.state);
  const end = ms(run.updated_at) ?? start;
  const durationMs = terminal ? Math.max(1, end - start) : Math.max(Date.now() - start, end - start) + 600_000;
  const rel = (iso?: string, fallback = 0) => Math.max(0, (ms(iso) ?? start + fallback) - start);
  const def = agents.find((a) => a.id === run.agent_id && a.version === run.agent_version) ?? agents.find((a) => a.id === run.agent_id);
  const invocations = run.invocations ?? [];
  const agentOf = new Map(invocations.map((i) => [i.id, i.agent_id]));
  const ops: Op[] = [{ at: 0, k: "event", type: "RUN_STARTED", agent: run.agent_id }];
  const calls: Call[] = [];
  const turns: Turn[] = [];

  for (const inv of invocations) {
    const at = rel(inv.started_at);
    ops.push({ at, k: "agent", agent: inv.agent_id, st: "RUNNING", now: inv.depth === 0 ? "Planning" : "Working" });
    if (inv.depth >= 1) ops.push({ at, k: "use", key: "delegations", delta: 1 });
    if (inv.state === "PENDING" || inv.state === "RUNNING") continue;
    const finish = Math.max(at, rel(inv.finished_at, durationMs));
    ops.push({ at: finish, k: "agent", agent: inv.agent_id, st: inv.state, now: inv.state === "SUCCEEDED" ? "Done" : inv.state === "CANCELLED" ? "Cancelled" : inv.error?.code ?? inv.state });
  }

  (run.tool_invocations ?? []).forEach((tool) => {
    const owner = agentOf.get(tool.agent_invocation_id) ?? run.agent_id;
    const at = rel(tool.started_at);
    const done = Math.max(at, rel(tool.finished_at, at));
    const failed = Boolean(tool.error);
    calls.push({ id: tool.id, agent: owner, name: tool.tool_id, denied: failed, kind: tool.tool_id === "subagent" ? "delegate" : "tool" });
    ops.push({ at, k: "call", call: tool.id, st: "RUNNING", text: "running…" });
    ops.push({ at, k: "use", key: "tools", delta: 1 });
    ops.push({ at: done, k: "call", call: tool.id, st: failed ? "BLOCKED" : "SUCCEEDED", text: failed ? tool.error?.code ?? "failed" : `${((done - at) / 1000).toFixed(1)}s` });
  });

  if (events) {
    for (const e of events) {
      const at = rel(e.timestamp);
      const data = (e.data ?? {}) as { tool_id?: string; code?: string; depth?: number; action?: string; usage?: { input_tokens?: number; output_tokens?: number; cost_usd?: number } };
      const agent = (e.invocation_id && agentOf.get(e.invocation_id)) || run.agent_id;
      if (EVENT_TYPES.has(e.type as EventType)) {
        const detail = data.tool_id ?? data.code ?? (data.depth !== undefined ? `depth ${data.depth}` : undefined);
        ops.push({ at, k: "event", type: e.type as EventType, agent, detail });
      } else if (e.type === "MODEL_SUCCEEDED" && data.usage) {
        const owner = invocations.find((i) => i.id === e.invocation_id);
        const n = turns.filter((t) => t.agent === agent).length + 1;
        turns.push({
          id: `${run.id}-t${turns.length + 1}`, agent, n, at, ms: 0,
          system: agents.find((a) => a.id === agent)?.instructions ?? "", input: text(owner?.input ?? run.input),
          output: text(owner?.output), action: data.action,
          tokensIn: data.usage.input_tokens ?? 0, tokensOut: data.usage.output_tokens ?? 0, cost: data.usage.cost_usd ?? 0,
        });
        ops.push({ at, k: "use", key: "turns", delta: 1 });
      }
    }
  } else {
    // List view: only the totals are known.
    const u = run.usage ?? { input_tokens: 0, output_tokens: 0, cost_usd: 0 };
    if (u.input_tokens || u.output_tokens || u.cost_usd) {
      turns.push({ id: `${run.id}-usage`, agent: run.agent_id, n: 1, at: durationMs, ms: 0, system: "", input: taskOf(run.input), output: "", tokensIn: u.input_tokens, tokensOut: u.output_tokens, cost: u.cost_usd });
    }
  }
  const code = run.error?.code;
  ops.sort((a, b) => a.at - b.at);
  return {
    id: run.id, agent: run.agent_id, task: taskOf(run.input),
    limits: def?.limits ?? NO_LIMITS, createdAt: start, durationMs, ops, calls, turns,
    output: run.state === "SUCCEEDED" ? run.output : undefined,
    invocations: invocations.map((i) => ({ id: i.id, agent: i.agent_id, parent: i.parent_id, depth: i.depth })),
    model: run.model_id,
    usage: { tokensIn: run.usage?.input_tokens ?? 0, tokensOut: run.usage?.output_tokens ?? 0, cost: run.usage?.cost_usd ?? 0 },
    final: run.state,
    error: code && knownErrors.has(code as ErrorCode) ? (code as ErrorCode) : undefined,
    errorDetail: run.error?.message,
    cancelledAt: run.state === "CANCELLED" ? start + durationMs : undefined,
  };
}

export async function loadRuns(agents: AgentDef[]): Promise<RunPlan[]> {
  const body = await agentApi.list();
  return (body.runs ?? []).map((run) => planFromRun(run, agents)).sort((a, b) => b.createdAt - a.createdAt);
}

/** One run with its events, or null when it does not exist. */
export async function loadRun(id: string, agents: AgentDef[]): Promise<RunPlan | null> {
  try {
    const [{ run }, { events }] = await Promise.all([agentApi.run(id), agentApi.events(id)]);
    return planFromRun(run, agents, events);
  } catch (err) {
    if (err instanceof AgentRequestError && err.status === 401) throw err;
    return null;
  }
}
