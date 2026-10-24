"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { agentById, entryAgents } from "@/config/agents";
import { scenarios, triggerKinds, triggers as seed, type Trigger } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { useStore } from "@/lib/store";
import s from "./triggers.module.css";

const defaultConfig: Record<Trigger["kind"], string> = {
  api: "POST /v1/agent-runs", schedule: "Mon 08:00 CT", webhook: "POST /hooks/new", slack: "slash command", cli: "nmc3 run",
};

export default function TriggersPage() {
  const { triggersOn, setTrigger, startRun, toast, openAgent } = useStore();
  const router = useRouter();
  const [list, setList] = useState<Trigger[]>(seed);
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState({ agent: entryAgents[0].id, kind: "schedule" as Trigger["kind"], label: "" });

  const fire = (t: Trigger) => {
    const task = scenarios.find((x) => x.agent === t.agent && !x.fail)?.task ?? "Run";
    startRun({ agent: t.agent, task, trigger: triggerKinds[t.kind].name });
    toast(`${t.label} fired`);
    router.push("/");
  };

  const add = () => {
    if (!draft.label.trim()) return;
    const t: Trigger = { id: `trg_${Date.now()}`, agent: draft.agent, kind: draft.kind, label: draft.label.trim(), config: defaultConfig[draft.kind], enabled: true, lastFired: "never" };
    setList((l) => [...l, t]);
    setTrigger(t.id, true);
    setAdding(false);
    setDraft({ ...draft, label: "" });
    toast("Trigger added");
  };

  return (
    <>
      <PageHeader
        title="Triggers"
        sub="What starts a run: API calls, schedules, webhooks, Slack or the CLI."
        code={{
          cli: `nmc3 triggers add schedule \\\n  --agent idea-consultant --cron "0 8 * * MON" --tz America/Chicago\nnmc3 triggers add webhook --agent vendor-orchestrator --path /hooks/procurement\nnmc3 triggers list`,
          sdk: `await nmc3.triggers.create({\n  agent: "idea-consultant",\n  kind: "schedule",\n  cron: "0 8 * * MON",\n  input: { task: "Weekly innovation brief" },\n});`,
        }}
        actions={<button type="button" className="btn solid" onClick={() => setAdding((a) => !a)}><Icon name="plus" size={15} />Add trigger</button>}
      />

      {adding && (
        <section className={`card ${s.form} rise`}>
          <label>when
            <select className="input" value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value as Trigger["kind"] })}>
              {Object.entries(triggerKinds).map(([k, v]) => <option key={k} value={k}>{v.name}</option>)}
            </select>
          </label>
          <label>run
            <select className="input" value={draft.agent} onChange={(e) => setDraft({ ...draft, agent: e.target.value })}>
              {entryAgents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
          </label>
          <label className={s.grow}>name
            <input className="input" placeholder="e.g. Monthly vendor review" value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} onKeyDown={(e) => e.key === "Enter" && add()} />
          </label>
          <button type="button" className="btn solid" onClick={add}>Save</button>
        </section>
      )}

      <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
        <ul className={`${s.list} rows`}>
          {list.map((t) => {
            const a = agentById(t.agent)!;
            const on = triggersOn[t.id] ?? t.enabled;
            return (
              <li key={t.id} className={on ? "" : s.off}>
                <span className={s.kind}><Icon name={triggerKinds[t.kind].icon} size={17} /></span>
                <div style={{ minWidth: 0 }}>
                  <b>{t.label}</b>
                  <small>{triggerKinds[t.kind].name} · <span className="mono">{t.config}</span></small>
                </div>
                <button type="button" className={s.agent} onClick={() => openAgent(a.id)}><Icon name={a.icon} size={14} />{a.name}</button>
                <span className={`${s.last} mono`}>{t.lastFired}</span>
                <button type="button" className="btn sm" onClick={() => fire(t)} disabled={!on}><Icon name="play" size={13} />Run now</button>
                <button type="button" className="switch" role="switch" aria-checked={on} aria-label={`${t.label} enabled`} onClick={() => setTrigger(t.id, !on)} />
              </li>
            );
          })}
        </ul>
      </section>
    </>
  );
}
