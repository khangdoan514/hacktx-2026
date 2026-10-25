"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { agentById, entryAgents } from "@/config/agents";
import { scenarios } from "@/config/workspace";
import Icon from "@/components/ui/Icon";
import { useStore } from "@/lib/store";
import type { Limits } from "@/lib/types";
import s from "./NewRunDialog.module.css";

const FIELDS: [keyof Limits, string][] = [["turns", "turns"], ["tools", "tools"], ["delegations", "delegations"], ["parallel", "parallel"], ["seconds", "seconds"]];

/** Same request as POST /v1/agent-runs: agent, task and per-run limits. */
export default function NewRunDialog({ open, onClose, agent: initial }: { open: boolean; onClose: () => void; agent?: string }) {
  const ref = useRef<HTMLDialogElement>(null);
  const { startRun, toast } = useStore();
  const router = useRouter();
  const [agent, setAgent] = useState(initial ?? entryAgents[0].id);
  const [task, setTask] = useState("");
  const [limits, setLimits] = useState<Limits>(entryAgents[0].limits);

  const pick = (id: string) => {
    setAgent(id);
    setLimits(agentById(id)!.limits);
    const opts = scenarios.filter((x) => x.agent === id && !x.fail);
    setTask(opts[Math.floor(Math.random() * opts.length)]?.task ?? "");
  };

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      pick(initial ?? agent);
      d.showModal();
    }
    if (!open && d.open) d.close();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  const go = () => {
    if (!task.trim()) return;
    startRun({ agent, task: task.trim(), limits, trigger: "Manual" });
    onClose();
    toast("Run started");
    router.push("/runs");
  };

  return (
    <dialog ref={ref} className={s.modal} onClose={onClose} onClick={(e) => e.target === ref.current && onClose()}>
      <div className={s.fm}>
        <h3>New run</h3>
        <label>agent
          <select className="input" value={agent} onChange={(e) => pick(e.target.value)}>
            {entryAgents.map((a) => <option key={a.id} value={a.id}>{a.name} · {a.id}@{a.version}</option>)}
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
      </div>
      <div className={s.ft}>
        <button type="button" className="btn ghost" onClick={onClose}>Cancel</button>
        <button type="button" className="btn solid" onClick={go}><Icon name="play" size={15} />Start</button>
      </div>
    </dialog>
  );
}
