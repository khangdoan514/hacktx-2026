// Dashboard compatibility shim. Arrays stay empty.
// The Runs page reads the live agent API. Do not invent sample agents here.

export type AgentStatus = "active" | "paused" | "draft";
export type RunStatus = "succeeded" | "running" | "failed";

export type Agent = {
  id: string;
  name: string;
  description: string;
  status: AgentStatus;
  model: string;
  updated: string;
  successRate: number;
  runs: number;
};

export type Run = {
  id: string;
  agent: string;
  status: RunStatus;
  duration: string;
  started: string;
  trigger: string;
};

export type WorkflowIcon =
  | "mail"
  | "calendar"
  | "file"
  | "search"
  | "chart"
  | "message"
  | "bell"
  | "code";

export type Workflow = {
  id: string;
  name: string;
  steps: number;
  icon: WorkflowIcon;
  color: string;
};

export const agents: Agent[] = [];
export const runs: Run[] = [];
export const workflows: Workflow[] = [];
