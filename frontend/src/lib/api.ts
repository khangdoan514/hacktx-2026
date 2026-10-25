import { readToken } from "@/product/api";

export async function apiGet<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api${path}`, init);
  if (!response.ok) throw new Error(`API request failed (${response.status})`);
  return response.json() as Promise<T>;
}

export type HelloResponse = { message: string };

export type RunState = "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED" | "CANCELLED";
export interface RunError { code: string; message: string }
export interface Invocation {
  id: string; parent_id: string | null; agent_id: string; agent_version: string;
  depth: number; state: RunState; input?: unknown; output?: unknown; error?: RunError;
  started_at?: string; finished_at?: string;
}
export interface AgentRun {
  id: string; state: RunState; agent_id: string; agent_version: string;
  model_id?: string;
  input: unknown; output?: unknown; error?: RunError; invocations: Invocation[];
  usage: { input_tokens: number; output_tokens: number; cost_usd: number };
  tool_invocations: { id: string; agent_invocation_id: string; tool_id: string; tool_version: string;
    output?: unknown; error?: RunError; started_at?: string; finished_at?: string }[];
  created_at: string; updated_at: string;
}
export interface RunEvent {
  sequence: number; run_id: string; invocation_id?: string; type: string;
  timestamp: string; data?: unknown;
}
export interface StartRequest {
  agent_id: string; agent_version: string; model_id?: string; input: unknown;
  limits: { max_delegations: number; max_concurrent_agents: number; max_tool_calls: number;
    max_model_turns: number; max_tokens: number; max_cost_usd: number; timeout_seconds: number };
}

export class AgentRequestError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

async function agentRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const token = readToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const response = await fetch(`/agent-api${path}`, { cache: "no-store", ...init, headers });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new AgentRequestError(body?.error?.message ?? body?.message ?? `Agent API failed (${response.status})`, response.status);
  }
  return body as T;
}
export const agentApi = {
  start: (request: StartRequest) => agentRequest<{ run_id: string; state: RunState }>("/agent-runs", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(request),
  }),
  run: (id: string) => agentRequest<{ run: AgentRun }>(`/agent-runs/${encodeURIComponent(id)}`),
  events: (id: string, after = 0) => agentRequest<{ run_id: string; events: RunEvent[] }>(`/agent-runs/${encodeURIComponent(id)}/events?after=${after}`),
  agents: () => agentRequest<{ agents: { id: string; version: string; name: string }[] }>("/agents"),
  list: () => agentRequest<{ runs: AgentRun[] }>("/agent-runs"),
  cancel: (id: string) => agentRequest<{ status: string }>(`/agent-runs/${encodeURIComponent(id)}/cancel`, { method: "POST" }),
};

export interface Automation {
  id: string; name: string; schedule: string; timezone: string; enabled: boolean;
  created_by: string; created_by_email?: string;
  request: StartRequest;
  next_run_at?: string; last_fired_at?: string; created_at: string; updated_at: string;
  last_firing?: AutomationFiring;
}
/** One attempt by an automation to start a run. State, usage and run_error come from the run. */
export interface AutomationFiring {
  id: string; automation_id: string; automation_name?: string; trigger: "schedule" | "manual";
  scheduled_for: string; fired_at: string; run_id?: string; error?: string;
  state?: RunState | "WAITING"; agent_id?: string; run_error?: string;
  usage?: { input_tokens: number; output_tokens: number; cost_usd: number };
}
export interface AutomationInput {
  name?: string; schedule?: string; timezone?: string; enabled?: boolean; request?: StartRequest;
}
const json = (method: string, body?: unknown): RequestInit => ({
  method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body),
});
export const automationApi = {
  list: () => agentRequest<{ automations: Automation[] }>("/automations"),
  create: (input: AutomationInput) => agentRequest<{ automation: Automation }>("/automations", json("POST", input)),
  update: (id: string, input: AutomationInput) => agentRequest<{ automation: Automation }>(`/automations/${encodeURIComponent(id)}`, json("PATCH", input)),
  remove: (id: string) => agentRequest<{ status: string }>(`/automations/${encodeURIComponent(id)}`, { method: "DELETE" }),
  run: (id: string) => agentRequest<{ firing: AutomationFiring }>(`/automations/${encodeURIComponent(id)}/run`, { method: "POST" }),
  firings: (id?: string) => agentRequest<{ firings: AutomationFiring[] }>(id ? `/automations/${encodeURIComponent(id)}/firings` : "/automation-firings"),
  preview: (schedule: string, timezone: string) => agentRequest<{ next: string[] }>("/automations/preview", json("POST", { schedule, timezone })),
};
