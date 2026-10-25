"use client";

import { useCallback, useEffect, useState } from "react";
import { readToken } from "@/product/api";
import { useEmployee } from "@/components/shell/ConsoleGate";
import s from "./ToolPacks.module.css";

type PackTool = { id: string; name: string; description: string; side_effect: string };
export type Pack = {
  id: string;
  name: string;
  description: string;
  connected: boolean;
  account?: string;
  method?: string;
  token_accepted: boolean;
  token_hint?: string;
  oauth_available: boolean;
  tools: PackTool[];
};

type Notice = { tone: "ok" | "err"; text: string };

const oauthResults: Record<string, Notice> = {
  connected: { tone: "ok", text: "Sign-in completed. Your connection is listed below." },
  denied: { tone: "err", text: "Sign-in was cancelled or denied." },
  invalid_state: { tone: "err", text: "That sign-in link expired or was already used. Select Connect account to start a new sign-in." },
  exchange_failed: { tone: "err", text: "The provider could not complete sign-in. Try connecting again; if it continues, contact your administrator." },
  verify_failed: { tone: "err", text: "Signed in, but the account could not be verified." },
  save_failed: { tone: "err", text: "Signed in, but the connection could not be saved." },
};

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  const token = readToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init?.body) headers.set("Content-Type", "application/json");
  const response = await fetch(`/agent-api${path}`, { cache: "no-store", ...init, headers });
  const body = await response.json().catch(() => null);
  if (!response.ok) {
    const message = body?.error?.message;
    throw new Error(typeof message === "string" ? message : `Request failed (${response.status})`);
  }
  return body as T;
}

export default function ToolPacks() {
  const employee = useEmployee();
  const [loading, setLoading] = useState(true);
  const [packs, setPacks] = useState<Pack[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [tokens, setTokens] = useState<Record<string, string>>({});
  const [open, setOpen] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);

  const refresh = useCallback(async () => {
    const body = await call<{ packs: Pack[] }>("/tool-packs");
    setPacks(body.packs);
    setLoading(false);
  }, []);

  useEffect(() => {
    refresh().catch((err: unknown) => { setLoading(false); setNotice({ tone: "err", text: err instanceof Error ? err.message : "Connections could not be loaded." }); });
    // The OAuth callback returns here with ?pack=&result=.
    const params = new URLSearchParams(window.location.search);
    const result = params.get("result");
    if (result) {
      setNotice(oauthResults[result] ?? { tone: "err", text: "Sign-in failed." });
      const pack = params.get("pack");
      if (pack) setOpen(pack);
      params.delete("pack");
      params.delete("result");
      window.history.replaceState(null, "", window.location.pathname + (params.size ? `?${params}` : ""));
    }
  }, [refresh]);

  const run = async (id: string, work: () => Promise<void>) => {
    setBusy(id);
    setNotice(null);
    try {
      await work();
      await refresh();
    } catch (err) {
      setNotice({ tone: "err", text: err instanceof Error ? err.message : "Request failed" });
    } finally {
      setBusy(null);
    }
  };

  const signIn = (pack: Pack) =>
    run(pack.id, async () => {
      const { url } = await call<{ url: string }>(`/tool-packs/${pack.id}/oauth/start`, { method: "POST" });
      window.location.assign(url);
    });

  const saveToken = (pack: Pack) =>
    run(pack.id, async () => {
      await call(`/tool-packs/${pack.id}/connect`, { method: "POST", body: JSON.stringify({ token: tokens[pack.id] ?? "" }) });
      setTokens((prev) => ({ ...prev, [pack.id]: "" }));
      setOpen(null);
      setNotice({ tone: "ok", text: `${pack.name} connected.` });
    });

  const testConnection = (pack: Pack) =>
    run(pack.id, async () => {
      const result = await call<{ account: string; message: string }>(`/tool-packs/${pack.id}/test`, { method: "POST" });
      setNotice({ tone: "ok", text: `${pack.name}: ${result.account}. ${result.message}` });
    });

  const disconnect = (pack: Pack) =>
    run(pack.id, async () => {
      await call(`/tool-packs/${pack.id}/disconnect`, { method: "POST" });
      setNotice({ tone: "ok", text: `${pack.name} disconnected.` });
    });

  return (
    <>
      {notice && <p className={`${s.note} ${notice.tone === "ok" ? s.ok : s.err}`} role="status">{notice.text}</p>}
      <section className={`card ${s.panel}`}>
        <div className="ch">
          <h2>Your connected accounts</h2>
          <span className="n">{packs.filter((p) => p.connected).length} connected</span>
        </div>
        <p className={s.lead}>
          Connect Google Drive, GitHub, Notion, or Slack to use your workspace content with agents.
          Connections belong to {employee?.email || "your signed-in account"}. These tools have read-only access.
        </p>
        {loading && <p className={s.lead} role="status">Loading connections…</p>}
        {!loading && packs.length === 0 && <div className={s.empty}><p>No connections could be loaded.</p><button className="btn" type="button" onClick={() => run("reload", refresh)} disabled={busy !== null}>Try again</button></div>}
        <div className={s.grid}>
          {packs.map((pack) => (
            <article key={pack.id} className={s.pack}>
              <header>
                <h3>{pack.name}</h3>
                <span className={`tag ${pack.connected ? "ok" : ""}`}>{pack.connected ? "Connected" : "Not connected"}</span>
              </header>
              <p>{pack.description}</p>
              {pack.id === "gdrive" && <p className={s.hint}>Search files, browse folders, and read supported documents you can access. Nothing is edited or deleted.</p>}
              {pack.connected && <p className={s.account}>{pack.account || "Connected"} via {pack.method === "oauth" ? "sign-in" : "token"}</p>}
              <details>
                <summary>What this connection can access</summary>
                <ul className={s.tools}>
                  {pack.tools.map((tool) => (
                    <li key={tool.id}>{tool.description}</li>
                  ))}
                </ul>
              </details>
              <div className={s.actions}>
                {pack.connected ? (
                  <>
                    <button type="button" className="btn" disabled={busy !== null} onClick={() => testConnection(pack)}>{busy === pack.id ? "Checking…" : "Test connection"}</button>
                    {pack.oauth_available && <button type="button" className="btn" disabled={busy !== null} onClick={() => signIn(pack)}>Reconnect</button>}
                    <button type="button" className="btn" disabled={busy !== null} onClick={() => disconnect(pack)}>Disconnect</button>
                  </>
                ) : (
                  <>
                    {pack.oauth_available && (
                      <button type="button" className="btn solid" disabled={busy !== null} onClick={() => signIn(pack)}>{busy === pack.id ? "Opening sign-in…" : `Connect ${pack.name}`}</button>
                    )}
                    {pack.token_accepted && (
                      <button type="button" className={pack.oauth_available ? "btn" : "btn solid"} disabled={busy !== null} onClick={() => setOpen(open === pack.id ? null : pack.id)}>Connect with token</button>
                    )}
                    {!pack.oauth_available && !pack.token_accepted && (
                      <span className={s.hint}>An administrator needs to enable this connection before you can sign in.</span>
                    )}
                  </>
                )}
              </div>
              {open === pack.id && !pack.connected && (
                <div className={s.token}>
                  <label>
                    {pack.token_hint}
                    <input aria-label={`${pack.name} access token`} className="input mono" type="password" autoComplete="off" spellCheck={false} value={tokens[pack.id] ?? ""} onChange={(e) => setTokens((prev) => ({ ...prev, [pack.id]: e.target.value }))} />
                  </label>
                  <button type="button" className="btn solid" disabled={busy !== null || !(tokens[pack.id] ?? "").trim()} onClick={() => saveToken(pack)}>Connect</button>
                </div>
              )}
            </article>
          ))}
        </div>
      </section>
    </>
  );
}
