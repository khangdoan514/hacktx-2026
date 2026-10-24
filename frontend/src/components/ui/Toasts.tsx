"use client";

import { useStore } from "@/lib/store";

export default function Toasts() {
  const { toasts } = useStore();
  return (
    <div aria-live="polite" style={{ position: "fixed", zIndex: 90, left: "50%", transform: "translateX(-50%)", bottom: 80, display: "flex", flexDirection: "column", alignItems: "center", gap: 8 }}>
      {toasts.map((t) => (
        <div key={t.id} className="rise" style={{ height: 40, padding: "0 16px", display: "flex", alignItems: "center", borderRadius: 10, background: "var(--solid)", color: "var(--solid-fg)", fontSize: 13, boxShadow: "var(--pop)", whiteSpace: "nowrap" }}>
          {t.msg}
        </div>
      ))}
    </div>
  );
}
