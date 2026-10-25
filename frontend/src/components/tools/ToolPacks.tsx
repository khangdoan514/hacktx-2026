"use client";

import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import Icon from "@/components/ui/Icon";
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
type OAuthMessage = { type: "agent-tool-oauth"; pack: string; result: string };

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
  const [selected, setSelected] = useState<Pack | null>(null);
  const [query, setQuery] = useState("");
  const [view, setView] = useState<"discover" | "connected">("discover");
  const [notice, setNotice] = useState<Notice | null>(null);
  const popupRef = useRef<Window | null>(null);

  const refresh = useCallback(async () => {
    const body = await call<{ packs: Pack[] }>("/tool-packs");
    setPacks(body.packs);
  }, []);

  useEffect(() => {
    refresh().catch((err: unknown) => setNotice({ tone: "err", text: err instanceof Error ? err.message : "Tool packs could not be loaded." }));
    const params = new URLSearchParams(window.location.search);
    const result = params.get("result");
    if (result) {
      setNotice(oauthResults[result] ?? { tone: "err", text: "Sign-in failed." });
      window.history.replaceState(null, "", window.location.pathname);
    }
    const onMessage = (event: MessageEvent<OAuthMessage>) => {
      if (event.origin !== window.location.origin || event.source !== popupRef.current || event.data?.type !== "agent-tool-oauth") return;
      popupRef.current = null;
      const message = oauthResults[event.data.result] ?? { tone: "err" as const, text: "Sign-in failed." };
      setNotice({ ...message, text: event.data.result === "connected" ? `${event.data.pack} connected.` : message.text });
      if (event.data.result === "connected") setSelected(null);
      refresh().catch((err: unknown) => setNotice({ tone: "err", text: err instanceof Error ? err.message : "Integration status could not be refreshed." }));
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
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

  const signIn = (pack: Pack) => {
    const popup = window.open("", `connect-${pack.id}`, "popup,width=520,height=720");
    if (!popup) {
      setNotice({ tone: "err", text: "Your browser blocked the sign-in window. Allow pop-ups for this app and try again." });
      return;
    }
    popupRef.current = popup;
    void run(pack.id, async () => {
      try {
        const { url } = await call<{ url: string }>(`/tool-packs/${pack.id}/oauth/start?popup=true`, { method: "POST" });
        popup.location.href = url;
      } catch (err) {
        popup.close();
        popupRef.current = null;
        throw err;
      }
    });
  };

  const saveToken = async (event: FormEvent) => {
    event.preventDefault();
    if (!selected) return;
    const pack = selected;
    await run(pack.id, async () => {
      await call(`/tool-packs/${pack.id}/connect`, { method: "POST", body: JSON.stringify({ token: tokens[pack.id] ?? "" }) });
      setTokens((prev) => ({ ...prev, [pack.id]: "" }));
      setSelected(null);
      setNotice({ tone: "ok", text: `${pack.name} connected.` });
    });
  };

  const disconnect = (pack: Pack) =>
    run(pack.id, async () => {
      await call(`/tool-packs/${pack.id}/disconnect`, { method: "POST" });
      setNotice({ tone: "ok", text: `${pack.name} disconnected.` });
    });

  const connectedCount = packs.filter((pack) => pack.connected).length;
  const normalizedQuery = query.trim().toLowerCase();
  const visiblePacks = packs.filter((pack) => {
    const matchesQuery = !normalizedQuery || [pack.name, pack.id, pack.description, ...pack.tools.flatMap((tool) => [tool.name, tool.id, tool.description])].some((value) => value.toLowerCase().includes(normalizedQuery));
    return matchesQuery && (view === "discover" || pack.connected);
  });

  return (
    <>
      {notice && <p className={`${s.note} ${notice.tone === "ok" ? s.ok : s.err}`} role="status">{notice.text}</p>}
      <section className={`card ${s.panel}`}>
        <div className={s.toolbar}>
          <div className={s.tabs} aria-label="Integration view">
            <button type="button" aria-pressed={view === "discover"} className={view === "discover" ? s.activeTab : ""} onClick={() => setView("discover")}>Discover</button>
            <button type="button" aria-pressed={view === "connected"} className={view === "connected" ? s.activeTab : ""} onClick={() => setView("connected")}>Connected <span>{connectedCount}</span></button>
          </div>
          <label className={s.search}>
            <Icon name="search" size={15} className={s.searchIcon} />
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search integrations" aria-label="Search integrations" />
          </label>
        </div>
        <p className={s.lead}>Connect a service once, then choose its tools in an agent&apos;s settings. Built-in tools are read-only.</p>
        {visiblePacks.length === 0 ? (
          <p className={s.empty}>{view === "connected" ? "No integrations connected yet. Browse available tools to get started." : "No integrations match your search."}</p>
        ) : <div className={s.grid}>
          {visiblePacks.map((pack) => (
            <article key={pack.id} className={s.pack}>
              <header className={s.packHeader}>
                <span className={s.packIcon} aria-hidden="true">{pack.name.slice(0, 1)}</span>
                <div className={s.packTitle}><h3>{pack.name}</h3><p>{pack.description}</p></div>
                {pack.connected ? (
                  <button type="button" className={`${s.switch} ${s.switchOn}`} role="switch" aria-checked="true" aria-label={`Disable ${pack.name}`} disabled={busy === pack.id} onClick={() => disconnect(pack)}><span /></button>
                ) : (
                  <button type="button" className={s.addButton} aria-label={`Set up ${pack.name}`} title={`Set up ${pack.name}`} disabled={busy === pack.id || (!pack.oauth_available && !pack.token_accepted)} onClick={() => setSelected(pack)}><span aria-hidden="true">+</span></button>
                )}
              </header>
              {pack.connected && <p className={s.account}>{pack.account || "Connected"} · {pack.method === "oauth" ? "Signed in" : "Access token"}</p>}
              <details>
                <summary>{pack.tools.length} tools</summary>
                <ul className={s.tools}>
                  {pack.tools.map((tool) => (
                    <li key={tool.id}><code>{tool.id}</code> {tool.description}</li>
                  ))}
                </ul>
              </details>
            </article>
          ))}
        </div>}
      </section>

      {selected && <div className={s.backdrop} onMouseDown={(event) => { if (event.target === event.currentTarget) setSelected(null); }}>
        <section className={s.dialog} role="dialog" aria-modal="true" aria-labelledby="connect-title">
          <button type="button" className={s.close} aria-label="Close setup" onClick={() => setSelected(null)}><Icon name="x" size={17} /></button>
          <span className={s.dialogIcon} aria-hidden="true">{selected.name.slice(0, 1)}</span>
          <h2 id="connect-title">Connect {selected.name}</h2>
          <p className={s.dialogLead}>Sign in to your {selected.name} account to let your agents use its tools.</p>
          {selected.oauth_available ? <button type="button" className={`btn solid ${s.signIn}`} disabled={busy === selected.id} onClick={() => signIn(selected)}>Continue with {selected.name}</button> : <p className={s.hint}>Account sign-in is not configured on this server yet.</p>}
          {selected.token_accepted && <>
            <div className={s.or}><span>or connect with an API key</span></div>
            <form className={s.tokenForm} onSubmit={(event) => void saveToken(event)}>
              <label>{selected.token_hint || "API key"}
                <input className="input mono" type="password" autoComplete="off" spellCheck={false} value={tokens[selected.id] ?? ""} onChange={(event) => setTokens((prev) => ({ ...prev, [selected.id]: event.target.value }))} required />
              </label>
              <button type="submit" className="btn" disabled={busy === selected.id || !(tokens[selected.id] ?? "").trim()}>Connect with API key</button>
            </form>
          </>}
          <p className={s.privacy}>Credentials are stored by the agent service and never shown again.</p>
        </section>
      </div>}
    </>
  );
}
