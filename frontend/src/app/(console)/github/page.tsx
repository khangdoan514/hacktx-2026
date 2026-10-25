"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import { nmc } from "@/product/nmc3/client";
import s from "./github.module.css";

type Project = { id: string; name: string };
type Candidate = { installation_id: number; account_login: string; repository_id: number; full_name: string };
type Binding = { id: string; repository_id: number; full_name: string; branch: string | null; enabled: boolean };
type Rule = { id: string; binding_id: string; full_name: string; event_name: string; action_name: string; enabled: boolean };
type Run = { id: string; repository: string; event_name: string; action_name: string; state: string; agent_run_id?: string; error?: string; created_at: string };
type Setup = { status: string; candidates: Candidate[]; error?: string; expires_at: string };
type Config = { configured: boolean; install_url: string };

const events = [
  { id: "issues/opened", name: "New issue" },
  { id: "pull_request/opened", name: "New pull request" },
  { id: "pull_request/synchronize", name: "Pull request updated" },
];

export default function GitHubPage() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [newProjectName, setNewProjectName] = useState("");
  const [config, setConfig] = useState<Config | null>(null);
  const [setupID, setSetupID] = useState("");
  const [setup, setSetup] = useState<Setup | null>(null);
  const [candidateBranches, setCandidateBranches] = useState<Record<number, string[]>>({});
  const [selectedBranches, setSelectedBranches] = useState<Record<number, string>>({});
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [rules, setRules] = useState<Rule[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [bindingID, setBindingID] = useState("");
  const [eventID, setEventID] = useState(events[0].id);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const fromProject = params.get("project") || "";
    setSetupID(params.get("setup") || "");
    if (params.get("result") === "failed") setError("GitHub authorization did not finish. Try connecting again.");
    void Promise.all([
      nmc<{ items: Project[] }>("/api/v1/agent-projects"),
      nmc<Config>("/api/v1/github/config"),
    ]).then(([projectList, appConfig]) => {
      setProjects(projectList.items);
      setConfig(appConfig);
      setProjectID(projectList.items.find((item) => item.id === fromProject)?.id || projectList.items[0]?.id || "");
    }).catch((err: Error) => setError(err.message));
  }, []);

  const refresh = useCallback(async () => {
    if (!projectID) return;
    const [repos, eventRules, history] = await Promise.all([
      nmc<{ items: Binding[] }>(`/api/v1/projects/${projectID}/github-repositories`),
      nmc<{ items: Rule[] }>(`/api/v1/projects/${projectID}/github-rules`),
      nmc<{ items: Run[] }>(`/api/v1/projects/${projectID}/github-runs`),
    ]);
    setBindings(repos.items);
    setRules(eventRules.items);
    setRuns(history.items);
    setBindingID((current) => repos.items.some((item) => item.id === current && item.enabled && item.branch) ? current : repos.items.find((item) => item.enabled && item.branch)?.id || "");
  }, [projectID]);

  useEffect(() => { void refresh().catch((err: Error) => setError(err.message)); }, [refresh]);
  useEffect(() => {
    if (!projectID || !setupID) { setSetup(null); return; }
    void nmc<Setup>(`/api/v1/projects/${projectID}/github/setup/${setupID}`)
      .then(setSetup).catch((err: Error) => setError(err.message));
  }, [projectID, setupID]);

  async function act(work: () => Promise<void>) {
    setBusy(true); setError(""); setNotice("");
    try { await work(); await refresh(); }
    catch (err) { setError(err instanceof Error ? err.message : "GitHub setup failed"); }
    finally { setBusy(false); }
  }
  async function connect() {
    if (!projectID) return;
    await act(async () => {
      const result = await nmc<{ authorize_url: string }>(`/api/v1/projects/${projectID}/github/setup`, { method: "POST" });
      window.location.assign(result.authorize_url);
    });
  }
  async function createProject() {
    if (!newProjectName.trim()) return;
    await act(async () => {
      const project = await nmc<Project>("/api/v1/agent-projects", { method: "POST", body: JSON.stringify({ name: newProjectName.trim() }) });
      setProjects((current) => [...current, project]);
      setProjectID(project.id);
      setNewProjectName("");
      setNotice(`${project.name} is ready.`);
    });
  }
  async function loadBranches(candidate: Candidate) {
    await act(async () => {
      const result = await nmc<{ branches: string[] }>(`/api/v1/projects/${projectID}/github/setup/${setupID}/repositories/${candidate.repository_id}/branches`);
      setCandidateBranches((current) => ({ ...current, [candidate.repository_id]: result.branches }));
      setSelectedBranches((current) => ({ ...current, [candidate.repository_id]: result.branches[0] || "" }));
    });
  }
  async function bind(candidate: Candidate) {
    const branch = selectedBranches[candidate.repository_id];
    if (!branch) return;
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-repositories`, {
        method: "POST", body: JSON.stringify({ session_id: setupID, repository_id: candidate.repository_id, branch }),
      });
      setNotice(`${candidate.full_name} (${branch}) is connected to this project.`);
    });
  }
  async function toggleBinding(binding: Binding) {
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-repositories/${binding.id}`, {
        method: "PATCH", body: JSON.stringify({ enabled: !binding.enabled }),
      });
      setNotice(binding.enabled ? "Repository disabled." : "Repository enabled.");
    });
  }
  async function createRule() {
    if (!bindingID) return;
    const [event_name, action_name] = eventID.split("/");
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-rules`, {
        method: "POST", body: JSON.stringify({ binding_id: bindingID, event_name, action_name }),
      });
      setNotice("The review agent will run when this event arrives.");
    });
  }
  async function toggleRule(rule: Rule) {
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-rules/${rule.id}`, {
        method: "PATCH", body: JSON.stringify({ enabled: !rule.enabled }),
      });
      setNotice(rule.enabled ? "Rule paused." : "Rule enabled.");
    });
  }

  return <>
    <PageHeader title="GitHub" sub="Connect selected repositories to a project and run the review agent on GitHub events." />
    {error && <p className={s.error} role="alert">{error}</p>}
    {notice && <p className={s.notice} role="status">{notice}</p>}
    <div className={s.grid}>
      <section className="card">
        <div className="ch"><h2>1. Choose a project</h2></div>
        <label className={s.field}>Project<select className="input" value={projectID} onChange={(event) => { setProjectID(event.target.value); setSetupID(""); }}>
          {!projects.length && <option value="">Choose or create a project</option>}
          {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
        </select></label>
        <div className={s.actions}>
          <input className="input" aria-label="New project name" placeholder="New project name" value={newProjectName} onChange={(event) => setNewProjectName(event.target.value)} maxLength={120} />
          <button className="btn" type="button" disabled={busy || newProjectName.trim().length < 2} onClick={() => void createProject()}>Create project</button>
        </div>
      </section>
      <section className="card">
        <div className="ch"><h2>2. Connect GitHub</h2></div>
        <p className="muted">Sign in to GitHub, install the App on selected repositories if prompted, then choose a repository and branch here.</p>
        <div className={s.actions}>
          <button className="btn solid" type="button" disabled={!projectID || !config?.configured || busy} onClick={() => void connect()}>Continue with GitHub</button>
          {config?.install_url && <a className="btn" href={config.install_url} target="_blank" rel="noopener noreferrer">Install the GitHub App</a>}
        </div>
        {config && !config.configured && <p className={s.hint}>The GitHub App OAuth client needs to be configured on this server.</p>}
        {setup?.status === "ready" && <div className={s.candidates}>
          <h3>Available repositories</h3>
          {setup.candidates.length ? setup.candidates.map((candidate) => <div className={s.row} key={`${candidate.installation_id}/${candidate.repository_id}`}>
            <span><b>{candidate.full_name}</b><small>{candidate.account_login}</small></span>
            {candidateBranches[candidate.repository_id] ? <div className={s.actions}>
              <select className="input" aria-label={`Branch for ${candidate.full_name}`} value={selectedBranches[candidate.repository_id] || ""}
                onChange={(event) => setSelectedBranches((current) => ({ ...current, [candidate.repository_id]: event.target.value }))}>
                {candidateBranches[candidate.repository_id].map((branch) => <option key={branch} value={branch}>{branch}</option>)}
              </select>
              <button className="btn" type="button" disabled={busy || !selectedBranches[candidate.repository_id]} onClick={() => void bind(candidate)}>Add to project</button>
            </div> : <button className="btn" type="button" disabled={busy} onClick={() => void loadBranches(candidate)}>Choose branch</button>}
          </div>) : <p className="muted">No accessible installations yet. Install the App on a repository, then connect again.</p>}
        </div>}
        {setup?.status === "failed" && <p className={s.error}>{setup.error || "GitHub authorization failed."}</p>}
        {setup?.status === "expired" && <p className={s.hint}>This selection expired. Connect GitHub again.</p>}
      </section>
      <section className="card">
        <div className="ch"><h2>3. Project repositories</h2><span className="n">{bindings.length}</span></div>
        {bindings.length ? bindings.map((binding) => <div className={s.row} key={binding.id}>
          <span><b>{binding.full_name}</b><small>{binding.branch || "Choose a branch by reconnecting"} · {binding.enabled ? "Enabled" : "Disabled"}</small></span>
          <button className="btn" type="button" disabled={busy} onClick={() => void toggleBinding(binding)}>{binding.enabled ? "Disable" : "Enable"}</button>
        </div>) : <p className="muted">No repository connected to this project.</p>}
      </section>
      <section className="card">
        <div className="ch"><h2>4. Agent triggers</h2></div>
        <p className="muted">A repository event starts the read-only GitHub review agent. You can pause each trigger here.</p>
        <div className={s.actions}>
          <select className="input" aria-label="Repository" value={bindingID} onChange={(event) => setBindingID(event.target.value)}>
            <option value="">Choose a repository</option>{bindings.filter((item) => item.enabled && item.branch).map((item) => <option key={item.id} value={item.id}>{item.full_name} · {item.branch}</option>)}
          </select>
          <select className="input" aria-label="GitHub event" value={eventID} onChange={(event) => setEventID(event.target.value)}>
            {events.map((event) => <option key={event.id} value={event.id}>{event.name}</option>)}
          </select>
          <button className="btn solid" type="button" disabled={!bindingID || busy} onClick={() => void createRule()}>Add trigger</button>
        </div>
        {rules.map((rule) => <div className={s.row} key={rule.id}>
          <span><b>{rule.full_name}</b><small>{rule.event_name}.{rule.action_name} · {rule.enabled ? "Active" : "Paused"}</small></span>
          <button className="btn" type="button" disabled={busy} onClick={() => void toggleRule(rule)}>{rule.enabled ? "Pause" : "Resume"}</button>
        </div>)}
      </section>
      <section className={`card ${s.wide}`}>
        <div className="ch"><h2>Recent GitHub agent runs</h2><span className="n">{runs.length}</span></div>
        {runs.length ? runs.map((run) => <div className={s.row} key={run.id}>
          <span><b>{run.repository} · {run.event_name}.{run.action_name}</b><small>{new Date(run.created_at).toLocaleString()} · {run.state}{run.error ? ` · ${run.error}` : ""}</small></span>
          {run.agent_run_id && <Link className="btn" href={`/live-run/${run.agent_run_id}`}>View run</Link>}
        </div>) : <p className="muted">Runs will appear here after a configured GitHub event.</p>}
      </section>
    </div>
  </>;
}
