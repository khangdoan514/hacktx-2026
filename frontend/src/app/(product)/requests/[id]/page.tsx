"use client";

import { useParams } from "next/navigation";
import { RequestDetailScreen } from "@/product/nmc3/screens";

export default function RequestPage() {
  const params = useParams<{ id: string }>();
  return <RequestDetailScreen id={params.id} />;
}
