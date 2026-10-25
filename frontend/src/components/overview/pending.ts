"use client";

import { useCallback, useEffect, useState } from "react";
import { readToken } from "@/product/api";

export type Pending = { id: string; run_id: string; tool_id: string; tool_version: string; state: string; expires_at: string };

async function call(path: string, init?: RequestInit) {
  const headers = new Headers(init?.headers);
  const token = readToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init?.body) headers.set("Content-Type", "application/json");
  return fetch(`/agent-api${path}`, { cache: "no-store", ...init, headers });
}

/** Approvals waiting on a person. `mounted` is false when the server has no approvals API. */
export function usePendingApprovals(intervalMs = 5000) {
  const [items, setItems] = useState<Pending[]>([]);
  const [mounted, setMounted] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const res = await call("/approvals/pending");
      if (res.status === 404 || res.status === 403) { setMounted(res.status === 403); setItems([]); return; }
      if (!res.ok) return;
      const body = (await res.json()) as { approvals?: Pending[] };
      setMounted(true);
      setItems((body.approvals ?? []).filter((a) => a.state === "WAITING"));
    } catch { /* keep the last list */ }
  }, []);

  useEffect(() => {
    const first = window.setTimeout(() => { void refresh(); }, 0);
    const id = window.setInterval(refresh, intervalMs);
    return () => { window.clearTimeout(first); window.clearInterval(id); };
  }, [refresh, intervalMs]);

  const decide = useCallback(async (id: string, outcome: "APPROVED" | "DENIED") => {
    const res = await call(`/approvals/${encodeURIComponent(id)}/decision`, { method: "POST", body: JSON.stringify({ outcome }) });
    if (!res.ok) {
      const body = (await res.json().catch(() => null)) as { error?: { message?: string }; message?: string } | null;
      throw new Error(body?.error?.message ?? body?.message ?? `Decision was not recorded (${res.status})`);
    }
    await refresh();
  }, [refresh]);

  return { items, mounted, decide, refresh };
}
