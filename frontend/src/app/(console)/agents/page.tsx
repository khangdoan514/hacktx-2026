"use client";

import Link from "next/link";
import { FormEvent, useEffect, useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { secs } from "@/lib/format";
import { useAllRuns, useStore } from "@/lib/store";
import type { AgentDef, RunView } from "@/lib/types";
import { nmc } from "@/product/nmc3/client";
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
  const { agents } = useStore();
  const sorted = [...agents].sort((x, y) => Number(y.kind === "orchestrator") - Number(x.kind === "orchestrator"));

  return (
    <>
      <PageHeader title="Agents" sub="Click an agent for its details and history." actions={<Link href="/agents/new" className="btn solid"><Icon name="plus" size={15} />New agent</Link>} />
      {agents.length === 0 && <p className="muted">The agent service has no agents.</p>}
      <div className={`${s.grid} rise`}>
        {sorted.map((a) => <AgentCard key={a.id} a={a} st={stats(a.id, runs)} />)}
        <Link href="/agents/new" className={`${s.card} ${s.add}`}>
          <span className={s.addIco}><Icon name="plus" size={18} /></span>
          <b>Build an agent</b>
          <p>Drag steps onto a canvas, connect them, and run branches in parallel.</p>
        </Link>
      </div>
      <ProjectContext agents={agents} />
    </>
  );
}

type ContextEntry = { id: string; agent_id: string; kind: string; title: string; body: string; visibility: string; owner_id: string; created_at: string; can_delete: boolean };

function ProjectContext({ agents }: { agents: AgentDef[] }) {
  const [projects, setProjects] = useState<{ id: string; name: string }[]>([]);
  const [projectID, setProjectID] = useState("");
  const [entries, setEntries] = useState<ContextEntry[]>([]);
  const [shared, setShared] = useState<string[]>([]);
  const [agentID, setAgentID] = useState("");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [visibility, setVisibility] = useState("private");
  const [error, setError] = useState("");
  useEffect(() => {
    nmc<{ items: { id: string; name: string }[] }>("/api/v1/agent-projects")
      .then((result) => { setProjects(result.items); setProjectID((current) => current || result.items[0]?.id || ""); })
      .catch((err: Error) => setError(err.message));
  }, []);
  useEffect(() => {
    if (!projectID) return;
    void Promise.all([
      nmc<{ items: ContextEntry[] }>(`/api/v1/projects/${projectID}/agent-context`),
      nmc<{ agents: { agent_id: string }[] }>(`/api/v1/projects/${projectID}/agents`),
    ]).then(([context, shares]) => { setEntries(context.items); setShared(shares.agents.map((item) => item.agent_id)); })
      .catch((err: Error) => setError(err.message));
  }, [projectID]);
  async function refresh() {
    const [context, shares] = await Promise.all([
      nmc<{ items: ContextEntry[] }>(`/api/v1/projects/${projectID}/agent-context`),
      nmc<{ agents: { agent_id: string }[] }>(`/api/v1/projects/${projectID}/agents`),
    ]);
    setEntries(context.items);
    setShared(shares.agents.map((item) => item.agent_id));
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await nmc(`/api/v1/projects/${projectID}/agent-context`, { method: "POST", body: JSON.stringify({ agent_id: agentID, kind: "memory", title, body, visibility }) });
      setTitle(""); setBody("");
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : "Could not save context"); }
  }
  async function toggleShare(id: string) {
    setError("");
    try {
      if (shared.includes(id)) await nmc(`/api/v1/projects/${projectID}/agents/${encodeURIComponent(id)}`, { method: "DELETE" });
      else await nmc(`/api/v1/projects/${projectID}/agents`, { method: "POST", body: JSON.stringify({ agent_id: id }) });
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : "Could not update sharing"); }
  }
  async function deleteEntry(id: string) {
    setError("");
    try {
      await nmc(`/api/v1/projects/${projectID}/agent-context/${id}`, { method: "DELETE" });
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : "Could not delete context"); }
  }
  if (!projects.length) return null;
  return <section className="card" style={{ marginTop: 24, padding: 20 }}>
    <h2>Project agent context</h2>
    <p className="muted">Project members can share agents and context. Private notes stay with you; shared notes and run reports are visible to the project.</p>
    <label>Project <select className="input" value={projectID} onChange={(event) => setProjectID(event.target.value)}>{projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}</select></label>
    <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 12 }}>
      {agents.map((agent) => <button key={agent.id} className="btn" type="button" onClick={() => void toggleShare(agent.id)}>{shared.includes(agent.id) ? `Unshare ${agent.name}` : `Share ${agent.name}`}</button>)}
    </div>
    <form onSubmit={(event) => void save(event)} style={{ display: "grid", gap: 8, marginTop: 16 }}>
      <select className="input" value={agentID} onChange={(event) => setAgentID(event.target.value)} required><option value="">Choose an agent</option>{agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select>
      <input className="input" value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Context title" required />
      <textarea className="input" value={body} onChange={(event) => setBody(event.target.value)} placeholder="Notes for the agent" required rows={4} />
      <select className="input" value={visibility} onChange={(event) => setVisibility(event.target.value)}><option value="private">Private</option><option value="project">Share with project</option></select>
      <button className="btn solid" type="submit">Save context</button>
    </form>
    {error && <p role="alert">{error}</p>}
    <ul style={{ marginTop: 16 }}>
      {entries.map((entry) => <li key={entry.id}><b>{entry.title}</b> · {entry.agent_id} · {entry.visibility} · {entry.kind} {entry.can_delete && <button className="btn" type="button" onClick={() => void deleteEntry(entry.id)}>Delete</button>}<p>{entry.body}</p></li>)}
    </ul>
  </section>;
}

function AgentCard({ a, st }: { a: AgentDef; st: ReturnType<typeof stats> }) {
  const { openAgent } = useStore();
  return (
    <div className={s.wrap}>
    <button type="button" className={s.card} onClick={() => openAgent(a.id)}>
      <div className={s.top}>
        <span className={`${s.ico} ${st.running ? s.live : ""}`}><Icon name={a.icon} size={18} /></span>
        <div style={{ minWidth: 0 }}>
          <b>{a.name}</b>
          <small className="mono">{a.id}@{a.version}</small>
        </div>
        <span className={`tag ${a.kind === "orchestrator" ? "accent" : ""}`}>{a.kind === "orchestrator" ? "multi-step" : "single step"}</span>
      </div>
      <p className={s.role}>{a.instructions || "No instructions recorded."}</p>
      <div className={s.foot}>
        <span><b>{st.n}</b> runs</span>
        <span><b>{st.rate}%</b> ok</span>
        <span><b>{st.avg ? secs(st.avg) : "—"}</b> avg</span>
      </div>
    </button>
    <Link href={`/agents/new?from=${a.id}`} className={`btn sm ${s.edit}`}>Edit</Link>
    </div>
  );
}
