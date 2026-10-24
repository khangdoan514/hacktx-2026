"use client";

import { runAgents } from "@/lib/simulate";
import type { EventType, RunView } from "@/lib/types";
import s from "./Timeline.module.css";

const tone: Partial<Record<EventType, string>> = {
  RUN_STARTED: s.run, AGENT_STARTED: s.run, TOOL_STARTED: s.run,
  TOOL_SUCCEEDED: s.ok, AGENT_SUCCEEDED: s.ok, RUN_SUCCEEDED: s.ok,
  TOOL_FAILED: s.err, DELEGATION_REJECTED: s.err, AGENT_FAILED: s.err, RUN_FAILED: s.err, RUN_CANCELLED: s.err,
};

/** One lane per agent: how long it ran, with every event as a dot. Replaces a text event log. */
export default function Timeline({ view }: { view: RunView }) {
  const live = view.state === "RUNNING";
  const t = view.elapsed / 1000;
  const span = live ? Math.max(20, Math.ceil((t + 3) / 5) * 5) : Math.max(5, Math.ceil(t));
  const x = (ms: number) => `${(Math.min(ms / 1000, span) / span) * 100}%`;
  const step = span <= 20 ? 5 : span <= 40 ? 10 : 15;
  const ticks: number[] = [];
  for (let v = 0; v <= span + 0.01; v += step) ticks.push(v);

  return (
    <div className={s.lanes}>
      {runAgents(view.plan.agent).map((a) => {
        const v = view.agents[a.id];
        const end = v?.end ?? view.elapsed;
        return (
          <div key={a.id} className={s.ln}>
            <span>{a.name}</span>
            <div className={s.track}>
              {v?.start !== undefined && (
                <i className={`${s.bar} ${v.end === undefined && live ? s.barRun : ""} ${v.st === "FAILED" || v.st === "CANCELLED" ? s.barErr : ""}`}
                  style={{ left: x(v.start), width: x(end - v.start) }} />
              )}
              {view.events.filter((e) => e.agent === a.id).map((e) => (
                <i key={e.seq} className={`${s.ev} ${tone[e.type] ?? ""}`} style={{ left: x(e.at) }}
                  data-tip={`#${e.seq} ${e.type}${e.detail ? " · " + e.detail : ""}`} />
              ))}
            </div>
          </div>
        );
      })}
      <div className={s.axis}>
        <span />
        <div className={s.ticks}>{ticks.map((v) => <span key={v} style={{ left: `${(v / span) * 100}%` }}>{v}s</span>)}</div>
      </div>
    </div>
  );
}
