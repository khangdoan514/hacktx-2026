"use client";

import { useState } from "react";
import Icon from "./Icon";
import s from "./CodeHint.module.css";

export type Snippets = { cli: string; sdk: string };

/** "</>" button that shows the CLI and SDK equivalent of what the page does. */
export default function CodeHint({ code }: { code: Snippets }) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<keyof Snippets>("cli");
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard?.writeText(code[tab]).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    });
  };
  return (
    <div className={s.wrap}>
      <button type="button" className="btn ghost sm" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
        <Icon name="code" size={15} />CLI · SDK
      </button>
      {open && (
        <div className={s.pop}>
          <div className={s.tabs}>
            {(["cli", "sdk"] as const).map((k) => (
              <button key={k} type="button" aria-pressed={tab === k} onClick={() => setTab(k)}>{k === "cli" ? "CLI" : "TypeScript SDK"}</button>
            ))}
            <button type="button" className={s.copy} onClick={copy} aria-label="Copy">
              <Icon name={copied ? "check" : "copy"} size={14} />
            </button>
          </div>
          <pre>{code[tab]}</pre>
        </div>
      )}
    </div>
  );
}
