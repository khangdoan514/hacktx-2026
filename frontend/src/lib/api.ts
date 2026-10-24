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
  depth: number; state: RunState; output?: unknown; error?: RunError;
}
export interface AgentRun {
  id: string; state: RunState; agent_id: string; agent_version: string;
  input: unknown; output?: unknown; error?: RunError; invocations: Invocation[];
  usage: { input_tokens: number; output_tokens: number; cost_usd: number };
  tool_invocations: { id: string; agent_invocation_id: string; tool_id: string; tool_version: string;
    output?: unknown; error?: RunError }[];
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
async function agentRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set("X-NMC3-Console", "1");
  const response = await fetch(`/agent-api${path}`, { cache: "no-store", ...init, headers });
  const body = await response.json();
  if (!response.ok) throw new Error(body?.error?.message ?? body?.message ?? `Agent API failed (${response.status})`);
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
