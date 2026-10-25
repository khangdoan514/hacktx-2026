import type { Metadata } from "next";
import Landing from "@/components/landing/Landing";

export const metadata: Metadata = {
  title: "NMC³ — Cloud, Compute, Consolidate",
  description: "A shared context for your agents, your teams, and the work between them.",
};

export default function HomePage() {
  return <Landing />;
}
