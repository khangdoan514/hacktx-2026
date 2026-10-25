/** Schedule presets offered by the automation dialog. Any five-field cron also works. */
export const PRESETS: { cron: string; label: string }[] = [
  { cron: "*/15 * * * *", label: "Every 15 minutes" },
  { cron: "0 * * * *", label: "Every hour" },
  { cron: "0 9 * * *", label: "Every day at 9:00" },
  { cron: "0 9 * * 1-5", label: "Weekdays at 9:00" },
  { cron: "0 9 * * 1", label: "Mondays at 9:00" },
  { cron: "0 9 1 * *", label: "First of the month at 9:00" },
];

const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

const at = (h: string, m: string) => `${h.padStart(2, "0")}:${m.padStart(2, "0")}`;
const num = (v: string) => /^\d+$/.test(v);

/** A short sentence for common cron shapes; anything else shows as the expression. */
export function describeSchedule(cron: string): string {
  const preset = PRESETS.find((p) => p.cron === cron.trim());
  if (preset) return preset.label;
  const f = cron.trim().split(/\s+/);
  if (f.length !== 5) return cron;
  const [min, hour, dom, mon, dow] = f;
  if (min === "*" && hour === "*" && dom === "*" && mon === "*" && dow === "*") return "Every minute";
  const step = /^\*\/(\d+)$/.exec(min);
  if (step && hour === "*" && dom === "*" && mon === "*" && dow === "*") return `Every ${step[1]} minutes`;
  if (num(min) && hour === "*" && dom === "*" && mon === "*" && dow === "*") return `Hourly at :${min.padStart(2, "0")}`;
  if (num(min) && num(hour) && mon === "*") {
    if (dom === "*" && dow === "*") return `Every day at ${at(hour, min)}`;
    if (dom === "*" && dow === "1-5") return `Weekdays at ${at(hour, min)}`;
    if (dom === "*" && /^[0-7]$/.test(dow)) return `${DAYS[+dow % 7]}s at ${at(hour, min)}`;
    if (num(dom) && dow === "*") return `Day ${dom} of each month at ${at(hour, min)}`;
  }
  return cron;
}

export function localZone(): string {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; }
}

/** "in 5m" / "5m ago" relative to now. */
export function relative(iso: string | undefined, now: number): string {
  if (!iso) return "—";
  const d = (new Date(iso).getTime() - now) / 1000;
  const a = Math.abs(d);
  const span = a < 60 ? `${Math.max(1, Math.round(a))}s` : a < 3600 ? `${Math.round(a / 60)}m` : a < 86400 ? `${Math.round(a / 3600)}h` : `${Math.round(a / 86400)}d`;
  return d >= 0 ? `in ${span}` : `${span} ago`;
}

export const when = (iso: string, zone?: string) => {
  try {
    return new Date(iso).toLocaleString(undefined, { weekday: "short", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", timeZone: zone });
  } catch {
    return new Date(iso).toLocaleString();
  }
};
