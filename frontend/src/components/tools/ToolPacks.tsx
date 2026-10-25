"use client";

import { useCallback, useEffect, useState } from "react";
import { readToken } from "@/product/api";
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
  connected: { tone: "ok", text: "Connected." },
  denied: { tone: "err", text: "Sign-in was cancelled or denied." },
  invalid_state: { tone: "err", text: "That sign-in link expired. Start again." },
  exchange_failed: { tone: "err", text: "The provider refused the sign-in. Check the OAuth client id, secret and redirect URI." },
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
  const [packs, setPacks] = useState<Pack[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [tokens, setTokens] = useState<Record<string, string>>({});
  const [open, setOpen] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);

  const refresh = useCallback(async () => {
    const body = await call<{ packs: Pack[] }>("/tool-packs");
    setPacks(body.packs);
  }, []);

  useEffect(() => {
    refresh().catch((err: unknown) => setNotice({ tone: "err", text: err instanceof Error ? err.message : "Tool packs could not be loaded." }));
    // The OAuth callback returns here with ?pack=&result=.
    const params = new URLSearchParams(window.location.search);
    const result = params.get("result");
    if (result) {
      setNotice(oauthResults[result] ?? { tone: "err", text: "Sign-in failed." });
      window.history.replaceState(null, "", window.location.pathname);
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
          <h2>Get tools</h2>
          <span className="n">{packs.filter((p) => p.connected).length} connected</span>
        </div>
        <p className={s.lead}>
          Connect an account once, then list a tool id (for example <code>github.search_code</code>) in an agent&apos;s Tools field.
          All built-in tools are read-only.
        </p>
        <div className={s.grid}>
          {packs.map((pack) => (
            <article key={pack.id} className={s.pack}>
              <header>
                <h3>{pack.name}</h3>
                <span className={`tag ${pack.connected ? "ok" : ""}`}>{pack.connected ? "Connected" : "Not connected"}</span>
              </header>
              <p>{pack.description}</p>
              {pack.connected && <p className={s.account}>{pack.account || "Connected"} via {pack.method === "oauth" ? "sign-in" : "token"}</p>}
              <details>
                <summary>{pack.tools.length} tools</summary>
                <ul className={s.tools}>
                  {pack.tools.map((tool) => (
                    <li key={tool.id}><code>{tool.id}</code> {tool.description}</li>
                  ))}
                </ul>
              </details>
              <div className={s.actions}>
                {pack.connected ? (
                  <button type="button" className="btn" disabled={busy === pack.id} onClick={() => disconnect(pack)}>Disconnect</button>
                ) : (
                  <>
                    {pack.oauth_available && (
                      <button type="button" className="btn solid" disabled={busy === pack.id} onClick={() => signIn(pack)}>Sign in with {pack.id === "github" ? "GitHub" : "Google"}</button>
                    )}
                    {pack.token_accepted && (
                      <button type="button" className={pack.oauth_available ? "btn" : "btn solid"} onClick={() => setOpen(open === pack.id ? null : pack.id)}>Use a token</button>
                    )}
                    {!pack.oauth_available && !pack.token_accepted && (
                      <span className={s.hint}>Sign-in is not set up on this server. Set the OAuth client id and secret (see agents/.env.example).</span>
                    )}
                  </>
                )}
              </div>
              {open === pack.id && !pack.connected && (
                <div className={s.token}>
                  <label>
                    {pack.token_hint}
                    <input className="input mono" type="password" autoComplete="off" spellCheck={false} value={tokens[pack.id] ?? ""} onChange={(e) => setTokens((prev) => ({ ...prev, [pack.id]: e.target.value }))} />
                  </label>
                  <button type="button" className="btn solid" disabled={busy === pack.id || !(tokens[pack.id] ?? "").trim()} onClick={() => saveToken(pack)}>Connect</button>
                </div>
              )}
            </article>
          ))}
        </div>
      </section>
    </>
  );
}
