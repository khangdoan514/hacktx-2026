// Shapes shared by the config, the run simulator and the UI.
// Run states, error codes and event types mirror agents/internal/contracts (v1).

import type { IconName } from "@/components/ui/Icon";

export type State = "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "CANCELLED";

export type ErrorCode =
  | "DELEGATION_DEPTH_EXCEEDED"
  | "TOOL_NOT_ALLOWED"
  | "BUDGET_EXCEEDED"
  | "TIMEOUT"
  | "OUTPUT_SCHEMA_INVALID";

export type EventType =
  | "RUN_STARTED"
  | "AGENT_STARTED"
  | "TOOL_STARTED"
  | "TOOL_SUCCEEDED"
  | "TOOL_FAILED"
  | "DELEGATION_REJECTED"
  | "AGENT_SUCCEEDED"
  | "AGENT_FAILED"
  | "RUN_SUCCEEDED"
  | "RUN_FAILED"
  | "RUN_CANCELLED";

export type Limits = {
  turns: number;
  tools: number;
  delegations: number;
  parallel: number;
  seconds: number;
};

/** One scripted step a simulated agent takes. Real agents decide these at runtime. */
export type SimStep =
  | { tool: string; result: string; ms?: number }
  | { blocked: "tool" | "delegate"; target: string }
  /** Hand work to a sub-specialist, which runs one level deeper. */
  | { delegate: string; ms?: number }
  | { think: string; ms?: number };

export type AgentDef = {
  id: string;
  version: string;
  name: string;
  icon: IconName;
  kind: "orchestrator" | "specialist";
  model: string;
  role: string;
  instructions: string;
  tools: string[];
  delegates: string[];
  /** Orchestrators only: groups of delegates; agents in one group run in parallel. */
  plan?: string[][];
  limits: Limits;
  input: string;
  output: string;
  /** Demo behaviour for the in-browser simulator. Omit to get a default. */
  sim?: { steps?: SimStep[]; result: string };
};

export type ToolDef = {
  id: string;
  name: string;
  mark: string;
  icon: IconName;
  provider: string;
  sideEffect: "READ_ONLY" | "WRITE";
  timeoutSeconds: number;
};

export type Idea = { title: string; why: string; score: number; sources: string[] };

export type Scenario = {
  agent: string;
  task: string;
  ideas: Idea[];
  /** Ends the run early with a typed failure, for history. */
  fail?: { atMs: number; code: ErrorCode };
};

// ---------- Timeline produced by the simulator ----------

export type Turn = {
  id: string;
  agent: string;
  n: number;
  at: number;
  ms: number;
  system: string;
  input: string;
  output: string;
  tokensIn: number;
  tokensOut: number;
  cost: number;
};

export type Call = { id: string; agent: string; name: string; denied: boolean; kind: "tool" | "delegate" };

export type Op =
  | { at: number; k: "agent"; agent: string; st: State; now: string }
  | { at: number; k: "call"; call: string; st: "RUNNING" | "SUCCEEDED" | "BLOCKED"; text: string }
  | { at: number; k: "event"; type: EventType; agent: string; detail?: string }
  | { at: number; k: "use"; key: keyof Limits; delta: number }
  | { at: number; k: "par"; value: number };

export type RunPlan = {
  id: string;
  agent: string;
  task: string;
  limits: Limits;
  createdAt: number;
  durationMs: number;
  ops: Op[];
  calls: Call[];
  turns: Turn[];
  ideas: Idea[];
  final: State;
  error?: ErrorCode;
  errorDetail?: string;
  trigger: string;
  cancelledAt?: number;
};

// ---------- What the UI renders at a moment in time ----------

export type AgentView = { st: State; now: string; start?: number; end?: number };
export type CallView = Call & { visible: boolean; st: "RUNNING" | "SUCCEEDED" | "BLOCKED" | "HIDDEN"; text: string };
export type EventView = { seq: number; at: number; type: EventType; agent: string; detail?: string };

export type RunView = {
  plan: RunPlan;
  state: State;
  elapsed: number;
  agents: Record<string, AgentView>;
  calls: CallView[];
  events: EventView[];
  turns: Turn[];
  used: Limits;
  cost: number;
  tokens: number;
};
