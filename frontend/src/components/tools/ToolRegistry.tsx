"use client";

import { useCallback, useEffect, useState } from "react";
import s from "./ToolRegistry.module.css";

const inputSchema = {
  type: "object",
  properties: { query: { type: "string" } },
  required: ["query"],
  additionalProperties: false,
};

const outputSchema = {
  type: "object",
  properties: { source: { type: "string" } },
  required: ["source"],
  additionalProperties: false,
};

type ToolClass = "http" | "mcp" | "worker";

type ToolRow = {
  id: string;
  version: string;
  class: string;
  lifecycle: string;
  side_effect?: string;
  endpoint?: { url?: string };
};

type Draft = {
  tenant: string;
  id: string;
  version: string;
  className: ToolClass;
  url: string;
  allow: string;
  mcpTool: string;
  internal: boolean;
};

const initial: Draft = {
  tenant: "",
  id: "",
  version: "1.0.0",
  className: "http",
  url: "",
  allow: "",
  mcpTool: "",
  internal: false,
};

const unwired = "The extension API is not mounted on this process. The tools page is unwired.";

type ErrorBody = { error?: { message?: string } };
type ListBody = { tools?: ToolRow[] };

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const token = window.localStorage.getItem("nmc3_token");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init?.body) headers.set("Content-Type", "application/json");
  let response: Response;
  try {
    response = await fetch(`/agent-api${path}`, { cache: "no-store", ...init, headers });
  } catch {
    throw new Error(unwired);
  }
  const text = await response.text();
  let body: (T & ErrorBody) | null = null;
  if (text) {
    try {
      body = JSON.parse(text) as T & ErrorBody;
    } catch {
      body = null;
    }
  }
  if (!response.ok) {
    const message = body?.error?.message;
    if (response.status === 404 || message === "endpoint not found") throw new Error(unwired);
    if ((response.status === 500 || response.status === 502 || response.status === 503) && typeof message !== "string") {
      throw new Error(unwired);
    }
    throw new Error(typeof message === "string" ? message : `Request failed (${response.status})`);
  }
  return (body ?? {}) as T;
}

function payload(draft: Draft) {
  const allow = draft.allow.split(",").map((part) => part.trim()).filter(Boolean);
  const endpoint: { url: string; method: "POST"; mcp_tool?: string; internal?: boolean } = {
    url: draft.url.trim(),
    method: "POST",
  };
  if (draft.className === "mcp") endpoint.mcp_tool = draft.mcpTool.trim();
  if (draft.className === "http" && draft.internal) endpoint.internal = true;
  return {
    tenant_id: draft.tenant.trim(),
    id: draft.id.trim(),
    version: draft.version.trim(),
    description: "Registered from the console",
    input_schema: inputSchema,
    output_schema: outputSchema,
    side_effect: "READ_ONLY",
    timeout_ms: 2000,
    class: draft.className,
    endpoint,
    retry: { max_attempts: 1 },
    resources: { max_output_bytes: 65536 },
    network: { allow },
  };
}

export default function ToolRegistry() {
  const [draft, setDraft] = useState<Draft>(initial);
  const [tools, setTools] = useState<ToolRow[]>([]);
  const [busy, setBusy] = useState(false);
  const [showRetired, setShowRetired] = useState(false);
  const [notice, setNotice] = useState<{ tone: "ok" | "err"; text: string } | null>(null);

  const refresh = useCallback(async (tenant: string) => {
    const body = await call<ListBody>(`/extension-tools?tenant_id=${encodeURIComponent(tenant)}`);
    setTools(body.tools ?? []);
  }, []);

  // The registry is keyed by tenant. The tenant is the signed-in employee's organization.
  useEffect(() => {
    let cancel = false;
    (async () => {
      const token = window.localStorage.getItem("nmc3_token");
      const res = await fetch("/api/v1/organization", { headers: token ? { Authorization: `Bearer ${token}` } : {} });
      if (!res.ok) throw new Error("The organization could not be loaded.");
      const org = (await res.json()) as { id: string };
      if (cancel) return;
      setDraft((prev) => ({ ...prev, tenant: org.id }));
      await refresh(org.id);
    })().catch((err: unknown) => {
      if (!cancel) setNotice({ tone: "err", text: err instanceof Error ? err.message : unwired });
    });
    return () => {
      cancel = true;
    };
  }, [refresh]);

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((prev) => ({ ...prev, [key]: value }));

  const save = async (publish: boolean) => {
    setBusy(true);
    setNotice(null);
    try {
      const body = payload(draft);
      if (!body.tenant_id || !body.id || !body.version || !body.endpoint.url || body.network.allow.length === 0) {
        throw new Error("Tenant, id, version, URL, and an IP allow entry are required.");
      }
      if (body.class === "mcp" && !body.endpoint.mcp_tool) {
        throw new Error("An MCP tool name is required.");
      }
      await call("/extension-tools", { method: "POST", body: JSON.stringify(body) });
      if (publish) {
        await call("/extension-tools/publish", {
          method: "POST",
          body: JSON.stringify({ tenant_id: body.tenant_id, id: body.id, version: body.version }),
        });
      }
      await refresh(body.tenant_id);
      setNotice({
        tone: "ok",
        text: publish ? `Published ${body.id} ${body.version}` : `Saved draft ${body.id} ${body.version}`,
      });
    } catch (err) {
      setNotice({ tone: "err", text: err instanceof Error ? err.message : "Request failed" });
    } finally {
      setBusy(false);
    }
  };

  const shown = tools.filter((t) => showRetired || t.lifecycle !== "retired");

  return (
    <>
      {notice && <p className={`${s.note} ${notice.tone === "ok" ? s.ok : s.err}`} role="status">{notice.text}</p>}
      <section className={`card ${s.panel}`}>
        <div className="ch">
          <h2>Register</h2>
          <span className="n">POST /v1/extension-tools</span>
        </div>
        <div className={s.fields}>
          <label>tenant
            <input className="input mono" value={draft.tenant} readOnly aria-readonly="true" />
          </label>
          <label>id
            <input className="input" value={draft.id} placeholder="extensions.http.name" onChange={(e) => set("id", e.target.value)} autoComplete="off" spellCheck={false} />
          </label>
          <label>version
            <input className="input" value={draft.version} onChange={(e) => set("version", e.target.value)} autoComplete="off" spellCheck={false} />
          </label>
          <label>class
            <select className="input" value={draft.className} onChange={(e) => set("className", e.target.value as ToolClass)}>
              <option value="http">HTTP</option>
              <option value="mcp">MCP</option>
              <option value="worker">Worker</option>
            </select>
          </label>
          <label className={s.span2}>endpoint URL
            <input className="input" value={draft.url} placeholder="http://10.0.0.5:8080/tool" onChange={(e) => set("url", e.target.value)} autoComplete="off" spellCheck={false} />
          </label>
          <label className={s.span2}>allow (IP:port)
            <input className="input" value={draft.allow} placeholder="10.0.0.5:8080" onChange={(e) => set("allow", e.target.value)} autoComplete="off" spellCheck={false} />
          </label>
          {draft.className === "mcp" && (
            <label>MCP tool
              <input className="input" value={draft.mcpTool} onChange={(e) => set("mcpTool", e.target.value)} autoComplete="off" spellCheck={false} />
            </label>
          )}
          {draft.className === "http" && (
            <label className={s.check}>
              <input type="checkbox" checked={draft.internal} onChange={(e) => set("internal", e.target.checked)} />
              Internal service
            </label>
          )}
        </div>
        <div className={s.actions}>
          <button type="button" className="btn" disabled={busy} onClick={() => save(false)}>Save draft</button>
          <button type="button" className="btn solid" disabled={busy} onClick={() => save(true)}>Publish</button>
        </div>
      </section>

      <section className={`card ${s.panel}`}>
        <div className="ch">
          <h2>Versions</h2>
          <span className="n">{shown.length}</span>
          <label className={s.check} style={{ marginLeft: "auto" }}>
            <input type="checkbox" checked={showRetired} onChange={(e) => setShowRetired(e.target.checked)} />
            Show retired
          </label>
        </div>
        {shown.length === 0 ? (
          <p className={s.empty}>No tools for this tenant.</p>
        ) : (
          <div className={s.scroll}>
            <table className={s.table}>
              <thead>
                <tr>
                  <th>Id</th>
                  <th>Version</th>
                  <th>Class</th>
                  <th>State</th>
                  <th>Endpoint</th>
                </tr>
              </thead>
              <tbody>
                {shown.map((tool) => (
                  <tr key={`${tool.id}@${tool.version}`}>
                    <td className="mono">{tool.id}</td>
                    <td className="mono">{tool.version}</td>
                    <td>{tool.class}</td>
                    <td><span className={`tag ${tool.lifecycle === "published" ? "ok" : ""}`}>{tool.lifecycle}</span></td>
                    <td className="mono">{tool.endpoint?.url}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
