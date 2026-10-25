"use client";

import { useState } from "react";
import { agentApi, AgentRequestError } from "@/lib/api";
import s from "./overview.module.css";

const LIMITS = { max_delegations: 2, max_concurrent_agents: 2, max_tool_calls: 4, max_model_turns: 8, max_tokens: 12000, max_cost_usd: 2, timeout_seconds: 120 };

/** Starts a real run of the service's orchestrator agent with the brief as its task. */
export default function BriefForm({ onToast, onStarted }: { onToast: (m: string) => void; onStarted: () => void }) {
  const [task, setTask] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const text = task.trim();
    if (!text || busy) return;
    setBusy(true);
    try {
      const { agents } = await agentApi.agents();
      const agent = agents.find((a) => a.id === "vendor-orchestrator") ?? agents.find((a) => a.id.endsWith("-orchestrator"));
      if (!agent) throw new Error("The agent service has no orchestrator agent");
      const started = await agentApi.start({ agent_id: agent.id, agent_version: agent.version, input: { task: text }, limits: LIMITS });
      setTask("");
      onToast(`Flow ${started.run_id} started`);
      onStarted();
    } catch (err) {
      onToast(err instanceof AgentRequestError || err instanceof Error ? err.message : "Could not start the flow");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className={s.brief} onSubmit={submit}>
      <label htmlFor="brief">Brief for a new flow</label>
      <input id="brief" value={task} onChange={(e) => setTask(e.target.value)} placeholder="Describe the task, e.g. compare vendors for a small support team" maxLength={500} />
      <button className="btn solid" type="submit" disabled={busy || !task.trim()}>{busy ? "Starting…" : "Start flow"}</button>
    </form>
  );
}
