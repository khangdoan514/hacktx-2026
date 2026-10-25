"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import Icon, { type IconName } from "@/components/ui/Icon";
import { useEmployee } from "@/components/shell/ConsoleGate";
import { endSession } from "@/product/supabase";
import s from "./overview.module.css";

const groups: { label: string; items: { href: string; label: string; icon: IconName }[] }[] = [
  { label: "Work", items: [
    { href: "/overview", label: "Overview", icon: "activity" },
    { href: "/runs", label: "Runs", icon: "play" },
    { href: "/approvals", label: "Approvals", icon: "badge-check" },
  ] },
  { label: "Build", items: [
    { href: "/agents", label: "Agents", icon: "bot" },
    { href: "/definitions", label: "Definitions", icon: "scroll-text" },
    { href: "/tools", label: "Tools", icon: "wrench" },
  ] },
  { label: "Govern", items: [
    { href: "/budgets", label: "Budgets", icon: "gauge" },
    { href: "/guardrails", label: "Guardrails", icon: "shield" },
    { href: "/org", label: "Organization", icon: "building" },
  ] },
];

export default function Sidebar({ waiting }: { waiting: number }) {
  const path = usePathname();
  const router = useRouter();
  const employee = useEmployee();
  return (
    <nav className={s.side} aria-label="Primary">
      <Link href="/" className={s.brand}><span className="mark">N³</span>NMC³ Console</Link>
      {groups.map((g) => (
        <div key={g.label} className={s.group}>
          <span className={s.gl}>{g.label}</span>
          {g.items.map((n) => (
            <Link key={n.href} href={n.href} className={s.nav} aria-current={path === n.href ? "page" : undefined}>
              <Icon name={n.icon} size={16} />{n.label}
              {n.href === "/approvals" && waiting > 0 && <span className={s.badge}>{waiting} waiting</span>}
            </Link>
          ))}
        </div>
      ))}
      <div className={s.foot}>
        {employee?.display_name || employee?.email}
        <button type="button" className="btn ghost sm" style={{ marginTop: 8, width: "100%" }}
          onClick={() => { void endSession().then(() => router.replace("/sign-in")); }}>Sign out</button>
      </div>
    </nav>
  );
}
