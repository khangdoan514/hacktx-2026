import AgentDrawer from "@/components/agent/AgentDrawer";
import Rail from "@/components/shell/Rail";
import Toasts from "@/components/ui/Toasts";
import s from "./console.module.css";

export default function ConsoleLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className={s.app}>
      <Rail />
      <main className={s.page}>{children}</main>
      <AgentDrawer />
      <Toasts />
    </div>
  );
}
