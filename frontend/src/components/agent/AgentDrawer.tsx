"use client";

import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import Icon from "@/components/ui/Icon";
import { stateColor } from "@/components/ui/StatePill";
import { compact, secs, usd } from "@/lib/format";
import { useAllRuns, useRunView, useStore } from "@/lib/store";
import type { AgentDef } from "@/lib/types";
import s from "./AgentDrawer.module.css";

type Tab = "spec" | "activity" | "prompts";

/** Everything about one agent: its definition, what it has done, and what it saw in a run. */
export default function AgentDrawer() {
  const { drawer, closeAgent, openAgent, agentById } = useStore();
  const ref = useRef<HTMLDialogElement>(null);
  const [tab, setTab] = useState<Tab>("spec");
  const def = drawer ? agentById(drawer.agent) : undefined;

  // Shareable links: ?agent=vendor-orchestrator&tab=activity opens the drawer on load.
  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    const a = q.get("agent"), t = q.get("tab") as Tab | null;
    if (a) openAgent(a, { tab: t && ["spec", "activity", "prompts"].includes(t) ? t : "spec" });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (drawer && !d.open) d.showModal();
    if (!drawer && d.open) d.close();
    if (drawer) setTab(drawer.tab ?? "spec");
  }, [drawer]);

  return (
    <dialog ref={ref} className={s.drawer} onClose={closeAgent} onClick={(e) => e.target === ref.current && closeAgent()}>
      {def && (
        <>
          <div className={s.head}>
            <span className={s.ico}><Icon name={def.icon} size={20} /></span>
            <div style={{ minWidth: 0 }}>
              <h3>{def.name}</h3>
              <span className={s.id}>{def.id}@{def.version} · {def.kind} · depth {def.kind === "orchestrator" ? 0 : 1}</span>
            </div>
            <button type="button" className="ibtn" onClick={closeAgent} aria-label="Close"><Icon name="x" /></button>
          </div>
          <div className={s.tabs} role="tablist">
            {(["spec", "activity", "prompts"] as Tab[]).map((t) => (
              <button key={t} type="button" role="tab" aria-selected={tab === t} onClick={() => setTab(t)}>
                {t === "spec" ? "Spec" : t === "activity" ? "Activity" : "Prompts"}
              </button>
            ))}
          </div>
          <div className={s.body}>
            {tab === "spec" && <Spec def={def} />}
            {tab === "activity" && <Activity def={def} />}
            {tab === "prompts" && <Prompts def={def} runId={drawer?.run} />}
          </div>
        </>
      )}
    </dialog>
  );
}

function Spec({ def }: { def: AgentDef }) {
  const { openAgent, agents, agentById } = useStore();
  const callers = agents.filter((a) => a.delegates.includes(def.id));
  return (
    <>
      <Sec title="Role"><p>{def.role}</p></Sec>
      <Sec title="Instructions"><p>{def.instructions}</p></Sec>
      <Sec title="Tools">
        <div className={s.chips}>
          {def.tools.length ? def.tools.map((t) => <span key={t} className="chip">{t}</span>) : <span className="chip none">none</span>}
        </div>
      </Sec>
      <Sec title="Delegates">
        <div className={s.chips}>
          {def.delegates.length ? def.delegates.map((d) => {
            const a = agentById(d);
            return a && <button key={d} type="button" className="chip" onClick={() => openAgent(d)}><Icon name={a.icon} size={13} />{d}</button>;
          }) : <span className="chip none">none</span>}
        </div>
        {callers.length > 0 && <p className={s.note}>Called by {callers.map((c) => c.name).join(", ")}</p>}
      </Sec>
      <Sec title="Schema"><pre className={s.code}>{pretty(def.input)}{"\n→ "}{pretty(def.output)}</pre></Sec>
      <Sec title="Limits">
        <div className={s.kvs}>
          <div><small>Model turns</small><b>{def.limits.turns}</b></div>
          <div><small>Tool calls</small><b>{def.limits.tools}</b></div>
          <div><small>Timeout</small><b>{def.limits.seconds}s</b></div>
          <div><small>Max cost</small><b>{def.maxCostUsd ? usd(def.maxCostUsd) : "—"}</b></div>
        </div>
      </Sec>
      <Sec title="Configure">
        <p className={s.note}>Definitions are edited and published from <Link href="/definitions" className="mono">Definitions</Link>. This is version {def.version} ({def.state}).</p>
      </Sec>
    </>
  );
}

const pretty = (json: string) => { try { return JSON.stringify(JSON.parse(json), null, 2); } catch { return json || "—"; } };

/** Runs where this agent was invoked, as root or delegate. */
function useInvocations(id: string) {
  const runs = useAllRuns();
  return useMemo(() => runs.filter((r) => r.agents[id]?.start !== undefined).map((r) => {
    const v = r.agents[id];
    const turns = r.turns.filter((t) => t.agent === id);
    return {
      run: r, st: v.st, result: v.now,
      ms: (v.end ?? r.elapsed) - (v.start ?? 0),
      tools: r.calls.filter((c) => c.agent === id && c.visible && !c.denied),
      blocked: r.calls.filter((c) => c.agent === id && c.visible && c.denied).length,
      tokens: turns.reduce((a, t) => a + t.tokensIn + t.tokensOut, 0),
      cost: turns.reduce((a, t) => a + t.cost, 0),
    };
  }), [runs, id]);
}

function Activity({ def }: { def: AgentDef }) {
  const inv = useInvocations(def.id);
  const done = inv.filter((i) => i.st !== "RUNNING");
  const ok = done.filter((i) => i.st === "SUCCEEDED").length;
  const toolCounts = new Map<string, number>();
  inv.forEach((i) => i.tools.forEach((c) => toolCounts.set(c.name, (toolCounts.get(c.name) ?? 0) + 1)));
  const maxTool = Math.max(1, ...toolCounts.values());

  return (
    <>
      <div className={s.stats}>
        <div><small>Runs</small><b>{inv.length}</b></div>
        <div><small>Succeeded</small><b>{done.length ? Math.round((ok / done.length) * 100) : 0}%</b></div>
        <div><small>Avg time</small><b>{done.length ? secs(done.reduce((a, i) => a + i.ms, 0) / done.length) : "—"}</b></div>
        <div><small>Spend</small><b>{usd(inv.reduce((a, i) => a + i.cost, 0))}</b></div>
      </div>
      {toolCounts.size > 0 && (
        <Sec title="Tools used">
          <div className={s.usage}>
            {[...toolCounts].map(([t, n]) => (
              <div key={t}><span className="mono">{t}</span><div className="bar"><i style={{ width: `${(n / maxTool) * 100}%` }} /></div><b className="mono">{n}</b></div>
            ))}
          </div>
        </Sec>
      )}
      <Sec title={`History · ${inv.reduce((a, i) => a + i.blocked, 0)} blocked actions`}>
        <ul className={s.hist}>
          {inv.slice(0, 12).map((i) => (
            <li key={i.run.plan.id}>
              <Link href={`/runs/${i.run.plan.id}`}>
                <i className={`dot ${stateColor(i.st)} ${i.st === "RUNNING" ? "live" : ""}`} />
                <div style={{ minWidth: 0 }}>
                  <b>{i.result}</b>
                  <small>{i.run.plan.id} · {i.run.plan.task}</small>
                </div>
                <span className="mono muted">
                  {i.blocked > 0 && <span className="tag err" style={{ marginRight: 6 }}>{i.blocked} blocked</span>}
                  {i.st === "RUNNING" ? "…" : secs(i.ms)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </Sec>
    </>
  );
}

function Prompts({ def, runId }: { def: AgentDef; runId?: string }) {
  // Default to the run the drawer was opened from, else this agent's latest run.
  const inv = useInvocations(def.id);
  const id = runId && inv.some((i) => i.run.plan.id === runId) ? runId : inv[0]?.run.plan.id;
  const view = useRunView(id);
  if (!id || !view) return <p className={s.note} style={{ marginTop: 12 }}>{id ? "Loading…" : "No runs yet."}</p>;
  const turns = view.turns.filter((t) => t.agent === def.id);
  return (
    <>
      <p className={s.note} style={{ marginTop: 12 }}>
        Run <Link href={`/runs/${view.plan.id}`} className="mono">{view.plan.id}</Link> · {turns.length} model turns
      </p>
      {turns.length === 0 && <p className={s.note}>This agent has not run in this run yet.</p>}
      <div className={s.turns}>
        {turns.map((t) => (
          <details key={t.id} className={s.turn}>
            <summary>
              <span className="mono">#{t.n}</span>
              <span className={s.action}>{t.action ?? "—"}</span>
              <span className="mono muted">{compact(t.tokensIn)}→{compact(t.tokensOut)} · {usd(t.cost)}</span>
            </summary>
            <Field k="system" v={t.system} />
            <Field k="input" v={t.input} />
            <Field k="output" v={t.output} />
          </details>
        ))}
      </div>
    </>
  );
}

function Field({ k, v }: { k: string; v: string }) {
  return <div className={s.field}><small>{k}</small><pre>{v}</pre></div>;
}

function Sec({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className={s.sec}><h4>{title}</h4>{children}</div>;
}
