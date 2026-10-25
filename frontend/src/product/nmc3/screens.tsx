"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useEffect, useState } from "react";
import Logo from "@/components/brand/Logo";
import PageHeader from "@/components/shell/PageHeader";
import { writeToken } from "@/product/api";
import { supabase } from "@/product/supabase";
import { nmc } from "./client";
import org from "./org.module.css";
import signIn from "./signin.module.css";

type RequestRow = {
  id: string;
  title: string;
  outcome: string;
  state: string;
  version: number;
  requester_id: string;
};

type Detail = {
  request: RequestRow;
  clarifications: { id: string; question: string; answer: string | null }[];
  specification: { id?: string; state?: string; version?: number; material_revision?: number; document?: Record<string, unknown> };
  handovers: { id: string; state: string; version: number }[];
  resources: { id: string; kind: string; requested_amount: number | null; approved_amount: number | null; usage_amount: number | null; unit: string }[];
  approvals: { id: string; state: string; version: number; reviewer_id: string }[];
  catalog: { id: string; state: string; version: number } | null;
};

const emptySpec = {
  identity: "pr-review",
  purpose: "Review a pull request",
  interface: { kind: "pull_request" },
  instructions: "Cite only the diff.",
  model_policy: { mode: "recorded" },
  tools: ["diff.read"],
  context: { repository: "billing" },
  resources: { gpu: 0 },
  budget: { usd: 0 },
  guardrails: { read_only: true },
  evaluations: { suite: "pr" },
  operations: { handover: true },
  output_action_types: ["comment"],
};

export function SignInScreen() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    if (process.env.NEXT_PUBLIC_LOCAL_AUTH === "true") {
      try {
        const session = await nmc<{ access_token: string }>("/api/v1/auth/local-session", { method: "POST", body: JSON.stringify({ email, password }) });
        writeToken(session.access_token);
      } catch (err) { setError(err instanceof Error ? err.message : "Sign-in failed"); return; }
    } else {
      if (!supabase) { setError("Supabase auth is not configured."); return; }
      const { error: signInError } = await supabase.auth.signInWithPassword({ email, password });
      if (signInError) {
        // Supabase rejects some seeded addresses, including ali-analytics@nmc2.test.
        // The local employee password still signs those people in.
        const local = await fetch("/api/v1/auth/local-session", {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          body: JSON.stringify({ email, password }),
        });
        const body = local.ok ? ((await local.json()) as { access_token?: string }) : null;
        if (!body?.access_token) {
          setError(signInError.message);
          return;
        }
        writeToken(body.access_token);
      }
    }
    const next = new URLSearchParams(window.location.search).get("next");
    router.push(next && next.startsWith("/") && !next.startsWith("//") && !next.includes("\\") ? next : "/dashboard");
  }

  return (
    <form onSubmit={onSubmit} className={signIn.page}>
      <div className={signIn.card}>
        <Logo className={signIn.logo} />
        <div>
          <h1>Sign in</h1>
          <p className="muted">NMC2 employees use the account issued for their team.</p>
        </div>
        <input className="input" placeholder="Email" value={email} onChange={(event) => setEmail(event.target.value)} data-testid="email" autoComplete="username" />
        <input className="input" placeholder="Password" type="password" value={password} onChange={(event) => setPassword(event.target.value)} data-testid="password" autoComplete="current-password" />
        {error ? <p className={signIn.error}>{error}</p> : null}
        <button className="btn solid" type="submit">Sign in</button>
      </div>
    </form>
  );
}

export function OrgScreen() {
  const [data, setData] = useState<{ name: string; divisions: { name: string; teams: { id: string; name: string }[] }[] } | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    nmc<typeof data>("/api/v1/organization").then(setData).catch((err: Error) => setError(err.message));
  }, []);
  return (
    <>
      <PageHeader
        title={data?.name || "NMC2"}
        sub="One organization. Hardware and Software are the divisions."
        actions={
          <>
            <Link className="btn" href="/requests">Requests</Link>
            <button
              className="btn"
              type="button"
              data-testid="remove-ada"
              onClick={() => {
                void nmc("/api/v1/members/20000000-0000-4000-8000-000000000001/remove", { method: "POST", body: "{}" }).catch((err: Error) => setError(err.message));
              }}
            >
              Remove Hardware access
            </button>
          </>
        }
      />
      {error ? <p className={org.err}>{error}</p> : null}
      <div className={org.grid}>
        {data?.divisions.map((division) => (
          <section key={division.name} className="card rise">
            <div className="ch">
              <h2>{division.name}</h2>
              <span className={`n ${org.count}`}>{division.teams.length}</span>
            </div>
            <div className="rows">
              {division.teams.map((team) => (
                <div key={team.id} className={org.row}>{team.name}</div>
              ))}
            </div>
          </section>
        ))}
      </div>
    </>
  );
}

export function RequestListScreen() {
  const router = useRouter();
  const [items, setItems] = useState<RequestRow[]>([]);
  const [title, setTitle] = useState("Review the billing change");
  const [outcome, setOutcome] = useState("Findings cite the diff");
  const [error, setError] = useState("");

  function reload() {
    nmc<{ items: RequestRow[] }>("/api/v1/requests").then((data) => setItems(data.items)).catch((err: Error) => setError(err.message));
  }
  useEffect(reload, []);

  async function create(event: FormEvent) {
    event.preventDefault();
    const created = await nmc<RequestRow>("/api/v1/requests", { method: "POST", body: JSON.stringify({ title, outcome }) });
    router.push(`/requests/${created.id}`);
  }

  return (
    <section className="space-y-4">
      <h1 className="text-xl font-semibold">Capability requests</h1>
      <form onSubmit={create} className="space-y-2 rounded-lg bg-white p-4">
        <input className="w-full rounded border px-3 py-2" value={title} onChange={(event) => setTitle(event.target.value)} data-testid="request-title" />
        <textarea className="w-full rounded border px-3 py-2" value={outcome} onChange={(event) => setOutcome(event.target.value)} data-testid="request-outcome" />
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">Save draft</button>
      </form>
      {error ? <p className="text-sm text-red-700">{error}</p> : null}
      <ul className="space-y-2">
        {items.map((item) => (
          <li key={item.id}><Link className="block rounded bg-white p-3" href={`/requests/${item.id}`}>{item.title} · {item.state}</Link></li>
        ))}
      </ul>
    </section>
  );
}

export function RequestDetailScreen({ id }: { id: string }) {
  const [detail, setDetail] = useState<Detail | null>(null);
  const [error, setError] = useState("");
  const [question, setQuestion] = useState("Which repository is in scope?");
  const [answer, setAnswer] = useState("The fictional billing repository.");
  const [comment, setComment] = useState("Matches the request");
  const [runId, setRunId] = useState("");

  function reload() {
    nmc<Detail>(`/api/v1/requests/${id}`).then(setDetail).catch((err: Error) => setError(err.message));
  }
  useEffect(reload, [id]);

  async function act(path: string, body: Record<string, unknown>) {
    setError("");
    try {
      await nmc(path, { method: "POST", body: JSON.stringify(body) });
      reload();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Request failed");
    }
  }

  if (!detail) return <p className="text-sm text-gray-500">{error || "Loading…"}</p>;
  const request = detail.request;
  const spec = detail.specification?.id ? detail.specification : null;
  const handover = detail.handovers[detail.handovers.length - 1];

  return (
    <article className="space-y-4">
      <header>
        <h1 className="text-xl font-semibold" data-testid="request-heading">{request.title}</h1>
        <p className="text-sm text-gray-600">{request.outcome}</p>
        <p data-testid="request-state">State: {request.state} · version {request.version}</p>
      </header>
      {error ? <p className="text-sm text-red-700" data-testid="request-error">{error}</p> : null}
      {request.state === "draft" ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/save`, { expected_version: request.version, title: request.title, outcome: request.outcome }).then(() => act(`/api/v1/requests/${id}/submit`, { expected_version: request.version + 1 }))} data-testid="submit-request">Submit</button> : null}
      {request.state === "submitted" ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/triage`, { expected_version: request.version })} data-testid="triage">Triage</button> : null}
      {request.state === "triage" ? (
        <div className="space-y-2 rounded bg-white p-4">
          <textarea className="w-full rounded border px-3 py-2" value={question} onChange={(event) => setQuestion(event.target.value)} data-testid="question" />
          <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/clarify`, { expected_version: request.version, question })} data-testid="ask">Ask</button>
          <button className="ml-2 rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/requests/${id}/accept`, { expected_version: request.version })} data-testid="accept-request">Accept</button>
        </div>
      ) : null}
      {request.state === "needs_information" ? (
        <div className="space-y-2 rounded bg-white p-4">
          {detail.clarifications.map((item) => <p key={item.id}>{item.question} {item.answer ? `— ${item.answer}` : ""}</p>)}
          <textarea className="w-full rounded border px-3 py-2" value={answer} onChange={(event) => setAnswer(event.target.value)} data-testid="answer" />
          <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/answer`, { expected_version: request.version, clarification_id: detail.clarifications.find((item) => !item.answer)?.id, answer })} data-testid="send-answer">Answer</button>
        </div>
      ) : null}
      {request.state === "accepted" && !spec ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/specifications`, { document: emptySpec })} data-testid="draft-spec">Draft specification</button> : null}
      {spec?.state === "draft" || spec?.state === "changes_requested" ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/specifications/${spec.id}/submit-review`, { expected_version: spec.version })} data-testid="submit-spec">Submit specification</button> : null}
      {spec?.state === "in_review" && !detail.approvals?.some((item) => item.state === "pending") ? (
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/approvals`, { reviewer_id: "20000000-0000-4000-8000-000000000003" })} data-testid="request-approval">Request approval</button>
      ) : null}
      {detail.approvals?.filter((item) => item.state === "pending").map((item) => (
        <div key={item.id} className="space-y-2">
          <input className="rounded border px-3 py-2" value={comment} onChange={(event) => setComment(event.target.value)} data-testid="approval-comment" />
          <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/approvals/${item.id}/decide`, { decision: "approved", expected_version: item.version, comment })} data-testid="approve">Approve</button>
        </div>
      ))}
      {spec ? <p data-testid="spec-state">Specification {spec.state} revision {spec.material_revision}</p> : null}
      {(request.state === "accepted" || request.state === "in_delivery") && spec?.state === "approved" ? <button className="rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/requests/${id}/start-delivery`, { expected_version: request.version })} data-testid="start-delivery">Start delivery</button> : null}
      {request.state === "in_delivery" && !handover ? <button className="rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/requests/${id}/handovers`, {})} data-testid="create-handover">Create handover</button> : null}
      {handover?.state === "draft" ? <button className="rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/handovers/${handover.id}/send`, { expected_version: handover.version })} data-testid="send-handover">Send handover</button> : null}
      {handover && ["sent", "acknowledged", "completed"].includes(handover.state) && request.state === "in_delivery" ? <button className="rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/requests/${id}/ready`, { expected_version: request.version })} data-testid="mark-ready">Ready for acceptance</button> : null}
      {handover?.state === "sent" ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/handovers/${handover.id}/acknowledge`, { expected_version: handover.version })} data-testid="acknowledge">Acknowledge</button> : null}
      {handover?.state === "acknowledged" ? <button className="rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/handovers/${handover.id}/complete`, { expected_version: handover.version })} data-testid="complete-handover">Complete handover</button> : null}
      {request.state === "ready_for_acceptance" ? <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={() => act(`/api/v1/requests/${id}/fulfill`, { expected_version: request.version })} data-testid="fulfill">Fulfill</button> : null}
      {spec?.state === "approved" ? <CatalogControls specificationId={spec.id || ""} initial={detail.catalog} onError={setError} /> : null}
      <section className="rounded bg-white p-4">
        <h2 className="font-medium">Resources</h2>
        <ul className="text-sm">
          {detail.resources.map((item) => <li key={item.id}>{item.kind}: requested {item.requested_amount ?? "—"}, approved {item.approved_amount ?? "—"}, usage {item.usage_amount === null ? "unmeasured" : item.usage_amount} {item.unit}</li>)}
        </ul>
        <button className="mt-2 rounded border px-3 py-2 text-sm" onClick={() => act(`/api/v1/requests/${id}/resources`, { kind: "budget", unit: "USD", amount: 25 })} data-testid="add-budget">Record budget</button>
      </section>
      {request.state === "in_delivery" || request.state === "fulfilled" || spec?.state === "approved" ? (
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" onClick={async () => {
          const diff = "diff --git a/billing/invoice.go b/billing/invoice.go\n--- a/billing/invoice.go\n+++ b/billing/invoice.go\n@@ -1,4 +1,9 @@\n package billing\n \n func Charge(lines []Line) int {\n-    return 0\n+    total := 0\n+    for _, line := range lines {\n+        total += line.Amount\n+    }\n+    return total\n }\n";
          const started = await nmc<{ run_id: string }>("/v1/agent-runs", { method: "POST", body: JSON.stringify({ agent_id: "pr-review-orchestrator", agent_version: "1.0.0", input: { diff }, limits: { max_delegations: 2, max_concurrent_agents: 2, max_tool_calls: 4, max_model_turns: 8, max_tokens: 12000, max_cost_usd: 2, timeout_seconds: 30 } }) });
          setRunId(started.run_id);
        }} data-testid="start-run">Run PR Review Assistant</button>
      ) : null}
      {runId ? <Link href={`/live-run/${runId}`} data-testid="open-run">Open run</Link> : null}
    </article>
  );
}

function CatalogControls({ specificationId, initial, onError }: { specificationId: string; initial: { id: string; version: number; state: string } | null; onError: (message: string) => void }) {
  const [catalog, setCatalog] = useState<{ id: string; version: number; state: string } | null>(initial);
  async function post(path: string, body: Record<string, unknown>) {
    try {
      const next = await nmc<{ id: string; version: number; state: string }>(path, { method: "POST", body: JSON.stringify(body) });
      setCatalog(next);
    } catch (err) {
      onError(err instanceof Error ? err.message : "Catalog request failed");
    }
  }
  return (
    <section className="rounded bg-white p-4 text-sm">
      <h2 className="font-medium">Catalog</h2>
      <p data-testid="catalog-state">{catalog ? `${catalog.state} v${catalog.version}` : "Not opened"}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        <button className="rounded border px-3 py-1" onClick={() => post(`/api/v1/specifications/${specificationId}/catalog`, {})} data-testid="open-catalog">Open release</button>
        {catalog ? <button className="rounded border px-3 py-1" onClick={() => post(`/api/v1/catalog/${catalog.id}/test`, { expected_version: catalog.version })} data-testid="start-test">Start test</button> : null}
        {catalog ? <button className="rounded border px-3 py-1" onClick={() => post(`/api/v1/catalog/${catalog.id}/evaluations`, { passed: true, evidence: "The deterministic review completed." })} data-testid="record-evaluation">Record evaluation</button> : null}
        {catalog ? <button className="rounded border px-3 py-1" onClick={() => post(`/api/v1/catalog/${catalog.id}/publish`, { expected_version: catalog.version })} data-testid="publish">Publish</button> : null}
      </div>
    </section>
  );
}

export function RunScreen({ id }: { id: string }) {
  const [run, setRun] = useState<{ state: string; agent_id: string; invocations: { id: string; agent_id: string; depth: number; state: string; parent_id: string | null }[] } | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let stop = false;
    async function load() {
      try {
        const body = await nmc<{ run: NonNullable<typeof run> }>(`/v1/agent-runs/${id}`);
        if (!stop) setRun(body.run);
      } catch (err) {
        if (!stop) setError(err instanceof Error ? err.message : "Could not load the run");
      }
    }
    void load();
    const timer = window.setInterval(() => void load(), 1000);
    return () => {
      stop = true;
      window.clearInterval(timer);
    };
  }, [id]);
  const name = run?.agent_id === "pr-review-orchestrator" ? "PR Review Assistant" : run?.agent_id;
  return (
    <section className="space-y-3">
      <h1 className="text-xl font-semibold" data-testid="run-name">{name || "Run"}</h1>
      <p data-testid="run-state">{run?.state || error || "Loading…"}</p>
      <ul className="space-y-1 text-sm" data-testid="invocation-tree">
        {run?.invocations.map((invocation) => (
          <li key={invocation.id} style={{ marginLeft: invocation.depth * 16 }}>
            {invocation.agent_id} · depth {invocation.depth} · {invocation.state}
          </li>
        ))}
      </ul>
    </section>
  );
}
