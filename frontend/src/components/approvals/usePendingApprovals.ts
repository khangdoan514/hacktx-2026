"use client";

import { useCallback, useEffect, useState } from "react";

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

type Body = { error?: { message?: string }; message?: string; approvals?: PendingApproval[] };

function headers(json = false) {
  const h = new Headers();
  const token = window.localStorage.getItem("nmc3_token");
  if (token) h.set("Authorization", `Bearer ${token}`);
  if (json) h.set("Content-Type", "application/json");
  return h;
}

async function readJSON(response: Response): Promise<Body> {
  const text = await response.text();
  if (!text) return {};
  try { return JSON.parse(text) as Body; } catch { return {}; }
}

/** Approvals waiting on a decision. Polls quietly; an unmounted API just means none. */
export function usePendingApprovals(intervalMs = 20_000) {
  const [approvals, setApprovals] = useState<PendingApproval[]>([]);

  const refresh = useCallback(async () => {
    try {
      const response = await fetch("/agent-api/approvals/pending", { cache: "no-store", headers: headers() });
      if (!response.ok) return setApprovals([]);
      const body = await readJSON(response);
      setApprovals((body.approvals ?? []).filter((a) => a.state === "WAITING"));
    } catch {
      setApprovals([]);
    }
  }, []);

  useEffect(() => {
    const first = window.setTimeout(() => { void refresh(); }, 0);
    const timer = window.setInterval(() => { void refresh(); }, intervalMs);
    return () => { window.clearTimeout(first); window.clearInterval(timer); };
  }, [refresh, intervalMs]);

  return { approvals, refresh };
}

export async function decideApproval(id: string, outcome: "APPROVED" | "DENIED"): Promise<string | null> {
  try {
    const response = await fetch(`/agent-api/approvals/${encodeURIComponent(id)}/decision`, {
      method: "POST", cache: "no-store", headers: headers(true), body: JSON.stringify({ outcome }),
    });
    if (response.ok) return null;
    const body = await readJSON(response);
    return body.error?.message ?? body.message ?? `Decision was not recorded (${response.status}).`;
  } catch {
    return "Could not reach the approval API.";
  }
}
