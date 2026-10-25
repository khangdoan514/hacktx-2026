"use client";

// Console state backed by the agent service: the agent roster, the run list
// (polled), and UI state (drawer, toasts). Nothing here is generated locally.

import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { endSession } from "@/product/supabase";
import { AgentRequestError, agentApi } from "./api";
import { limitsToWire, loadAgents } from "./agents";
import { isTerminal, loadRun, loadRuns } from "./live";
import { materialize } from "./replay";
import type { AgentDef, Limits, RunPlan, RunView } from "./types";

type DrawerState = { agent: string; run?: string; tab?: "spec" | "activity" | "prompts" } | null;
type Toast = { id: number; msg: string };

type Store = {
  agents: AgentDef[];
  agentById: (id: string) => AgentDef | undefined;
  /** Every run, newest first. */
  runs: RunPlan[];
  /** The run in flight, else the latest one. */
  live: RunPlan | null;
  plan: (id: string) => RunPlan | undefined;
  /** Starts a run on the service and resolves with its id. Rejects with the service's message. */
  startRun: (o: { agent: string; task: string; period?: string; limits?: Limits }) => Promise<string>;
  cancelRun: (id: string) => Promise<void>;
  refresh: () => Promise<void>;
  drawer: DrawerState;
  openAgent: (agent: string, o?: { run?: string; tab?: "spec" | "activity" | "prompts" }) => void;
  closeAgent: () => void;
  toasts: Toast[];
  toast: (msg: string) => void;
};

const StoreCtx = createContext<Store | null>(null);
const NowCtx = createContext(0);
const POLL_MS = 2000;

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [ready, setReady] = useState(false);
  const [now, setNow] = useState(0);
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [runs, setRuns] = useState<RunPlan[]>([]);
  const [drawer, setDrawer] = useState<DrawerState>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const agentsRef = useRef<AgentDef[]>([]);
  agentsRef.current = agents;

  const toast = useCallback((msg: string) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 2600);
  }, []);

  const expire = useCallback(() => {
    void endSession().then(() => router.replace(`/sign-in?next=${encodeURIComponent(pathname)}`));
  }, [router, pathname]);

  const refresh = useCallback(async () => {
    try {
      setRuns(await loadRuns(agentsRef.current));
    } catch (err) {
      if (err instanceof AgentRequestError && err.status === 401) expire();
    }
  }, [expire]);

  useEffect(() => {
    let stop = false;
    void (async () => {
      try {
        const defs = await loadAgents();
        if (stop) return;
        agentsRef.current = defs;
        setAgents(defs);
      } catch (err) {
        if (err instanceof AgentRequestError && err.status === 401) { expire(); return; }
      }
      await refresh();
      if (stop) return;
      setNow(Date.now());
      setReady(true);
    })();
    const poll = setInterval(() => { void refresh(); }, POLL_MS);
    const tick = setInterval(() => setNow(Date.now()), 100);
    return () => { stop = true; clearInterval(poll); clearInterval(tick); };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const startRun = useCallback<Store["startRun"]>(async ({ agent, task, period, limits }) => {
    const def = agentsRef.current.find((a) => a.id === agent);
    if (!def) throw new Error(`Unknown agent ${agent}`);
    const input = period ? { task, period } : { task };
    const started = await agentApi.start({
      agent_id: def.id, agent_version: def.version, input,
      limits: limitsToWire(limits ?? def.limits, { max_tokens: def.maxTokens || undefined, max_cost_usd: def.maxCostUsd || undefined }),
    });
    await refresh();
    return started.run_id;
  }, [refresh]);

  const cancelRun = useCallback(async (id: string) => {
    await agentApi.cancel(id);
    toast("Cancel requested");
    await refresh();
  }, [refresh, toast]);

  const live = useMemo(() => runs.find((r) => !isTerminal(r.final)) ?? runs[0] ?? null, [runs]);
  const plan = useCallback((id: string) => runs.find((r) => r.id === id), [runs]);
  const agentById = useCallback((id: string) => agents.find((a) => a.id === id), [agents]);

  const value = useMemo<Store>(() => ({
    agents, agentById, runs, live, plan, startRun, cancelRun, refresh, drawer,
    openAgent: (agent, o) => { setDrawer({ agent, ...o }); },
    closeAgent: () => setDrawer(null),
    toasts, toast,
  }), [agents, agentById, runs, live, plan, startRun, cancelRun, refresh, drawer, toasts, toast]);

  if (!ready) return null;
  return (
    <StoreCtx.Provider value={value}>
      <NowCtx.Provider value={now}>{children}</NowCtx.Provider>
    </StoreCtx.Provider>
  );
}

export function useStore() {
  const s = useContext(StoreCtx);
  if (!s) throw new Error("useStore must be used inside StoreProvider");
  return s;
}

export const useNow = () => useContext(NowCtx);

/**
 * One run with its events, refetched every second while it is in flight.
 * `undefined` while loading, `null` when the service has no such run.
 */
export function useRunPlan(id?: string): RunPlan | null | undefined {
  const { agents, runs } = useStore();
  const [detail, setDetail] = useState<{ id: string; plan: RunPlan | null } | undefined>();
  const target = id ?? runs.find((r) => !isTerminal(r.final))?.id ?? runs[0]?.id;
  const terminal = useRef(false);

  useEffect(() => {
    if (!target) return;
    let stop = false;
    terminal.current = false;
    const load = async () => {
      try {
        const next = await loadRun(target, agents);
        if (stop) return;
        setDetail({ id: target, plan: next });
        terminal.current = !next || isTerminal(next.final);
      } catch { /* keep the last detail; the list poll handles sign-out */ }
    };
    void load();
    const iv = setInterval(() => { if (!terminal.current) void load(); }, 1000);
    return () => { stop = true; clearInterval(iv); };
  }, [target, agents]);

  if (!target) return null;
  return detail?.id === target ? detail.plan : undefined;
}

/** A run as it looks right now. Finished runs are materialized at their end. */
export function useRunView(id?: string): RunView | null | undefined {
  const plan = useRunPlan(id);
  const now = useNow();
  const live = !!plan && !isTerminal(plan.final);
  return useMemo(() => (plan ? materialize(plan, live ? now : Infinity) : plan), [plan, live, live ? now : 0]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** Every run, newest first; running ones at now, finished ones at their end. */
export function useAllRuns(): RunView[] {
  const { runs } = useStore();
  const now = useNow();
  return useMemo(() => runs.map((p) => materialize(p, isTerminal(p.final) ? Infinity : now)), [runs, now]);
}

/** Every run with its events, so per-agent turns and usage are known. Finished runs are fetched once. */
export function useRunDetails(): RunPlan[] {
  const { agents, runs } = useStore();
  const [cache, setCache] = useState<Record<string, RunPlan>>({});
  const stamp = runs.map((r) => `${r.id}:${r.final}`).join(",");

  useEffect(() => {
    let stop = false;
    void Promise.all(runs.map(async (r) => {
      const have = cache[r.id];
      if (have && isTerminal(have.final) && isTerminal(r.final)) return null;
      return loadRun(r.id, agents).catch(() => null);
    })).then((loaded) => {
      if (stop) return;
      const next = loaded.filter((p): p is RunPlan => !!p);
      if (next.length) setCache((c) => ({ ...c, ...Object.fromEntries(next.map((p) => [p.id, p])) }));
    });
    return () => { stop = true; };
  }, [stamp, agents]); // eslint-disable-line react-hooks/exhaustive-deps

  return useMemo(() => runs.map((r) => cache[r.id] ?? r), [runs, cache]);
}
