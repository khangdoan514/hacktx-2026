"use client";

import { Suspense } from "react";
import { useSearchParams } from "next/navigation";
import { AcceptInvitationScreen } from "@/product/screens";

function AcceptInner() {
  const params = useSearchParams();
  return <AcceptInvitationScreen token={params.get("token") || ""} />;
}

export default function AcceptInvitationPage() {
  return (
    <Suspense fallback={<p className="text-sm text-gray-500">Loading invitation…</p>}>
      <AcceptInner />
    </Suspense>
  );
}
