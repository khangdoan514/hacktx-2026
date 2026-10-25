"use client";

import Link from "next/link";
import PageHeader from "@/components/shell/PageHeader";
import { useAllRuns, useStore } from "@/lib/store";
import s from "../table.module.css";

/** What each definition allows. The service denies everything else, and records the denials. */
export default function GuardrailsPage() {
  const runs = useAllRuns();
  const { agents } = useStore();
  const stopped = runs.filter((r) => r.plan.error || r.plan.final === "FAILED").map((r) => r.plan);
  const denied = runs.flatMap((r) => r.calls.filter((c) => c.denied && c.visible).map((c) => ({ run: r.plan.id, call: c })));

  return (
    <>
      <PageHeader title="Guardrails" sub="What each definition allows, and what the service actually denied or stopped on recorded runs." />
      <section className="card">
        <div className={s.scroll}>
          <table className={s.table}>
            <thead><tr><th>Agent</th><th>Allowed tools</th><th>Allowed delegates</th><th className={s.r}>Max delegations</th><th className={s.r}>Max tool calls</th></tr></thead>
            <tbody>
              {agents.map((a) => (
                <tr key={a.id}>
                  <td>{a.name} <span className="mono muted">{a.id}@{a.version}</span></td>
                  <td>{a.tools.length ? a.tools.map((t) => <span key={t} className="tag accent" style={{ marginRight: 4 }}>{t}</span>) : <span className="muted">none</span>}</td>
                  <td>{a.delegates.length ? a.delegates.map((d) => <span key={d} className="tag" style={{ marginRight: 4 }}>{d}</span>) : <span className="muted">none</span>}</td>
                  <td className={`${s.r} mono`}>{a.limits.delegations}</td>
                  <td className={`${s.r} mono`}>{a.limits.tools}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <section className="card">
        <div className="ch"><h2>Denied actions</h2><span className="n">{denied.length}</span></div>
        {denied.length === 0 ? <p className="muted" style={{ padding: 20 }}>No run has had a tool call denied.</p> : (
          <ul style={{ listStyle: "none", margin: 0, padding: "8px 20px 16px" }}>
            {denied.map(({ run, call }) => (
              <li key={call.id} style={{ padding: "8px 0", borderBottom: "1px solid var(--line)" }}>
                <span className="tag err">{call.name}</span> by {call.agent} in <Link href={`/runs/${run}`} className="mono">{run}</Link>
              </li>
            ))}
          </ul>
        )}
      </section>
      <section className="card">
        <div className="ch"><h2>Runs stopped by the service</h2><span className="n">{stopped.length}</span></div>
        {stopped.length === 0 ? <p className="muted" style={{ padding: 20 }}>No run has been stopped by a limit or a failed check.</p> : (
          <ul style={{ listStyle: "none", margin: 0, padding: "8px 20px 16px" }}>
            {stopped.map((p) => (
              <li key={p.id} style={{ padding: "8px 0", borderBottom: "1px solid var(--line)" }}>
                <span className="tag err">{p.error ?? "FAILED"}</span> <Link href={`/runs/${p.id}`} className="mono">{p.id}</Link> {p.errorDetail}
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
  );
}
