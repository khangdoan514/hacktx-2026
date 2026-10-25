"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { useEmployee } from "@/components/shell/ConsoleGate";
import Icon from "@/components/ui/Icon";
import { canUseThreads } from "@/lib/permissions";
import { threadsApi, type Thread, type ThreadRef, type ThreadTool } from "@/lib/threads";
import s from "./threads.module.css";

const POLL_MS = 1200;

export default function ThreadsPage() {
  const employee = useEmployee();
  const router = useRouter();
  const allowed = canUseThreads(employee?.roles);
  useEffect(() => {
    if (!allowed) router.replace("/dashboard");
  }, [allowed, router]);
  if (!allowed) return null;
  return <ThreadsWorkspace />;
}

function ThreadsWorkspace() {
  const [refs, setRefs] = useState<ThreadRef[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [thread, setThread] = useState<Thread | null>(null);
  const [pending, setPending] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [selectedFiles, setSelectedFiles] = useState<string[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState("");
  const [starting, setStarting] = useState(false);
  const [search, setSearch] = useState("");
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const end = useRef<HTMLDivElement>(null);
  const stream = useRef<AbortController | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => {
    let alive = true;
    void threadsApi.list().then((saved) => { if (alive) { setRefs(saved); if (saved[0]) setActive(saved[0].id); } })
      .catch((err) => { if (alive) setError(err instanceof Error ? err.message : "Could not load threads."); });
    return () => { alive = false; };
  }, []);

  const persist = (next: ThreadRef[]) => setRefs(next);

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

  const watch = useCallback((id: string) => {
    stream.current?.abort();
    const controller = new AbortController();
    stream.current = controller;
    void threadsApi.stream(id, controller.signal, (next) => { setThread(next); setError(next.error ?? ""); })
      .catch((err) => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : "Thread stream disconnected."); });
  }, []);

  // Load once, then consume live transcript/tool snapshots over SSE.
  useEffect(() => {
    if (!active) { setThread(null); return; }
    const controller = new AbortController();
    setThread(null);
    setPending(null);
    const connect = async () => {
      const t = await load(active);
      if (!t || controller.signal.aborted) return;
      setPending((p) => (p && t.messages.some((m) => m.role === "user" && m.text.trim() === p.trim()) ? null : p));
      if (t.busy) watch(active);
    };
    void connect();
    return () => { controller.abort(); stream.current?.abort(); };
  }, [active, load, watch]);

  const busy = Boolean(thread?.busy) || pending !== null;
  useEffect(() => { end.current?.scrollIntoView({ block: "end" }); }, [thread?.messages.length, pending, busy]);

  // Sending is accepted before OpenCode marks its session busy; refresh once
  // to attach to the stream as soon as the backend starts the turn.
  useEffect(() => {
    if (!active || pending === null || thread?.busy) return;
    const timer = window.setTimeout(async () => {
      const latest = await load(active);
      if (latest?.busy) watch(active);
    }, POLL_MS);
    return () => window.clearTimeout(timer);
  }, [active, pending, thread?.busy, load, watch]);

  useEffect(() => {
    if (pending === null || !thread || thread.busy) return;
    // OpenCode may omit a user message from a snapshot; an idle turn must
    // always release the composer so the next message can be sent.
    setPending(null);
  }, [thread, pending]);

  async function newThread() {
    setStarting(true);
    setError("");
    try {
      const t = await threadsApi.create();
      setRefs((current) => [{ id: t.id, title: "New thread", at: Date.now() }, ...current]);
      setActive(t.id);
      setSelectedFiles([]);
      setSearch("");
      setSidebarOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start a thread.");
    } finally {
      setStarting(false);
    }
  }

  async function send() {
    const text = draft.trim();
    if ((!text && selectedFiles.length === 0) || busy || uploading) return;
    const displayText = text || "Analyze the attached file(s).";
    let id = active;
    setError("");
    try {
      if (!id) {
        const t = await threadsApi.create();
        id = t.id;
        setRefs((current) => [{ id: t.id, title: "New thread", at: Date.now() }, ...current]);
        setActive(id);
        setSearch("");
      }
      setDraft("");
      setPending(displayText);
      await threadsApi.send(id, text, selectedFiles);
      setSelectedFiles([]);
      const current = await threadsApi.get(id);
      setThread(current);
      if (current.busy) watch(id);
      const title = displayText.length > 48 ? `${displayText.slice(0, 48)}…` : displayText;
      setRefs((cur) => {
        const next = cur.map((r) => (r.id === id ? { ...r, title: r.title === "New thread" || r.title === "Thread" ? title : r.title, at: Date.now() } : r));
        return next;
      });
      if (current.busy === false && !current.messages.length) setPending(null);
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

  async function replyPermission(requestId: string, decision: "once" | "reject") {
    if (!active) return;
    try {
      await threadsApi.replyPermission(active, requestId, decision);
      await load(active);
    } catch (err) { setError(err instanceof Error ? err.message : "Could not answer the permission request."); }
  }

  async function attach(files?: FileList | null) {
    if (!files?.length || uploading) return;
    if (files.length + selectedFiles.length > 10) { setError("Attach at most 10 files per message."); return; }
    setUploading(true);
    setError("");
    try {
      let id = active;
      if (!id) {
        const created = await threadsApi.create(); id = created.id;
        setRefs((current) => [{ id: created.id, title: "New thread", at: Date.now() }, ...current]);
        setActive(id);
        setSearch("");
      }
      for (const file of Array.from(files)) {
        const uploaded = await threadsApi.upload(id, file);
        setSelectedFiles((current) => [...current, uploaded.id]);
      }
      const latest = await threadsApi.get(id);
      setThread(latest);
    } catch (err) { setError(err instanceof Error ? err.message : "Could not attach the file."); }
    finally { setUploading(false); if (fileInput.current) fileInput.current.value = ""; }
  }

  async function archive(id: string, title: string) {
    try {
      await threadsApi.archive(id);
      const next = refs.filter((item) => item.id !== id);
      persist(next);
      if (active === id) { setSelectedFiles([]); setActive(next[0]?.id ?? null); }
    } catch (err) { setError(err instanceof Error ? err.message : `Could not archive ${title}.`); }
  }

  const messages = thread?.messages ?? [];
  const showPending = pending !== null && !messages.some((m) => m.role === "user" && m.text.trim() === pending.trim());
  const filteredRefs = useMemo(() => refs
    .filter((r) => r.title.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()))
    .sort((a, b) => b.at - a.at), [refs, search]);
  const groupedRefs = useMemo(() => {
    const groups: { label: string; items: ThreadRef[] }[] = [];
    for (const item of filteredRefs) {
      const label = threadDateGroup(item.at);
      const group = groups.find((candidate) => candidate.label === label);
      if (group) group.items.push(item);
      else groups.push({ label, items: [item] });
    }
    return groups;
  }, [filteredRefs]);

  return (
    <div className={s.workspace} data-thread-workspace>
      {sidebarOpen && <button type="button" className={s.scrim} aria-label="Close threads" onClick={() => setSidebarOpen(false)} />}
      <aside className={`${s.sidebar} ${sidebarOpen ? s.sidebarOpen : ""}`} aria-label="Threads">
        <div className={s.sidebarTop}>
          <div className={s.sidebarTitle}><Icon name="message" size={18} /><strong>Threads</strong><span>{refs.length}</span></div>
          <button type="button" className={s.newThread} onClick={() => { void newThread(); }} disabled={starting}>
            <Icon name="plus" size={17} /> New thread
          </button>
          <label className={s.search}>
            <Icon name="search" size={16} />
            <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search threads" aria-label="Search threads" />
          </label>
        </div>
        <nav className={s.threadList} aria-label="Recent threads">
          {groupedRefs.length === 0 ? <p className={s.empty}>{search ? "No matching threads" : "Your threads will appear here."}</p> : groupedRefs.map((group) => (
            <div className={s.threadGroup} key={group.label}>
              <h2>{group.label}</h2>
              <ul>
                {group.items.map((r) => (
                  <li key={r.id} className={s.threadItem}>
                    <button type="button" className={s.item} aria-current={r.id === active ? "page" : undefined} onClick={() => { setSelectedFiles([]); setActive(r.id); setSidebarOpen(false); }}>
                      <span>{r.title}</span>
                    </button>
                    <button type="button" className={s.archive} aria-label={`Archive ${r.title}`} title="Archive thread" onClick={() => { void archive(r.id, r.title); }}>
                      <Icon name="archive" size={15} />
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
        <div className={s.sidebarFoot}>Agent conversations</div>
      </aside>

      <section className={s.chat} aria-label={active ? `Thread: ${thread?.title || refs.find((r) => r.id === active)?.title || "Thread"}` : "New thread"}>
        <header className={s.chatHeader}>
          <button type="button" className={s.mobileToggle} aria-label="Show threads" onClick={() => setSidebarOpen(true)}><Icon name="panel-left" size={19} /></button>
          <div className={s.chatHeading}>
            <strong>{active ? (thread?.title === "Thread" ? "New thread" : thread?.title || refs.find((r) => r.id === active)?.title || "New thread") : "New thread"}</strong>
            <span>{uploading ? "Uploading files…" : busy ? <><i className={s.liveDot} /> Agent working</> : "Agent chat"}</span>
          </div>
          <button type="button" className={s.headerNew} onClick={() => { void newThread(); }} disabled={starting} aria-label="New thread" title="New thread"><Icon name="plus" size={18} /></button>
        </header>
        <div className={`${s.scroll} ${messages.length === 0 && !showPending ? s.scrollEmpty : ""}`}>
          <div className={s.transcript}>
            {messages.length === 0 && !showPending && (
              <div className={s.blank}>
                <div className={s.blankMark}><Icon name="sparkles" size={22} /></div>
                <h1>What should we work on?</h1>
                <p>Ask the agent a question, or give it a task to work through.</p>
              </div>
            )}
            {messages.map((m) => <Bubble key={m.id + m.role} m={m} />)}
            {thread?.permissions?.map((request) => (
              <div className={s.permissionCard} key={request.id} role="group" aria-label="Agent permission request">
                <strong>Allow agent action?</strong>
                <p>{request.action}: {request.resources.join(", ")}</p>
                <div className={s.permissionActions}>
                  <button type="button" onClick={() => { void replyPermission(request.id, "reject"); }}>Deny</button>
                  <button type="button" onClick={() => { void replyPermission(request.id, "once"); }}>Allow once</button>
                </div>
              </div>
            ))}
            {showPending && <Bubble m={{ role: "user", text: pending!, tools: [] }} />}
            {busy && <div className={s.working} aria-label="The agent is working"><span className={s.typing}><i /><i /><i /></span>Working on it</div>}
            <div ref={end} />
          </div>
        </div>
        <div className={s.composerDock}>
          {error && <p className={s.error} role="alert">{error}</p>}
          <form className={s.composer} onSubmit={(e) => { e.preventDefault(); void send(); }}>
            <div className={s.attachments}>
              {thread?.files?.map((file) => <span className={s.fileGroup} key={file.id}>
                <button type="button" className={`${s.fileTag} ${selectedFiles.includes(file.id) ? s.fileTagSelected : ""}`} aria-pressed={selectedFiles.includes(file.id)} title={`${selectedFiles.includes(file.id) ? "Attached to next message" : "Attach to next message"}: ${file.name}`} onClick={() => setSelectedFiles((current) => current.includes(file.id) ? current.filter((id) => id !== file.id) : [...current, file.id])}><Icon name="file" size={14} />{file.name}</button>
                <button type="button" className={s.fileDownload} aria-label={`Download ${file.name}`} title={`Download ${file.name}`} onClick={() => { if (active) void threadsApi.download(active, file).catch((err) => setError(err instanceof Error ? err.message : "Could not download the file.")); }}>↓</button>
              </span>)}
            </div>
            <textarea
              className={s.input} rows={2} value={draft} placeholder="Message the agent…" aria-label="Message the agent"
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void send(); } }}
            />
            <input ref={fileInput} type="file" multiple hidden onChange={(e) => { void attach(e.currentTarget.files); }} />
            <div className={s.composerActions}>
              <button type="button" className={s.attachButton} aria-label="Attach files" title="Attach up to 10 files (20 MB each)" disabled={uploading || busy} onClick={() => fileInput.current?.click()}><Icon name="plus" size={20} /></button>
              {uploading && <span className={s.composerHint}>Uploading files…</span>}
              {busy && active ? (
                <button type="button" className={s.sendButton} onClick={() => { void stop(); }} aria-label="Stop agent" title="Stop agent"><Icon name="square" size={15} /></button>
              ) : (
                <button type="submit" className={s.sendButton} disabled={uploading || (!draft.trim() && selectedFiles.length === 0)} aria-label="Send message" title="Send message"><Icon name="arrow-up" size={19} /></button>
              )}
            </div>
          </form>
        </div>
      </section>
    </div>
  );
}

function threadDateGroup(timestamp: number) {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return "Older";
  const now = new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const day = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
  if (day === today) return "Today";
  const daysAgo = Math.round((today - day) / 86400000);
  if (daysAgo === 1) return "Yesterday";
  if (daysAgo <= 7) return "Previous 7 days";
  return "Older";
}

function Bubble({ m }: { m: { role: "user" | "assistant"; text: string; tools: ThreadTool[]; error?: string } }) {
  const text = m.role === "assistant" ? cleanAssistantText(m.text) : m.text;
  return (
    <div className={`${s.row} ${m.role === "user" ? s.user : s.assistant}`}>
      {m.tools.length > 0 && <div className={s.tools}>
        {m.tools.map((t, i) => <details key={i} className={s.toolLog}>
          <summary><Icon name="wrench" size={11} />{t.name}<span>{t.status || "running"}</span></summary>
          {t.input !== undefined && <pre>{formatToolData(t.input)}</pre>}
          {t.output !== undefined && <pre>{formatToolData(t.output)}</pre>}
        </details>)}
      </div>}
      {text && <div className={s.bubble}>{m.role === "assistant" ? <Markdown remarkPlugins={[remarkGfm]}>{text}</Markdown> : text}</div>}
      {m.error && <div className={`${s.bubble} ${s.fail}`}>{m.error}</div>}
    </div>
  );
}

function cleanAssistantText(text: string) {
  return text.replace(/(?:\n+|^)\(?No tools used for this response\.\)?\s*$/i, "").trimEnd();
}

function formatToolData(value: unknown) {
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}
