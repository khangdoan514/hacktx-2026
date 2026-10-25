"use client";

import Link from "next/link";
import PageHeader from "@/components/shell/PageHeader";
import { useRunDetails, useStore } from "@/lib/store";

/** The instructions each published definition gives its agent. Edit them from Definitions. */
export default function PromptsPage() {
  const { agents, openAgent } = useStore();
  const runs = useRunDetails();
  return (
    <>
      <PageHeader title="Prompts" sub="The instructions each agent receives. Change them by publishing a new definition version." actions={<Link href="/definitions" className="btn">Open definitions</Link>} />
      {agents.length === 0 && <p className="muted">The agent service has no agents.</p>}
      {agents.map((a) => {
        const last = runs.flatMap((r) => r.turns.filter((t) => t.agent === a.id).map((t) => ({ run: r.id, t }))).sort((x, y) => y.t.at - x.t.at)[0];
        return (
        <section key={a.id} className="card rise">
          <div className="ch">
            <h2>{a.name}</h2><span className="n">{a.id}@{a.version}</span>
            <div className="right"><button type="button" className="btn sm" onClick={() => openAgent(a.id, { tab: "prompts" })}>See turns</button></div>
          </div>
          <p style={{ padding: "14px 20px", color: "var(--ink-2)", whiteSpace: "pre-wrap" }}>{a.instructions || "No instructions recorded."}</p>
          {last && (
            <div style={{ padding: "0 20px 16px" }}>
              <small className="mono muted">Latest turn · <Link href={`/live-run/${last.run}`}>{last.run}</Link> · {last.t.action ?? "—"}</small>
              <pre className="mono" style={{ margin: "6px 0 0", fontSize: 12, color: "var(--ink-2)", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{last.t.output || last.t.input}</pre>
            </div>
          )}
        </section>
        );
      })}
    </>
  );
}
