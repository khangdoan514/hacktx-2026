"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { takesTask } from "@/lib/agents";
import Icon from "@/components/ui/Icon";
import { useStore } from "@/lib/store";
import type { Limits } from "@/lib/types";
import s from "./NewRunDialog.module.css";

const FIELDS: [keyof Limits, string][] = [["turns", "turns"], ["tools", "tools"], ["delegations", "delegations"], ["parallel", "parallel"], ["seconds", "seconds"]];

/** Same request as POST /v1/agent-runs: agent, task and per-run limits, starting from the definition's limits. */
export default function NewRunDialog({ open, onClose, agent: initial }: { open: boolean; onClose: () => void; agent?: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  const { startRun, toast, agents, agentById } = useStore();
  const router = useRouter();
  const entry = agents.filter(takesTask);
  const [agent, setAgent] = useState(initial ?? entry[0]?.id ?? "");
  const [task, setTask] = useState("");
  const [limits, setLimits] = useState<Limits>(agentById(agent)?.limits ?? { turns: 8, tools: 4, delegations: 2, parallel: 2, seconds: 60 });
  const [busy, setBusy] = useState(false);

  const pick = (id: string) => {
    setAgent(id);
    const def = agentById(id);
    if (def) setLimits(def.limits);
  };

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      const first = initial && entry.some((a) => a.id === initial) ? initial : entry[0]?.id;
      if (first) pick(first);
      d.showModal();
    }
    if (!open && d.open) d.close();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const go = async () => {
    if (!task.trim() || !agent || busy) return;
    setBusy(true);
    try {
      const id = await startRun({ agent, task: task.trim(), limits });
      setTask("");
      onClose();
      toast("Run started");
      router.push(`/runs/${id}`);
    } catch (err) {
      toast(err instanceof Error ? err.message : "The run could not be started");
    } finally {
      setBusy(false);
    }
  };

  return (
    <dialog ref={ref} className={s.modal} onClose={onClose} onClick={(e) => e.target === ref.current && onClose()}>
      <div className={s.fm}>
        <h3>New run</h3>
        {entry.length === 0 ? <p className="muted">The agent service has no agent that accepts a task.</p> : (
          <>
            <label>agent
              <select className="input" value={agent} onChange={(e) => pick(e.target.value)}>
                {entry.map((a) => <option key={a.id} value={a.id}>{a.name} · {a.id}@{a.version}</option>)}
              </select>
            </label>
            <label>task<textarea className="input" value={task} onChange={(e) => setTask(e.target.value)} /></label>
            <div>
              <label style={{ marginBottom: 6 }}>limits</label>
              <div className={s.lims}>
                {FIELDS.map(([k, l]) => (
                  <label key={k}>{l}<input className="input mono" type="number" min={1} value={limits[k]} onChange={(e) => setLimits({ ...limits, [k]: +e.target.value || 1 })} /></label>
                ))}
              </div>
            </div>
          </>
        )}
      </div>
      <div className={s.ft}>
        <button type="button" className="btn ghost" onClick={onClose}>Cancel</button>
        <button type="button" className="btn solid" disabled={busy || entry.length === 0 || !task.trim()} onClick={() => { void go(); }}><Icon name="play" size={15} />{busy ? "Starting…" : "Start"}</button>
      </div>
    </dialog>
  );
}
