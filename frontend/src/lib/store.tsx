"use client";

// Client-side stand-in for the agent service: run history, the live run,
// workspace settings the UI can edit, and UI state (drawer, toasts).
// Real runs are loaded from /v1/agent-runs when this API knows the agent.
// Otherwise the in-browser simulator keeps the console usable.

import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { clearToken } from "@/product/api";
import { agentById } from "@/config/agents";
import { budgets, guardrails, scenarios, triggers } from "@/config/workspace";
import { AgentRequestError, agentApi } from "./api";
import { loadBackendRun, loadBackendRuns, startBackendRun } from "./live";
import { buildPlan, materialize } from "./simulate";
import type { Limits, RunPlan, RunView } from "./types";

type DrawerState = { agent: string; run?: string; tab?: "spec" | "activity" | "prompts" } | null;
type Toast = { id: number; msg: string };

type Store = {
  history: RunPlan[];
  live: RunPlan | null;
  plan: (id: string) => RunPlan | undefined;
  startRun: (o: { agent: string; task: string; limits?: Limits; trigger?: string }) => string;
  cancelLive: () => void;
  drawer: DrawerState;
  openAgent: (agent: string, o?: { run?: string; tab?: "spec" | "activity" | "prompts" }) => void;
  closeAgent: () => void;
  toasts: Toast[];
  toast: (msg: string) => void;
  guardrailsOn: Record<string, boolean>;
  setGuardrail: (id: string, on: boolean) => void;
  caps: Record<string, number>;
  setCap: (agent: string, usd: number) => void;
  triggersOn: Record<string, boolean>;
  setTrigger: (id: string, on: boolean) => void;
};

const StoreCtx = createContext<Store | null>(null);
const NowCtx = createContext(0);

const TRIGGERS = ["API", "Schedule", "Slack", "API", "Webhook", "Manual"];
let runCounter = 0x8f2c41;
const nextId = () => "run_" + (++runCounter).toString(16);

function seedHistory(now: number): RunPlan[] {
  const out: RunPlan[] = [];
  let at = now - 4 * 60_000;
  for (let i = 0; i < 18; i++) {
    const scenario = scenarios[(i * 3 + 1) % scenarios.length];
    out.push(buildPlan({
      id: "run_" + (0x8f2c40 - i * 0x91).toString(16), seed: 900 + i, scenario, createdAt: at,
      trigger: TRIGGERS[i % TRIGGERS.length],
    }));
    at -= (25 + ((i * 37) % 50)) * 60_000;
  }
  return out;
}

const liveScenarios = scenarios.filter((s) => !s.fail);

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const [ready, setReady] = useState(false);
  const [now, setNow] = useState(0);
  const [history, setHistory] = useState<RunPlan[]>([]);
  const [live, setLive] = useState<RunPlan | null>(null);
  const [drawer, setDrawer] = useState<DrawerState>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [guardrailsOn, setGuardrailsOn] = useState(() => Object.fromEntries(guardrails.map((g) => [g.id, g.enabled])));
  const [caps, setCaps] = useState<Record<string, number>>(budgets.perAgentDailyUsd);
  const [triggersOn, setTriggersOn] = useState(() => Object.fromEntries(triggers.map((t) => [t.id, t.enabled])));
  const archived = useRef<string | null>(null);
  const cycle = useRef(0);
  const liveRef = useRef<RunPlan | null>(null);
  const backendIds = useRef(new Set<string>());
  liveRef.current = live;

  const toast = useCallback((msg: string) => {
    const id = Date.now() + Math.random();
    setToasts((t) => [...t, { id, msg }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 2600);
  }, []);

  const startRun = useCallback<Store["startRun"]>(({ agent, task, limits, trigger }) => {
    const scenario = scenarios.find((s) => s.task === task && s.agent === agent)
      ?? { ...(liveScenarios.find((s) => s.agent === agent) ?? liveScenarios[0]), agent, task };
    // A run still in flight when a new one starts is filed as cancelled.
    const prev = liveRef.current;
    if (prev && archived.current !== prev.id) {
      const t = Date.now();
      setHistory((h) => [t >= prev.createdAt + prev.durationMs ? prev : { ...prev, cancelledAt: prev.cancelledAt ?? t }, ...h]);
    }
    const id = nextId();
    const local = buildPlan({ id, seed: runCounter, scenario, limits, createdAt: Date.now(), trigger });
    setLive(local);
    archived.current = null;
    void startBackendRun(agent, task, limits ?? local.limits).then(async (remoteId) => {
      if (!remoteId) return;
      backendIds.current.add(remoteId);
      const plan = await loadBackendRun(remoteId);
      if (!plan) return;
      setLive(plan);
      if (liveRef.current?.id === id) archived.current = null;
    });
    return id;
  }, []);

  // Seed history and the first live run on the client only, so server and client markup match.
  useEffect(() => {
    const t = Date.now();
    let stop = false;
    void loadBackendRuns().then((remote) => {
      if (stop) return;
      if (remote && remote.length) {
        remote.forEach((plan) => backendIds.current.add(plan.id));
        const featured = remote.find((plan) => plan.final === "SUCCEEDED") ?? remote[0];
        setHistory(remote.filter((plan) => plan.id !== featured.id));
        setLive(featured);
      } else {
        setHistory(seedHistory(t));
        setLive(buildPlan({ id: nextId(), seed: runCounter, scenario: liveScenarios[0], createdAt: t, trigger: "API" }));
      }
      setNow(t);
      setReady(true);
    }).catch((err: unknown) => {
      if (stop) return;
      if (err instanceof AgentRequestError && err.status === 401) {
        clearToken();
        router.replace(`/sign-in?next=${encodeURIComponent(pathname)}`);
        return;
      } else {
        setHistory(seedHistory(t));
        setLive(buildPlan({ id: nextId(), seed: runCounter, scenario: liveScenarios[0], createdAt: t, trigger: "API" }));
      }
      setNow(t);
      setReady(true);
    });
    const iv = setInterval(() => setNow(Date.now()), 100);
    return () => {
      stop = true;
      clearInterval(iv);
    };
  }, []);

  // When the live run ends, file it in history and start the next demo run a little later.
  useEffect(() => {
    if (!live || archived.current === live.id) return;
    if (backendIds.current.has(live.id) && live.final !== "RUNNING" && live.final !== "PENDING") {
      archived.current = live.id;
      return;
    }
    const end = Math.min(live.createdAt + live.durationMs, live.cancelledAt ?? Infinity);
    if (now < end) return;
    archived.current = live.id;
    setHistory((h) => [live, ...h]);
    if (live.cancelledAt === undefined) toast(live.final === "SUCCEEDED" ? "Run succeeded" : "Run failed");
    const id = live.id;
    if (backendIds.current.has(id)) return;
    setTimeout(() => {
      if (liveRef.current?.id !== id || backendIds.current.has(id)) return;
      cycle.current = (cycle.current + 1) % liveScenarios.length;
      archived.current = null;
      setLive(buildPlan({ id: nextId(), seed: runCounter, scenario: liveScenarios[cycle.current], createdAt: Date.now(), trigger: "Schedule" }));
    }, 12_000);
  }, [now, live, toast]);

  const cancelLive = useCallback(() => {
    const current = liveRef.current;
    if (current && backendIds.current.has(current.id)) void agentApi.cancel(current.id).catch(() => undefined);
    setLive((l) => (l && l.cancelledAt === undefined ? { ...l, cancelledAt: Date.now() } : l));
    toast("Run cancelled");
  }, [toast]);

  const plan = useCallback((id: string) => (live?.id === id ? live : history.find((h) => h.id === id)), [live, history]);

  useEffect(() => {
    if (!live || !backendIds.current.has(live.id)) return;
    if (live.final !== "RUNNING" && live.final !== "PENDING") return;
    const id = live.id;
    const iv = window.setInterval(() => {
      void loadBackendRun(id).then((next) => {
        if (next) setLive(next);
      });
    }, 1000);
    return () => window.clearInterval(iv);
  }, [live]);

  const value = useMemo<Store>(() => ({
    history, live, plan, startRun, cancelLive, drawer,
    openAgent: (agent, o) => { if (agentById(agent)) setDrawer({ agent, ...o }); },
    closeAgent: () => setDrawer(null),
    toasts, toast,
    guardrailsOn, setGuardrail: (id, on) => setGuardrailsOn((g) => ({ ...g, [id]: on })),
    caps, setCap: (agent, v) => setCaps((c) => ({ ...c, [agent]: v })),
    triggersOn, setTrigger: (id, on) => setTriggersOn((t) => ({ ...t, [id]: on })),
  }), [history, live, plan, startRun, cancelLive, drawer, toasts, toast, guardrailsOn, caps, triggersOn]);

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

/** A run as it looks right now. Finished runs are materialized at their end. */
export function useRunView(id?: string): RunView | null {
  const { plan, live } = useStore();
  const now = useNow();
  const p = id ? plan(id) : live ?? undefined;
  const isLive = !!p && p.id === live?.id;
  return useMemo(() => (p ? materialize(p, isLive ? now : Infinity) : null), [p, isLive, isLive ? now : 0]); // eslint-disable-line react-hooks/exhaustive-deps
}

/** Every run, newest first, materialized at its end (the live one at now). */
export function useAllRuns(): RunView[] {
  const { history, live } = useStore();
  const now = useNow();
  const done = useMemo(() => history.map((p) => materialize(p, Infinity)), [history]);
  const liveView = live && !history.some((h) => h.id === live.id) ? materialize(live, now) : null;
  return liveView ? [liveView, ...done] : done;
}
