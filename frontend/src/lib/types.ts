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

export type AgentDef = {
  id: string;
  version: string;
  name: string;
  icon: IconName;
  kind: "orchestrator" | "specialist";
  /** What the definition's role says; the agent service has no separate description. */
  role: string;
  instructions: string;
  tools: string[];
  delegates: string[];
  limits: Limits;
  /** Spend ceiling per run, USD. */
  maxCostUsd: number;
  maxTokens: number;
  input: string;
  output: string;
  /** The definition's default model, when it names one. */
  model?: string;
  state: string;
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
  /** What the model decided this turn (FINISH, TOOL, DELEGATE), from the event. */
  action?: string;
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
  /** The run's structured output once it has succeeded. */
  output?: unknown;
  /** Agent invocations in start order; the graph and lanes draw from these. */
  invocations: { id: string; agent: string; parent: string | null; depth: number }[];
  model?: string;
  usage: { tokensIn: number; tokensOut: number; cost: number };
  final: State;
  error?: ErrorCode;
  errorDetail?: string;
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
