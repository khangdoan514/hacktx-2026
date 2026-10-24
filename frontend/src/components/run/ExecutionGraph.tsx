"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { agentById } from "@/config/agents";
import { toolById } from "@/config/tools";
import Icon from "@/components/ui/Icon";
import { layoutRun, type Box } from "@/lib/layout";
import { useStore } from "@/lib/store";
import type { RunView } from "@/lib/types";
import s from "./ExecutionGraph.module.css";

type EdgeState = "" | "on" | "done" | "deny" | "fail";

const curve = (a: Box, b: Box) => {
  const x1 = a.x + a.w, y1 = a.y + a.h / 2, x2 = b.x, y2 = b.y + b.h / 2, dx = (x2 - x1) * 0.55;
  return `M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
};

/** A quiet base line plus a bright dash that travels along it while work flows. */
function Edge({ d, state, hidden }: { d: string; state: EdgeState; hidden?: boolean }) {
  const flow = useRef<SVGPathElement>(null);
  useLayoutEffect(() => {
    const el = flow.current;
    if (!el) return;
    const L = el.getTotalLength(), dash = Math.min(26, L * 0.3);
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) { el.style.strokeDasharray = "none"; return; }
    el.style.strokeDasharray = `${dash} ${L}`;
    const anim = el.animate([{ strokeDashoffset: L + dash }, { strokeDashoffset: 0 }], { duration: Math.max(700, L * 4.2), iterations: Infinity });
    return () => anim.cancel();
  }, [d]);
  return (
    <g style={{ opacity: hidden ? 0 : 1, transition: "opacity .4s" }}>
      <path className={`${s.edge} ${state ? s[state] : ""}`} d={d} />
      <path ref={flow} className={`${s.flow} ${state === "on" ? s.on : ""}`} d={d} />
    </g>
  );
}

export default function ExecutionGraph({ view }: { view: RunView }) {
  const { openAgent } = useStore();
  const layout = useMemo(() => layoutRun(view.plan), [view.plan]);
  const stage = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(1);
  const [width, setWidth] = useState(layout.width);

  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const fit = () => {
      setWidth(el.clientWidth);
      setScale(Math.max(0.5, Math.min(1.12, (el.clientWidth - 16) / layout.width)));
    };
    fit();
    const ro = new ResizeObserver(fit);
    ro.observe(el);
    return () => ro.disconnect();
  }, [layout.width]);

  const boxOf = (id: string) => layout.nodes.find((n) => n.id === id)!.box;
  const rootId = view.plan.agent;
  const anyChildRunning = layout.nodes.some((n) => n.kind === "agent" && n.parent && view.agents[n.id]?.st === "RUNNING");
  const rootSt = view.agents[rootId]?.st;
  const rootEdge: EdgeState = rootSt === "RUNNING" ? (anyChildRunning ? "done" : "on") : rootSt === "SUCCEEDED" ? "done" : rootSt === "FAILED" ? "fail" : "";

  return (
    <div className={s.stage} ref={stage} style={{ height: Math.round(layout.height * scale) }}>
      <div
        className={s.canvas}
        style={{ width: layout.width, height: layout.height, transform: `scale(${scale})`, marginLeft: Math.max(0, (width - layout.width * scale) / 2) }}
      >
        {layout.cols.map((c) => <div key={c.label + c.x} className={s.colh} style={{ left: c.x }}>{c.label}</div>)}
        <svg className={s.wires} aria-hidden>
          <Edge d={curve(layout.you, boxOf(rootId))} state={rootEdge} />
          {layout.nodes.map((n) => {
            if (!n.parent) return null;
            const d = curve(boxOf(n.parent), n.box);
            if (n.kind === "agent") {
              const st = view.agents[n.id]?.st;
              return <Edge key={n.id} d={d} state={st === "RUNNING" ? "on" : st === "SUCCEEDED" ? "done" : st === "FAILED" ? "fail" : ""} />;
            }
            const c = view.calls.find((x) => x.id === n.id)!;
            const st: EdgeState = c.denied ? "deny" : c.st === "RUNNING" ? "on" : "done";
            return <Edge key={n.id} d={d} state={st} hidden={!c.visible} />;
          })}
        </svg>

        <div className={`${s.node} ${s.you}`} style={pos(layout.you)} title="You · POST /v1/agent-runs">KD</div>

        {layout.nodes.map((n) => {
          if (n.kind === "agent") {
            const def = agentById(n.id)!;
            const v = view.agents[n.id] ?? { st: "PENDING", now: "Waiting" };
            return (
              <button key={n.id} type="button" className={s.node} data-st={v.st} style={pos(n.box)} title={`${def.id}@${def.version}`}
                onClick={() => openAgent(n.id, { run: view.plan.id })}>
                <span className={s.ico}><Icon name={def.icon} size={18} /><span className={s.badge}><Icon name="check" size={9} strokeWidth={3.5} /></span></span>
                <b>{def.name}</b>
                <span className={s.now}>{v.now}</span>
              </button>
            );
          }
          const c = view.calls.find((x) => x.id === n.id)!;
          const tool = toolById(c.name);
          return (
            <div key={n.id} className={`${s.tool} ${c.visible ? "" : s.hide} ${c.denied ? s.deny : ""} ${c.st === "RUNNING" ? s.running : ""}`} style={pos(n.box)} title={c.name}>
              <span className={s.t}>{c.denied ? "×" : tool?.mark ?? "→"}</span>
              <span className={s.nm}>{c.name.replace(/^serpapi\./, "")}</span>
              <span className={s.rs}>{c.text}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

const pos = (b: Box) => ({ left: b.x, top: b.y, width: b.w, height: b.h });
