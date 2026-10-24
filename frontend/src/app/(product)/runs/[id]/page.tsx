"use client";

import { useParams } from "next/navigation";
import { RunScreen } from "@/product/nmc3/screens";

export default function RunPage() {
  const params = useParams<{ id: string }>();
  return <RunScreen id={params.id} />;
}
