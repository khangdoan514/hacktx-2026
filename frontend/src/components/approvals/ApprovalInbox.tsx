"use client";

import { useCallback, useEffect, useState } from "react";
import s from "./inbox.module.css";

export type PendingApproval = {
  id: string;
  run_id: string;
  tool_id: string;
  tool_version: string;
  requester_id: string;
  state: string;
  input_digest: string;
  expires_at: string;
};

type Load =
  | { status: "loading" }
  | { status: "ready"; approvals: PendingApproval[] }
  | { status: "error"; message: string };

async function readJSON(response: Response) {
  const text = await response.text();
  if (!text) return {};
  try {
    return JSON.parse(text) as { error?: { message?: string }; approvals?: PendingApproval[] };
  } catch {
    return {};
  }
}

export default function ApprovalInbox() {
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [pendingID, setPendingID] = useState<string | null>(null);
  const [notice, setNotice] = useState("");

  const refresh = useCallback(async () => {
    setLoad({ status: "loading" });
    try {
      const response = await fetch("/agent-api/approvals/pending", {
        cache: "no-store",
        headers: { "X-NMC3-Console": "1" },
      });
      const body = await readJSON(response);
      if (response.status === 404) {
        setLoad({ status: "error", message: "The approval API is not mounted on this server." });
        return;
      }
      if (!response.ok) {
        setLoad({ status: "error", message: body.error?.message ?? `Could not load approvals (${response.status}).` });
        return;
      }
      setLoad({ status: "ready", approvals: (body.approvals ?? []).filter((item) => item.state === "WAITING") });
    } catch {
      setLoad({ status: "error", message: "Could not reach the approval API." });
    }
  }, []);

  useEffect(() => {
    const timer = window.setTimeout(() => { void refresh(); }, 0);
    return () => window.clearTimeout(timer);
  }, [refresh]);

  async function decide(id: string, outcome: "APPROVED" | "DENIED") {
    setPendingID(id);
    setNotice("");
    try {
      const response = await fetch(`/agent-api/approvals/${encodeURIComponent(id)}/decision`, {
        method: "POST",
        cache: "no-store",
        headers: { "Content-Type": "application/json", "X-NMC3-Console": "1" },
        body: JSON.stringify({ outcome }),
      });
      const body = await readJSON(response);
      if (!response.ok) {
        setNotice(body.error?.message ?? `Decision was not recorded (${response.status}).`);
        return;
      }
      setNotice(outcome === "APPROVED" ? "Approved." : "Denied.");
      await refresh();
    } catch {
      setNotice("Could not reach the approval API.");
    } finally {
      setPendingID(null);
    }
  }

  return (
    <section className="card rise" aria-busy={load.status === "loading"}>
      <div className="ch">
        <h2>Waiting</h2>
        <span className="n">{load.status === "ready" ? load.approvals.length : ""}</span>
        <div className="right">
          <button type="button" className="btn sm" onClick={() => { void refresh(); }}>Refresh</button>
        </div>
      </div>
      {load.status === "loading" && <p className={s.empty}>Loading approval requests.</p>}
      {load.status === "error" && <p className={s.empty} role="alert">{load.message}</p>}
      {load.status === "ready" && load.approvals.length === 0 && (
        <p className={s.empty}>No protected writes are waiting.</p>
      )}
      {load.status === "ready" && load.approvals.length > 0 && (
        <div className={s.scroll}>
          <table className={s.table}>
            <caption className={s.caption}>Protected writes waiting for a decision</caption>
            <thead>
              <tr>
                <th scope="col">Tool</th>
                <th scope="col">Run</th>
                <th scope="col">Requester</th>
                <th scope="col">Expires</th>
                <th scope="col"><span className={s.sr}>Decision</span></th>
              </tr>
            </thead>
            <tbody>
              {load.approvals.map((item) => {
                const busy = pendingID === item.id;
                return (
                  <tr key={item.id}>
                    <td>{item.tool_id}<span className={s.ver}> {item.tool_version}</span></td>
                    <td className="mono">{item.run_id}</td>
                    <td>{item.requester_id}</td>
                    <td>{formatWhen(item.expires_at)}</td>
                    <td className={s.actions}>
                      <button type="button" className="btn sm solid" disabled={busy} onClick={() => { void decide(item.id, "APPROVED"); }}>
                        Approve
                      </button>
                      <button type="button" className="btn sm" disabled={busy} onClick={() => { void decide(item.id, "DENIED"); }}>
                        Deny
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {notice && <p className={s.notice} role="status">{notice}</p>}
    </section>
  );
}

function formatWhen(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
