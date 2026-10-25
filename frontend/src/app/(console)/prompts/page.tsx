"use client";

import Link from "next/link";
import PageHeader from "@/components/shell/PageHeader";
import { useStore } from "@/lib/store";

/** The instructions each published definition gives its agent. Edit them from Definitions. */
export default function PromptsPage() {
  const { agents, openAgent } = useStore();
  return (
    <>
      <PageHeader title="Prompts" sub="The instructions each agent receives. Change them by publishing a new definition version." actions={<Link href="/definitions" className="btn">Open definitions</Link>} />
      {agents.length === 0 && <p className="muted">The agent service has no agents.</p>}
      {agents.map((a) => (
        <section key={a.id} className="card rise">
          <div className="ch">
            <h2>{a.name}</h2><span className="n">{a.id}@{a.version}</span>
            <div className="right"><button type="button" className="btn sm" onClick={() => openAgent(a.id, { tab: "prompts" })}>See turns</button></div>
          </div>
          <p style={{ padding: "14px 20px", color: "var(--ink-2)", whiteSpace: "pre-wrap" }}>{a.instructions || "No instructions recorded."}</p>
        </section>
      ))}
    </>
  );
}
