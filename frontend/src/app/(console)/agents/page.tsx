"use client";

import Link from "next/link";
import PageHeader from "@/components/shell/PageHeader";
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
  const { agents, agentById } = useStore();
  const orchestrators = agents.filter((a) => a.kind === "orchestrator");
  const loose = agents.filter((a) => a.kind === "specialist" && !agents.some((o) => o.delegates.includes(a.id)));

  return (
    <>
      <PageHeader title="Agents" sub="Agents the service can run. Click one for its spec, history and prompts." actions={<Link href="/definitions" className="btn"><Icon name="plus" size={15} />Add an agent</Link>} />
      {agents.length === 0 && <p className="muted">The agent service has no agents.</p>}
      {orchestrators.map((o) => (
        <section key={o.id} className={`${s.team} rise`}>
          <h2 className={s.teamH}>{o.name} <span className="mono muted">team</span></h2>
          <div className={s.grid}>
            {[o, ...o.delegates.map((d) => agentById(d)).filter((a): a is AgentDef => !!a)].map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}
          </div>
        </section>
      ))}
      {loose.length > 0 && (
        <section className={`${s.team} rise`}>
          <h2 className={s.teamH}>Standalone</h2>
          <div className={s.grid}>{loose.map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}</div>
        </section>
      )}
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
        <span className={`tag ${a.kind === "orchestrator" ? "accent" : ""}`}>{a.kind}</span>
      </div>
      <p className={s.role}>{a.instructions || "No instructions recorded."}</p>
      <div className={s.chips}>
        {a.delegates.map((d) => <span key={d} className="tag">{d}</span>)}
        {a.tools.map((t) => <span key={t} className="tag accent">{t}</span>)}
        {a.tools.length + a.delegates.length === 0 && <span className="tag">model only</span>}
      </div>
      <div className={s.foot}>
        <span><b>{st.n}</b> runs</span>
        <span><b>{st.rate}%</b> ok</span>
        <span><b>{st.avg ? secs(st.avg) : "—"}</b> avg</span>
      </div>
    </button>
  );
}
