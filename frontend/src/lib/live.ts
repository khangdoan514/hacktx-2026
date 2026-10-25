import { agentById } from "@/config/agents";
import { agentApi, type AgentRun, type RunState } from "@/lib/api";
import type { Call, ErrorCode, Idea, Limits, Op, RunPlan, Turn } from "@/lib/types";

const knownErrors = new Set<ErrorCode>([
  "DELEGATION_DEPTH_EXCEEDED", "TOOL_NOT_ALLOWED", "BUDGET_EXCEEDED", "TIMEOUT", "OUTPUT_SCHEMA_INVALID",
]);

function limitsOf(agent: string): Limits {
  return agentById(agent)?.limits ?? { turns: 8, tools: 4, delegations: 2, parallel: 2, seconds: 60 };
}

function ideasOf(output: unknown): Idea[] {
  if (!output || typeof output !== "object") return [];
  const body = output as { ideas?: { title?: string; why?: string; score?: number; novelty?: number; source_ids?: string[] }[]; recommendation?: string; source_ids?: string[] };
  if (Array.isArray(body.ideas) && body.ideas.length) {
    return body.ideas.map((idea) => ({
      title: idea.title || "Result",
      why: idea.why || "",
      score: typeof idea.score === "number" ? idea.score : typeof idea.novelty === "number" ? idea.novelty : 0,
      sources: idea.source_ids ?? [],
    }));
  }
  if (body.recommendation) {
    return [{ title: body.recommendation, why: "Recommendation from the run.", score: 1, sources: body.source_ids ?? [] }];
  }
  return [];
}

function taskOf(input: unknown): string {
  if (input && typeof input === "object" && "task" in input && typeof (input as { task: unknown }).task === "string") {
    return (input as { task: string }).task;
  }
  return "Run";
}

/** A persisted API run, drawn with the console's existing timeline shape. */
export function planFromRun(run: AgentRun): RunPlan {
  const created = Date.parse(run.created_at);
  const updated = Date.parse(run.updated_at);
  const start = Number.isFinite(created) ? created : Date.now();
  const duration = Math.max(800, (Number.isFinite(updated) ? updated : start) - start);
  const agent = run.agent_id;
  const limits = limitsOf(agent);
  const ops: Op[] = [{ at: 0, k: "event", type: "RUN_STARTED", agent }];
  const calls: Call[] = [];
  const turns: Turn[] = [];
  const terminal = run.state === "SUCCEEDED" || run.state === "FAILED" || run.state === "CANCELLED";
  const span = terminal ? duration : Math.max(duration, 60_000);
  const invocations = run.invocations ?? [];
  invocations.forEach((invocation, index) => {
    const at = Math.round((span * (index + 1)) / (invocations.length + 2));
    ops.push({ at, k: "agent", agent: invocation.agent_id, st: "RUNNING", now: invocation.depth === 0 ? "Planning" : "Working" });
    ops.push({ at, k: "event", type: "AGENT_STARTED", agent: invocation.agent_id, detail: `depth ${invocation.depth}` });
    if (invocation.depth === 1) ops.push({ at, k: "use", key: "delegations", delta: 1 });
    const done = invocation.state === "PENDING" || invocation.state === "RUNNING" ? null : invocation.state;
    if (done) {
      const end = Math.min(span, at + 400);
      ops.push({ at: end, k: "agent", agent: invocation.agent_id, st: done as RunState, now: done === "SUCCEEDED" ? "Done" : done });
      ops.push({ at: end, k: "event", type: done === "SUCCEEDED" ? "AGENT_SUCCEEDED" : done === "CANCELLED" ? "RUN_CANCELLED" : "AGENT_FAILED", agent: invocation.agent_id, detail: invocation.error?.code });
    }
  });
  const reported = run.usage ?? { input_tokens: 0, output_tokens: 0, cost_usd: 0 };
  (run.tool_invocations ?? []).forEach((tool, index) => {
    const owner = invocations.find((invocation) => invocation.id === tool.agent_invocation_id)?.agent_id ?? agent;
    const at = Math.round(span * 0.45) + index * 200;
    calls.push({ id: tool.id, agent: owner, name: tool.tool_id, denied: Boolean(tool.error), kind: "tool" });
    ops.push({ at, k: "call", call: tool.id, st: tool.error ? "BLOCKED" : "SUCCEEDED", text: tool.error?.code ?? tool.tool_id });
    ops.push({ at, k: "event", type: tool.error ? "TOOL_FAILED" : "TOOL_SUCCEEDED", agent: owner, detail: tool.tool_id });
    ops.push({ at, k: "use", key: "tools", delta: 1 });
    turns.push({
      id: `${run.id}-t${index + 1}`, agent: owner, n: index + 1, at, ms: 300,
      system: "", input: taskOf(run.input), output: "",
      tokensIn: index === 0 ? reported.input_tokens : 0,
      tokensOut: index === 0 ? reported.output_tokens : 0,
      cost: index === 0 ? reported.cost_usd : 0,
    });
  });
  if (turns.length === 0 && (reported.input_tokens || reported.output_tokens || reported.cost_usd)) {
    turns.push({
      id: `${run.id}-usage`, agent, n: 1, at: span, ms: duration,
      system: "", input: taskOf(run.input), output: "",
      tokensIn: reported.input_tokens, tokensOut: reported.output_tokens, cost: reported.cost_usd,
    });
  }
  if (terminal) {
    const type = run.state === "SUCCEEDED" ? "RUN_SUCCEEDED" : run.state === "CANCELLED" ? "RUN_CANCELLED" : "RUN_FAILED";
    ops.push({ at: span, k: "event", type, agent, detail: run.error?.code });
  }
  const code = run.error?.code;
  return {
    id: run.id,
    agent,
    task: taskOf(run.input),
    limits,
    createdAt: start,
    durationMs: terminal ? span : span,
    ops,
    calls,
    turns,
    ideas: run.state === "SUCCEEDED" ? ideasOf(run.output) : [],
    final: terminal ? run.state : "RUNNING",
    error: code && knownErrors.has(code as ErrorCode) ? code as ErrorCode : undefined,
    errorDetail: run.error?.message,
    trigger: "API",
    cancelledAt: run.state === "CANCELLED" ? start + span : undefined,
  };
}

export async function loadBackendRuns(): Promise<RunPlan[] | null> {
  try {
    const body = await agentApi.list();
    return (body.runs ?? []).map(planFromRun).filter((plan) => agentById(plan.agent));
  } catch {
    return null;
  }
}

export async function loadBackendRun(id: string): Promise<RunPlan | null> {
  try {
    const body = await agentApi.run(id);
    return planFromRun(body.run);
  } catch {
    return null;
  }
}

export async function startBackendRun(agent: string, task: string, limits: Limits): Promise<string | null> {
  try {
    const started = await agentApi.start({
      agent_id: agent,
      agent_version: agentById(agent)?.version ?? "1.0.0",
      input: { task },
      limits: {
        max_delegations: limits.delegations,
        max_concurrent_agents: limits.parallel,
        max_tool_calls: limits.tools,
        max_model_turns: limits.turns,
        max_tokens: 12000,
        max_cost_usd: 2,
        timeout_seconds: limits.seconds,
      },
    });
    return started.run_id;
  } catch {
    return null;
  }
}
