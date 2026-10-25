"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import PageHeader from "@/components/shell/PageHeader";
import { useEmployee } from "@/components/shell/ConsoleGate";
import { nmc } from "@/product/nmc3/client";
import { endSession } from "@/product/supabase";
import s from "./settings.module.css";

type Organization = { name: string; divisions: { name: string; teams: { id: string; name: string }[] }[] };

export default function SettingsPage() {
  const router = useRouter();
  const employee = useEmployee();
  const [org, setOrg] = useState<Organization | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    nmc<Organization>("/api/v1/organization").then(setOrg).catch((err: Error) => setError(err.message));
  }, []);

  return (
    <>
      <PageHeader title="Settings" sub="Your account and the organizations you belong to." />
      <section className="card rise">
        <div className="ch"><h2>Account</h2></div>
        <dl className={s.kv}>
          <div><dt>Name</dt><dd>{employee?.display_name || "—"}</dd></div>
          <div><dt>Email</dt><dd>{employee?.email}</dd></div>
        </dl>
        <div className={s.foot}>
          <button type="button" className="btn" onClick={() => { void endSession().then(() => router.replace("/sign-in")); }}>Sign out</button>
        </div>
      </section>
      <section className="card rise" style={{ "--d": "60ms" } as React.CSSProperties}>
        <div className="ch"><h2>Organizations</h2><span className="n">{org ? 1 : ""}</span></div>
        {error && <p className={s.err} role="alert">{error}</p>}
        {org && (
          <ul className="rows">
            <li className={s.org}>
              <b>{org.name}</b>
              <span className={s.tags}>{org.divisions.map((d) => <span key={d.name} className="tag">{d.name}</span>)}</span>
            </li>
          </ul>
        )}
      </section>
    </>
  );
}
