// In-browser stand-in for the Go agent runtime. `buildPlan` turns an agent
// definition and a task into a timeline of operations that follow the v1
// contract rules (two-level delegation, tool allowlists, shared limits, typed
// errors). `materialize` replays that timeline up to any moment, so the same
// code renders live runs and finished history.

import { agentById } from "@/config/agents";
import { toolById } from "@/config/tools";
import { MAX_DELEGATION_DEPTH, models } from "@/config/workspace";
import type {
  AgentDef, AgentView, Call, CallView, EventView, Limits, Op, RunPlan, RunView, Scenario, State, Turn,
} from "./types";

function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** Agents drawn in a run's graph: the root and, for orchestrators, its delegates. */
export function runAgents(rootId: string): AgentDef[] {
  const root = agentById(rootId);
  if (!root) return [];
  const out: AgentDef[] = [];
  const walk = (a: AgentDef, depth: number) => {
    if (out.includes(a)) return;
    out.push(a);
    if (depth >= MAX_DELEGATION_DEPTH) return;
    for (const d of a.delegates) { const c = agentById(d); if (c) walk(c, depth + 1); }
  };
  walk(root, 0);
  return out;
}

export function buildPlan(opts: {
  id: string;
  seed: number;
  scenario: Scenario;
  limits?: Limits;
  createdAt: number;
  trigger?: string;
}): RunPlan {
  const { scenario } = opts;
  const r = rng(opts.seed);
  const root = agentById(scenario.agent)!;
  const ops: Op[] = [];
  const calls: Call[] = [];
  const turns: Turn[] = [];
  let callN = 0;

  const turn = (a: AgentDef, at: number, input: string, output: string) => {
    const tokensIn = Math.round(600 + r() * 2400);
    const tokensOut = Math.round(80 + r() * 520);
    const price = models[a.model] ?? { in: 0.003, out: 0.015 };
    turns.push({
      id: `${opts.id}-t${turns.length + 1}`, agent: a.id, n: turns.filter((t) => t.agent === a.id).length + 1, at,
      ms: Math.round(300 + r() * 900), system: a.instructions, input, output, tokensIn, tokensOut,
      cost: (tokensIn / 1000) * price.in + (tokensOut / 1000) * price.out,
    });
    ops.push({ at, k: "use", key: "turns", delta: 1 });
  };

  // One agent's own work: its scripted steps, then a final answer. Returns the finish time.
  const runAgent = (a: AgentDef, start: number, depth: number, input: string): number => {
    ops.push({ at: start, k: "agent", agent: a.id, st: "RUNNING", now: "Planning" });
    ops.push({ at: start, k: "event", type: "AGENT_STARTED", agent: a.id, detail: `depth ${depth}` });
    if (depth >= 1) ops.push({ at: start, k: "use", key: "delegations", delta: 1 });
    let t = start + 400;
    const steps = a.sim?.steps ?? a.tools.map((tool) => ({ tool, result: "ok", ms: 1200 }));
    for (const step of steps) {
      if ("tool" in step) {
        const tool = toolById(step.tool);
        const id = `${opts.id}-c${++callN}`;
        calls.push({ id, agent: a.id, name: step.tool, denied: false, kind: "tool" });
        turn(a, t, input, JSON.stringify({ action: "TOOL", tool: step.tool, input: { q: input.slice(0, 48) } }));
        t += 300;
        const ms = Math.round((step.ms ?? 1200) * (0.85 + r() * 0.3));
        ops.push({ at: t, k: "agent", agent: a.id, st: "RUNNING", now: `Calling ${tool?.name ?? step.tool}` });
        ops.push({ at: t, k: "call", call: id, st: "RUNNING", text: "running…" });
        ops.push({ at: t, k: "event", type: "TOOL_STARTED", agent: a.id, detail: step.tool });
        ops.push({ at: t, k: "use", key: "tools", delta: 1 });
        t += ms;
        ops.push({ at: t, k: "call", call: id, st: "SUCCEEDED", text: `${step.result} · ${(ms / 1000).toFixed(1)}s` });
        ops.push({ at: t, k: "event", type: "TOOL_SUCCEEDED", agent: a.id, detail: `${step.tool} · ${step.result}` });
        input = `Tool result from ${step.tool}: ${step.result}`;
        t += 200;
      } else if ("blocked" in step) {
        const id = `${opts.id}-c${++callN}`;
        const delegate = step.blocked === "delegate";
        calls.push({ id, agent: a.id, name: delegate ? `delegate → ${step.target}` : step.target, denied: true, kind: step.blocked });
        turn(a, t, input, JSON.stringify(delegate ? { action: "DELEGATE", target: step.target } : { action: "TOOL", tool: step.target }));
        t += 400;
        ops.push({ at: t, k: "call", call: id, st: "BLOCKED", text: delegate ? "blocked · depth limit" : "blocked · not allowed" });
        ops.push({
          at: t, k: "event", type: delegate ? "DELEGATION_REJECTED" : "TOOL_FAILED", agent: a.id,
          detail: delegate ? "DELEGATION_DEPTH_EXCEEDED" : `${step.target} · TOOL_NOT_ALLOWED`,
        });
        t += 300;
      } else if ("delegate" in step) {
        const child = agentById(step.delegate);
        if (child && depth + 1 <= MAX_DELEGATION_DEPTH) {
          ops.push({ at: t, k: "agent", agent: a.id, st: "RUNNING", now: `Delegating to ${child.name}` });
          turn(a, t, input, JSON.stringify({ action: "DELEGATE", target: child.id }));
          t += 300;
          t = runAgent(child, t, depth + 1, input) + 300;
        }
      } else {
        ops.push({ at: t, k: "agent", agent: a.id, st: "RUNNING", now: step.think });
        t += Math.round((step.ms ?? 1200) * (0.85 + r() * 0.3));
      }
    }
    turn(a, t, input, JSON.stringify({ action: "FINISH", output: a.sim?.result ?? "done" }));
    t += 400;
    ops.push({ at: t, k: "agent", agent: a.id, st: "SUCCEEDED", now: a.sim?.result ?? "Done" });
    ops.push({ at: t, k: "event", type: "AGENT_SUCCEEDED", agent: a.id });
    return t;
  };

  ops.push({ at: 0, k: "event", type: "RUN_STARTED", agent: root.id });
  let t: number;
  if (root.kind === "orchestrator" && root.delegates.length) {
    ops.push({ at: 300, k: "agent", agent: root.id, st: "RUNNING", now: "Planning" });
    ops.push({ at: 300, k: "event", type: "AGENT_STARTED", agent: root.id, detail: "depth 0" });
    turn(root, 300, `Task: ${scenario.task}`, JSON.stringify({ action: "DELEGATE", targets: root.plan?.flat() ?? root.delegates }));
    t = 1200;
    for (const group of root.plan ?? root.delegates.map((d) => [d])) {
      ops.push({ at: t, k: "agent", agent: root.id, st: "RUNNING", now: "Delegating" });
      ops.push({ at: t, k: "par", value: group.length });
      const ends = group.map((id, i) => runAgent(agentById(id)!, t + i * 150, 1, `Task: ${scenario.task}`));
      t = Math.max(...ends) + 300;
      ops.push({ at: t, k: "par", value: 0 });
      turn(root, t, `Results from ${group.join(", ")}`, JSON.stringify({ action: "CONTINUE" }));
      t += 300;
    }
    ops.push({ at: t, k: "agent", agent: root.id, st: "RUNNING", now: "Ranking" });
    t += 700;
    turn(root, t, "All specialist results", JSON.stringify({ action: "FINISH", output: root.sim?.result ?? "done" }));
    t += 300;
    ops.push({ at: t, k: "agent", agent: root.id, st: "SUCCEEDED", now: root.sim?.result ?? "Done" });
    ops.push({ at: t, k: "event", type: "AGENT_SUCCEEDED", agent: root.id });
  } else {
    t = runAgent(root, 300, 0, `Task: ${scenario.task}`);
  }
  t += 300;
  ops.push({ at: t, k: "event", type: "RUN_SUCCEEDED", agent: root.id });

  let final: State = "SUCCEEDED";
  let end = t;
  if (scenario.fail) {
    // Cut the timeline where the failure happens and fail whatever was still running.
    end = scenario.fail.atMs;
    const kept = ops.filter((o) => o.at < end);
    const running = new Set<string>();
    for (const o of [...kept].sort((a, b) => a.at - b.at)) {
      if (o.k === "agent") {
        if (o.st === "RUNNING") running.add(o.agent);
        else running.delete(o.agent);
      }
    }
    ops.length = 0;
    ops.push(...kept);
    for (const id of running) {
      ops.push({ at: end, k: "agent", agent: id, st: "FAILED", now: scenario.fail.code });
      ops.push({ at: end, k: "event", type: "AGENT_FAILED", agent: id, detail: scenario.fail.code });
    }
    ops.push({ at: end, k: "event", type: "RUN_FAILED", agent: root.id, detail: scenario.fail.code });
    final = "FAILED";
  }

  ops.sort((a, b) => a.at - b.at);
  return {
    id: opts.id, agent: root.id, task: scenario.task, limits: opts.limits ?? root.limits, createdAt: opts.createdAt,
    durationMs: end, ops, calls, turns: turns.filter((x) => x.at <= end), ideas: final === "SUCCEEDED" ? scenario.ideas : [],
    final, error: scenario.fail?.code, trigger: opts.trigger ?? "Manual",
  };
}

/** The run as it looks `now` (epoch ms). */
export function materialize(plan: RunPlan, now: number): RunView {
  const raw = now - plan.createdAt;
  const cancelAt = plan.cancelledAt !== undefined ? plan.cancelledAt - plan.createdAt : Infinity;
  const t = Math.max(0, Math.min(raw, cancelAt, plan.durationMs));
  const cancelled = cancelAt < plan.durationMs && raw >= cancelAt;
  const state: State = cancelled ? "CANCELLED" : raw >= plan.durationMs ? plan.final : "RUNNING";

  const agents: Record<string, AgentView> = {};
  for (const a of runAgents(plan.agent)) agents[a.id] = { st: "PENDING", now: "Waiting" };
  const calls: Record<string, CallView> = {};
  for (const c of plan.calls) calls[c.id] = { ...c, visible: false, st: "HIDDEN", text: "" };
  const used: Limits = { turns: 0, tools: 0, delegations: 0, parallel: 0, seconds: t / 1000 };
  const events: EventView[] = [];

  for (const o of plan.ops) {
    if (o.at > t) break;
    if (o.k === "agent") {
      const v = agents[o.agent] ?? (agents[o.agent] = { st: "PENDING", now: "" });
      if (o.st === "RUNNING" && v.start === undefined) v.start = o.at;
      if (o.st !== "RUNNING" && v.start !== undefined && v.end === undefined) v.end = o.at;
      v.st = o.st;
      v.now = o.now;
    } else if (o.k === "call") {
      Object.assign(calls[o.call], { visible: true, st: o.st, text: o.text });
    } else if (o.k === "event") {
      events.push({ seq: events.length + 1, at: o.at, type: o.type, agent: o.agent, detail: o.detail });
    } else if (o.k === "use") {
      used[o.key] += o.delta;
    } else {
      used.parallel = o.value;
    }
  }

  if (cancelled) {
    for (const v of Object.values(agents)) {
      if (v.st === "RUNNING") Object.assign(v, { st: "CANCELLED", now: "Cancelled", end: t });
    }
    used.parallel = 0;
    events.push({ seq: events.length + 1, at: t, type: "RUN_CANCELLED", agent: plan.agent });
  }

  const turns = plan.turns.filter((x) => x.at <= t);
  return {
    plan, state, elapsed: t, agents, calls: Object.values(calls), events, turns, used,
    cost: turns.reduce((s, x) => s + x.cost, 0),
    tokens: turns.reduce((s, x) => s + x.tokensIn + x.tokensOut, 0),
  };
}
