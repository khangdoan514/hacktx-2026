"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import { useStore } from "@/lib/store";
import type { AgentDef } from "@/lib/types";
import { nmc } from "@/product/nmc3/client";
import s from "./automations.module.css";

type Schedule = { kind: "once" | "interval" | "daily" | "weekly"; run_at?: string; interval_seconds?: number; local_time?: string; weekday?: number; timezone?: string };
type Automation = { id: string; name: string; project_id?: string; github_binding_id?: string; agent_id: string; agent_version: string; input: { task?: string }; schedule: Schedule; status: "active" | "paused" | "completed"; next_run_at?: string; created_at: string };
type Occurrence = { id: string; scheduled_for: string; state: string; agent_run_id?: string; attempts: number; error?: string };
type Project = { id: string; name: string };
type GitHubBinding = { id: string; full_name: string; branch: string | null; enabled: boolean };

const days = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const showTime = (value?: string) => value ? new Date(value).toLocaleString() : "—";
const zone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
function promptField(agent: AgentDef): "task" | "question" | null {
  try {
    const schema = JSON.parse(agent.input) as { properties?: Record<string, unknown> };
    if (schema.properties?.task) return "task";
    if (schema.properties?.question) return "question";
  } catch { /* A roster entry has no published input schema. */ }
  return null;
}

export default function AutomationsPage() {
  const { agents } = useStore();
  const [items, setItems] = useState<Automation[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [runs, setRuns] = useState<Occurrence[]>([]);
  const [name, setName] = useState("");
  const [task, setTask] = useState("");
  const [agentID, setAgentID] = useState("");
  const [projectID, setProjectID] = useState("");
  const [githubBindings, setGitHubBindings] = useState<GitHubBinding[]>([]);
  const [githubBindingID, setGitHubBindingID] = useState("");
  const [kind, setKind] = useState<Schedule["kind"]>("weekly");
  const [onceAt, setOnceAt] = useState("");
  const [interval, setIntervalValue] = useState(60);
  const [localTime, setLocalTime] = useState("09:00");
  const [weekday, setWeekday] = useState(1);
  const [timezone, setTimezone] = useState(zone);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    const body = await nmc<{ items: Automation[] }>("/api/v1/agent-automations");
    setItems(body.items);
    setSelectedID((current) => current || body.items[0]?.id || "");
  }, []);
  useEffect(() => {
    void refresh().catch((err: Error) => setError(err.message));
    void nmc<{ items: Project[] }>("/api/v1/agent-projects").then((body) => setProjects(body.items)).catch(() => {});
    const timer = window.setInterval(() => { void refresh().catch(() => {}); }, 15000);
    return () => window.clearInterval(timer);
  }, [refresh]);
  useEffect(() => {
    if (!selectedID) { setRuns([]); return; }
    const load = () => nmc<{ items: Occurrence[] }>(`/api/v1/agent-automations/${selectedID}/runs`).then((body) => setRuns(body.items));
    void load().catch((err: Error) => setError(err.message));
    const timer = window.setInterval(() => { void load().catch(() => {}); }, 10000);
    return () => window.clearInterval(timer);
  }, [selectedID]);
  useEffect(() => {
    if (!projectID) { setGitHubBindings([]); setGitHubBindingID(""); return; }
    void nmc<{ items: GitHubBinding[] }>(`/api/v1/projects/${projectID}/github-repositories`)
      .then((body) => { setGitHubBindings(body.items.filter((item) => item.enabled && item.branch)); setGitHubBindingID(""); })
      .catch(() => { setGitHubBindings([]); setGitHubBindingID(""); });
  }, [projectID]);

  const selected = items.find((item) => item.id === selectedID);
  const schedulableAgents = agents.filter((item) => promptField(item) !== null);
  const agent = schedulableAgents.find((item) => item.id === agentID);
  async function create(event: FormEvent) {
    event.preventDefault();
    const field = agent && promptField(agent);
    if (!agent || !field || !task.trim() || !name.trim() || (agent.id === "github-repo-reviewer" && !githubBindingID)) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const schedule: Schedule = { kind, timezone };
      if (kind === "once") schedule.run_at = new Date(onceAt).toISOString();
      if (kind === "interval") schedule.interval_seconds = interval * 60;
      if (kind === "daily" || kind === "weekly") schedule.local_time = localTime;
      if (kind === "weekly") schedule.weekday = weekday;
      const body = await nmc<{ id: string }>("/api/v1/agent-automations", {
        method: "POST",
        body: JSON.stringify({
          name: name.trim(), project_id: projectID, github_binding_id: agent.id === "github-repo-reviewer" ? githubBindingID : "", agent_id: agent.id, agent_version: agent.version,
          input: { [field]: task.trim() }, schedule,
          limits: {
            max_delegations: Math.max(1, agent.limits.delegations),
            max_concurrent_agents: Math.max(1, agent.limits.parallel),
            max_tool_calls: Math.max(1, agent.limits.tools),
            max_model_turns: Math.max(1, agent.limits.turns),
            max_tokens: Math.max(1, agent.maxTokens || 4000),
            max_cost_usd: Math.max(0.01, agent.maxCostUsd || 1),
            timeout_seconds: Math.max(1, agent.limits.seconds),
          },
        }),
      });
      setName(""); setTask(""); setSelectedID(body.id); setNotice("Automation scheduled.");
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : "Could not schedule automation"); }
    finally { setBusy(false); }
  }
  async function setStatus(status: "active" | "paused") {
    if (!selected) return;
    setBusy(true); setError(""); setNotice("");
    try {
      await nmc(`/api/v1/agent-automations/${selected.id}`, { method: "PATCH", body: JSON.stringify({ status }) });
      setNotice(status === "active" ? "Automation resumed." : "Automation paused.");
      await refresh();
    } catch (err) { setError(err instanceof Error ? err.message : "Could not update automation"); }
    finally { setBusy(false); }
  }
  async function runNow() {
    if (!selected) return;
    setBusy(true); setError(""); setNotice("");
    try {
      await nmc(`/api/v1/agent-automations/${selected.id}/run-now`, { method: "POST" });
      setNotice("Run queued. It will appear in history shortly.");
      const body = await nmc<{ items: Occurrence[] }>(`/api/v1/agent-automations/${selected.id}/runs`);
      setRuns(body.items);
    } catch (err) { setError(err instanceof Error ? err.message : "Could not queue run"); }
    finally { setBusy(false); }
  }

  return <>
    <PageHeader title="Automations" sub="Run an agent once, on an interval, or at a local daily or weekly time." />
    {error && <p className={s.error} role="alert">{error}</p>}
    {notice && <p className={s.notice} role="status">{notice}</p>}
    <div className={s.layout}>
      <section className={`card ${s.create}`}>
        <div className="ch"><h2>New automation</h2></div>
        <form onSubmit={(event) => void create(event)}>
          <label>Name<input className="input" value={name} onChange={(event) => setName(event.target.value)} placeholder="Weekly project report" required maxLength={120} /></label>
          <label>Agent<select className="input" value={agentID} onChange={(event) => setAgentID(event.target.value)} required><option value="">Choose an agent</option>{schedulableAgents.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
          {!schedulableAgents.length && <p className={s.empty}>Publish an agent with a task or question input to schedule it.</p>}
          <label>Project<select className="input" value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">No project</option>{projects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
          {agentID === "github-repo-reviewer" && <label>GitHub repository and branch<select className="input" value={githubBindingID} onChange={(event) => setGitHubBindingID(event.target.value)} required>
            <option value="">Choose a repository</option>{githubBindings.map((item) => <option key={item.id} value={item.id}>{item.full_name} · {item.branch}</option>)}
          </select>{!githubBindings.length && <Link href="/github">Connect a repository first</Link>}</label>}
          <label>Task<textarea className="input" rows={4} value={task} onChange={(event) => setTask(event.target.value)} placeholder="Review this week's GitHub activity and summarize progress." required /></label>
          <label>When<select className="input" value={kind} onChange={(event) => setKind(event.target.value as Schedule["kind"])}><option value="once">Once</option><option value="interval">Every interval</option><option value="daily">Daily</option><option value="weekly">Weekly</option></select></label>
          {kind === "once" && <label>Date and time<input className="input" type="datetime-local" value={onceAt} onChange={(event) => setOnceAt(event.target.value)} required /></label>}
          {kind === "interval" && <label>Every how many minutes?<input className="input" type="number" min={1} max={43200} value={interval} onChange={(event) => setIntervalValue(Number(event.target.value))} required /></label>}
          {kind === "weekly" && <label>Day<select className="input" value={weekday} onChange={(event) => setWeekday(Number(event.target.value))}>{days.map((day, index) => <option key={day} value={index}>{day}</option>)}</select></label>}
          {(kind === "daily" || kind === "weekly") && <><label>Local time<input className="input" type="time" value={localTime} onChange={(event) => setLocalTime(event.target.value)} required /></label><label>Time zone<input className="input" value={timezone} onChange={(event) => setTimezone(event.target.value)} placeholder="America/Chicago" required /></label></>}
          <button type="submit" className="btn solid" disabled={busy || !agent}>Schedule automation</button>
        </form>
      </section>
      <div className={s.main}>
        <section className="card">
          <div className="ch"><h2>Scheduled</h2><span className="n">{items.length}</span></div>
          {items.length ? <div className={s.list}>{items.map((item) => <button type="button" key={item.id} className={`${s.item} ${item.id === selectedID ? s.active : ""}`} onClick={() => setSelectedID(item.id)}><b>{item.name}</b><span>{item.agent_id} · {item.status}</span><small>Next: {showTime(item.next_run_at)}</small></button>)}</div> : <p className={s.empty}>No automations yet. Schedule one to get started.</p>}
        </section>
        {selected && <section className="card">
          <div className="ch"><h2>{selected.name}</h2><span className="n">{selected.status}</span></div>
          <div className={s.detail}>
            <p><b>Agent:</b> {selected.agent_id}@{selected.agent_version}</p>
            <p><b>Schedule:</b> {selected.schedule.kind}{selected.schedule.kind === "interval" ? ` · every ${selected.schedule.interval_seconds! / 60} minutes` : ""}{selected.schedule.kind === "weekly" ? ` · ${days[selected.schedule.weekday ?? 0]}` : ""}{selected.schedule.local_time ? ` · ${selected.schedule.local_time} ${selected.schedule.timezone}` : ""}</p>
            <p><b>Next run:</b> {showTime(selected.next_run_at)}</p>
            <p><b>Task:</b> {selected.input.task || (selected.input as { question?: string }).question || JSON.stringify(selected.input)}</p>
            <div className={s.actions}>
              {selected.status !== "completed" && <button type="button" className="btn" disabled={busy} onClick={() => void setStatus(selected.status === "active" ? "paused" : "active")}>{selected.status === "active" ? "Pause" : "Resume"}</button>}
              {selected.status === "active" && <button type="button" className="btn" disabled={busy} onClick={() => void runNow()}>Run now</button>}
            </div>
          </div>
          <div className="ch"><h2>Run history</h2><span className="n">{runs.length}</span></div>
          {runs.length ? <ul className={s.history}>{runs.map((run) => <li key={run.id}><div><b>{showTime(run.scheduled_for)}</b><span className={`tag ${run.state === "succeeded" ? "ok" : ""}`}>{run.state}</span></div>{run.agent_run_id && <Link href={`/live-run/${run.agent_run_id}`}>View agent run</Link>}{run.error && <p>{run.error}</p>}</li>)}</ul> : <p className={s.empty}>No runs yet.</p>}
        </section>}
      </div>
    </div>
  </>;
}
