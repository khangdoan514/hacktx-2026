"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { FormEvent, useCallback, useEffect, useState } from "react";
import { api, clearToken, writeToken } from "./api";
import { ProjectNav } from "./shell";

type Row = Record<string, unknown>;

function asRows(data: unknown): Row[] {
  if (data && typeof data === "object" && "items" in data && Array.isArray((data as { items: unknown }).items)) {
    return (data as { items: Row[] }).items;
  }
  return [];
}

function ErrorText({ error }: { error: string }) {
  if (!error) return null;
  return <p className="mt-2 text-sm text-red-700">{error}</p>;
}

function useLoad<T>(path: string | null) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState("");
  const reload = useCallback(() => {
    if (!path) return;
    api<T>(path)
      .then((next) => {
        setData(next);
        setError("");
      })
      .catch((err: Error) => setError(err.message));
  }, [path]);
  useEffect(() => {
    reload();
    if (!path) return;
    const timer = window.setInterval(reload, 15000);
    return () => window.clearInterval(timer);
  }, [path, reload]);
  return { data, error, reload };
}

export function SignInScreen() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [create, setCreate] = useState(true);
  const [error, setError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const supabaseUrl = process.env.NEXT_PUBLIC_SUPABASE_URL;
      const supabaseKey = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;
      let token = "";
      if (supabaseUrl && supabaseKey) {
        const { createClient } = await import("@supabase/supabase-js");
        const supabase = createClient(supabaseUrl, supabaseKey);
        const result = create
          ? await supabase.auth.signUp({ email, password })
          : await supabase.auth.signInWithPassword({ email, password });
        if (result.error || !result.data.session) {
          throw new Error(result.error?.message || "Sign-in did not return a session");
        }
        token = result.data.session.access_token;
      } else {
        const session = await api<{ access_token: string }>("/auth/local-session", {
          method: "POST",
          body: JSON.stringify({
            email,
            password,
            create,
            display_name: displayName || undefined,
          }),
        });
        token = session.access_token;
      }
      writeToken(token);
      const next = new URLSearchParams(window.location.search).get("next") || "/workspaces";
      router.push(next.startsWith("/") && !next.startsWith("//") ? next : "/workspaces");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign-in failed");
    }
  }

  return (
    <form onSubmit={onSubmit} className="mx-auto max-w-md space-y-3 rounded-xl bg-white p-6 ring-1 ring-gray-200">
      <h1 className="text-xl font-semibold">Sign in</h1>
      <p className="text-sm text-gray-600">Use your company account. Assistance is not part of this screen.</p>
      <label className="block text-sm">
        Email
        <input className="mt-1 w-full rounded border px-3 py-2" value={email} onChange={(event) => setEmail(event.target.value)} required />
      </label>
      <label className="block text-sm">
        Password
        <input className="mt-1 w-full rounded border px-3 py-2" type="password" value={password} onChange={(event) => setPassword(event.target.value)} required minLength={8} />
      </label>
      {create ? (
        <label className="block text-sm">
          Display name
          <input className="mt-1 w-full rounded border px-3 py-2" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
        </label>
      ) : null}
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" checked={create} onChange={(event) => setCreate(event.target.checked)} />
        Create account
      </label>
      <button className="rounded bg-gray-900 px-4 py-2 text-sm text-white" type="submit">
        Continue
      </button>
      <ErrorText error={error} />
    </form>
  );
}

export function CallbackScreen() {
  return (
    <div className="rounded-xl bg-white p-6 ring-1 ring-gray-200">
      <h1 className="text-xl font-semibold">Auth callback</h1>
      <p className="mt-2 text-sm text-gray-600">
        If a Supabase project is configured, return here after email verification. Otherwise continue from sign-in.
      </p>
      <Link className="mt-4 inline-block text-sm underline" href="/sign-in">
        Back to sign-in
      </Link>
    </div>
  );
}

export function WorkspacesScreen({ onboarding = false }: { onboarding?: boolean }) {
  const { data, error, reload } = useLoad<{ items: Row[] }>("/workspaces");
  const [name, setName] = useState(onboarding ? "NMC3" : "");
  const [formError, setFormError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setFormError("");
    try {
      await api("/workspaces", { method: "POST", body: JSON.stringify({ name, timezone: "UTC" }) });
      setName("");
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not create workspace");
    }
  }

  return (
    <section>
      <h1 className="text-xl font-semibold">{onboarding ? "Create your company" : "Workspaces"}</h1>
      <ul className="mt-4 space-y-2">
        {(data?.items || []).map((item) => (
          <li key={String(item.id)}>
            <Link className="text-sm underline" href={`/w/${item.id}/projects`}>
              {String(item.name)}
            </Link>
          </li>
        ))}
      </ul>
      <form onSubmit={onSubmit} className="mt-6 flex gap-2">
        <input className="rounded border px-3 py-2 text-sm" placeholder="Company name" value={name} onChange={(event) => setName(event.target.value)} required />
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
          Create workspace
        </button>
      </form>
      <ErrorText error={error || formError} />
    </section>
  );
}

export function AccountScreen() {
  const { data, error, reload } = useLoad<{ profile: Row }>("/me");
  const [displayName, setDisplayName] = useState("");
  const [timezone, setTimezone] = useState("UTC");
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (data?.profile) {
      setDisplayName(String(data.profile.display_name || ""));
      setTimezone(String(data.profile.timezone || "UTC"));
    }
  }, [data]);

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      await api("/me", { method: "PATCH", body: JSON.stringify({ display_name: displayName, timezone }) });
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not save");
    }
  }

  return (
    <form onSubmit={onSubmit} className="max-w-md space-y-3">
      <h1 className="text-xl font-semibold">Account</h1>
      <input className="w-full rounded border px-3 py-2" value={displayName} onChange={(event) => setDisplayName(event.target.value)} />
      <input className="w-full rounded border px-3 py-2" value={timezone} onChange={(event) => setTimezone(event.target.value)} />
      <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
        Save
      </button>
      <button
        type="button"
        className="ml-3 text-sm"
        onClick={() => {
          clearToken();
          window.location.href = "/sign-in";
        }}
      >
        Sign out
      </button>
      <ErrorText error={error || formError} />
    </form>
  );
}

export function AcceptInvitationScreen({ token }: { token: string }) {
  const router = useRouter();
  const [preview, setPreview] = useState<Row | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!token) return;
    fetch(`/api/v1/invitations/${encodeURIComponent(token)}`)
      .then(async (response) => {
        if (!response.ok) throw new Error("Invitation not found");
        setPreview((await response.json()) as Row);
      })
      .catch((err: Error) => setError(err.message));
  }, [token]);

  async function accept() {
    setError("");
    try {
      const result = await api<{ invitation: Row }>(`/invitations/${encodeURIComponent(token)}/accept`, { method: "POST" });
      router.push(`/w/${result.invitation.workspace_id}/projects`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not accept");
    }
  }

  return (
    <section className="rounded-xl bg-white p-6 ring-1 ring-gray-200">
      <h1 className="text-xl font-semibold">Invitation</h1>
      {preview ? (
        <p className="mt-2 text-sm">
          {String(preview.workspace_name)} {preview.project_name ? ` / ${String(preview.project_name)}` : ""} for {String(preview.email)}
        </p>
      ) : null}
      <button className="mt-4 rounded bg-gray-900 px-3 py-2 text-sm text-white" type="button" onClick={accept}>
        Accept
      </button>
      <ErrorText error={error} />
    </section>
  );
}

export function RecordList({
  title,
  path,
  empty,
  labelKey,
}: {
  title: string;
  path: string;
  empty: string;
  labelKey: string;
}) {
  const { data, error } = useLoad<{ items: Row[] }>(path);
  const items = asRows(data);
  return (
    <section>
      <h1 className="text-xl font-semibold">{title}</h1>
      {items.length === 0 ? <p className="mt-3 text-sm text-gray-500">{empty}</p> : null}
      <ul className="mt-4 space-y-2">
        {items.map((row) => (
          <li key={String(row.id)}>{String(row[labelKey] || row.id)}</li>
        ))}
      </ul>
      <ErrorText error={error} />
    </section>
  );
}

export function ProjectsScreen({ workspaceId }: { workspaceId: string }) {
  const { data, error, reload } = useLoad<{ items: Row[] }>(`/workspaces/${workspaceId}/projects`);
  const [name, setName] = useState("");
  const [projectType, setProjectType] = useState("internal");
  const [formError, setFormError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setFormError("");
    try {
      await api(`/workspaces/${workspaceId}/projects`, {
        method: "POST",
        body: JSON.stringify({ name, project_type: projectType, objective: "" }),
      });
      setName("");
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not create project");
    }
  }

  return (
    <section>
      <h1 className="text-xl font-semibold">Projects</h1>
      <ul className="mt-4 space-y-2">
        {asRows(data).map((item) => (
          <li key={String(item.id)}>
            <Link className="text-sm underline" href={`/w/${workspaceId}/projects/${item.id}`}>
              {String(item.name)}
            </Link>
            <span className="ml-2 text-xs text-gray-500">{String(item.state)}</span>
          </li>
        ))}
      </ul>
      <form onSubmit={onSubmit} className="mt-6 flex flex-wrap gap-2">
        <input className="rounded border px-3 py-2 text-sm" placeholder="Project name" value={name} onChange={(event) => setName(event.target.value)} required />
        <select className="rounded border px-3 py-2 text-sm" value={projectType} onChange={(event) => setProjectType(event.target.value)}>
          <option value="internal">Internal</option>
          <option value="partner">Partner</option>
        </select>
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
          Create project
        </button>
      </form>
      <ErrorText error={error || formError} />
    </section>
  );
}

export function ProjectScreen({ workspaceId, projectId }: { workspaceId: string; projectId: string }) {
  const { data, error, reload } = useLoad<Row>(`/projects/${projectId}`);
  const [objective, setObjective] = useState("");
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (data?.objective !== undefined) setObjective(String(data.objective));
  }, [data]);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!data) return;
    try {
      await api(`/projects/${projectId}`, {
        method: "PATCH",
        body: JSON.stringify({ expected_version: data.version, objective }),
      });
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not save");
    }
  }

  return (
    <section>
      <ProjectNav workspaceId={workspaceId} projectId={projectId} />
      <h1 className="text-xl font-semibold">{data ? String(data.name) : "Project"}</h1>
      <form onSubmit={save} className="mt-4 space-y-2">
        <textarea className="w-full rounded border px-3 py-2 text-sm" value={objective} onChange={(event) => setObjective(event.target.value)} />
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
          Save objective
        </button>
      </form>
      <ErrorText error={error || formError} />
    </section>
  );
}

export function TasksScreen({ workspaceId, projectId }: { workspaceId: string; projectId: string }) {
  const { data, error, reload } = useLoad<{ items: Row[] }>(`/projects/${projectId}/tasks`);
  const [title, setTitle] = useState("");
  const [formError, setFormError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setFormError("");
    try {
      await api(`/projects/${projectId}/tasks`, {
        method: "POST",
        body: JSON.stringify({ title, acceptance_criteria: "Record the result" }),
      });
      setTitle("");
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not create task");
    }
  }

  return (
    <section>
      <ProjectNav workspaceId={workspaceId} projectId={projectId} />
      <h1 className="text-xl font-semibold">Tasks</h1>
      <ul className="mt-4 space-y-2">
        {asRows(data).map((item) => (
          <li key={String(item.id)} className="text-sm">
            <Link className="underline" href={`/w/${workspaceId}/projects/${projectId}/tasks/${item.id}`}>
              {String(item.title)}
            </Link>
            <span className="ml-2 text-gray-500">{String(item.state)}</span>
          </li>
        ))}
      </ul>
      <form onSubmit={onSubmit} className="mt-6 flex gap-2">
        <input className="rounded border px-3 py-2 text-sm" placeholder="Task title" value={title} onChange={(event) => setTitle(event.target.value)} required />
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
          Add task
        </button>
      </form>
      <ErrorText error={error || formError} />
    </section>
  );
}

export function TaskScreen({ workspaceId, projectId, taskId }: { workspaceId: string; projectId: string; taskId: string }) {
  const { data, error, reload } = useLoad<Row>(`/tasks/${taskId}`);
  const [formError, setFormError] = useState("");

  async function move(action: string, extra: Record<string, string> = {}) {
    if (!data) return;
    setFormError("");
    try {
      await api(`/tasks/${taskId}/${action}`, {
        method: "POST",
        body: JSON.stringify({ expected_version: data.version, ...extra }),
      });
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not update task");
    }
  }

  return (
    <section>
      <ProjectNav workspaceId={workspaceId} projectId={projectId} />
      <h1 className="text-xl font-semibold">{data ? String(data.title) : "Task"}</h1>
      <p className="mt-2 text-sm text-gray-600">State: {data ? String(data.state) : "…"}</p>
      <div className="mt-4 flex gap-2">
        <button className="rounded border px-3 py-2 text-sm" type="button" onClick={() => move("start")}>
          Start
        </button>
        <button className="rounded border px-3 py-2 text-sm" type="button" onClick={() => move("submit-review")}>
          Submit review
        </button>
        <button className="rounded border px-3 py-2 text-sm" type="button" onClick={() => move("complete", { evidence: "Checked against the acceptance criteria" })}>
          Complete
        </button>
      </div>
      <ErrorText error={error || formError} />
    </section>
  );
}

export function EntityScreen({ path, title }: { path: string; title: string }) {
  const { data, error } = useLoad<Row>(path);
  return (
    <section>
      <h1 className="text-xl font-semibold">{title}</h1>
      <pre className="mt-4 overflow-auto rounded bg-white p-4 text-xs">{JSON.stringify(data, null, 2)}</pre>
      <ErrorText error={error} />
    </section>
  );
}

export function MyWorkScreen({ workspaceId }: { workspaceId: string }) {
  const { data, error } = useLoad<Row>(`/workspaces/${workspaceId}/my-work`);
  const groups = ["tasks", "approvals", "handoffs", "questions"] as const;
  return (
    <section>
      <h1 className="text-xl font-semibold">My work</h1>
      {groups.map((group) => (
        <div key={group} className="mt-4">
          <h2 className="text-sm font-medium capitalize">{group}</h2>
          <ul className="mt-1 text-sm text-gray-700">
            {Array.isArray(data?.[group]) && (data?.[group] as Row[]).length === 0 ? <li>None</li> : null}
            {Array.isArray(data?.[group])
              ? (data?.[group] as Row[]).map((item) => (
                  <li key={String(item.id)}>{String(item.title || item.question || item.state || item.id)}</li>
                ))
              : null}
          </ul>
        </div>
      ))}
      <ErrorText error={error} />
    </section>
  );
}

export function SimpleCreate({
  title,
  path,
  field,
  placeholder,
  extra,
}: {
  title: string;
  path: string;
  field: string;
  placeholder: string;
  extra?: Record<string, unknown>;
}) {
  const { data, error, reload } = useLoad<{ items: Row[] }>(path);
  const [value, setValue] = useState("");
  const [formError, setFormError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    try {
      await api(path, { method: "POST", body: JSON.stringify({ [field]: value, ...(extra || {}) }) });
      setValue("");
      reload();
    } catch (err) {
      setFormError(err instanceof Error ? err.message : "Could not save");
    }
  }

  return (
    <section>
      <h1 className="text-xl font-semibold">{title}</h1>
      <ul className="mt-4 space-y-2 text-sm">
        {asRows(data).map((item) => (
          <li key={String(item.id)}>{String(item.name || item.title || item.question || item.state || item.id)}</li>
        ))}
      </ul>
      <form onSubmit={onSubmit} className="mt-4 flex gap-2">
        <input className="rounded border px-3 py-2 text-sm" placeholder={placeholder} value={value} onChange={(event) => setValue(event.target.value)} required />
        <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">
          Save
        </button>
      </form>
      <ErrorText error={error || formError} />
    </section>
  );
}
