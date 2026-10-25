"use client";

import { useRouter } from "next/navigation";
import { createContext, useContext, useEffect, useState } from "react";
import { clearToken, readToken } from "@/product/api";

export type Employee = { email: string; display_name: string };

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
    const token = readToken();
    if (!token) {
      router.replace(signInPath());
      return;
    }
    let stop = false;
    fetch("/api/v1/me", { headers: { Accept: "application/json", Authorization: `Bearer ${token}` } })
      .then(async (response) => {
        if (stop) return;
        if (response.status === 401 || response.status === 403) {
          clearToken();
          router.replace(signInPath());
          return;
        }
        if (!response.ok) {
          setProblem("The application API is unavailable.");
          return;
        }
        setEmployee(await response.json() as Employee);
      })
      .catch(() => {
        if (!stop) setProblem("The application API is unavailable.");
      });
    return () => {
      stop = true;
    };
  }, [router]);

  if (!employee) {
    return (
      <p style={{ margin: 0, minHeight: "100vh", padding: 28, color: "var(--ink-3)" }}>
        {problem || "Checking session…"}
      </p>
    );
  }

  return <EmployeeContext.Provider value={employee}>{children}</EmployeeContext.Provider>;
}
