"use client";

import Link from "next/link";
import { agentById } from "@/config/agents";
import { stateColor } from "@/components/ui/StatePill";
import { ago, secs } from "@/lib/format";
import { useNow } from "@/lib/store";
import type { RunView } from "@/lib/types";
import s from "./RunList.module.css";

/** Compact run rows. `current` highlights the run being viewed. */
export default function RunList({ runs, current, showAgent }: { runs: RunView[]; current?: string; showAgent?: boolean }) {
  const now = useNow();
  return (
    <ul className={s.runs}>
      {runs.map((r) => (
        <li key={r.plan.id} className={r.plan.id === current ? s.cur : ""}>
          <Link href={`/runs/${r.plan.id}`}>
            <i className={`dot ${stateColor(r.state)} ${r.state === "RUNNING" ? "live" : ""}`} />
            <div style={{ minWidth: 0 }}>
              <b>{r.plan.task}</b>
              <small>{r.plan.id}{showAgent && ` · ${agentById(r.plan.agent)?.name}`} · {ago(r.plan.createdAt, now)}</small>
            </div>
            <span className={`${s.t} ${r.state === "FAILED" || r.state === "CANCELLED" ? "c-err" : ""}`}>
              {r.state === "FAILED" ? r.plan.error?.toLowerCase() : r.state === "CANCELLED" ? "cancelled" : r.state === "RUNNING" ? "…" : secs(r.elapsed)}
            </span>
          </Link>
        </li>
      ))}
    </ul>
  );
}
