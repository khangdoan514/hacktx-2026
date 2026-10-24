"use client";

import Link from "next/link";
import { useState } from "react";
import { agentById, agents } from "@/config/agents";
import { models } from "@/config/workspace";
import { actionOf } from "@/components/agent/AgentDrawer";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { ago, compact, usd } from "@/lib/format";
import { useAllRuns, useNow, useStore } from "@/lib/store";
import type { RunView, Turn } from "@/lib/types";
import s from "./prompts.module.css";

export default function PromptsPage() {
  const runs = useAllRuns();
  const now = useNow();
  const { guardrailsOn, openAgent } = useStore();
  const [agent, setAgent] = useState("all");
  const [sel, setSel] = useState<string | null>(null);

  const rows: { run: RunView; t: Turn }[] = [];
  for (const run of runs.slice(0, 10)) {
    for (const t of [...run.turns].reverse()) if (agent === "all" || t.agent === agent) rows.push({ run, t });
  }
  const picked = rows.find((r) => r.t.id === sel) ?? rows[0];
  const avgMs = rows.length ? Math.round(rows.reduce((a, r) => a + r.t.ms, 0) / rows.length) : 0;

  return (
    <>
      <PageHeader
        title="Prompts"
        sub="Every model turn: what the agent was told, what it saw and what it decided."
        code={{
          cli: "nmc3 prompts tail --agent market-scout\nnmc3 prompts get <run-id> --turn 3 --json\nnmc3 prompts diff idea-consultant@0.9.0 idea-consultant@1.0.0",
          sdk: `for await (const turn of nmc3.prompts.stream({ agent: "market-scout" })) {\n  console.log(turn.agent, turn.action, turn.tokens);\n}`,
        }}
      />
      <div className={`${s.summary} rise`}>
        <div><small>turns</small><b>{rows.length}</b></div>
        <div><small>tokens</small><b>{compact(rows.reduce((a, r) => a + r.t.tokensIn + r.t.tokensOut, 0))}</b></div>
        <div><small>avg latency</small><b>{avgMs}ms</b></div>
        <div><small>cost</small><b>{usd(rows.reduce((a, r) => a + r.t.cost, 0))}</b></div>
      </div>
      <div className={s.filters}>
        <select className="input" value={agent} onChange={(e) => { setAgent(e.target.value); setSel(null); }}>
          <option value="all">All agents</option>
          {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
        {guardrailsOn.pii && <span className="tag ok"><Icon name="shield" size={12} />personal data redacted</span>}
      </div>

      <div className={s.split}>
        <section className="card rise">
          <ul className={`${s.list} rows`}>
            {rows.slice(0, 60).map(({ run, t }) => {
              const a = agentById(t.agent)!;
              return (
                <li key={t.id}>
                  <button type="button" aria-pressed={picked?.t.id === t.id} onClick={() => setSel(t.id)}>
                    <span className={s.ico}><Icon name={a.icon} size={14} /></span>
                    <div style={{ minWidth: 0 }}>
                      <b>{a.name} <span className="mono muted">#{t.n}</span></b>
                      <small className="mono">{actionOf(t.output)}</small>
                    </div>
                    <span className="mono muted">{compact(t.tokensIn + t.tokensOut)}</span>
                    <span className="mono muted">{ago(run.plan.createdAt + t.at, now)}</span>
                  </button>
                </li>
              );
            })}
          </ul>
        </section>

        {picked && (
          <section className={`card ${s.detail} rise`} style={{ "--d": "60ms" } as React.CSSProperties}>
            <div className="ch">
              <div>
                <h2>{agentById(picked.t.agent)?.name} · turn {picked.t.n}</h2>
                <Link href={`/runs/${picked.run.plan.id}`} className="mono muted" style={{ fontSize: 12 }}>{picked.run.plan.id}</Link>
              </div>
              <button type="button" className="right btn sm" onClick={() => openAgent(picked.t.agent, { run: picked.run.plan.id, tab: "prompts" })}>Agent</button>
            </div>
            <div className={s.kv}>
              <div><small>model</small><b>{models[agentById(picked.t.agent)!.model]?.label}</b></div>
              <div><small>tokens</small><b className="mono">{compact(picked.t.tokensIn)} → {compact(picked.t.tokensOut)}</b></div>
              <div><small>latency</small><b className="mono">{picked.t.ms}ms</b></div>
              <div><small>cost</small><b className="mono">{usd(picked.t.cost)}</b></div>
            </div>
            <Block k="system" v={picked.t.system} />
            <Block k="input" v={picked.t.input} />
            <Block k="output" v={pretty(picked.t.output)} />
          </section>
        )}
      </div>
    </>
  );
}

const pretty = (v: string) => { try { return JSON.stringify(JSON.parse(v), null, 2); } catch { return v; } };

function Block({ k, v }: { k: string; v: string }) {
  return <div className={s.block}><small>{k}</small><pre>{v}</pre></div>;
}
