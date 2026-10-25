import {
  Activity, Archive, ArrowUp, BadgeCheck, Bot, Boxes, Building2, Calculator, Check, CircleSlash, Clock, Code, Compass, Copy, File as FileIcon, FlaskConical,
  Gauge, Globe, GraduationCap, Hourglass, Link, LogOut, MessageSquare, Moon, Network, Play, Plus, Scale, ScrollText,
  PanelLeft, Search, Server, Settings2, ShieldCheck, ShieldX, Sparkles, Square, SquareTerminal, Sun, Terminal, Webhook,
  Wrench, X, Zap,
  type LucideProps,
} from "lucide-react";

// Icons available to config files. Add one here to use it in src/config.
const icons = {
  activity: Activity, archive: Archive, "arrow-up": ArrowUp, "badge-check": BadgeCheck, bot: Bot, boxes: Boxes, building: Building2, calculator: Calculator, check: Check,
  "circle-slash": CircleSlash, clock: Clock, code: Code, compass: Compass, copy: Copy, flask: FlaskConical,
  file: FileIcon, gauge: Gauge, globe: Globe, "graduation-cap": GraduationCap, hourglass: Hourglass, link: Link,
  "log-out": LogOut, message: MessageSquare, moon: Moon, network: Network, "panel-left": PanelLeft, play: Play, plus: Plus, scale: Scale,
  "scroll-text": ScrollText, search: Search, server: Server, settings: Settings2, shield: ShieldCheck,
  "shield-x": ShieldX, sparkles: Sparkles, square: Square, terminal: Terminal, "square-terminal": SquareTerminal,
  sun: Sun, webhook: Webhook, wrench: Wrench, x: X, zap: Zap,
};

export type IconName = keyof typeof icons;

export default function Icon({ name, size = 16, ...rest }: { name: IconName } & LucideProps) {
  const C = icons[name];
  return <C size={size} strokeWidth={1.6} aria-hidden {...rest} />;
}
