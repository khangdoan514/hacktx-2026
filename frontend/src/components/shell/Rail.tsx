"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import Icon, { type IconName } from "@/components/ui/Icon";
import { endSession } from "@/product/supabase";
import { useNow, useStore } from "@/lib/store";
import { materialize } from "@/lib/simulate";
import { useEmployee } from "./ConsoleGate";
import s from "./Rail.module.css";

const groups: { href: string; label: string; icon: IconName }[][] = [
  [
    { href: "/dashboard", label: "Live run", icon: "activity" },
    { href: "/runs", label: "Runs", icon: "play" },
    { href: "/agents", label: "Agents", icon: "bot" },
  ],
  [
    { href: "/evaluate", label: "Evaluate", icon: "flask" },
    { href: "/triggers", label: "Triggers", icon: "zap" },
    { href: "/budgets", label: "Budgets", icon: "gauge" },
    { href: "/guardrails", label: "Guardrails", icon: "shield" },
    { href: "/prompts", label: "Prompts", icon: "message" },
  ],
  [
    { href: "/definitions", label: "Definitions", icon: "scroll-text" },
    { href: "/tools", label: "Tools", icon: "wrench" },
    { href: "/approvals", label: "Approvals", icon: "badge-check" },
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
  const now = useNow();
  const running = live ? materialize(live, now).state === "RUNNING" : false;
  const active = (href: string) => (href === "/dashboard" ? path === "/dashboard" : path.startsWith(href));
  const label = employee?.display_name || employee?.email || "Signed in";

  return (
    <nav className={s.rail} aria-label="Primary">
      <Link href="/" className={s.mark} aria-label="NMC³ home"><span>N³</span></Link>
      {groups.map((g, i) => (
        <div key={i} className={s.group}>
          {i > 0 && <span className={s.sep} />}
          {g.map((n) => (
            <Link key={n.href} href={n.href} className="ibtn" aria-current={active(n.href) ? "page" : undefined} data-tip={n.label} data-tip-side aria-label={n.label}>
              <Icon name={n.icon} size={17} />
              {n.href === "/dashboard" && running && <i className={`dot live c-run ${s.live}`} />}
            </Link>
          ))}
        </div>
      ))}
      <div className={s.bot}>
        <button type="button" className={`ibtn ${s.theme}`} onClick={toggleTheme} aria-label="Switch theme" data-tip="Theme" data-tip-side>
          <span className={s.moon}><Icon name="moon" size={17} /></span>
          <span className={s.sun}><Icon name="sun" size={17} /></span>
        </button>
        <button type="button" className="ibtn" onClick={() => { void endSession().then(() => router.replace("/sign-in")); }} aria-label="Sign out" data-tip="Sign out" data-tip-side>
          <Icon name="log-out" size={17} />
        </button>
        <div className={s.avatar} title={label}>{initials(employee?.display_name ?? "", employee?.email ?? "")}</div>
      </div>
    </nav>
  );
}
