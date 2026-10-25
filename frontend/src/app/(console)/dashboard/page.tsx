"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import NewRunDialog from "@/components/run/NewRunDialog";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import StatePill from "@/components/ui/StatePill";
import { automationApi } from "@/lib/api";
import { ago, compact, secs, usd } from "@/lib/format";
import { useAllRuns, useNow, useStore } from "@/lib/store";
import type { State } from "@/lib/types";
import s from "../table.module.css";

const FILTERS: ("ALL" | State)[] = ["ALL", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED"];

export default function DashboardPage() {
  const runs = useAllRuns();
  const now = useNow();
  const { agentById, agents, openAgent, serviceError } = useStore();
  const [st, setSt] = useState<"ALL" | State>("ALL");
  const [agent, setAgent] = useState("all");
  const [dialog, setDialog] = useState(false);
  // Run ID -> automation name, so runs a schedule started are labeled.
  const [scheduled, setScheduled] = useState<Record<string, string>>({});
  useEffect(() => {
    let stop = false;
    const load = () => automationApi.firings().then(({ firings }) => {
      if (stop) return;
      setScheduled(Object.fromEntries(firings.filter((f) => f.run_id).map((f) => [f.run_id!, f.automation_name ?? f.automation_id])));
    }).catch(() => {});
    void load();
    const poll = setInterval(() => { void load(); }, 10000);
    return () => { stop = true; clearInterval(poll); };
  }, [runs.length]);
  const rows = runs.filter((r) => (st === "ALL" || r.state === st) && (agent === "all" || r.plan.agent === agent));
  const picks = [...agents].sort((x, y) => Number(y.kind === "orchestrator") - Number(x.kind === "orchestrator"));
  const listed = picks.length ? picks.map((a) => a.id) : [...new Set(runs.map((r) => r.plan.agent))];
  const empty = serviceError && runs.length === 0 ? serviceError : runs.length === 0 ? "No runs yet." : "No runs match.";

  return (
    <>
      <PageHeader title="Dashboard" sub="Agent runs recorded by the service, newest first. Open one to watch or replay it." actions={<button type="button" className="btn solid" onClick={() => setDialog(true)}><Icon name="play" size={15} />New run</button>} />
      <div className={s.filters}>
        <div className={s.seg}>
          {FILTERS.map((f) => (
            <button key={f} type="button" aria-pressed={st === f} onClick={() => setSt(f)}>
              {f === "ALL" ? "All" : f.toLowerCase()} <span>{f === "ALL" ? runs.length : runs.filter((r) => r.state === f).length}</span>
            </button>
          ))}
        </div>
        <select className="input" value={agent} onChange={(e) => setAgent(e.target.value)}>
          <option value="all">All agents</option>
          {listed.map((id) => <option key={id} value={id}>{agentById(id)?.name ?? id}</option>)}
        </select>
      </div>
      <section className="card">
        {rows.length === 0 ? (
          <div className={s.empty}>
            <p className="muted">{empty}</p>
            {runs.length === 0 && picks.length > 0 && (
              <div className={s.picks}>
                {picks.map((a) => (
                  <button key={a.id} type="button" className="btn" onClick={() => openAgent(a.id)}>{a.name}</button>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className={s.scroll}>
            <table className={s.table}>
              <thead><tr><th>Run</th><th>Agent</th><th>Task</th><th>State</th><th className={s.r}>Tokens</th><th className={s.r}>Cost</th><th className={s.r}>Time</th><th className={s.r}>Started</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.plan.id}>
                    <td><Link href={`/live-run/${r.plan.id}`} className="mono">{r.plan.id}</Link></td>
                    <td>{agentById(r.plan.agent)?.name ?? r.plan.agent}</td>
                    <td className={s.task}>
                      {scheduled[r.plan.id] && <Link href="/automations" className="tag accent" style={{ marginRight: 6 }} title={`Started by ${scheduled[r.plan.id]}`}><Icon name="clock" size={11} />{scheduled[r.plan.id]}</Link>}
                      <Link href={`/live-run/${r.plan.id}`}>{r.plan.task}</Link>
                    </td>
                    <td><StatePill state={r.state} />{r.plan.error && r.state === "FAILED" && <span className="tag err" style={{ marginLeft: 6 }}>{r.plan.error}</span>}</td>
                    <td className={`${s.r} mono`}>{compact(r.plan.usage.tokensIn + r.plan.usage.tokensOut)}</td>
                    <td className={`${s.r} mono`}>{usd(r.plan.usage.cost)}</td>
                    <td className={`${s.r} mono`}>{r.state === "RUNNING" ? "…" : secs(r.elapsed)}</td>
                    <td className={`${s.r} muted`}>{ago(r.plan.createdAt, now)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
      <NewRunDialog open={dialog} onClose={() => setDialog(false)} />
    </>
  );
}
