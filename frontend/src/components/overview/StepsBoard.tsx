import { STAGES } from "@/lib/flows/engine";
import type { FlowState } from "@/lib/flows/useFlowSimulation";
import s from "./overview.module.css";

export default function StepsBoard({ lanes }: { lanes: FlowState["lanes"] }) {
  return (
    <section className="card" aria-labelledby="live-h">
      <div className="ch"><h2 id="live-h">Live runs</h2><span className="n">{lanes.length} flows</span></div>
      <div className={s.scroll}>
        <div className={s.board}>
          <div className={`${s.lane} ${s.colh}`}>
            <div />
            {STAGES.map((st) => <div key={st.key}>{st.label}</div>)}
          </div>
          {lanes.map((lane) => (
            <div key={lane.code} className={s.lane} style={{ ["--fc" as string]: `var(--f${lane.color})` }}>
              <div className={s.lh}><span className="code">{lane.code}</span><span className={s.t}>{lane.s.title}</span></div>
              {STAGES.map((st, k) => (
                <div key={st.key} className={s.cell} data-s={lane.states[k]}>
                  <b>{lane.states[k] === "idle" ? "—" : lane.mains[k] || lane.states[k]}</b>
                  <span>{lane.subs[k]}</span>
                  {lane.states[k] === "run" && <div className={s.bar}><i style={{ width: `${Math.round(lane.p * 100)}%` }} /></div>}
                </div>
              ))}
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
