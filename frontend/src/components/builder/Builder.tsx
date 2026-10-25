"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { models, toolTiles as pickable } from "@/lib/catalog";
import { useStore } from "@/lib/store";
import { DefinitionAPIError, definitionsApi } from "@/components/definitions/api";
import PageHeader from "@/components/shell/PageHeader";
import { useEmployee } from "@/components/shell/ConsoleGate";
import Icon from "@/components/ui/Icon";
import {
  autoLayout, blankFlow, canConnect, connectedSteps, flowFromAgent, flowFromStages, newStep, presetById, presets,
  problems, sizeOf, stagesOf, templates, toSpecs, uid,
  type Flow, type FlowNode, type PresetId,
} from "@/lib/builder";
import s from "./Builder.module.css";

const STORE = "nmc3-builder-draft";

type Sel = { kind: "node" | "edge"; id: string } | null;
type Notice = { tone: "ok" | "err"; text: string } | null;
type View = { x: number; y: number; k: number };

const GRID = 22;
const clampK = (k: number) => Math.min(2, Math.max(0.3, k));

const curve = (x1: number, y1: number, x2: number, y2: number) => {
  const dx = Math.max(40, Math.abs(x2 - x1) * 0.5);
  return `M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
};

export default function Builder() {
  const router = useRouter();
  const employee = useEmployee();
  const { agentById, refreshAgents } = useStore();
  const [flow, setFlow] = useState<Flow>(blankFlow);
  const [sel, setSel] = useState<Sel>(null);
  const [link, setLink] = useState<{ from: string; x: number; y: number } | null>(null);
  const [notice, setNotice] = useState<Notice>(null);
  const [busy, setBusy] = useState(false);
  const [json, setJson] = useState(false);
  const [view, setView] = useState<View>({ x: 0, y: 0, k: 1 });
  const [fitTick, setFitTick] = useState(0);
  const [panning, setPanning] = useState(false);
  const [ready, setReady] = useState(false);
  const stage = useRef<HTMLDivElement>(null);
  const flowRef = useRef(flow);
  flowRef.current = flow;
  const viewRef = useRef(view);
  viewRef.current = view;

  // Load: ?from=<agent id> opens an existing agent once the roster has it, otherwise resume the last draft.
  const fromId = useRef<string | null>(null);
  useEffect(() => {
    const query = new URLSearchParams(window.location.search);
    fromId.current = query.get("from");
    if (!fromId.current) {
      if (query.get("template") === "single") {
        const single = flowFromStages([["custom"]]);
        setFlow(single);
        setSel({ kind: "node", id: single.nodes.find((n) => n.kind === "step")!.id });
        setReady(true);
        setFitTick((n) => n + 1);
        return;
      }
      try {
        const saved = window.localStorage.getItem(STORE);
        if (saved) setFlow(JSON.parse(saved) as Flow);
      } catch { /* a missing or corrupt draft just means a blank canvas */ }
      setReady(true);
      setFitTick((n) => n + 1);
    }
  }, []);
  useEffect(() => {
    if (ready || !fromId.current) return;
    const def = agentById(fromId.current);
    if (!def) return;
    setFlow(flowFromAgent(def, agentById));
    setReady(true);
    setFitTick((n) => n + 1);
  }, [agentById, ready]);

  useEffect(() => {
    if (!ready) return;
    try { window.localStorage.setItem(STORE, JSON.stringify(flow)); } catch { /* storage can be unavailable */ }
  }, [flow, ready]);

  // Frame everything on the canvas, as Excalidraw's "zoom to fit" does.
  useEffect(() => {
    const el = stage.current;
    if (!el || fitTick === 0) return;
    const ns = flowRef.current.nodes;
    const x0 = Math.min(...ns.map((n) => n.x)), y0 = Math.min(...ns.map((n) => n.y));
    const x1 = Math.max(...ns.map((n) => n.x + sizeOf(n).w)), y1 = Math.max(...ns.map((n) => n.y + sizeOf(n).h));
    const pad = 70, w = el.clientWidth, h = el.clientHeight;
    const k = clampK(Math.min((w - pad * 2) / (x1 - x0), (h - pad * 2) / (y1 - y0), 1));
    setView({ k, x: (w - (x1 - x0) * k) / 2 - x0 * k, y: (h - (y1 - y0) * k) / 2 - y0 * k });
  }, [fitTick]);

  // Trackpad scroll pans, pinch or ctrl+scroll zooms around the cursor.
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      const v = viewRef.current;
      if (e.ctrlKey || e.metaKey) {
        const r = el.getBoundingClientRect();
        const cx = e.clientX - r.left, cy = e.clientY - r.top;
        const k = clampK(v.k * Math.exp(-e.deltaY * 0.01));
        setView({ k, x: cx - ((cx - v.x) / v.k) * k, y: cy - ((cy - v.y) / v.k) * k });
      } else {
        setView({ ...v, x: v.x - e.deltaX, y: v.y - e.deltaY });
      }
    };
    el.addEventListener("wheel", wheel, { passive: false });
    return () => el.removeEventListener("wheel", wheel);
  }, []);

  const zoomBy = (factor: number) => {
    const el = stage.current!;
    const v = viewRef.current;
    const cx = el.clientWidth / 2, cy = el.clientHeight / 2;
    const k = clampK(v.k * factor);
    setView({ k, x: cx - ((cx - v.x) / v.k) * k, y: cy - ((cy - v.y) / v.k) * k });
  };

  const patch = useCallback((id: string, change: Partial<FlowNode>) => {
    setFlow((f) => ({ ...f, nodes: f.nodes.map((n) => (n.id === id ? { ...n, ...change } : n)) }));
  }, []);

  const connect = useCallback((from: string, to: string) => {
    const why = canConnect(flowRef.current, from, to);
    if (why) return setNotice({ tone: "err", text: why });
    setNotice(null);
    setFlow((f) => ({ ...f, edges: [...f.edges, { id: uid("e"), from, to }] }));
  }, []);

  const remove = useCallback((target: NonNullable<Sel>) => {
    setFlow((f) => target.kind === "edge"
      ? { ...f, edges: f.edges.filter((e) => e.id !== target.id) }
      : { ...f, nodes: f.nodes.filter((n) => n.id !== target.id), edges: f.edges.filter((e) => e.from !== target.id && e.to !== target.id) });
    setSel(null);
  }, []);

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (t.closest("input, textarea, select, [contenteditable]")) return;
      if ((e.key === "Delete" || e.key === "Backspace") && sel && !(sel.kind === "node" && (sel.id === "start" || sel.id === "end"))) {
        e.preventDefault();
        remove(sel);
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [sel, remove]);

  const point = (clientX: number, clientY: number) => {
    const el = stage.current!;
    const r = el.getBoundingClientRect();
    const v = viewRef.current;
    return { x: (clientX - r.left - v.x) / v.k, y: (clientY - r.top - v.y) / v.k };
  };

  const addStep = (preset: PresetId, at?: { x: number; y: number }) => {
    const n = flowRef.current.nodes.filter((x) => x.kind === "step").length;
    const size = sizeOf({ kind: "step" } as FlowNode);
    const el = stage.current!;
    const c = at ?? { ...point(el.getBoundingClientRect().left + el.clientWidth / 2, el.getBoundingClientRect().top + el.clientHeight / 2) };
    const jig = at ? 0 : (n % 5) * 28;
    const node = newStep(preset, c.x - size.w / 2 + jig, c.y - size.h / 2 + jig);
    setFlow((f) => ({ ...f, nodes: [...f.nodes, node] }));
    setSel({ kind: "node", id: node.id });
  };

  // Dragging a step around the canvas.
  const startMove = (e: React.PointerEvent, n: FlowNode) => {
    if ((e.target as HTMLElement).closest("[data-port]")) return;
    setSel({ kind: "node", id: n.id });
    const p = point(e.clientX, e.clientY);
    const dx = p.x - n.x, dy = p.y - n.y;
    const move = (ev: PointerEvent) => {
      const q = point(ev.clientX, ev.clientY);
      patch(n.id, { x: Math.round(q.x - dx), y: Math.round(q.y - dy) });
    };
    const up = () => { window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  // Dragging the empty canvas moves the view.
  const startPan = (e: React.PointerEvent) => {
    if (e.target !== e.currentTarget || e.button > 1) return;
    setSel(null);
    const sx = e.clientX, sy = e.clientY, v0 = viewRef.current;
    setPanning(true);
    const move = (ev: PointerEvent) => setView({ ...v0, x: v0.x + ev.clientX - sx, y: v0.y + ev.clientY - sy });
    const up = () => { setPanning(false); window.removeEventListener("pointermove", move); window.removeEventListener("pointerup", up); };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  // Dragging a new connection out of a step's right-hand port.
  const startLink = (e: React.PointerEvent, from: FlowNode) => {
    e.stopPropagation();
    e.preventDefault();
    const move = (ev: PointerEvent) => { const q = point(ev.clientX, ev.clientY); setLink({ from: from.id, ...q }); };
    const up = (ev: PointerEvent) => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setLink(null);
      const q = point(ev.clientX, ev.clientY);
      const hit = flowRef.current.nodes.find((n) => {
        const z = sizeOf(n);
        return q.x >= n.x - 10 && q.x <= n.x + z.w + 10 && q.y >= n.y && q.y <= n.y + z.h;
      });
      if (hit && hit.id !== from.id) connect(from.id, hit.id);
    };
    move(e.nativeEvent);
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const byId = useMemo(() => new Map(flow.nodes.map((n) => [n.id, n])), [flow.nodes]);
  const stages = useMemo(() => stagesOf(flow), [flow]);
  const connected = useMemo(() => connectedSteps(flow), [flow]);
  const parallel = useMemo(() => new Set(stages.filter((g) => g.length > 1).flat().map((n) => n.id)), [stages]);
  const issues = useMemo(() => problems(flow), [flow]);
  const steps = flow.nodes.filter((n) => n.kind === "step");
  const selected = sel?.kind === "node" ? byId.get(sel.id) : undefined;
  const selectedEdge = sel?.kind === "edge" ? flow.edges.find((e) => e.id === sel.id) : undefined;

  async function save(publish: boolean) {
    if (issues.length) return setNotice({ tone: "err", text: issues[0] });
    setBusy(true);
    setNotice(null);
    const actor = employee?.display_name || employee?.email || "console";
    try {
      const specs = toSpecs(flow);
      for (const spec of specs) {
        if (publish) await definitionsApi.publish(actor, spec);
        else await definitionsApi.draft(actor, spec);
      }
      if (publish) {
        await refreshAgents();
        router.push(`/agents#${encodeURIComponent(specs[specs.length - 1].id)}`);
      } else {
        setNotice({ tone: "ok", text: `Draft of ${flow.name.trim()} saved.` });
      }
    } catch (err) {
      const text = err instanceof DefinitionAPIError
        ? (err.fields[0] ? `${err.fields[0].field}: ${err.fields[0].message}` : err.message)
        : "Could not save the agent.";
      setNotice({ tone: "err", text });
    } finally {
      setBusy(false);
    }
  }

  const empty = steps.length === 0;

  return (
    <>
      <PageHeader
        crumbs={<><Link href="/agents">Agents</Link><span>/</span><b>{flow.name.trim() || "New agent"}</b></>}
        title="Build an agent"
        sub={steps.length === 1 ? "Name the agent, describe its job, then publish it." : "Drag steps onto the canvas and connect them. Steps that start from the same place run in parallel."}
        actions={
          <>
            <button type="button" className="btn" onClick={() => setJson((v) => !v)} aria-pressed={json}><Icon name="code" size={15} />{json ? "Hide JSON" : "View JSON"}</button>
            <button type="button" className="btn" disabled={busy} onClick={() => { void save(false); }}>Save draft</button>
            <button type="button" className="btn solid" disabled={busy} onClick={() => { void save(true); }}>Publish</button>
          </>
        }
      />
      {notice && <p className={`${s.notice} ${notice.tone === "ok" ? s.ok : s.err}`} role="status">{notice.text}</p>}

      <div className={s.shell}>
        <aside className={`card ${s.palette}`} aria-label="Steps">
          <div className="ch"><h2>Steps</h2></div>
          <p className={s.tip}>Drag onto the canvas, or click to add.</p>
          <ul>
            {presets.map((p) => (
              <li key={p.id}>
                <button type="button" className={s.preset} draggable
                  onDragStart={(e) => { e.dataTransfer.setData("text/preset", p.id); e.dataTransfer.effectAllowed = "copy"; }}
                  onClick={() => addStep(p.id)}>
                  <span className={s.pico}><Icon name={p.icon} size={16} /></span>
                  <span><b>{p.label}</b><small>{p.hint}</small></span>
                </button>
              </li>
            ))}
          </ul>
          <div className={s.sep} />
          <button type="button" className="btn sm" onClick={() => { setFlow((f) => autoLayout(f)); setFitTick((n) => n + 1); }} disabled={empty}>Auto-arrange</button>
        </aside>

        <div
          className={`${s.stage} ${panning ? s.panning : ""}`} ref={stage}
          style={{ backgroundPosition: `${view.x}px ${view.y}px`, backgroundSize: `${GRID * view.k}px ${GRID * view.k}px` }}
          onDragOver={(e) => { if (e.dataTransfer.types.includes("text/preset")) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } }}
          onDrop={(e) => {
            const id = e.dataTransfer.getData("text/preset") as PresetId;
            if (!id) return;
            e.preventDefault();
            addStep(id, point(e.clientX, e.clientY));
          }}
          onPointerDown={startPan}
        >
          <div className={s.canvas} style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.k})` }}>
            <svg className={s.wires} width={1} height={1} aria-hidden>
              {flow.edges.map((e) => {
                const a = byId.get(e.from), b = byId.get(e.to);
                if (!a || !b) return null;
                const d = curve(a.x + sizeOf(a).w, a.y + sizeOf(a).h / 2, b.x, b.y + sizeOf(b).h / 2);
                const on = sel?.kind === "edge" && sel.id === e.id;
                return (
                  <g key={e.id}>
                    <path className={`${s.edge} ${on ? s.edgeOn : ""}`} d={d} />
                    <path className={s.hit} d={d} onPointerDown={(ev) => { ev.stopPropagation(); setSel({ kind: "edge", id: e.id }); }} />
                  </g>
                );
              })}
              {link && byId.get(link.from) && (() => {
                const a = byId.get(link.from)!;
                return <path className={`${s.edge} ${s.edgeOn} ${s.ghost}`} d={curve(a.x + sizeOf(a).w, a.y + sizeOf(a).h / 2, link.x, link.y)} />;
              })()}
            </svg>

            {flow.nodes.map((n) => {
              const z = sizeOf(n);
              const on = sel?.kind === "node" && sel.id === n.id;
              const style = { left: n.x, top: n.y, width: z.w, height: z.h };
              if (n.kind !== "step") {
                return (
                  <div key={n.id} className={`${s.pill} ${on ? s.on : ""}`} style={style} onPointerDown={(e) => startMove(e, n)}>
                    {n.kind === "end" && <i data-port="in" className={`${s.port} ${s.in}`} />}
                    <Icon name={n.kind === "start" ? "play" : "check"} size={14} />{n.name}
                    {n.kind === "start" && <i data-port="out" className={`${s.port} ${s.out}`} onPointerDown={(e) => startLink(e, n)} title="Drag to a step" />}
                  </div>
                );
              }
              const p = presetById(n.preset);
              const loose = !connected.has(n.id);
              return (
                <div key={n.id} className={`${s.node} ${on ? s.on : ""} ${loose ? s.loose : ""}`} style={style} onPointerDown={(e) => startMove(e, n)}>
                  <i data-port="in" className={`${s.port} ${s.in}`} />
                  <span className={s.ico}><Icon name={p.icon} size={18} /></span>
                  <b>{n.name || "Untitled step"}</b>
                  <span className={s.sub}>
                    {loose ? "Not connected" : parallel.has(n.id) ? "Runs in parallel" : n.model ? models[n.model]?.label ?? n.model : "Service default"}
                  </span>
                  <i data-port="out" className={`${s.port} ${s.out}`} onPointerDown={(e) => startLink(e, n)} title="Drag to the next step" />
                </div>
              );
            })}

            {empty && (
              <div className={s.start}>
                <b>Start from a template</b>
                <p>Or drag a step in from the left.</p>
                <div>
                  {templates.map((t) => (
                    <button key={t.id} type="button" className="btn sm" title={t.hint} onClick={() => { setFlow((f) => ({ ...flowFromStages(t.stages, f.name), goal: f.goal })); setSel(null); setFitTick((n) => n + 1); }}>{t.label}</button>
                  ))}
                </div>
              </div>
            )}
          </div>
          <div className={s.zoom} onPointerDown={(e) => e.stopPropagation()}>
            <button type="button" aria-label="Zoom out" onClick={() => zoomBy(1 / 1.25)}>−</button>
            <span>{Math.round(view.k * 100)}%</span>
            <button type="button" aria-label="Zoom in" onClick={() => zoomBy(1.25)}>+</button>
            <button type="button" onClick={() => setFitTick((n) => n + 1)}>Fit</button>
          </div>
        </div>

        <aside className={`card ${s.inspector}`} aria-label="Details">
          {selected && selected.kind === "step" ? (
            <StepForm n={selected} agentName={steps.length === 1 ? flow.name : undefined} onAgentNameChange={(name) => setFlow((f) => ({ ...f, name, nodes: f.nodes.map((n) => n.id === selected.id ? { ...n, name: name || "Agent" } : n) }))} onChange={(c) => patch(selected.id, c)} onRemove={() => remove({ kind: "node", id: selected.id })} />
          ) : selectedEdge ? (
            <>
              <div className="ch"><h2>Connection</h2></div>
              <div className={s.form}>
                <p>{byId.get(selectedEdge.from)?.name} → {byId.get(selectedEdge.to)?.name}</p>
                <button type="button" className="btn sm" onClick={() => remove({ kind: "edge", id: selectedEdge.id })}>Remove connection</button>
              </div>
            </>
          ) : (
            <>
              <div className="ch"><h2>Agent</h2></div>
              <div className={s.form}>
                <label>Name
                  <input className="input" value={flow.name} placeholder="e.g. Blog post writer" onChange={(e) => setFlow({ ...flow, name: e.target.value })} />
                </label>
                {steps.length > 1 && (
                  <label>Overall goal <small>(optional)</small>
                    <textarea className="input" rows={3} value={flow.goal} placeholder="What should the whole team accomplish?" onChange={(e) => setFlow({ ...flow, goal: e.target.value })} />
                  </label>
                )}
                <div>
                  <small className={s.label}>Run order</small>
                  {stages.length === 0 ? <p className={s.muted}>Nothing connected yet.</p> : (
                    <ol className={s.order}>
                      {stages.map((g, i) => (
                        <li key={i}>{g.map((n) => n.name).join(" + ")}{g.length > 1 && <span className="tag accent">parallel</span>}</li>
                      ))}
                    </ol>
                  )}
                </div>
                {issues.length > 0 && !empty && <ul className={s.issues}>{issues.map((p) => <li key={p}>{p}</li>)}</ul>}
                <details>
                  <summary>Advanced</summary>
                  <label>Version
                    <input className="input mono" value={flow.version} onChange={(e) => setFlow({ ...flow, version: e.target.value })} />
                  </label>
                  <button type="button" className="btn sm" onClick={() => { setFlow(blankFlow()); setSel(null); setFitTick((n) => n + 1); }}>Clear canvas</button>
                </details>
              </div>
            </>
          )}
        </aside>
      </div>

      {json && <pre className={`card ${s.json}`}>{JSON.stringify(issues.length ? { problems: issues } : toSpecs(flow), null, 2)}</pre>}
    </>
  );
}

function StepForm({ n, agentName, onAgentNameChange, onChange, onRemove }: { n: FlowNode; agentName?: string; onAgentNameChange: (name: string) => void; onChange: (c: Partial<FlowNode>) => void; onRemove: () => void }) {
  const toggle = (id: string) => onChange({ tools: n.tools.includes(id) ? n.tools.filter((t) => t !== id) : [...n.tools, id] });
  return (
    <>
      <div className="ch"><h2>{n.name || "Step"}</h2><div className="right"><button type="button" className="btn ghost sm" onClick={onRemove}>Delete</button></div></div>
      <div className={s.form}>
        {agentName !== undefined && <label>Agent name
          <input className="input" value={agentName} placeholder="e.g. Research assistant" onChange={(e) => onAgentNameChange(e.target.value)} />
        </label>}
        {agentName === undefined && <label>Step name
          <input className="input" value={n.name} onChange={(e) => onChange({ name: e.target.value })} />
        </label>}
        <label>What should it do?
          <textarea className="input" rows={5} value={n.instructions} placeholder="Describe the job in plain language." onChange={(e) => onChange({ instructions: e.target.value })} />
        </label>
        <label>Model
          <select className="input" value={n.model} onChange={(e) => onChange({ model: e.target.value })}>
            <option value="">Service default</option>
            {Object.entries(models).map(([id, m]) => <option key={id} value={id}>{m.label}</option>)}
          </select>
        </label>
        <div>
          <small className={s.label}>Tools it can use</small>
          <div className={s.tools}>
            {pickable.map((t) => {
              const on = n.tools.includes(t.id);
              return (
                <button key={t.id} type="button" className={s.tool} aria-pressed={on} onClick={() => toggle(t.id)} title={`${t.name} · ${t.provider}${on ? "" : " (off)"}`}>
                  <Icon name={t.icon} size={20} />
                  <span>{t.name}</span>
                </button>
              );
            })}
          </div>
        </div>
        <details>
          <summary>Advanced</summary>
          <label>Max model turns
            <input className="input mono" type="number" min={1} value={n.turns} onChange={(e) => onChange({ turns: Math.max(1, +e.target.value || 1) })} />
          </label>
          <label>Timeout (seconds)
            <input className="input mono" type="number" min={1} value={n.seconds} onChange={(e) => onChange({ seconds: Math.max(1, +e.target.value || 1) })} />
          </label>
        </details>
      </div>
    </>
  );
}
