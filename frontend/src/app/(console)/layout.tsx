import { StoreProvider } from "@/lib/store";
import AgentDrawer from "@/components/agent/AgentDrawer";
import Rail from "@/components/shell/Rail";
import Toasts from "@/components/ui/Toasts";
import s from "./console.module.css";

export default function ConsoleLayout({ children }: { children: React.ReactNode }) {
  return (
    <StoreProvider><div className={s.app}>
      <Rail />
      <main className={s.page}>{children}</main>
      <AgentDrawer />
      <Toasts />
    </div></StoreProvider>
  );
}
