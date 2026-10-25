"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import Logo from "@/components/brand/Logo";
import Icon, { type IconName } from "@/components/ui/Icon";
import { endSession } from "@/product/supabase";
import { isTerminal } from "@/lib/live";
import { useStore } from "@/lib/store";
import { usePendingApprovals } from "@/components/approvals/usePendingApprovals";
import { useEmployee } from "./ConsoleGate";
import s from "./Rail.module.css";

const groups: { href: string; label: string; icon: IconName }[][] = [
  [
    { href: "/dashboard", label: "Dashboard", icon: "activity" },
    { href: "/threads", label: "Threads", icon: "message" },
    { href: "/agents", label: "Agents", icon: "bot" },
    { href: "/tools", label: "Tools", icon: "wrench" },
  ],
];

function toggleTheme(e: React.MouseEvent<HTMLButtonElement>) {
  const root = document.documentElement;
  const next = root.dataset.theme === "dark" ? "light" : "dark";
  const apply = () => { root.dataset.theme = next; };
  try { localStorage.setItem("nmc3-theme", next); } catch {}
  if (!document.startViewTransition || matchMedia("(prefers-reduced-motion: reduce)").matches) return apply();
  const r = e.currentTarget.getBoundingClientRect(), x = r.left + r.width / 2, y = r.top + r.height / 2;
  const end = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y));
  document.startViewTransition(apply).ready.then(() =>
    root.animate({ clipPath: [`circle(0 at ${x}px ${y}px)`, `circle(${end}px at ${x}px ${y}px)`] },
      { duration: 650, easing: "cubic-bezier(0.16, 1, 0.3, 1)", pseudoElement: "::view-transition-new(root)" }));
}

function initials(name: string, email: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const letters = parts.length >= 2 ? parts[0][0] + parts[1][0] : (parts[0] || email).slice(0, 2);
  return letters.toUpperCase();
}

export default function Rail() {
  const path = usePathname();
  const router = useRouter();
  const employee = useEmployee();
  const { live } = useStore();
  const waiting = usePendingApprovals().approvals.length;
  const running = live ? !isTerminal(live.final) : false;
  const active = (href: string) => (href === "/dashboard" ? path === "/dashboard" || path.startsWith("/live-run") : path.startsWith(href));
  const [open, setOpen] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => { if (!menu.current?.contains(e.target as Node)) setOpen(false); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => { document.removeEventListener("mousedown", down); document.removeEventListener("keydown", key); };
  }, [open]);
  const signOut = () => { void endSession().then(() => router.replace("/sign-in")); };

  return (
    <nav className={s.rail} aria-label="Primary">
      <Link href="/" className={s.mark} aria-label="NMC³ home"><Logo /></Link>
      {groups.map((g, i) => (
        <div key={i} className={s.group}>
          {i > 0 && <span className={s.sep} />}
          {g.map((n) => (
            <Link key={n.href} href={n.href} className="ibtn" aria-current={active(n.href) ? "page" : undefined} data-tip={n.label} data-tip-side aria-label={n.label}>
              <Icon name={n.icon} size={17} />
              {n.href === "/dashboard" && waiting > 0
                ? <i className={`dot ${s.live} ${s.wait}`} title={`${waiting} waiting for approval`} />
                : n.href === "/dashboard" && running && <i className={`dot live c-run ${s.live}`} />}
            </Link>
          ))}
        </div>
      ))}
      <div className={s.bot}>
        <button type="button" className={`ibtn ${s.theme}`} onClick={toggleTheme} aria-label="Switch theme" data-tip="Theme" data-tip-side>
          <span className={s.moon}><Icon name="moon" size={17} /></span>
          <span className={s.sun}><Icon name="sun" size={17} /></span>
        </button>
        <Link href="/settings" className="ibtn" aria-current={path.startsWith("/settings") ? "page" : undefined} aria-label="Settings" data-tip="Settings" data-tip-side>
          <Icon name="settings" size={17} />
        </Link>
        <div className={s.profile} ref={menu}>
          <button type="button" className={s.avatar} onClick={() => setOpen((v) => !v)} aria-label="Account" aria-haspopup="dialog" aria-expanded={open}>
            {initials(employee?.display_name ?? "", employee?.email ?? "")}
          </button>
          {open && (
            <div className={s.card} role="dialog" aria-label="Your profile">
              <div className={s.head}>
                <div className={`${s.avatar} ${s.big}`} aria-hidden>{initials(employee?.display_name ?? "", employee?.email ?? "")}</div>
                <div className={s.who}>
                  <strong>{employee?.display_name || "Unnamed"}</strong>
                  <span>{employee?.email}</span>
                </div>
              </div>
              <dl className={s.facts}>
                <dt>Roles</dt>
                <dd>{employee?.roles?.length ? employee.roles.map((r) => <span key={r} className={s.chip}>{r.replace(/_/g, " ")}</span>) : <span className={s.none}>None assigned</span>}</dd>
                <dt>Teams</dt>
                <dd>
                  {employee?.teams?.length
                    ? employee.teams.map((t) => <div key={t.id} className={s.team}>{t.name}<span>{t.division}</span></div>)
                    : <span className={s.none}>No active teams</span>}
                </dd>
              </dl>
              <Link href="/org" className={s.row} onClick={() => setOpen(false)}><Icon name="building" size={15} />Organization</Link>
              <button type="button" className={s.row} onClick={signOut}><Icon name="log-out" size={15} />Sign out</button>
            </div>
          )}
        </div>
      </div>
    </nav>
  );
}
