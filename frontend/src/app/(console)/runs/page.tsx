"use client";

import Link from "next/link";
import { useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import StatePill from "@/components/ui/StatePill";
import { ago, compact, secs, usd } from "@/lib/format";
import { useAllRuns, useNow, useStore } from "@/lib/store";
import type { State } from "@/lib/types";
import s from "../table.module.css";

const FILTERS: ("ALL" | State)[] = ["ALL", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED"];

export default function RunsPage() {
  const runs = useAllRuns();
  const now = useNow();
  const { agentById } = useStore();
  const [st, setSt] = useState<"ALL" | State>("ALL");
  const [agent, setAgent] = useState("all");
  const rows = runs.filter((r) => (st === "ALL" || r.state === st) && (agent === "all" || r.plan.agent === agent));
  const rootAgents = [...new Set(runs.map((r) => r.plan.agent))];

  return (
    <>
      <PageHeader title="Runs" sub="Every run the agent service has recorded, newest first. Open one to replay it." />
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
          {rootAgents.map((id) => <option key={id} value={id}>{agentById(id)?.name ?? id}</option>)}
        </select>
      </div>
      <section className="card">
        {rows.length === 0 ? <p className="muted" style={{ padding: 20 }}>No runs match.</p> : (
          <div className={s.scroll}>
            <table className={s.table}>
              <thead><tr><th>Run</th><th>Agent</th><th>Task</th><th>State</th><th className={s.r}>Tokens</th><th className={s.r}>Cost</th><th className={s.r}>Time</th><th className={s.r}>Started</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.plan.id}>
                    <td><Link href={`/runs/${r.plan.id}`} className="mono">{r.plan.id}</Link></td>
                    <td>{agentById(r.plan.agent)?.name ?? r.plan.agent}</td>
                    <td className={s.task}><Link href={`/runs/${r.plan.id}`}>{r.plan.task}</Link></td>
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
    </>
  );
}
