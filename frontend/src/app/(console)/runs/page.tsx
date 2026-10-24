"use client";

import Link from "next/link";
import { useState } from "react";
import { agentById, entryAgents } from "@/config/agents";
import PageHeader from "@/components/shell/PageHeader";
import StatePill from "@/components/ui/StatePill";
import { ago, compact, secs, usd } from "@/lib/format";
import { useAllRuns, useNow } from "@/lib/store";
import type { State } from "@/lib/types";
import s from "../table.module.css";

const FILTERS: ("ALL" | State)[] = ["ALL", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED"];

export default function RunsPage() {
  const runs = useAllRuns();
  const now = useNow();
  const [st, setSt] = useState<"ALL" | State>("ALL");
  const [agent, setAgent] = useState("all");
  const rows = runs.filter((r) => (st === "ALL" || r.state === st) && (agent === "all" || r.plan.agent === agent));

  return (
    <>
      <PageHeader
        title="Runs"
        sub="Every run, newest first. Open one to replay it."
        code={{
          cli: "nmc3 runs list --limit 20\nnmc3 runs list --state FAILED --agent idea-consultant\nnmc3 runs watch <run-id>",
          sdk: `const runs = await nmc3.runs.list({ state: "FAILED", limit: 20 });\nfor (const r of runs) console.log(r.id, r.state, r.error?.code);`,
        }}
      />
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
          {entryAgents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
      </div>
      <section className="card">
        <div className={s.scroll}>
          <table className={s.table}>
            <thead><tr><th>Run</th><th>Agent</th><th>Task</th><th>State</th><th className={s.r}>Turns</th><th className={s.r}>Tokens</th><th className={s.r}>Cost</th><th className={s.r}>Time</th><th>Trigger</th><th className={s.r}>Started</th></tr></thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.plan.id}>
                  <td><Link href={`/runs/${r.plan.id}`} className="mono">{r.plan.id}</Link></td>
                  <td>{agentById(r.plan.agent)?.name}</td>
                  <td className={s.task}><Link href={`/runs/${r.plan.id}`}>{r.plan.task}</Link></td>
                  <td><StatePill state={r.state} />{r.plan.error && r.state === "FAILED" && <span className="tag err" style={{ marginLeft: 6 }}>{r.plan.error}</span>}</td>
                  <td className={`${s.r} mono`}>{r.used.turns}/{r.plan.limits.turns}</td>
                  <td className={`${s.r} mono`}>{compact(r.tokens)}</td>
                  <td className={`${s.r} mono`}>{usd(r.cost)}</td>
                  <td className={`${s.r} mono`}>{r.state === "RUNNING" ? "…" : secs(r.elapsed)}</td>
                  <td className="muted">{r.plan.trigger}</td>
                  <td className={`${s.r} muted`}>{ago(r.plan.createdAt, now)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
