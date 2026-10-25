// Replays a run's recorded timeline up to any moment, so live and finished
// runs render through the same code. All inputs come from the agent service.

import type { AgentView, CallView, EventView, Limits, RunPlan, RunView, State } from "./types";

/** Agents drawn in a run: the ones the service recorded invocations for. */
export function runAgents(plan: RunPlan): string[] {
  const seen: string[] = [];
  for (const i of [...plan.invocations].sort((a, b) => a.depth - b.depth)) if (!seen.includes(i.agent)) seen.push(i.agent);
  return seen.length ? seen : [plan.agent];
}

/** The run as it looks `now` (epoch ms). Pass Infinity for the finished state. */
export function materialize(plan: RunPlan, now: number): RunView {
  const raw = now - plan.createdAt;
  const live = plan.final === "RUNNING" || plan.final === "PENDING";
  const t = Math.max(0, Math.min(raw, plan.durationMs));
  const state: State = live ? plan.final : raw >= plan.durationMs ? plan.final : "RUNNING";

  const agents: Record<string, AgentView> = {};
  for (const id of runAgents(plan)) agents[id] = { st: "PENDING", now: "Waiting" };
  const calls: Record<string, CallView> = {};
  for (const c of plan.calls) calls[c.id] = { ...c, visible: false, st: "HIDDEN", text: "" };
  const used: Limits = { turns: 0, tools: 0, delegations: 0, parallel: 0, seconds: t / 1000 };
  const events: EventView[] = [];
  let running = 0;

  for (const o of plan.ops) {
    if (o.at > t) break;
    if (o.k === "agent") {
      const v = agents[o.agent] ?? (agents[o.agent] = { st: "PENDING", now: "" });
      if (o.st === "RUNNING" && v.start === undefined) { v.start = o.at; running++; }
      if (o.st !== "RUNNING" && v.start !== undefined && v.end === undefined) { v.end = o.at; running = Math.max(0, running - 1); }
      v.st = o.st;
      v.now = o.now;
    } else if (o.k === "call") {
      if (calls[o.call]) Object.assign(calls[o.call], { visible: true, st: o.st, text: o.text });
    } else if (o.k === "event") {
      events.push({ seq: events.length + 1, at: o.at, type: o.type, agent: o.agent, detail: o.detail });
    } else if (o.k === "use") {
      used[o.key] += o.delta;
    }
  }
  used.parallel = running;

  const turns = plan.turns.filter((x) => x.at <= t);
  return {
    plan, state, elapsed: live ? Math.max(0, raw) : t, agents, calls: Object.values(calls), events, turns, used,
    cost: turns.reduce((s, x) => s + x.cost, 0),
    tokens: turns.reduce((s, x) => s + x.tokensIn + x.tokensOut, 0),
  };
}
