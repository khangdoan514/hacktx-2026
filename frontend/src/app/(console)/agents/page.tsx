"use client";

import { agents, agentById } from "@/config/agents";
import { toolById } from "@/config/tools";
import { models } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import Link from "next/link";
import Icon from "@/components/ui/Icon";
import { secs } from "@/lib/format";
import { useAllRuns, useStore } from "@/lib/store";
import type { AgentDef, RunView } from "@/lib/types";
import s from "./agents.module.css";

function stats(id: string, runs: RunView[]) {
  const inv = runs.filter((r) => r.agents[id]?.start !== undefined);
  const done = inv.filter((r) => r.agents[id].st !== "RUNNING");
  const ok = done.filter((r) => r.agents[id].st === "SUCCEEDED").length;
  const avg = done.length ? done.reduce((a, r) => a + ((r.agents[id].end ?? 0) - (r.agents[id].start ?? 0)), 0) / done.length : 0;
  const running = inv.some((r) => r.agents[id].st === "RUNNING");
  return { n: inv.length, rate: done.length ? Math.round((ok / done.length) * 100) : 0, avg, running };
}

export default function AgentsPage() {
  const runs = useAllRuns();
  // Agents that run other agents come first; the rest follow in config order.
  const sorted = [...agents].sort((x, y) => Number(y.kind === "orchestrator") - Number(x.kind === "orchestrator"));

  return (
    <>
      <PageHeader
        title="Agents"
        sub="Click an agent for its details and history."
        actions={<Link href="/agents/new" className="btn solid"><Icon name="plus" size={15} />New agent</Link>}
      />
      <div className={`${s.grid} rise`}>
        {sorted.map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}
        <Link href="/agents/new" className={`${s.card} ${s.add}`}>
          <span className={s.addIco}><Icon name="plus" size={18} /></span>
          <b>Build an agent</b>
          <p>Drag steps onto a canvas, connect them, and run branches in parallel.</p>
        </Link>
      </div>
    </>
  );
}

function AgentCard({ a, st }: { a: AgentDef; st: ReturnType<typeof stats> }) {
  const { openAgent } = useStore();
  return (
    <button type="button" className={s.card} onClick={() => openAgent(a.id)}>
      <div className={s.top}>
        <span className={`${s.ico} ${st.running ? s.live : ""}`}><Icon name={a.icon} size={18} /></span>
        <div style={{ minWidth: 0 }}>
          <b>{a.name}</b>
          <small className="mono">{a.id}@{a.version}</small>
        </div>
        <span className={`tag ${a.kind === "orchestrator" ? "accent" : ""}`}>{a.kind === "orchestrator" ? "multi-step" : "single step"}</span>
      </div>
      <p className={s.role}>{a.role}</p>
      <div className={s.chips}>
        {a.delegates.map((d) => <span key={d} className="tag">{agentById(d)?.name}</span>)}
        {a.tools.map((t) => <span key={t} className="tag accent">{toolById(t)?.name ?? t}</span>)}
        {a.tools.length + a.delegates.length === 0 && <span className="tag">model only</span>}
      </div>
      <div className={s.foot}>
        <span><b>{st.n}</b> runs</span>
        <span><b>{st.rate}%</b> ok</span>
        <span><b>{st.avg ? secs(st.avg) : "—"}</b> avg</span>
        <span className="muted">{models[a.model]?.label}</span>
      </div>
    </button>
  );
}
