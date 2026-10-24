"use client";

import Link from "next/link";
import { agentById } from "@/config/agents";
import { guardrails, type Guardrail } from "@/config/workspace";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { ago } from "@/lib/format";
import { useAllRuns, useNow, useStore } from "@/lib/store";
import type { EventView, RunView } from "@/lib/types";
import s from "./guardrails.module.css";

/** Which guardrail an event shows firing, if any. */
const firedBy = (e: EventView): Guardrail["id"] | null => {
  if (e.type === "DELEGATION_REJECTED") return "depth";
  if (e.type === "TOOL_FAILED" && e.detail?.includes("TOOL_NOT_ALLOWED")) return "allowlist";
  if (e.type === "RUN_FAILED" && e.detail === "BUDGET_EXCEEDED") return "limits";
  if (e.type === "RUN_FAILED" && e.detail === "OUTPUT_SCHEMA_INVALID") return "schema";
  return null;
};

export default function GuardrailsPage() {
  const runs = useAllRuns();
  const now = useNow();
  const { guardrailsOn, setGuardrail, toast, openAgent } = useStore();
  const blocks: { run: RunView; e: EventView; g: string }[] = [];
  for (const r of runs) for (const e of r.events) {
    const g = firedBy(e);
    if (g) blocks.push({ run: r, e, g });
  }
  const count = (id: string) => blocks.filter((b) => b.g === id).length;

  return (
    <>
      <PageHeader
        title="Guardrails"
        sub="Rules checked in code before any action runs. Prompts guide agents; guardrails enforce."
        code={{
          cli: "nmc3 guardrails list\nnmc3 guardrails enable citations\nnmc3 guardrails blocks --since 24h",
          sdk: `await nmc3.guardrails.update("citations", { enabled: true });\nconst blocks = await nmc3.guardrails.blocks({ since: "24h" });`,
        }}
      />
      <div className={s.grid}>
        <section className="card rise">
          <div className="ch"><h2>Rules</h2><span className="n">{guardrails.filter((g) => guardrailsOn[g.id]).length} on</span></div>
          <ul className={`${s.rules} rows`}>
            {guardrails.map((g) => {
              const on = guardrailsOn[g.id];
              const locked = g.enforcement === "runtime";
              return (
                <li key={g.id}>
                  <span className={`${s.ico} ${on ? s.on : ""}`}><Icon name={g.icon} size={17} /></span>
                  <div style={{ minWidth: 0 }}>
                    <b>{g.name}</b>
                    <p>{g.detail}</p>
                    <div className={s.meta}>
                      <span className={`tag ${locked ? "" : "accent"}`}>{locked ? "runtime" : "policy"}</span>
                      {g.code && <span className="tag">{g.code}</span>}
                      {count(g.id) > 0 && <span className="tag err">{count(g.id)} blocked</span>}
                    </div>
                  </div>
                  <button
                    type="button" className="switch" role="switch" aria-checked={on} disabled={locked}
                    aria-label={g.name} title={locked ? "Enforced by the runtime" : undefined}
                    onClick={() => { setGuardrail(g.id, !on); toast(`${g.name} ${on ? "off" : "on"}`); }}
                  />
                </li>
              );
            })}
          </ul>
        </section>

        <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
          <div className="ch"><h2>Recent blocks</h2><span className="n">{blocks.length}</span></div>
          <ul className={`${s.feed} rows`}>
            {blocks.slice(0, 14).map(({ run, e }) => {
              const a = agentById(e.agent);
              return (
                <li key={`${run.plan.id}-${e.seq}`}>
                  <i className={s.diamond} />
                  <div style={{ minWidth: 0 }}>
                    <b className="mono">{e.type === "RUN_FAILED" ? e.detail : e.type}</b>
                    <small>
                      {a && <button type="button" onClick={() => openAgent(a.id, { run: run.plan.id, tab: "prompts" })}>{a.name}</button>}
                      {e.detail && e.type !== "RUN_FAILED" && <> · {e.detail}</>}
                    </small>
                  </div>
                  <Link href={`/runs/${run.plan.id}`} className="mono muted">{run.plan.id}</Link>
                  <span className="mono muted">{ago(run.plan.createdAt + e.at, now)}</span>
                </li>
              );
            })}
          </ul>
        </section>
      </div>
    </>
  );
}
