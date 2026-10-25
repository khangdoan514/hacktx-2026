"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { endSession, restoreSession } from "./supabase";

const PUBLIC = [/^\/sign-in$/, /^\/auth\/callback$/, /^\/invitations\/accept$/];

export default function ProductShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [ready, setReady] = useState(false);
  const isPublic = PUBLIC.some((pattern) => pattern.test(pathname));
  const onDashboard = pathname.includes("/dashboards");

  useEffect(() => {
    let stop = false;
    void (async () => {
      if (!isPublic && !(await restoreSession())) {
        if (!stop) router.replace(`/sign-in?next=${encodeURIComponent(pathname)}`);
        return;
      }
      if (!stop) setReady(true);
    })();
    return () => {
      stop = true;
    };
  }, [isPublic, pathname, router]);

  if (!ready && !isPublic) {
    return <p className="p-6 text-sm text-gray-500">Checking session…</p>;
  }

  if (isPublic) {
    return <div className="min-h-screen">{children}</div>;
  }

  return (
    <div className="min-h-screen bg-[#f6f7f9] text-gray-900">
      <header className="border-b border-gray-200 bg-white">
        <div className="mx-auto flex max-w-5xl items-center justify-between px-4 py-3">
          <Link href={onDashboard ? "/workspaces" : "/org"} className="text-sm font-semibold tracking-wide">
            NMC3
          </Link>
          <nav className="flex items-center gap-4 text-sm">
            {onDashboard ? (
              <>
                <Link href="/workspaces">Workspaces</Link>
                <Link href="/account">Account</Link>
              </>
            ) : (
              <>
                <Link href="/org">Organization</Link>
                <Link href="/requests">Requests</Link>
              </>
            )}
            <button
              type="button"
              className="text-gray-600"
              onClick={() => {
                void endSession().then(() => router.push("/sign-in"));
              }}
            >
              Sign out
            </button>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-4 py-6">{children}</main>
    </div>
  );
}

export function WorkspaceNav({ workspaceId }: { workspaceId: string }) {
  const base = `/w/${workspaceId}`;
  const links = [
    ["My work", `${base}/my-work`],
    ["Projects", `${base}/projects`],
    ["Requests", `${base}/requests`],
    ["Approvals", `${base}/approvals`],
    ["Partners", `${base}/partners`],
    ["Data", `${base}/data`],
    ["Metrics", `${base}/metrics`],
    ["Questions", `${base}/data/questions`],
    ["Inbox", `${base}/inbox`],
    ["Members", `${base}/settings/members`],
    ["Audit", `${base}/settings/audit`],
  ];
  return (
    <nav className="mb-6 flex flex-wrap gap-3 text-sm">
      {links.map(([label, href]) => (
        <Link key={href} href={href} className="rounded-full bg-white px-3 py-1 ring-1 ring-gray-200">
          {label}
        </Link>
      ))}
    </nav>
  );
}

export function ProjectNav({ workspaceId, projectId }: { workspaceId: string; projectId: string }) {
  const base = `/w/${workspaceId}/projects/${projectId}`;
  const links = [
    ["Overview", base],
    ["Tasks", `${base}/tasks`],
    ["Requirements", `${base}/requirements`],
    ["Handoffs", `${base}/handoffs`],
    ["Documents", `${base}/documents`],
    ["Discussion", `${base}/discussions`],
    ["Analyses", `${base}/analyses`],
    ["Dashboards", `${base}/dashboards`],
    ["Members", `${base}/members`],
    ["Activity", `${base}/activity`],
  ];
  return (
    <nav className="mb-4 flex flex-wrap gap-2 text-sm text-gray-700">
      {links.map(([label, href]) => (
        <Link key={href} href={href} className="underline-offset-2 hover:underline">
          {label}
        </Link>
      ))}
    </nav>
  );
}
