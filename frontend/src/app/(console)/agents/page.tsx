"use client";

import { agents, agentById } from "@/config/agents";
import { toolById } from "@/config/tools";
import { models } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import CodeHint from "@/components/ui/CodeHint";
import Icon from "@/components/ui/Icon";
import { secs } from "@/lib/format";
import { useAllRuns, useStore } from "@/lib/store";
import type { AgentDef, RunView } from "@/lib/types";
import s from "./agents.module.css";

const NEW_AGENT = {
  cli: `nmc3 agents create support-triage \\\n  --kind specialist \\\n  --model claude-haiku-5-5 \\\n  --tools kb.search,tickets.read \\\n  --instructions ./prompts/triage.md`,
  sdk: `// src/config/agents.ts — add an entry and it appears here,\n// in the spec drawer and in any orchestrator's graph.\n{\n  id: "support-triage", version: "1.0.0", name: "Support Triage",\n  icon: "message", kind: "specialist", model: "claude-haiku-5-5",\n  role: "Drafts replies from the help center.",\n  instructions: "Read the ticket, search the help center, draft a reply.",\n  tools: ["kb.search"], delegates: [],\n  limits: { turns: 4, tools: 3, delegations: 0, parallel: 1, seconds: 20 },\n  input: "{ ticket_id }", output: "{ reply, article_id }",\n}`,
};

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
  const orchestrators = agents.filter((a) => a.kind === "orchestrator");
  const loose = agents.filter((a) => a.kind === "specialist" && !agents.some((o) => o.delegates.includes(a.id)));

  return (
    <>
      <PageHeader
        title="Agents"
        sub="Click an agent for its spec, history and prompts."
        code={{ cli: "nmc3 agents list\nnmc3 agents get idea-consultant --json", sdk: `const agents = await nmc3.agents.list();\nconst def = await nmc3.agents.get("idea-consultant@1.0.0");` }}
      />
      {orchestrators.map((o) => (
        <section key={o.id} className={`${s.team} rise`}>
          <h2 className={s.teamH}>{o.name} <span className="mono muted">team</span></h2>
          <div className={s.grid}>
            {[o, ...o.delegates.map((d) => agentById(d)!).filter(Boolean)].map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}
          </div>
        </section>
      ))}
      <section className={`${s.team} rise`}>
        {loose.length > 0 && <h2 className={s.teamH}>Standalone</h2>}
        <div className={s.grid}>
          {loose.map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}
          <div className={`${s.card} ${s.add}`}>
            <span className={s.addIco}><Icon name="plus" size={18} /></span>
            <b>Add an agent</b>
            <p>Declare it in <span className="mono">src/config/agents.ts</span>, or create it from the CLI.</p>
            <CodeHint code={NEW_AGENT} />
          </div>
        </div>
      </section>
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
