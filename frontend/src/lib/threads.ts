// Client for durable threads and their live OpenCode transcript stream.

export type ThreadTool = { name: string; status?: string; input?: unknown; output?: unknown };
export type ThreadMessage = { id: string; role: "user" | "assistant"; text: string; tools: ThreadTool[]; error?: string };
export type ThreadFile = { id: string; name: string; content_type: string; size: number };
export type ThreadPermission = { id: string; action: string; resources: string[] };
export type Thread = { id: string; title?: string; busy: boolean; error?: string; messages: ThreadMessage[]; files?: ThreadFile[]; permissions?: ThreadPermission[] };
export type ThreadRef = { id: string; title: string; at: number };

function headers(json = false) {
  const result = new Headers();
  const token = window.localStorage.getItem("nmc3_token");
  if (token) result.set("Authorization", `Bearer ${token}`);
  if (json) result.set("Content-Type", "application/json");
  return result;
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const requestHeaders = headers(body !== undefined);
  let response: Response;
  try {
    response = await fetch(`/agent-api/threads${path}`, { method, headers: requestHeaders, cache: "no-store", body: body === undefined ? undefined : JSON.stringify(body) });
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
  list: async () => {
    const result = await call<{ threads: (Thread & { updated_at?: string })[] }>("GET", "");
    return result.threads.map((t) => ({ id: t.id, title: !t.title || t.title === "Thread" ? "New thread" : t.title, at: t.updated_at ? Date.parse(t.updated_at) : Date.now() }));
  },
  create: (title?: string) => call<Thread>("POST", "", { title }),
  get: (id: string) => call<Thread>("GET", `/${encodeURIComponent(id)}`),
  send: (id: string, text: string, fileIds: string[] = []) => call<{ id: string }>("POST", `/${encodeURIComponent(id)}/messages`, { text, file_ids: fileIds }),
  stop: (id: string) => call<{ id: string }>("POST", `/${encodeURIComponent(id)}/interrupt`),
  replyPermission: (id: string, requestId: string, decision: "once" | "reject") => call<{ id: string }>("POST", `/${encodeURIComponent(id)}/permissions/${encodeURIComponent(requestId)}/reply`, { decision }),
  archive: (id: string) => call<{ id: string; archived: boolean }>("POST", `/${encodeURIComponent(id)}/archive`),
  upload: async (id: string, file: File) => {
    const data = new FormData(); data.set("file", file);
    const response = await fetch(`/agent-api/threads/${encodeURIComponent(id)}/files`, { method: "POST", headers: headers(), body: data, cache: "no-store" });
    if (!response.ok) {
      const payload = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      throw new Error(payload?.error?.message ?? `File upload failed (${response.status})`);
    }
    return response.json() as Promise<ThreadFile>;
  },
  download: async (id: string, file: ThreadFile) => {
    const response = await fetch(`/agent-api/threads/${encodeURIComponent(id)}/files/${encodeURIComponent(file.id)}`, { headers: headers(), cache: "no-store" });
    if (!response.ok) throw new Error(`File download failed (${response.status})`);
    const url = URL.createObjectURL(await response.blob());
    const anchor = document.createElement("a"); anchor.href = url; anchor.download = file.name; anchor.click();
    URL.revokeObjectURL(url);
  },
  stream: async (id: string, signal: AbortSignal, onThread: (thread: Thread) => void) => {
    const response = await fetch(`/agent-api/threads/${encodeURIComponent(id)}/events`, { headers: headers(), cache: "no-store", signal });
    if (!response.ok || !response.body) throw new Error(`Thread stream failed (${response.status})`);
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n"); buffer = chunks.pop() ?? "";
      for (const chunk of chunks) {
        const data = chunk.split("\n").find((line) => line.startsWith("data: "))?.slice(6);
        if (data) onThread(JSON.parse(data) as Thread);
      }
    }
  },
};
