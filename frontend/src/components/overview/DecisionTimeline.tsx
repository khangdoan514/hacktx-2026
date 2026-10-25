"use client";

import { useState } from "react";
import type { FlowState } from "@/lib/flows/useFlowSimulation";
import type { Pending } from "./pending";
import s from "./overview.module.css";

const clock = (at: number) => new Date(at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });

interface Props {
  flow: FlowState;
  pending: Pending[];
  approvalsMounted: boolean;
  decide: (id: string, outcome: "APPROVED" | "DENIED") => Promise<void>;
  onToast: (m: string) => void;
}

/** Agent decisions plus real approvals waiting on a person, newest first. */
export default function DecisionTimeline({ flow, pending, approvalsMounted, decide, onToast }: Props) {
  const [busy, setBusy] = useState<string | null>(null);

  async function send(id: string, outcome: "APPROVED" | "DENIED") {
    setBusy(id);
    try { await decide(id, outcome); onToast(outcome === "APPROVED" ? "Approved" : "Denied"); }
    catch (err) { onToast(err instanceof Error ? err.message : "Decision failed"); }
    finally { setBusy(null); }
  }

  const empty = pending.length === 0 && flow.decisions.length === 0;
  return (
    <section className="card" aria-labelledby="dec-h">
      <div className="ch"><h2 id="dec-h">Decisions</h2><span className="n">{pending.length + flow.decisions.length}</span></div>
      {empty && <p className={s.empty}>{approvalsMounted ? "No decisions yet." : "The approvals API is not mounted on this server, so there is nothing to approve."}</p>}
      <div className={s.tl}>
        {pending.map((a) => (
          <div key={a.id} className={s.ev}>
            <span className={s.time}>needs you</span>
            <div>
              <div className={s.what}>Allow {a.tool_id}@{a.tool_version} to write?</div>
              <div className={s.meta}><span className="tag warn">Waiting</span><span>run {a.run_id}</span><span>expires {new Date(a.expires_at).toLocaleTimeString()}</span></div>
              <div className={s.acts}>
                <button type="button" className="btn solid sm" disabled={busy === a.id} onClick={() => void send(a.id, "APPROVED")}>Approve</button>
                <button type="button" className="btn sm" disabled={busy === a.id} onClick={() => void send(a.id, "DENIED")}>Deny</button>
              </div>
            </div>
          </div>
        ))}
        {flow.decisions.map((d) => (
          <div key={d.id} className={s.ev} style={{ ["--fc" as string]: `var(--f${d.color})` }}>
            <span className={s.time}>{clock(d.at)}</span>
            <div>
              <div className={s.what}>{d.what}</div>
              <div className={s.meta}>
                <span className="code">{d.code}</span><span>{d.agent}</span>
                <span className={`tag ${d.needsYou && !d.resolved ? "warn" : ""}`}>{d.outcome}</span>
                {d.conf !== undefined && <span className="num">conf {d.conf.toFixed(2)}</span>}
              </div>
              {d.why && <p className={s.why}>{d.why}</p>}
              {d.needsYou && !d.resolved && (
                <div className={s.acts}>
                  <button type="button" className="btn solid sm" onClick={() => { flow.approve(d.code); onToast("Approved the ranking"); }}>Approve ranking</button>
                  <button type="button" className="btn sm" onClick={() => { flow.anotherRound(d.code); onToast("Asked the panel for another round"); }}>Run another round</button>
                </div>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
