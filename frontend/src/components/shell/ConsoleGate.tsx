"use client";

import { useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState } from "react";
import { endSession, restoreSession } from "@/product/supabase";

export type Team = { id: string; name: string; division: string };
export type Employee = { user_id?: string; email: string; display_name: string; roles?: string[]; teams?: Team[] };

const EmployeeContext = createContext<Employee | null>(null);

export function useEmployee() {
  return useContext(EmployeeContext);
}

function signInPath() {
  const next = window.location.pathname + window.location.search;
  return `/sign-in?next=${encodeURIComponent(next)}`;
}

export default function ConsoleGate({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const [employee, setEmployee] = useState<Employee | null>(null);
  const [problem, setProblem] = useState("");

  useEffect(() => {
    let stop = false;
    void (async () => {
      const token = await restoreSession();
      if (stop) return;
      if (!token) {
        router.replace(signInPath());
        return;
      }
      try {
        const response = await fetch("/api/v1/me", { headers: { Accept: "application/json", Authorization: `Bearer ${token}` } });
        if (stop) return;
        if (response.status === 401) {
          await endSession();
          router.replace(signInPath());
          return;
        }
        if (!response.ok) {
          const body = await response.json().catch(() => null) as { message?: string } | null;
          setProblem(body?.message || "The application API is unavailable.");
          return;
        }
        setEmployee(await response.json() as Employee);
      } catch {
        if (!stop) setProblem("The application API is unavailable.");
      }
    })();
    return () => {
      stop = true;
    };
  }, [router]);

  if (!employee) {
    return (
      <div style={{ margin: 0, minHeight: "100vh", padding: 28, color: "var(--ink-3)" }}>
        <p>{problem || "Checking session…"}</p>
        {problem ? (
          <button className="btn" type="button" style={{ marginTop: 16 }} onClick={() => { void endSession().then(() => router.replace("/sign-in")); }}>
            Sign out
          </button>
        ) : null}
      </div>
    );
  }

  return <EmployeeContext.Provider value={employee}>{children}</EmployeeContext.Provider>;
}
