"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import AutomationDialog from "@/components/automations/AutomationDialog";
import PageHeader from "@/components/shell/PageHeader";
import { useEmployee } from "@/components/shell/ConsoleGate";
import Icon from "@/components/ui/Icon";
import StatePill from "@/components/ui/StatePill";
import { automationApi, type Automation, type AutomationFiring } from "@/lib/api";
import { describeSchedule, relative, when } from "@/lib/automations";
import { compact, usd } from "@/lib/format";
import { canManageAutomations } from "@/lib/permissions";
import { useNow, useStore } from "@/lib/store";
import type { State } from "@/lib/types";
import t from "../table.module.css";
import s from "./automations.module.css";

const POLL_MS = 5000;

function Outcome({ f }: { f?: AutomationFiring }) {
  if (!f) return <span className="muted">Not run yet</span>;
  if (!f.run_id) return <span className="tag err" title={f.error}>did not start</span>;
  const state = (f.state === "WAITING" ? "PENDING" : f.state ?? "PENDING") as State;
  return <Link href={`/live-run/${f.run_id}`} title={f.run_error || f.run_id}><StatePill state={state} /></Link>;
}

export default function AutomationsPage() {
  const employee = useEmployee();
  const manage = canManageAutomations(employee?.roles);
  const { agentById, toast, refresh } = useStore();
  const now = useNow();
  const [items, setItems] = useState<Automation[] | null>(null);
  const [firings, setFirings] = useState<AutomationFiring[]>([]);
  const [error, setError] = useState("");
  const [dialog, setDialog] = useState(false);
  const [editing, setEditing] = useState<Automation | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [list, recent] = await Promise.all([automationApi.list(), automationApi.firings()]);
      setItems(list.automations);
      setFirings(recent.firings);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Automations could not be loaded");
      setItems((v) => v ?? []);
    }
  }, []);

  useEffect(() => {
    void load();
    const poll = setInterval(() => { void load(); }, POLL_MS);
    return () => clearInterval(poll);
  }, [load]);

  const act = async (id: string, work: () => Promise<unknown>, done: string) => {
    setBusy(id);
    try {
      await work();
      toast(done);
    } catch (err) {
      toast(err instanceof Error ? err.message : "That did not work");
    } finally {
      setBusy(null);
      await load();
    }
  };

  const runNow = (a: Automation) => act(a.id, async () => {
    const { firing } = await automationApi.run(a.id);
    await refresh();
    return firing;
  }, `${a.name} started`);
  const toggle = (a: Automation) => act(a.id, () => automationApi.update(a.id, { enabled: !a.enabled }), a.enabled ? "Paused" : "Resumed");
  const remove = (a: Automation) => {
    if (!confirm(`Delete “${a.name}” and its history? Runs it started stay on the dashboard.`)) return;
    void act(a.id, () => automationApi.remove(a.id), "Automation deleted");
  };
  const openNew = () => { setEditing(null); setDialog(true); };

  return (
    <>
      <PageHeader
        title="Automations"
        sub="Agents that start on a schedule. Each firing starts a normal run you can open, watch and replay."
        code={{
          cli: `curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \\\n  http://127.0.0.1:8000/agent-api/automations \\\n  -d '{"name":"Weekly digest","schedule":"0 9 * * 1","timezone":"America/Chicago","request":{...}}'`,
          sdk: `await fetch("/agent-api/automations", {\n  method: "POST",\n  headers: { Authorization: \`Bearer \${token}\`, "Content-Type": "application/json" },\n  body: JSON.stringify({ name: "Weekly digest", schedule: "0 9 * * 1", timezone: "America/Chicago", request }),\n});`,
        }}
        actions={manage && <button type="button" className="btn solid" onClick={openNew}><Icon name="plus" size={15} />New automation</button>}
      />

      <section className="card">
        {items === null ? <div className={t.empty}><p className="muted">Loading…</p></div>
          : items.length === 0 ? (
            <div className={t.empty}>
              <p className="muted">{error || "No automations yet. Schedule an agent to run every hour, every weekday morning, or on any cron schedule."}</p>
              {manage && !error && <div className={t.picks}><button type="button" className="btn" onClick={openNew}><Icon name="clock" size={15} />Schedule an agent</button></div>}
            </div>
          ) : (
            <div className={t.scroll}>
              <table className={t.table}>
                <thead><tr><th>Automation</th><th>Agent</th><th>Schedule</th><th>Next</th><th>Last run</th>{manage && <th className={t.r}>Actions</th>}</tr></thead>
                <tbody>
                  {items.map((a) => (
                    <tr key={a.id} className={a.enabled ? undefined : s.off}>
                      <td className={t.task}>
                        <div className={s.name}>{a.name}</div>
                        <div className={s.sub}>{a.created_by_email || a.created_by}</div>
                      </td>
                      <td>{agentById(a.request.agent_id)?.name ?? a.request.agent_id}</td>
                      <td>
                        <div>{describeSchedule(a.schedule)}</div>
                        <div className={`${s.sub} mono`}>{a.schedule} · {a.timezone}</div>
                      </td>
                      <td title={a.next_run_at ? when(a.next_run_at, a.timezone) : undefined}>
                        {a.enabled ? (a.next_run_at ? relative(a.next_run_at, now) : <span className="muted">never</span>) : <span className="tag">paused</span>}
                      </td>
                      <td>
                        <Outcome f={a.last_firing} />
                        {a.last_firing && <span className={s.sub} style={{ marginLeft: 8 }}>{relative(a.last_firing.fired_at, now)}</span>}
                      </td>
                      {manage && (
                        <td className={t.r}>
                          <div className={s.actions}>
                            <button type="button" className="ibtn" disabled={busy === a.id} onClick={() => { void runNow(a); }} data-tip="Run now" aria-label={`Run ${a.name} now`}><Icon name="play" size={15} /></button>
                            <button type="button" className="ibtn" disabled={busy === a.id} onClick={() => { void toggle(a); }} data-tip={a.enabled ? "Pause" : "Resume"} aria-label={a.enabled ? `Pause ${a.name}` : `Resume ${a.name}`}><Icon name={a.enabled ? "square" : "zap"} size={15} /></button>
                            <button type="button" className="ibtn" disabled={busy === a.id} onClick={() => { setEditing(a); setDialog(true); }} data-tip="Edit" aria-label={`Edit ${a.name}`}><Icon name="settings" size={15} /></button>
                            <button type="button" className="ibtn" disabled={busy === a.id} onClick={() => remove(a)} data-tip="Delete" aria-label={`Delete ${a.name}`}><Icon name="x" size={15} /></button>
                          </div>
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
      </section>

      <h2 className={s.h2}>Activity</h2>
      <section className="card">
        {firings.length === 0 ? <div className={t.empty}><p className="muted">Nothing has fired yet. Runs started by a schedule or by Run now appear here.</p></div> : (
          <div className={t.scroll}>
            <table className={t.table}>
              <thead><tr><th>Fired</th><th>Automation</th><th>Trigger</th><th>Run</th><th>Outcome</th><th className={t.r}>Tokens</th><th className={t.r}>Cost</th></tr></thead>
              <tbody>
                {firings.map((f) => (
                  <tr key={f.id}>
                    <td title={when(f.fired_at)}>{relative(f.fired_at, now)}</td>
                    <td>{f.automation_name ?? f.automation_id}</td>
                    <td><span className={`tag ${f.trigger === "schedule" ? "accent" : ""}`}>{f.trigger}</span></td>
                    <td>{f.run_id ? <Link href={`/live-run/${f.run_id}`} className="mono" title={f.run_id}>{f.run_id.slice(0, 16)}…</Link> : <span className={s.err}>{f.error}</span>}</td>
                    <td><Outcome f={f} />{f.run_error && <span className={s.sub} style={{ marginLeft: 8 }}>{f.run_error}</span>}</td>
                    <td className={`${t.r} mono`}>{f.usage ? compact(f.usage.input_tokens + f.usage.output_tokens) : "—"}</td>
                    <td className={`${t.r} mono`}>{f.usage ? usd(f.usage.cost_usd) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <AutomationDialog open={dialog} editing={editing} onClose={() => setDialog(false)} onSaved={() => { void load(); }} />
    </>
  );
}
