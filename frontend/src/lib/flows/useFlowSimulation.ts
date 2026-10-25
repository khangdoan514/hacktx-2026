"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { agentApi, AgentRequestError, type AgentRun, type RunEvent } from "@/lib/api";
import { FlowSim, STAGES, TICK_MS, type Decision, type Lane, type FlowSnapshot } from "./engine";
import { SCENARIOS } from "./scenarios";

export type FlowSource = "sim" | "live";

export interface FlowState extends FlowSnapshot {
  source: FlowSource;
  runs: AgentRun[];
  /** Backend runs that are not finished. */
  active: number;
  refresh: () => Promise<void>;
  approve: (code?: string) => void;
  anotherRound: (code?: string) => void;
}

const LIVE_POLL_MS = 4000;
const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

const EVENT_TEXT: Record<string, [string, string]> = {
  RUN_STARTED: ["Started the flow", "Start"],
  AGENT_STARTED: ["Started an agent", "Delegate"],
  AGENT_SUCCEEDED: ["Agent finished its part", "Done"],
  AGENT_FAILED: ["Agent stopped with an error", "Failed"],
  TOOL_SUCCEEDED: ["Used a tool", "Tool"],
  TOOL_FAILED: ["A tool call was refused or failed", "Blocked"],
  RUN_SUCCEEDED: ["Delivered the result", "Deliver"],
  RUN_FAILED: ["The flow failed", "Failed"],
  RUN_CANCELLED: ["The flow was cancelled", "Cancelled"],
};

/** Real run events as timeline rows. Noisy model events are left out. */
export function decisionsFromEvents(run: AgentRun, laneIndex: number, events: RunEvent[]): Decision[] {
  const code = `R-${run.id.replace(/^run-/, "").slice(0, 4)}`;
  return events.filter((e) => EVENT_TEXT[e.type]).map((e) => {
    const [what, outcome] = EVENT_TEXT[e.type];
    const data = (e.data ?? {}) as { agent_id?: string; tool_id?: string; code?: string };
    const agent = data.agent_id ?? run.invocations?.find((i) => i.id === e.invocation_id)?.agent_id ?? run.agent_id;
    const detail = data.tool_id ?? data.code;
    return {
      id: Date.parse(e.timestamp) + e.sequence / 1000, at: Date.parse(e.timestamp) || Date.now(),
      code, color: (laneIndex % 3) + 1, agent, what: detail ? `${what}: ${detail}` : what, outcome,
    };
  });
}

/** Draws a persisted run on the same lane shape as the simulation. */
export function laneFromRun(run: AgentRun, i: number): Lane {
  const terminal = TERMINAL.has(run.state);
  const progress = (run.invocations?.length ?? 0) + (run.tool_invocations?.length ?? 0);
  const stage = terminal ? STAGES.length - 1 : Math.min(STAGES.length - 2, Math.max(1, 1 + progress));
  const failed = run.state === "FAILED" || run.state === "CANCELLED";
  const states = STAGES.map((_, k) => (k < stage ? "done" : k === stage ? (terminal && !failed ? "done" : "run") : "idle")) as Lane["states"];
  const created = Date.parse(run.created_at);
  const task = run.input && typeof run.input === "object" && "task" in run.input ? String((run.input as { task: unknown }).task) : run.agent_id;
  return {
    i, color: (i % 3) + 1, code: `R-${run.id.replace(/^run-/, "").slice(0, 4)}`,
    s: { ...SCENARIOS[0], title: task, short: task.slice(0, 24), client: run.agent_id },
    states, mains: STAGES.map((_, k) => (k === stage ? run.state.toLowerCase() : "")), subs: STAGES.map(() => ""),
    stage, t: Number.isFinite(created) ? created : Date.now(), dur: 1, p: terminal ? 1 : 0.5,
    pivoted: false, escalated: false, waiting: false, done: terminal,
    ideaStatus: null, loopUntil: 0, autoApproveAt: 0, recycleAt: 0,
  };
}

/**
 * Flow state for every overview view. It polls the agent service; while the
 * service returns runs those drive the lanes, and the scripted simulation only
 * runs when the backend is empty or unreachable, so fake flows never mix with
 * real ones.
 */
export function useFlowSimulation(onToast?: (message: string) => void): FlowState {
  const toastRef = useRef(onToast);
  toastRef.current = onToast;
  const simRef = useRef<FlowSim | null>(null);
  if (!simRef.current) simRef.current = new FlowSim(Date.now(), (m) => toastRef.current?.(m));
  const sim = simRef.current;

  const [snapshot, setSnapshot] = useState<FlowSnapshot>(() => sim.snapshot());
  const [runs, setRuns] = useState<AgentRun[]>([]);
  const [liveDecisions, setLiveDecisions] = useState<Decision[]>([]);
  const liveRef = useRef(false);

  const poll = useCallback(async () => {
    try {
      const body = await agentApi.list();
      const list = body.runs ?? [];
      liveRef.current = list.length > 0;
      setRuns(list);
      const feeds = await Promise.all(list.slice(0, 3).map(async (run, i) => {
        try { return decisionsFromEvents(run, i, (await agentApi.events(run.id)).events); } catch { return []; }
      }));
      setLiveDecisions(feeds.flat().sort((a, b) => b.at - a.at).slice(0, 60));
    } catch (err) {
      if (err instanceof AgentRequestError && err.status === 401) return;
      liveRef.current = false;
      setRuns([]);
    }
  }, []);

  useEffect(() => {
    const first = window.setTimeout(() => { void poll(); }, 0);
    const id = window.setInterval(poll, LIVE_POLL_MS);
    return () => { window.clearTimeout(first); window.clearInterval(id); };
  }, [poll]);

  useEffect(() => {
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const id = window.setInterval(() => {
      if (liveRef.current) return;
      sim.tick(Date.now());
      setSnapshot(sim.snapshot());
    }, reduced ? 500 : TICK_MS);
    return () => window.clearInterval(id);
  }, [sim]);

  const approve = useCallback((code?: string) => { sim.approve(code); setSnapshot(sim.snapshot()); }, [sim]);
  const anotherRound = useCallback((code?: string) => { sim.anotherRound(code); setSnapshot(sim.snapshot()); }, [sim]);

  const live = runs.length > 0;
  const lanes = live ? runs.slice(0, 3).map(laneFromRun) : snapshot.lanes;
  const decisions: Decision[] = live ? liveDecisions : snapshot.decisions;
  return {
    source: live ? "live" : "sim",
    runs,
    active: runs.filter((r) => !TERMINAL.has(r.state)).length,
    lanes,
    decisions,
    waiting: live ? 0 : snapshot.waiting,
    delivered: live ? runs.filter((r) => r.state === "SUCCEEDED").length : snapshot.delivered,
    refresh: poll,
    approve,
    anotherRound,
  };
}
