"use client";

import PageHeader from "@/components/shell/PageHeader";
import { compact, usd } from "@/lib/format";
import { useRunDetails, useStore } from "@/lib/store";
import s from "../table.module.css";

/** Per-run ceilings come from each definition; spend is summed from the model turns each agent reported. */
export default function BudgetsPage() {
  const runs = useRunDetails();
  const { agents } = useStore();
  const total = runs.reduce((a, r) => a + r.usage.cost, 0);
  const tokens = runs.reduce((a, r) => a + r.usage.tokensIn + r.usage.tokensOut, 0);
  const rows = agents.map((a) => {
    const turns = runs.flatMap((r) => r.turns.filter((t) => t.agent === a.id && t.id !== `${r.id}-usage`));
    const ran = runs.filter((r) => r.invocations.some((i) => i.agent === a.id)).length;
    const cost = turns.reduce((x, t) => x + t.cost, 0);
    const tok = turns.reduce((x, t) => x + t.tokensIn + t.tokensOut, 0);
    // The service reports usage for the orchestrator's session only; a specialist that only ever ran delegated has none of its own.
    const reported = turns.length > 0;
    return { a, n: ran, cost, tok, avg: ran ? cost / ran : 0, reported };
  });

  return (
    <>
      <PageHeader title="Budgets" sub="Each definition caps tokens, cost and time per run; a run that would go over stops with BUDGET_EXCEEDED. Spend is summed from the model turns the service reported. Delegated specialists are billed inside their parent run, so their rows read “in parent run”." />
      <section className="card" style={{ display: "flex", gap: 32, padding: "18px 20px", flexWrap: "wrap" }}>
        <div><small className="muted mono">Total spend (all runs)</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{usd(total)}</div></div>
        <div><small className="muted mono">Tokens</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{compact(tokens)}</div></div>
        <div><small className="muted mono">Runs</small><div style={{ fontSize: 28, fontWeight: 300, color: "var(--ink)" }}>{runs.length}</div></div>
      </section>
      <section className="card">
        <div className={s.scroll}>
          <table className={s.table}>
            <thead><tr><th>Agent</th><th className={s.r}>Runs</th><th className={s.r}>Tokens</th><th className={s.r}>Spend</th><th className={s.r}>Avg / run</th><th className={s.r}>Max tokens / run</th><th className={s.r}>Max cost / run</th><th className={s.r}>Timeout</th></tr></thead>
            <tbody>
              {rows.map(({ a, n, cost, tok, avg, reported }) => (
                <tr key={a.id}>
                  <td>{a.name} <span className="mono muted">{a.id}@{a.version}</span></td>
                  <td className={`${s.r} mono`}>{n}</td>
                  <td className={`${s.r} mono`}>{reported ? compact(tok) : "—"}</td>
                  <td className={`${s.r} mono`}>{reported ? usd(cost) : n ? "in parent run" : "—"}</td>
                  <td className={`${s.r} mono`}>{reported && n ? usd(avg) : "—"}</td>
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
