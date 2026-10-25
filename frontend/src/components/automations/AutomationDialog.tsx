"use client";

import { useEffect, useRef, useState } from "react";
import Icon from "@/components/ui/Icon";
import { automationApi, type Automation } from "@/lib/api";
import { limitsFromWire, limitsToWire, takesTask } from "@/lib/agents";
import { PRESETS, localZone, when } from "@/lib/automations";
import { taskOf } from "@/lib/live";
import { useStore } from "@/lib/store";
import type { Limits } from "@/lib/types";
import s from "@/components/run/NewRunDialog.module.css";
import own from "./AutomationDialog.module.css";

const FIELDS: [keyof Limits, string][] = [["turns", "turns"], ["tools", "tools"], ["delegations", "delegations"], ["parallel", "parallel"], ["seconds", "seconds"]];
const PERIODS = ["week", "month", "quarter", "project"] as const;
const takesPeriod = (input: string | undefined) => Boolean(input?.includes('"period"'));
const CUSTOM = "custom";

/**
 * Create or edit an automation: the same agent, task and limits as a new run,
 * plus a cron schedule. Saving stores the run request; the agent service
 * starts it each time the schedule comes due.
 */
export default function AutomationDialog({ open, onClose, onSaved, editing }: {
  open: boolean; onClose: () => void; onSaved: (a: Automation) => void; editing?: Automation | null;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const { agents, agentById, toast } = useStore();
  const entry = agents.filter(takesTask);
  const [name, setName] = useState("");
  const [agent, setAgent] = useState("");
  const [task, setTask] = useState("");
  const [period, setPeriod] = useState<(typeof PERIODS)[number]>("month");
  const [preset, setPreset] = useState(PRESETS[2].cron);
  const [cron, setCron] = useState(PRESETS[2].cron);
  const [zone, setZone] = useState("UTC");
  const [limits, setLimits] = useState<Limits>({ turns: 8, tools: 4, delegations: 2, parallel: 2, seconds: 60 });
  const [next, setNext] = useState<string[]>([]);
  const [scheduleError, setScheduleError] = useState("");
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
      if (editing) {
        const input = (editing.request.input ?? {}) as { period?: (typeof PERIODS)[number] };
        setName(editing.name);
        setAgent(editing.request.agent_id);
        setTask(taskOf(editing.request.input));
        if (input.period) setPeriod(input.period);
        setCron(editing.schedule);
        setPreset(PRESETS.some((p) => p.cron === editing.schedule) ? editing.schedule : CUSTOM);
        setZone(editing.timezone || "UTC");
        setLimits(limitsFromWire(editing.request.limits));
      } else {
        setName(""); setTask(""); setPreset(PRESETS[2].cron); setCron(PRESETS[2].cron); setZone(localZone());
        if (entry[0]) pick(entry[0].id);
      }
      d.showModal();
    }
    if (!open && d.open) d.close();
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps

  // The agent service parses the schedule; show its next times as the user types.
  useEffect(() => {
    if (!open || !cron.trim()) return;
    const t = setTimeout(() => {
      automationApi.preview(cron, zone)
        .then((r) => { setNext(r.next); setScheduleError(r.next.length ? "" : "This schedule never runs."); })
        .catch((err: unknown) => { setNext([]); setScheduleError(err instanceof Error ? err.message : "Invalid schedule"); });
    }, 250);
    return () => clearTimeout(t);
  }, [cron, zone, open]);

  const def = agentById(agent);
  const valid = Boolean(name.trim() && def && task.trim() && cron.trim() && !scheduleError);

  const save = async () => {
    if (!valid || !def || busy) return;
    setBusy(true);
    try {
      const request = {
        agent_id: def.id, agent_version: def.version,
        input: takesPeriod(def.input) ? { task: task.trim(), period } : { task: task.trim() },
        limits: limitsToWire(limits, { max_tokens: def.maxTokens || undefined, max_cost_usd: def.maxCostUsd || undefined }),
      };
      const body = { name: name.trim(), schedule: cron.trim(), timezone: zone, request };
      const saved = editing ? await automationApi.update(editing.id, body) : await automationApi.create({ ...body, enabled: true });
      toast(editing ? "Automation saved" : "Automation created");
      onSaved(saved.automation);
      onClose();
    } catch (err) {
      toast(err instanceof Error ? err.message : "The automation could not be saved");
    } finally {
      setBusy(false);
    }
  };

  return (
    <dialog ref={ref} className={`${s.modal} ${own.modal}`} onClose={onClose} onClick={(e) => e.target === ref.current && onClose()}>
      <div className={`${s.fm} ${own.body}`}>
        <h3>{editing ? "Edit automation" : "New automation"}</h3>
        {entry.length === 0 ? <p className="muted">The agent service has no agent that accepts a task.</p> : (
          <>
            <label>name<input className="input" value={name} maxLength={120} placeholder="Weekly vendor digest" onChange={(e) => setName(e.target.value)} /></label>
            <label>agent
              <select className="input" value={agent} onChange={(e) => pick(e.target.value)}>
                {entry.map((a) => <option key={a.id} value={a.id}>{a.name} · {a.id}@{a.version}</option>)}
              </select>
            </label>
            <label>task<textarea className="input" value={task} onChange={(e) => setTask(e.target.value)} placeholder="What the agent should do each time it runs" /></label>
            {takesPeriod(def?.input) && (
              <label>period
                <select className="input" value={period} onChange={(e) => setPeriod(e.target.value as (typeof PERIODS)[number])}>
                  {PERIODS.map((item) => <option key={item} value={item}>{item}</option>)}
                </select>
              </label>
            )}
            <label>schedule
              <select className="input" value={preset} onChange={(e) => { setPreset(e.target.value); if (e.target.value !== CUSTOM) setCron(e.target.value); }}>
                {PRESETS.map((p) => <option key={p.cron} value={p.cron}>{p.label}</option>)}
                <option value={CUSTOM}>Custom cron…</option>
              </select>
            </label>
            {preset === CUSTOM && (
              <label>cron · minute hour day month weekday
                <input className="input mono" value={cron} onChange={(e) => setCron(e.target.value)} placeholder="*/30 9-17 * * 1-5" />
              </label>
            )}
            <label>time zone<input className="input mono" value={zone} onChange={(e) => setZone(e.target.value.trim())} /></label>
            <p className="muted" style={{ fontSize: 12, margin: 0 }} aria-live="polite">
              {scheduleError ? <span style={{ color: "var(--err)" }}>{scheduleError}</span>
                : next.length > 0 ? <>Next: {next.slice(0, 3).map((t) => when(t, zone)).join(" · ")}</> : " "}
            </p>
            <div>
              <label style={{ marginBottom: 6 }}>limits per run</label>
              <div className={s.lims}>
                {FIELDS.map(([k, l]) => (
                  <label key={k}>{l}<input className="input mono" type="number" min={1} value={limits[k]} onChange={(e) => setLimits({ ...limits, [k]: +e.target.value || 1 })} /></label>
                ))}
              </div>
            </div>
          </>
        )}
      </div>
      <div className={`${s.ft} ${own.ft}`}>
        <button type="button" className="btn ghost" onClick={onClose}>Cancel</button>
        <button type="button" className="btn solid" disabled={busy || !valid} onClick={() => { void save(); }}>
          <Icon name="clock" size={15} />{busy ? "Saving…" : editing ? "Save" : "Create"}
        </button>
      </div>
    </dialog>
  );
}
