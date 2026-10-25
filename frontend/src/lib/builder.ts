// The visual agent builder's model: a graph of steps between a Start and an End.
// A step with several outgoing connections starts those branches in parallel, and a
// step with several incoming connections waits for all of them. Publishing turns the
// graph into one specialist definition per step plus an orchestrator that runs the
// stages in order (see toSpecs).

import type { IconName } from "@/components/ui/Icon";
import type { Spec } from "@/components/definitions/api";
import type { AgentDef } from "@/lib/types";

export type PresetId = "researcher" | "writer" | "reviewer" | "checker" | "summarizer" | "custom";

export type Preset = {
  id: PresetId;
  label: string;
  hint: string;
  icon: IconName;
  instructions: string;
  tools: string[];
  model: string;
};

export const presets: Preset[] = [
  { id: "researcher", label: "Researcher", hint: "Finds sources and facts", icon: "compass", model: "", tools: ["serpapi.google_search"],
    instructions: "Search for what is relevant to the task. Return the key findings and where each came from." },
  { id: "writer", label: "Writer", hint: "Drafts from what it is given", icon: "sparkles", model: "", tools: [],
    instructions: "Write a clear draft using only the material you were given. Do not invent facts." },
  { id: "reviewer", label: "Reviewer", hint: "Checks work and flags problems", icon: "scale", model: "", tools: [],
    instructions: "Review the work you were given. List what is wrong or missing, then return a corrected version." },
  { id: "checker", label: "Fact-checker", hint: "Verifies claims against research", icon: "search", model: "", tools: ["serpapi.google_scholar"],
    instructions: "Check each claim against published sources. Keep the ones that hold up and say why you dropped the rest." },
  { id: "summarizer", label: "Summarizer", hint: "Condenses results", icon: "message", model: "", tools: [],
    instructions: "Summarize the material you were given in a few short points." },
  { id: "custom", label: "Custom step", hint: "Start blank and describe it", icon: "bot", model: "", tools: [],
    instructions: "" },
];

export const presetById = (id: PresetId) => presets.find((p) => p.id === id)!;

export type FlowNode = {
  id: string;
  kind: "start" | "end" | "step";
  preset: PresetId;
  name: string;
  instructions: string;
  model: string;
  tools: string[];
  turns: number;
  seconds: number;
  x: number;
  y: number;
};
export type FlowEdge = { id: string; from: string; to: string };
export type Flow = { name: string; goal: string; version: string; nodes: FlowNode[]; edges: FlowEdge[] };

export const SIZE = {
  step: { w: 232, h: 76 },
  pill: { w: 124, h: 52 },
};
export const sizeOf = (n: FlowNode) => (n.kind === "step" ? SIZE.step : SIZE.pill);

let counter = 0;
export const uid = (p: string) => `${p}${Date.now().toString(36)}${(counter++).toString(36)}`;

export function blankFlow(): Flow {
  return {
    name: "", goal: "", version: "1.0.0",
    nodes: [
      { id: "start", kind: "start", preset: "custom", name: "Task", instructions: "", model: "", tools: [], turns: 0, seconds: 0, x: 40, y: 220 },
      { id: "end", kind: "end", preset: "custom", name: "Result", instructions: "", model: "", tools: [], turns: 0, seconds: 0, x: 760, y: 220 },
    ],
    edges: [],
  };
}

export function newStep(preset: PresetId, x: number, y: number): FlowNode {
  const p = presetById(preset);
  return {
    id: uid("s"), kind: "step", preset, name: p.label, instructions: p.instructions,
    model: p.model, tools: [...p.tools], turns: 4, seconds: 30, x, y,
  };
}

/** Ready-made starting points, as a list of stages; steps in one stage run in parallel. */
export const templates: { id: string; label: string; hint: string; stages: PresetId[][] }[] = [
  { id: "single", label: "Single agent", hint: "One agent that handles the task directly", stages: [["custom"]] },
  { id: "review", label: "Draft and review", hint: "Write, then check it", stages: [["writer"], ["reviewer"]] },
  { id: "research", label: "Research, write, review", hint: "Three steps in a row", stages: [["researcher"], ["writer"], ["reviewer"]] },
  { id: "parallel", label: "Parallel research", hint: "Two researchers, then one writer", stages: [["researcher", "researcher"], ["writer"]] },
];

export function flowFromStages(stages: PresetId[][], name = ""): Flow {
  const flow = blankFlow();
  flow.name = name;
  let prev = ["start"];
  for (const stage of stages) {
    const ids: string[] = [];
    for (const preset of stage) {
      const n = newStep(preset, 0, 0);
      flow.nodes.push(n);
      ids.push(n.id);
      for (const from of prev) flow.edges.push({ id: uid("e"), from, to: n.id });
    }
    prev = ids;
  }
  for (const from of prev) flow.edges.push({ id: uid("e"), from, to: "end" });
  return autoLayout(flow);
}

/** Turn an existing agent definition into a flow so it can be edited visually. */
export function flowFromAgent(def: AgentDef, agentById: (id: string) => AgentDef | undefined): Flow {
  const flow = blankFlow();
  flow.name = def.name;
  flow.goal = def.kind === "orchestrator" ? def.instructions : "";
  flow.version = def.version;
  const make = (a: AgentDef) => {
    const n = newStep("custom", 0, 0);
    Object.assign(n, { name: a.name, instructions: a.instructions, model: "", tools: [...a.tools], turns: a.limits.turns, seconds: a.limits.seconds });
    flow.nodes.push(n);
    return n.id;
  };
  const stages: AgentDef[][] = def.kind === "orchestrator"
    ? def.delegates.map((d) => [agentById(d)].filter((a): a is AgentDef => !!a))
    : [[def]];
  let prev = ["start"];
  for (const stage of stages) {
    const ids = stage.map(make);
    for (const to of ids) for (const from of prev) flow.edges.push({ id: uid("e"), from, to });
    prev = ids;
  }
  for (const from of prev) flow.edges.push({ id: uid("e"), from, to: "end" });
  return autoLayout(flow);
}

const succ = (f: Flow, id: string) => f.edges.filter((e) => e.from === id).map((e) => e.to);
const pred = (f: Flow, id: string) => f.edges.filter((e) => e.to === id).map((e) => e.from);

function reach(f: Flow, from: string, next: (f: Flow, id: string) => string[]) {
  const seen = new Set<string>([from]);
  const stack = [from];
  while (stack.length) for (const n of next(f, stack.pop()!)) if (!seen.has(n)) { seen.add(n); stack.push(n); }
  return seen;
}

/** Longest distance from Start, so every step lands after everything it waits for. */
export function levels(f: Flow): Map<string, number> {
  const memo = new Map<string, number>();
  const go = (id: string, trail: Set<string>): number => {
    if (id === "start") return 0;
    if (memo.has(id)) return memo.get(id)!;
    if (trail.has(id)) return 0;
    trail.add(id);
    const ps = pred(f, id);
    const v = ps.length ? 1 + Math.max(...ps.map((p) => go(p, trail))) : 1;
    trail.delete(id);
    memo.set(id, v);
    return v;
  };
  for (const n of f.nodes) go(n.id, new Set());
  return memo;
}

/** Connected steps grouped by stage. Steps in one group run in parallel. */
export function stagesOf(f: Flow): FlowNode[][] {
  const ok = connectedSteps(f);
  const lv = levels(f);
  const groups = new Map<number, FlowNode[]>();
  for (const n of f.nodes) {
    if (n.kind !== "step" || !ok.has(n.id)) continue;
    const l = lv.get(n.id) ?? 1;
    groups.set(l, [...(groups.get(l) ?? []), n]);
  }
  return [...groups.keys()].sort((a, b) => a - b).map((k) => groups.get(k)!);
}

/** Steps that can be reached from Start and can reach End. */
export function connectedSteps(f: Flow): Set<string> {
  const fwd = reach(f, "start", succ);
  const back = reach(f, "end", pred);
  return new Set(f.nodes.filter((n) => n.kind === "step" && fwd.has(n.id) && back.has(n.id)).map((n) => n.id));
}

export function wouldCycle(f: Flow, from: string, to: string) {
  return reach(f, to, succ).has(from);
}

export function canConnect(f: Flow, from: string, to: string): string | null {
  if (from === to) return "A step cannot connect to itself.";
  if (to === "start") return "Nothing can feed into Start.";
  if (from === "end") return "Nothing can follow Result.";
  if (f.edges.some((e) => e.from === from && e.to === to)) return "Those are already connected.";
  if (wouldCycle(f, from, to)) return "That would make a loop.";
  return null;
}

export function autoLayout(f: Flow): Flow {
  const lv = levels(f);
  const stepMax = Math.max(0, ...f.nodes.filter((n) => n.kind === "step").map((n) => lv.get(n.id) ?? 1));
  const col = (n: FlowNode) => (n.kind === "start" ? 0 : n.kind === "end" ? stepMax + 1 : lv.get(n.id) ?? 1);
  const cols = new Map<number, FlowNode[]>();
  for (const n of f.nodes) cols.set(col(n), [...(cols.get(col(n)) ?? []), n]);
  const tallest = Math.max(...[...cols.values()].map((c) => c.length));
  const H = Math.max(440, tallest * 112 + 80);
  const nodes = f.nodes.map((n) => {
    const group = cols.get(col(n))!;
    const i = group.indexOf(n);
    const size = sizeOf(n);
    const slot = 112;
    const top = H / 2 - (group.length * slot) / 2 + slot / 2;
    return { ...n, x: 40 + col(n) * 270, y: Math.round(top + i * slot - size.h / 2) };
  });
  return { ...f, nodes };
}

export function canvasSize(f: Flow) {
  const w = Math.max(900, ...f.nodes.map((n) => n.x + sizeOf(n).w + 80));
  const h = Math.max(480, ...f.nodes.map((n) => n.y + sizeOf(n).h + 80));
  return { w, h };
}

export function problems(f: Flow): string[] {
  const out: string[] = [];
  if (!f.name.trim()) out.push("Give the agent a name.");
  const steps = f.nodes.filter((n) => n.kind === "step");
  if (steps.length === 0) out.push("Add at least one step.");
  const ok = connectedSteps(f);
  const loose = steps.filter((n) => !ok.has(n.id));
  if (steps.length && loose.length) out.push(`${loose.map((n) => n.name || "A step").join(", ")} ${loose.length > 1 ? "are" : "is"} not connected from Start to Result.`);
  for (const n of steps) if (ok.has(n.id) && !n.instructions.trim()) out.push(`${n.name || "A step"} needs instructions.`);
  if (steps.length && !f.edges.some((e) => e.to === "end")) out.push("Connect a step to Result.");
  return out;
}

export const slug = (s: string) => s.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "agent";

/** One specialist per connected step, plus an orchestrator when there is more than one. */
export function toSpecs(f: Flow): Spec[] {
  const stages = stagesOf(f);
  const base = slug(f.name);
  const used = new Set<string>();
  const idOf = new Map<string, string>();
  for (const n of stages.flat()) {
    let id = `${base}-${slug(n.name)}`;
    for (let i = 2; used.has(id); i++) id = `${base}-${slug(n.name)}-${i}`;
    used.add(id);
    idOf.set(n.id, id);
  }
  const schema = (key: string) => ({ type: "object", additionalProperties: false, properties: { [key]: { type: "string" } }, required: [key] });
  const specialist = (n: FlowNode, id: string, name: string, role: Spec["role"]): Spec => ({
    id, version: f.version.trim() || "1.0.0", name, role, instructions: n.instructions,
    input_schema: schema("task"), output_schema: schema("result"),
    allowed_tools: n.tools, allowed_delegates: [],
    // The definition API requires positive budget caps even when the
    // allowlists below contain no delegates or tools.
    limits: { max_delegations: 1, max_concurrent_agents: 1, max_tool_calls: n.tools.length ? 3 : 1, max_model_turns: n.turns, max_tokens: 4000, max_cost_usd: 1, timeout_seconds: n.seconds },
    model_policy: n.model ? { default_model: n.model } : undefined,
  });
  const steps = stages.flat();
  if (steps.length === 1) return [specialist(steps[0], base, f.name.trim(), "specialist")];

  const plan = stages.map((g, i) => `${i + 1}. ${g.map((n) => idOf.get(n.id)).join(" and ")}${g.length > 1 ? " (in parallel)" : ""}`).join("\n");
  const orchestrator: Spec = {
    id: base, version: f.version.trim() || "1.0.0", name: f.name.trim(), role: "orchestrator",
    instructions: `${f.goal.trim() ? f.goal.trim() + "\n\n" : ""}Delegate to these specialists in order. Steps in the same group run at the same time, and the next group waits for all of them:\n${plan}\nReturn the last group's result.`,
    input_schema: schema("task"), output_schema: schema("result"),
    allowed_tools: [], allowed_delegates: steps.map((n) => idOf.get(n.id)!),
    limits: {
      max_delegations: steps.length,
      max_concurrent_agents: Math.max(...stages.map((g) => g.length)),
      max_tool_calls: 1,
      max_model_turns: 4 + steps.length * 2,
      max_tokens: 4000 * steps.length,
      max_cost_usd: steps.length,
      timeout_seconds: stages.reduce((a, g) => a + Math.max(...g.map((n) => n.seconds)), 0) + 15,
    },
  };
  return [...steps.map((n) => specialist(n, idOf.get(n.id)!, n.name, "specialist")), orchestrator];
}
