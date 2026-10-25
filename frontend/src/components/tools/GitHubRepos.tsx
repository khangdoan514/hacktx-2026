"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import Icon from "@/components/ui/Icon";
import { nmc } from "@/product/nmc3/client";
import s from "./GitHubRepos.module.css";

type Project = { id: string; name: string };
type Candidate = { installation_id: number; account_login: string; repository_id: number; full_name: string };
type Binding = { id: string; repository_id: number; full_name: string; branch: string | null; enabled: boolean };
type Rule = { id: string; binding_id: string; full_name: string; event_name: string; action_name: string; instructions: string; enabled: boolean };
type Run = { id: string; repository: string; event_name: string; action_name: string; state: string; agent_run_id?: string; error?: string; created_at: string };
type Setup = { status: string; candidates: Candidate[]; error?: string; expires_at: string };
type Config = { configured: boolean; install_url: string };
type SetupMessage = { type: "github-app-setup"; project: string; setup: string; result: string };

/** Where the GitHub sign-in returned, when it was not in a popup. */
export type GitHubReturn = { project: string; setup: string; result: string };

const events = [
  { id: "issues/opened", name: "New issue" },
  { id: "pull_request/opened", name: "New pull request" },
  { id: "pull_request/synchronize", name: "Pull request updated" },
];
const eventNames: Record<string, string> = Object.fromEntries(events.map((event) => [event.id, event.name]));

// Starting points for a trigger's task. The agent stays read-only; these only
// change what it looks for and reports.
const presets = [
  { name: "General review", text: "" },
  { name: "Triage", text: "Suggest labels, a priority, and who should own this. Point out duplicates of open issues." },
  { name: "Test coverage", text: "Check whether the change adds or updates tests. List files that change behavior without tests." },
  { name: "Security", text: "Look for leaked secrets, unsafe input handling, and permission changes. Rank findings by risk." },
  { name: "Release notes", text: "Write a short user-facing release note for this change." },
];

export default function GitHubRepos({ initial, onClose }: { initial?: GitHubReturn | null; onClose: () => void }) {
  const [projects, setProjects] = useState<Project[]>([]);
  const [projectID, setProjectID] = useState("");
  const [newProjectName, setNewProjectName] = useState("");
  const [config, setConfig] = useState<Config | null>(null);
  const [setupID, setSetupID] = useState(initial?.setup ?? "");
  const [setup, setSetup] = useState<Setup | null>(null);
  const [candidateBranches, setCandidateBranches] = useState<Record<number, string[]>>({});
  const [selectedBranches, setSelectedBranches] = useState<Record<number, string>>({});
  const [bindings, setBindings] = useState<Binding[]>([]);
  const [rules, setRules] = useState<Rule[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [draft, setDraft] = useState<{ bindingID: string; eventID: string; instructions: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(initial?.result === "failed" ? "GitHub authorization did not finish. Try again." : "");
  const [notice, setNotice] = useState("");
  const popupRef = useRef<Window | null>(null);

  useEffect(() => {
    void Promise.all([
      nmc<{ items: Project[] }>("/api/v1/agent-projects"),
      nmc<Config>("/api/v1/github/config"),
    ]).then(([projectList, appConfig]) => {
      setProjects(projectList.items);
      setConfig(appConfig);
      setProjectID(projectList.items.find((item) => item.id === initial?.project)?.id || projectList.items[0]?.id || "");
    }).catch((err: Error) => setError(err.message));
  }, [initial?.project]);

  // The sign-in popup lands on /tools, which forwards its result here.
  useEffect(() => {
    const onMessage = (event: MessageEvent<SetupMessage>) => {
      if (event.origin !== window.location.origin || event.source !== popupRef.current || event.data?.type !== "github-app-setup") return;
      popupRef.current = null;
      if (event.data.result === "failed") setError("GitHub authorization did not finish. Try again.");
      setProjectID(event.data.project);
      setSetupID(event.data.setup);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

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

  function chooseRepositories() {
    if (!projectID) return;
    // Open the window during the click so the browser does not block it.
    const popup = window.open("", "connect-github-app", "popup,width=640,height=760");
    if (!popup) {
      setError("Your browser blocked the GitHub window. Allow pop-ups for this app and try again.");
      return;
    }
    popupRef.current = popup;
    void act(async () => {
      try {
        const result = await nmc<{ authorize_url: string }>(`/api/v1/projects/${projectID}/github/setup`, { method: "POST" });
        popup.location.href = result.authorize_url;
      } catch (err) {
        popup.close();
        popupRef.current = null;
        throw err;
      }
    });
  }

  async function createProject() {
    if (!newProjectName.trim()) return;
    await act(async () => {
      const project = await nmc<Project>("/api/v1/agent-projects", { method: "POST", body: JSON.stringify({ name: newProjectName.trim() }) });
      setProjects((current) => [...current, project]);
      setProjectID(project.id);
      setSetupID("");
      setNewProjectName("");
      setNotice(`${project.name} is ready. Choose its repositories next.`);
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
      const created = await nmc<{ id: string }>(`/api/v1/projects/${projectID}/github-repositories`, {
        method: "POST", body: JSON.stringify({ session_id: setupID, repository_id: candidate.repository_id, branch }),
      });
      setNotice(`${candidate.full_name} (${branch}) is connected. Add an agent task to it below.`);
      setDraft({ bindingID: created.id, eventID: events[0].id, instructions: "" });
    });
  }

  async function toggleBinding(binding: Binding) {
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-repositories/${binding.id}`, {
        method: "PATCH", body: JSON.stringify({ enabled: !binding.enabled }),
      });
      setNotice(binding.enabled ? `${binding.full_name} paused. Its agents will not run.` : `${binding.full_name} enabled.`);
    });
  }

  async function createRule(event: FormEvent) {
    event.preventDefault();
    if (!draft) return;
    const [event_name, action_name] = draft.eventID.split("/");
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-rules`, {
        method: "POST", body: JSON.stringify({ binding_id: draft.bindingID, event_name, action_name, instructions: draft.instructions.trim() }),
      });
      setNotice(`The agent will run on each ${eventNames[draft.eventID].toLowerCase()}.`);
      setDraft(null);
    });
  }

  async function toggleRule(rule: Rule) {
    await act(async () => {
      await nmc(`/api/v1/projects/${projectID}/github-rules/${rule.id}`, {
        method: "PATCH", body: JSON.stringify({ enabled: !rule.enabled }),
      });
      setNotice(rule.enabled ? "Agent task paused." : "Agent task resumed.");
    });
  }

  const boundIDs = new Set(bindings.map((binding) => binding.repository_id));
  const candidates = setup?.status === "ready" ? setup.candidates.filter((candidate) => !boundIDs.has(candidate.repository_id)) : [];

  return (
    <div className={s.backdrop} onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className={s.dialog} role="dialog" aria-modal="true" aria-labelledby="github-repos-title">
        <button type="button" className={s.close} aria-label="Close repository setup" onClick={onClose}><Icon name="x" size={17} /></button>
        <header className={s.head}>
          <span className={s.icon} aria-hidden="true">G</span>
          <div>
            <h2 id="github-repos-title">GitHub repositories</h2>
            <p>Install the GitHub App on the repositories you choose, then give agents a task to run on issues and pull requests. Agents only read the repository and branch you connect.</p>
          </div>
        </header>

        {error && <p className={`${s.note} ${s.err}`} role="alert">{error}</p>}
        {notice && <p className={`${s.note} ${s.ok}`} role="status">{notice}</p>}

        <div className={s.step}>
          <h3><span>1</span>Project</h3>
          <p className={s.muted}>Repositories, agent tasks, and run history belong to a project.</p>
          <div className={s.row}>
            <select className="input" aria-label="Project" value={projectID} onChange={(event) => { setProjectID(event.target.value); setSetupID(""); setDraft(null); }}>
              {!projects.length && <option value="">Create a project first</option>}
              {projects.map((project) => <option key={project.id} value={project.id}>{project.name}</option>)}
            </select>
            <input className="input" aria-label="New project name" placeholder="Or name a new project" value={newProjectName} onChange={(event) => setNewProjectName(event.target.value)} maxLength={120} />
            <button className="btn" type="button" disabled={busy || newProjectName.trim().length < 2} onClick={() => void createProject()}>Create</button>
          </div>
        </div>

        <div className={s.step}>
          <h3><span>2</span>Choose repositories</h3>
          <p className={s.muted}>Sign in to GitHub. If the App is not installed yet, GitHub asks which repositories it may read.</p>
          {config && !config.configured ? (
            <p className={s.hint}>The GitHub App is not configured on this server. An administrator needs to set the App ID, private key, OAuth client, webhook secret, and scope signing key.</p>
          ) : (
            <div className={s.row}>
              <button className="btn solid" type="button" disabled={!projectID || !config || busy} onClick={chooseRepositories}>Continue with GitHub</button>
              {config?.install_url && <a className="btn" href={config.install_url} target="_blank" rel="noopener noreferrer">Install on more repositories</a>}
            </div>
          )}
          {setup?.status === "ready" && (
            <ul className={s.list}>
              {candidates.length ? candidates.map((candidate) => (
                <li key={`${candidate.installation_id}/${candidate.repository_id}`}>
                  <span className={s.repo}><b>{candidate.full_name}</b><small>Installed on {candidate.account_login}</small></span>
                  {candidateBranches[candidate.repository_id] ? (
                    <span className={s.inline}>
                      <select className="input" aria-label={`Branch for ${candidate.full_name}`} value={selectedBranches[candidate.repository_id] || ""}
                        onChange={(event) => setSelectedBranches((current) => ({ ...current, [candidate.repository_id]: event.target.value }))}>
                        {candidateBranches[candidate.repository_id].map((branch) => <option key={branch} value={branch}>{branch}</option>)}
                      </select>
                      <button className="btn solid" type="button" disabled={busy || !selectedBranches[candidate.repository_id]} onClick={() => void bind(candidate)}>Connect</button>
                    </span>
                  ) : <button className="btn" type="button" disabled={busy} onClick={() => void loadBranches(candidate)}>Choose branch</button>}
                </li>
              )) : <li className={s.muted}>{setup.candidates.length ? "Every repository you authorized is already connected." : "The App is not installed on any repository you can access. Install it, then continue with GitHub again."}</li>}
            </ul>
          )}
          {setup?.status === "failed" && <p className={`${s.note} ${s.err}`}>{setup.error || "GitHub authorization failed."}</p>}
          {setup?.status === "expired" && <p className={s.hint}>This repository list expired. Continue with GitHub again.</p>}
        </div>

        <div className={s.step}>
          <h3><span>3</span>Agent tasks <small>{bindings.length} {bindings.length === 1 ? "repository" : "repositories"}</small></h3>
          {bindings.length === 0 ? <p className={s.muted}>No repository is connected to this project yet.</p> : (
            <ul className={s.repos}>
              {bindings.map((binding) => {
                const own = rules.filter((rule) => rule.binding_id === binding.id);
                const ready = binding.enabled && !!binding.branch;
                return (
                  <li key={binding.id} className={binding.enabled ? "" : s.paused}>
                    <div className={s.repoHead}>
                      <span className={s.repo}><b>{binding.full_name}</b><small>{binding.branch ? `Branch ${binding.branch}` : "Reconnect to choose a branch"}{binding.enabled ? "" : " · Paused"}</small></span>
                      <button type="button" className={`${s.switch} ${binding.enabled ? s.switchOn : ""}`} role="switch" aria-checked={binding.enabled} aria-label={`${binding.enabled ? "Pause" : "Enable"} ${binding.full_name}`} disabled={busy} onClick={() => void toggleBinding(binding)}><span /></button>
                    </div>
                    {own.length > 0 && (
                      <ul className={s.rules}>
                        {own.map((rule) => (
                          <li key={rule.id}>
                            <span>
                              <b>{eventNames[`${rule.event_name}/${rule.action_name}`] || `${rule.event_name}.${rule.action_name}`}</b>
                              <small>{rule.instructions || "Review and summarize next steps"}</small>
                            </span>
                            <button className="btn" type="button" disabled={busy} onClick={() => void toggleRule(rule)}>{rule.enabled ? "Pause" : "Resume"}</button>
                          </li>
                        ))}
                      </ul>
                    )}
                    {draft?.bindingID === binding.id ? (
                      <form className={s.draft} onSubmit={(event) => void createRule(event)}>
                        <div className={s.row}>
                          <label>When
                            <select className="input" value={draft.eventID} onChange={(event) => setDraft({ ...draft, eventID: event.target.value })}>
                              {events.map((event) => <option key={event.id} value={event.id}>{event.name}</option>)}
                            </select>
                          </label>
                          <label>Start from
                            <select className="input" value={presets.find((preset) => preset.text === draft.instructions)?.name ?? ""} onChange={(event) => setDraft({ ...draft, instructions: presets.find((preset) => preset.name === event.target.value)?.text ?? draft.instructions })}>
                              {!presets.some((preset) => preset.text === draft.instructions) && <option value="">Custom</option>}
                              {presets.map((preset) => <option key={preset.name} value={preset.name}>{preset.name}</option>)}
                            </select>
                          </label>
                        </div>
                        <label>What should the agent do?
                          <textarea className="input" rows={3} maxLength={2000} placeholder="Leave empty to review the change and summarize next steps." value={draft.instructions} onChange={(event) => setDraft({ ...draft, instructions: event.target.value })} />
                        </label>
                        <div className={s.row}>
                          <button className="btn solid" type="submit" disabled={busy}>Add task</button>
                          <button className="btn" type="button" onClick={() => setDraft(null)}>Cancel</button>
                        </div>
                      </form>
                    ) : ready && <button className={s.addTask} type="button" disabled={busy} onClick={() => setDraft({ bindingID: binding.id, eventID: events[0].id, instructions: "" })}><Icon name="plus" size={14} /> Add agent task</button>}
                  </li>
                );
              })}
            </ul>
          )}
          {bindings.some((binding) => binding.enabled && binding.branch) && <p className={s.muted}>For a weekly report on a repository, <Link href="/automations">schedule the GitHub reviewer</Link>.</p>}
        </div>

        <div className={s.step}>
          <h3>Recent runs <small>{runs.length}</small></h3>
          {runs.length ? (
            <ul className={s.list}>
              {runs.slice(0, 8).map((run) => (
                <li key={run.id}>
                  <span className={s.repo}>
                    <b>{run.repository} · {eventNames[`${run.event_name}/${run.action_name}`] || `${run.event_name}.${run.action_name}`}</b>
                    <small>{new Date(run.created_at).toLocaleString()} · {run.state}{run.error ? ` · ${run.error}` : ""}</small>
                  </span>
                  {run.agent_run_id && <Link className="btn" href={`/live-run/${run.agent_run_id}`}>View run</Link>}
                </li>
              ))}
            </ul>
          ) : <p className={s.muted}>Runs appear here after a matching issue or pull request event.</p>}
        </div>
      </section>
    </div>
  );
}
