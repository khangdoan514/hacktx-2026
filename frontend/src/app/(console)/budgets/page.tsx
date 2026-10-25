"use client";

import PageHeader from "@/components/shell/PageHeader";
import { compact, usd } from "@/lib/format";
import { useAllRuns, useStore } from "@/lib/store";
import s from "../table.module.css";

/** Per-run ceilings come from each definition; spend is what finished runs reported. */
export default function BudgetsPage() {
  const runs = useAllRuns();
  const { agents } = useStore();
  const total = runs.reduce((a, r) => a + r.plan.usage.cost, 0);
  const tokens = runs.reduce((a, r) => a + r.plan.usage.tokensIn + r.plan.usage.tokensOut, 0);
  const rows = agents.map((a) => {
    const own = runs.filter((r) => r.plan.agent === a.id);
    const cost = own.reduce((x, r) => x + r.plan.usage.cost, 0);
    const tok = own.reduce((x, r) => x + r.plan.usage.tokensIn + r.plan.usage.tokensOut, 0);
    return { a, n: own.length, cost, tok, avg: own.length ? cost / own.length : 0 };
  });

  return (
    <>
      <PageHeader title="Budgets" sub="Each definition caps tokens, cost and time per run; a run that would go over stops with BUDGET_EXCEEDED. Spend is what the service recorded." />
      <section className="card" style={{ display: "flex", gap: 32, padding: "18px 20px", flexWrap: "wrap" }}>
        <div><small className="muted mono">Total spend</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{usd(total)}</div></div>
        <div><small className="muted mono">Tokens</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{compact(tokens)}</div></div>
        <div><small className="muted mono">Runs</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{runs.length}</div></div>
      </section>
      <section className="card">
        <div className={s.scroll}>
          <table className={s.table}>
            <thead><tr><th>Agent</th><th className={s.r}>Runs</th><th className={s.r}>Tokens</th><th className={s.r}>Spend</th><th className={s.r}>Avg / run</th><th className={s.r}>Max tokens / run</th><th className={s.r}>Max cost / run</th><th className={s.r}>Timeout</th></tr></thead>
            <tbody>
              {rows.map(({ a, n, cost, tok, avg }) => (
                <tr key={a.id}>
                  <td>{a.name} <span className="mono muted">{a.id}@{a.version}</span></td>
                  <td className={`${s.r} mono`}>{n}</td>
                  <td className={`${s.r} mono`}>{compact(tok)}</td>
                  <td className={`${s.r} mono`}>{usd(cost)}</td>
                  <td className={`${s.r} mono`}>{n ? usd(avg) : "—"}</td>
                  <td className={`${s.r} mono`}>{a.maxTokens ? compact(a.maxTokens) : "—"}</td>
                  <td className={`${s.r} mono`}>{a.maxCostUsd ? usd(a.maxCostUsd) : "—"}</td>
                  <td className={`${s.r} mono`}>{a.limits.seconds ? `${a.limits.seconds}s` : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
