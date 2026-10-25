"use client";

import Icon from "@/components/ui/Icon";
import type { FlowSource } from "@/lib/flows/useFlowSimulation";
import s from "./overview.module.css";

function toggleTheme(e: React.MouseEvent<HTMLButtonElement>) {
  const root = document.documentElement;
  const next = (root.dataset.theme ?? (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")) === "dark" ? "light" : "dark";
  const apply = () => { root.dataset.theme = next; };
  try { localStorage.setItem("nmc3-theme", next); } catch {}
  if (!document.startViewTransition || matchMedia("(prefers-reduced-motion: reduce)").matches) return apply();
  const r = e.currentTarget.getBoundingClientRect(), x = r.left + r.width / 2, y = r.top + r.height / 2;
  const end = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y));
  document.startViewTransition(apply).ready.then(() =>
    root.animate({ clipPath: [`circle(0 at ${x}px ${y}px)`, `circle(${end}px at ${x}px ${y}px)`] },
      { duration: 650, easing: "cubic-bezier(0.16, 1, 0.3, 1)", pseudoElement: "::view-transition-new(root)" }));
}

export default function TopBar({ source }: { source: FlowSource }) {
  return (
    <header className={s.top}>
      <span className={s.crumb}>Overview</span>
      <span className={s.src} title={source === "live" ? "Showing runs from the agent service" : "No runs from the agent service yet; showing a scripted demo"}>
        <i className={`dot ${source === "live" ? "live" : ""}`} style={{ color: source === "live" ? "var(--ok)" : "var(--ink-3)" }} />
        {source === "live" ? "Live data" : "Demo data"}
      </span>
      <button type="button" className="ibtn" onClick={toggleTheme} aria-label="Switch theme"><Icon name="moon" size={17} /></button>
    </header>
  );
}
