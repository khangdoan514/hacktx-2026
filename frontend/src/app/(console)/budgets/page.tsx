"use client";

import { agents, entryAgents } from "@/config/agents";
import { budgets, models } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { compact, usd } from "@/lib/format";
import { useAllRuns, useNow, useStore } from "@/lib/store";
import s from "./budgets.module.css";

const tone = (p: number) => (p >= 1 ? "err" : p >= 0.75 ? "warn" : "");

export default function BudgetsPage() {
  const runs = useAllRuns();
  const { caps, setCap, openAgent } = useStore();
  const day = useNow() - 86_400_000;
  const today = runs.filter((r) => r.plan.createdAt >= day);

  // Spend per agent comes from that agent's own model turns, including specialists.
  const spendBy = (id: string) => today.reduce((a, r) => a + r.turns.filter((t) => t.agent === id).reduce((b, t) => b + t.cost, 0), 0);
  const tokensBy = (id: string) => today.reduce((a, r) => a + r.turns.filter((t) => t.agent === id).reduce((b, t) => b + t.tokensIn + t.tokensOut, 0), 0);
  const teamSpend = (id: string) => today.filter((r) => r.plan.agent === id).reduce((a, r) => a + r.cost, 0);
  const todayTotal = today.reduce((a, r) => a + r.cost, 0);
  const month = budgets.spentThisMonthUsd + todayTotal;
  const maxAgent = Math.max(0.001, ...agents.map((a) => spendBy(a.id)));
  const byModel = Object.keys(models).map((m) => ({ m, v: agents.filter((a) => a.model === m).reduce((x, a) => x + spendBy(a.id), 0) }));

  return (
    <>
      <PageHeader
        title="Budgets"
        sub="Spend caps per workspace and per agent. A run that would go over stops with BUDGET_EXCEEDED."
        code={{
          cli: "nmc3 budgets set --workspace --monthly 500\nnmc3 budgets set idea-consultant --daily 40\nnmc3 usage --since 24h --by agent",
          sdk: `await nmc3.budgets.set({ agent: "idea-consultant", dailyUsd: 40 });\nconst usage = await nmc3.usage({ since: "24h", groupBy: "agent" });`,
        }}
      />

      <section className={`card ${s.month} rise`}>
        <div>
          <small>this month</small>
          <b>{usd(month)} <span>/ {usd(budgets.monthlyUsd)}</span></b>
          <div className={`bar ${tone(month / budgets.monthlyUsd)}`} style={{ height: 6, marginTop: 10 }}><i style={{ width: `${Math.min(100, (month / budgets.monthlyUsd) * 100)}%` }} /></div>
        </div>
        <div><small>last 24h</small><b>{usd(todayTotal)}</b></div>
        <div><small>runs · 24h</small><b>{today.length}</b></div>
        <div><small>tokens · 24h</small><b>{compact(today.reduce((a, r) => a + r.tokens, 0))}</b></div>
      </section>

      <div className={s.grid}>
        <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
          <div className="ch"><h2>Daily caps</h2><span className="n">per entry agent, whole team included</span></div>
          <ul className={`${s.caps} rows`}>
            {entryAgents.map((a) => {
              const spent = teamSpend(a.id), cap = caps[a.id] ?? 0, p = cap ? spent / cap : 0;
              return (
                <li key={a.id}>
                  <button type="button" className={s.name} onClick={() => openAgent(a.id)}><Icon name={a.icon} size={15} />{a.name}</button>
                  <div className={s.meter}>
                    <div className={`bar ${tone(p)}`} style={{ height: 6 }}><i style={{ width: `${Math.min(100, p * 100)}%` }} /></div>
                    <small className="mono">{usd(spent)} of {usd(cap)} today</small>
                  </div>
                  <label className={s.cap}>$<input className="input mono" type="number" min={1} value={cap} onChange={(e) => setCap(a.id, Math.max(1, +e.target.value || 1))} /></label>
                </li>
              );
            })}
          </ul>
          <div className={s.limits}>
            <small>per-run limits</small>
            {entryAgents.map((a) => (
              <div key={a.id}>
                <span>{a.name}</span>
                <span className="mono">{a.limits.turns} turns · {a.limits.tools} tools · {a.limits.delegations} delegations · {a.limits.parallel} parallel · {a.limits.seconds}s</span>
              </div>
            ))}
          </div>
        </section>

        <section className="card rise" style={{ "--d": "100ms" } as React.CSSProperties}>
          <div className="ch"><h2>Spend by agent</h2><span className="n">24h</span></div>
          <ul className={`${s.spend} rows`}>
            {[...agents].sort((a, b) => spendBy(b.id) - spendBy(a.id)).map((a) => (
              <li key={a.id}>
                <button type="button" className={s.name} onClick={() => openAgent(a.id, { tab: "activity" })}><Icon name={a.icon} size={15} />{a.name}</button>
                <div className="bar"><i style={{ width: `${(spendBy(a.id) / maxAgent) * 100}%` }} /></div>
                <span className="mono">{usd(spendBy(a.id))}</span>
                <span className="mono muted">{compact(tokensBy(a.id))}</span>
              </li>
            ))}
          </ul>
          <div className={s.models}>
            {byModel.map(({ m, v }) => <div key={m}><small>{models[m].label}</small><b className="mono">{usd(v)}</b></div>)}
          </div>
        </section>
      </div>
    </>
  );
}
