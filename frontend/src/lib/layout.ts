// Tree layout for the execution graph: caller -> root agent -> delegates -> tool calls.
// Each subtree gets as much height as its children need, so a run with more
// agents or tool calls grows the canvas instead of overlapping.

import type { Call, RunPlan } from "./types";

export type Box = { x: number; y: number; w: number; h: number };
export type LayoutNode =
  | { kind: "agent"; id: string; box: Box; parent: string | null }
  | { kind: "call"; id: string; call: Call; box: Box; parent: string };

export type Layout = { width: number; height: number; you: Box; nodes: LayoutNode[]; cols: { label: string; x: number }[] };

const AGENT = { w: 236, h: 76, slot: 104 };
const CALL = { w: 250, h: 42, slot: 50 };
const COL_X = [128, 478, 806];
// Used when delegation goes a level deeper, so every column still fits.
const COL_X_DEEP = [128, 430, 732, 1034];
const TOP = 52;

type T = { kind: "agent" | "call"; id: string; call?: Call; children: T[] };

export function layoutRun(plan: RunPlan): Layout {
  // A delegation is shown as the specialist's own node, not as a call.
  const callsOf = (agent: string) => plan.calls.filter((c) => c.agent === agent && c.kind !== "delegate");
  const invs = plan.invocations;
  const rootInv = invs.find((i) => i.depth === 0);
  // Specialists are drawn under the invocation that started them, up to the depth limit.
  const tree = (id: string, depth: number, inv: string | null): T => {
    const kids: T[] = [];
    for (const i of invs.filter((x) => x.parent === inv && x.depth === depth + 1)) {
      if (!kids.some((k) => k.kind === "agent" && k.id === i.agent)) kids.push(tree(i.agent, depth + 1, i.id));
    }
    for (const c of callsOf(id)) kids.push({ kind: "call", id: c.id, call: c, children: [] });
    return { kind: "agent", id, children: kids };
  };
  const root = tree(plan.agent, 0, rootInv?.id ?? null);
  const specialists = invs.filter((i) => i.depth >= 1);

  const heightOf = (n: T): number => {
    const own = n.kind === "agent" ? AGENT.slot : CALL.slot;
    const kids = n.children.reduce((s, c) => s + heightOf(c), 0);
    return Math.max(own, kids);
  };

  const maxDepth = Math.max(0, ...invs.map((i) => i.depth));
  const X = maxDepth >= 2 ? COL_X_DEEP : COL_X;
  const nodes: LayoutNode[] = [];
  const place = (n: T, top: number, col: number, parent: string | null) => {
    const H = heightOf(n);
    const size = n.kind === "agent" ? AGENT : CALL;
    const box = { x: X[Math.min(col, X.length - 1)], y: top + H / 2 - size.h / 2, w: size.w, h: size.h };
    if (n.kind === "agent") nodes.push({ kind: "agent", id: n.id, box, parent });
    else nodes.push({ kind: "call", id: n.id, call: n.call!, box, parent: parent! });
    const kidsH = n.children.reduce((s, c) => s + heightOf(c), 0);
    let y = top + (H - kidsH) / 2;
    for (const c of n.children) {
      place(c, y, col + 1, n.id);
      y += heightOf(c);
    }
  };
  place(root, TOP, 0, null);

  const height = Math.max(320, TOP + heightOf(root) + 16);
  const rootBox = nodes[0].box;
  const colOf = (n: LayoutNode) => X.indexOf(n.box.x);
  const maxCol = nodes.reduce((m, n) => Math.max(m, colOf(n)), 0);
  const cols = [{ label: specialists.length ? "orchestrator" : "agent", x: X[0] }];
  for (let c = 1; c <= maxCol; c++) {
    const inCol = nodes.filter((n) => colOf(n) === c);
    const agentsHere = inCol.some((n) => n.kind === "agent"), callsHere = inCol.some((n) => n.kind === "call");
    const label = agentsHere && callsHere ? "agents & tools" : agentsHere ? (c === 1 ? "specialists" : "sub-specialists") : "tool calls";
    cols.push({ label, x: X[c] });
  }
  return {
    width: maxDepth >= 2 ? 1330 : 1080, height, nodes, cols,
    you: { x: 26, y: rootBox.y + rootBox.h / 2 - 26, w: 52, h: 52 },
  };
}
