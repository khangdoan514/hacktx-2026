"use client";

import { useState } from "react";
import { decideApproval, usePendingApprovals } from "./usePendingApprovals";
import s from "./inbox.module.css";

/** Protected writes a run is waiting on. Renders nothing unless this run has some. */
export default function RunApprovals({ runId }: { runId: string }) {
  const { approvals, refresh } = usePendingApprovals();
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const mine = approvals.filter((a) => a.run_id === runId);
  if (mine.length === 0 && !notice) return null;

  async function decide(id: string, outcome: "APPROVED" | "DENIED") {
    setBusy(id);
    const problem = await decideApproval(id, outcome);
    setNotice(problem ?? (outcome === "APPROVED" ? "Approved." : "Denied."));
    await refresh();
    setBusy(null);
  }

  return (
    <section className={`card rise ${s.banner}`} aria-label="Approvals">
      {mine.map((a) => (
        <div key={a.id} className={s.item}>
          <div>
            <b>Waiting for your OK</b>
            <p>This run wants to use <span className="mono">{a.tool_id}</span>. Someone other than the requester ({a.requester_id}) needs to decide.</p>
          </div>
          <div className={s.actions}>
            <button type="button" className="btn sm solid" disabled={busy === a.id} onClick={() => { void decide(a.id, "APPROVED"); }}>Approve</button>
            <button type="button" className="btn sm" disabled={busy === a.id} onClick={() => { void decide(a.id, "DENIED"); }}>Deny</button>
          </div>
        </div>
      ))}
      {notice && <p className={s.notice} role="status">{notice}</p>}
    </section>
  );
}
