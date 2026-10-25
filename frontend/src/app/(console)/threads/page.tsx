"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import Icon from "@/components/ui/Icon";
import { loadRefs, saveRefs, threadsApi, type Thread, type ThreadRef } from "@/lib/threads";
import s from "./threads.module.css";

const POLL_MS = 1200;

export default function ThreadsPage() {
  const [refs, setRefs] = useState<ThreadRef[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [thread, setThread] = useState<Thread | null>(null);
  const [pending, setPending] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const end = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const saved = loadRefs();
    setRefs(saved);
    if (saved[0]) setActive(saved[0].id);
  }, []);

  const persist = (next: ThreadRef[]) => { setRefs(next); saveRefs(next); };

  const load = useCallback(async (id: string) => {
    try {
      const t = await threadsApi.get(id);
      setThread(t);
      setError(t.error ?? "");
      return t;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load the thread.");
      return null;
    }
  }, []);

  // Load the open thread, then keep polling while the agent is answering.
  useEffect(() => {
    if (!active) { setThread(null); return; }
    let stop = false;
    let timer: number | undefined;
    setThread(null);
    setPending(null);
    const tick = async () => {
      const t = await load(active);
      if (stop) return;
      if (t) setPending((p) => (p && t.messages.some((m) => m.role === "user" && m.text.trim() === p.trim()) ? null : p));
      if (t?.busy) timer = window.setTimeout(tick, POLL_MS);
    };
    void tick();
    return () => { stop = true; window.clearTimeout(timer); };
  }, [active, load]);

  const busy = Boolean(thread?.busy) || pending !== null;
  useEffect(() => { end.current?.scrollIntoView({ block: "end" }); }, [thread?.messages.length, pending, busy]);

  // After a send the first poll can arrive before the server marks the thread busy,
  // so keep polling while we are waiting on our own message to show up.
  useEffect(() => {
    if (!active || pending === null) return;
    const timer = window.setInterval(() => { void load(active); }, POLL_MS);
    return () => window.clearInterval(timer);
  }, [active, pending, load]);

  useEffect(() => {
    if (pending === null || !thread || thread.busy) return;
    const shown = thread.messages.some((m) => m.role === "user" && m.text.trim() === pending.trim());
    const answered = shown && thread.messages[thread.messages.length - 1]?.role === "assistant";
    if (answered || thread.error) setPending(null);
  }, [thread, pending]);

  async function newThread() {
    setStarting(true);
    setError("");
    try {
      const t = await threadsApi.create();
      persist([{ id: t.id, title: "New thread", at: Date.now() }, ...refs]);
      setActive(t.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start a thread.");
    } finally {
      setStarting(false);
    }
  }

  async function send() {
    const text = draft.trim();
    if (!text || busy) return;
    let id = active;
    setError("");
    try {
      if (!id) {
        const t = await threadsApi.create();
        id = t.id;
        persist([{ id, title: "New thread", at: Date.now() }, ...refs]);
        setActive(id);
      }
      setDraft("");
      setPending(text);
      await threadsApi.send(id, text);
      const title = text.length > 48 ? `${text.slice(0, 48)}…` : text;
      setRefs((cur) => {
        const next = cur.map((r) => (r.id === id && r.title === "New thread" ? { ...r, title } : r));
        saveRefs(next);
        return next;
      });
      const t = await load(id);
      if (t?.busy === false && !t.messages.length) setPending(null);
    } catch (err) {
      setPending(null);
      setDraft(text);
      setError(err instanceof Error ? err.message : "Could not send the message.");
    }
  }

  async function stop() {
    if (!active) return;
    try { await threadsApi.stop(active); } catch (err) { setError(err instanceof Error ? err.message : "Could not stop the thread."); }
  }

  const messages = thread?.messages ?? [];
  const showPending = pending !== null && !messages.some((m) => m.role === "user" && m.text.trim() === pending.trim());

  return (
    <>
      <PageHeader
        title="Threads"
        sub="Talk to an agent directly. It can use the tools it has been given."
        actions={<button type="button" className="btn solid" onClick={() => { void newThread(); }} disabled={starting}><Icon name="plus" size={15} />New thread</button>}
      />
      <div className={s.shell}>
        <aside className={`card ${s.list}`} aria-label="Threads">
          {refs.length === 0 ? <p className={s.empty}>No threads yet.</p> : (
            <ul>
              {refs.map((r) => (
                <li key={r.id}>
                  <button type="button" className={s.item} aria-current={r.id === active ? "true" : undefined} onClick={() => setActive(r.id)}>
                    <span>{r.title}</span>
                  </button>
                  <button type="button" className={`ibtn ${s.x}`} aria-label={`Remove ${r.title}`} title="Remove from this list"
                    onClick={() => { const next = refs.filter((x) => x.id !== r.id); persist(next); if (active === r.id) setActive(next[0]?.id ?? null); }}>
                    <Icon name="x" size={13} />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>

        <section className={`card ${s.chat}`}>
          <div className={s.scroll}>
            {messages.length === 0 && !showPending && (
              <div className={s.blank}>
                <Icon name="message" size={22} />
                <b>Start a conversation</b>
                <p>Ask anything. There are no preset prompts.</p>
              </div>
            )}
            {messages.map((m) => <Bubble key={m.id + m.role} m={m} />)}
            {showPending && <Bubble m={{ role: "user", text: pending!, tools: [] }} />}
            {busy && <div className={s.typing} aria-label="The agent is working"><i /><i /><i /></div>}
            <div ref={end} />
          </div>
          {error && <p className={s.error} role="alert">{error}</p>}
          <form className={s.composer} onSubmit={(e) => { e.preventDefault(); void send(); }}>
            <textarea
              className={s.input} rows={2} value={draft} placeholder="Message the agent…" aria-label="Message"
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }}
            />
            {busy && active ? (
              <button type="button" className="btn" onClick={() => { void stop(); }}><Icon name="square" size={14} />Stop</button>
            ) : (
              <button type="submit" className="btn solid" disabled={!draft.trim()}>Send</button>
            )}
          </form>
        </section>
      </div>
    </>
  );
}

function Bubble({ m }: { m: { role: "user" | "assistant"; text: string; tools: { name: string; status?: string }[]; error?: string } }) {
  return (
    <div className={`${s.row} ${m.role === "user" ? s.user : s.assistant}`}>
      {m.tools.length > 0 && (
        <div className={s.tools}>
          {m.tools.map((t, i) => <span key={i} className="tag"><Icon name="wrench" size={11} />{t.name}{t.status && t.status !== "completed" ? ` · ${t.status}` : ""}</span>)}
        </div>
      )}
      {m.text && <div className={s.bubble}>{m.text}</div>}
      {m.error && <div className={`${s.bubble} ${s.fail}`}>{m.error}</div>}
    </div>
  );
}
