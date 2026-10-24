import Icon from "@/components/ui/Icon";
import type { RunView } from "@/lib/types";
import s from "./Output.module.css";

const R = 20, C = 2 * Math.PI * R;

function Ring({ v }: { v: number }) {
  return (
    <div className={s.ring}>
      <svg viewBox="0 0 48 48">
        <circle className={s.bg} cx="24" cy="24" r={R} />
        <circle className={s.fg} cx="24" cy="24" r={R} style={{ "--c": C, strokeDasharray: C, strokeDashoffset: C * (1 - v) } as React.CSSProperties} />
      </svg>
      {v.toFixed(2)}
    </div>
  );
}

export function OutputBadge({ view }: { view: RunView }) {
  if (view.state !== "SUCCEEDED") return null;
  return <span className={s.ok}><Icon name="badge-check" size={14} />schema valid</span>;
}

export default function Output({ view }: { view: RunView }) {
  if (view.state === "CANCELLED") return <div className={s.empty}><Icon name="circle-slash" />Cancelled</div>;
  if (view.state === "FAILED") return <div className={`${s.empty} ${s.fail}`}><Icon name="shield-x" /><span className="mono">{view.plan.error}</span></div>;
  if (view.state !== "SUCCEEDED") {
    return (
      <div className={s.skel}>
        {[0, 1, 2].map((i) => (
          <div key={i}><i className={s.sq} /><span><i style={{ width: "46%" }} /><i style={{ width: "72%", opacity: 0.6 }} /></span><i className={s.c} /></div>
        ))}
      </div>
    );
  }
  return (
    <ol className={s.ideas}>
      {[...view.plan.ideas].sort((a, b) => b.score - a.score).map((d, k) => (
        <li key={d.title} className={s.idea} style={{ animationDelay: `${k * 90}ms` }}>
          <span className={s.rk}>0{k + 1}</span>
          <div style={{ minWidth: 0 }}>
            <b>{d.title}</b>
            <p>{d.why}</p>
            <div className={s.src}>{d.sources.map((x) => <span key={x}>{x}</span>)}</div>
          </div>
          <Ring v={d.score} />
        </li>
      ))}
    </ol>
  );
}
