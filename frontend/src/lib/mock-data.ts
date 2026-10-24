// Starter dashboard content; the Runs page uses the live NMC3 API.
export type AgentStatus = "active" | "paused" | "draft";
export type RunStatus = "succeeded" | "running" | "failed";
export type Run = { id: string; agent: string; trigger: string; status: RunStatus; duration: string; started: string };
export type Workflow = { id: string; name: string; steps: number; color: string; icon: "mail" | "calendar" | "file" | "search" | "chart" | "message" | "bell" | "code" };
export const agents: { id: string; name: string; description: string; model: string; updated: string; successRate: number; runs: number; status: AgentStatus }[] = [];
export const runs: Run[] = [];
export const workflows: Workflow[] = [];
