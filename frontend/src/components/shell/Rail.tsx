"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import Icon, { type IconName } from "@/components/ui/Icon";
import { useNow, useStore } from "@/lib/store";
import { materialize } from "@/lib/simulate";
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

export default function Rail() {
  const path = usePathname();
  const { live } = useStore();
  const now = useNow();
  const running = live ? materialize(live, now).state === "RUNNING" : false;
  const active = (href: string) => (href === "/dashboard" ? path === "/dashboard" : path.startsWith(href));

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
        <div className={s.avatar} title="Khang Doan">KD</div>
      </div>
    </nav>
  );
}
