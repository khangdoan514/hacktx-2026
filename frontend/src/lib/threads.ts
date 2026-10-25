// Client for the threads API: plain chat with an OpenCode-backed agent.
// OpenCode keeps each transcript; the browser only remembers which threads exist.

export type ThreadTool = { name: string; status?: string };
export type ThreadMessage = { id: string; role: "user" | "assistant"; text: string; tools: ThreadTool[]; error?: string };
export type Thread = { id: string; title?: string; busy: boolean; error?: string; messages: ThreadMessage[] };
export type ThreadRef = { id: string; title: string; at: number };

const KEY = "nmc3-threads";

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers = new Headers();
  const token = window.localStorage.getItem("nmc3_token");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (body !== undefined) headers.set("Content-Type", "application/json");
  let response: Response;
  try {
    response = await fetch(`/agent-api/threads${path}`, { method, headers, cache: "no-store", body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new Error("The agent service is not reachable.");
  }
  const text = await response.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* non-JSON error pages */ }
  if (!response.ok) {
    const message = (data as { error?: { message?: string }; message?: string } | null);
    if (response.status === 404 && !message?.error) throw new Error("Threads are not available on this server yet.");
    throw new Error(message?.error?.message ?? message?.message ?? `Request failed (${response.status})`);
  }
  return data as T;
}

export const threadsApi = {
  create: (title?: string) => call<Thread>("POST", "", { title }),
  get: (id: string) => call<Thread>("GET", `/${encodeURIComponent(id)}`),
  send: (id: string, text: string) => call<{ id: string }>("POST", `/${encodeURIComponent(id)}/messages`, { text }),
  stop: (id: string) => call<{ id: string }>("POST", `/${encodeURIComponent(id)}/interrupt`),
};

export function loadRefs(): ThreadRef[] {
  try {
    const raw = JSON.parse(window.localStorage.getItem(KEY) ?? "[]") as ThreadRef[];
    return Array.isArray(raw) ? raw : [];
  } catch {
    return [];
  }
}

export function saveRefs(refs: ThreadRef[]) {
  try { window.localStorage.setItem(KEY, JSON.stringify(refs)); } catch { /* storage can be unavailable */ }
}
