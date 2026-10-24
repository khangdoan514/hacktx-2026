import type { Limits } from "@/lib/types";
import s from "./LimitMeters.module.css";

const LABEL: Record<keyof Limits, string> = { turns: "turns", tools: "tools", delegations: "delegations", parallel: "parallel", seconds: "time" };

/** Run limits as segmented meters: one segment per allowed unit. */
export default function LimitMeters({ used, limits }: { used: Limits; limits: Limits }) {
  return (
    <div className={s.lims}>
      {(Object.keys(LABEL) as (keyof Limits)[]).map((k) => {
        const u = k === "seconds" ? Math.floor(used.seconds) : used[k];
        const max = limits[k];
        return (
          <div key={k} className={s.lim}>
            <small>{LABEL[k]}<b>{k === "seconds" ? `${u}s/${max}s` : `${u}/${max}`}</b></small>
            {k === "seconds" ? (
              <div className="bar"><i style={{ width: `${Math.min(100, (used.seconds / max) * 100)}%`, transition: "width .3s linear" }} /></div>
            ) : (
              <div className="seg">
                {Array.from({ length: Math.min(max, 24) }, (_, j) => <i key={j} className={j < Math.round(Math.min(1, u / max) * Math.min(max, 24)) ? "on" : ""} />)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
