import type { FlowState } from "@/lib/flows/useFlowSimulation";
import s from "./overview.module.css";

export default function KpiStrip({ flow, waiting }: { flow: FlowState; waiting: number }) {
  const running = flow.source === "live" ? flow.active : flow.lanes.filter((l) => !l.done).length;
  const cost = flow.runs.reduce((sum, r) => sum + (r.usage?.cost_usd ?? 0), 0);
  const cells: [string, string, string?][] = [
    ["Flows running", String(running)],
    ["Waiting on you", String(waiting)],
    ["Delivered", String(flow.delivered)],
    flow.source === "live" ? ["Spend", cost.toFixed(2), "USD"] : ["Decisions logged", String(flow.decisions.length)],
  ];
  return (
    <section className={s.kpis} aria-label="Key numbers">
      {cells.map(([label, value, unit]) => (
        <div key={label} className={s.kpi}>
          <div className={s.kl}>{label}</div>
          <div className={s.kv}>{value}{unit && <small>{unit}</small>}</div>
        </div>
      ))}
    </section>
  );
}
