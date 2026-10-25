import ConsoleGate from "@/components/shell/ConsoleGate";
import Toasts from "@/components/ui/Toasts";
import { StoreProvider } from "@/lib/store";

export default function OverviewLayout({ children }: { children: React.ReactNode }) {
  return (
    <ConsoleGate>
      <StoreProvider>
        {children}
        <Toasts />
      </StoreProvider>
    </ConsoleGate>
  );
}
