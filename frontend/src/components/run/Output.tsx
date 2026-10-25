import Icon from "@/components/ui/Icon";
import type { RunView } from "@/lib/types";
import s from "./Output.module.css";

export function OutputBadge({ view }: { view: RunView }) {
  if (view.state !== "SUCCEEDED") return null;
  return <span className={s.ok}><Icon name="badge-check" size={14} />schema valid</span>;
}

/** The run's structured output exactly as the service returned it. */
export default function Output({ view }: { view: RunView }) {
  if (view.state === "CANCELLED") return <div className={s.empty}><Icon name="circle-slash" />Cancelled</div>;
  if (view.state === "FAILED") return <div className={`${s.empty} ${s.fail}`}><Icon name="shield-x" /><span>{view.plan.errorDetail || view.plan.error || "Run failed"}</span></div>;
  if (view.state !== "SUCCEEDED") {
    return (
      <div className={s.skel}>
        {[0, 1, 2].map((i) => (
          <div key={i}><i className={s.sq} /><span><i style={{ width: "46%" }} /><i style={{ width: "72%", opacity: 0.6 }} /></span><i className={s.c} /></div>
        ))}
      </div>
    );
  }
  const out = view.plan.output;
  const headline = out && typeof out === "object"
    ? Object.values(out as Record<string, unknown>).find((v): v is string => typeof v === "string" && v.length > 0)
    : undefined;
  return (
    <div style={{ padding: "16px 20px", display: "flex", flexDirection: "column", gap: 12, minWidth: 0 }}>
      {headline && <p style={{ color: "var(--ink)", fontSize: 14 }}>{headline}</p>}
      <pre className="mono" style={{ margin: 0, fontSize: 12, color: "var(--ink-2)", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{JSON.stringify(out ?? null, null, 2)}</pre>
    </div>
  );
}
