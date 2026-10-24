"use client";

import { FormEvent, useState } from "react";
import { useParams } from "next/navigation";
import { api } from "@/product/api";

export default function WorkspaceSettingsPage() {
  const params = useParams<{ w: string }>();
  const [name, setName] = useState("");
  const [version, setVersion] = useState(1);
  const [error, setError] = useState("");

  async function onSubmit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const current = await api<{ version: number }>(`/workspaces/${params.w}`);
      const updated = await api<{ version: number }>(`/workspaces/${params.w}`, {
        method: "PATCH",
        body: JSON.stringify({ expected_version: current.version, name }),
      });
      setVersion(updated.version);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save");
    }
  }

  return (
    <form onSubmit={onSubmit} className="max-w-md space-y-3">
      <h1 className="text-xl font-semibold">Workspace settings</h1>
      <input className="w-full rounded border px-3 py-2 text-sm" value={name} onChange={(event) => setName(event.target.value)} placeholder="Workspace name" required />
      <button className="rounded bg-gray-900 px-3 py-2 text-sm text-white" type="submit">Save</button>
      <p className="text-xs text-gray-500">Version {version}</p>
      {error ? <p className="text-sm text-red-700">{error}</p> : null}
    </form>
  );
}
