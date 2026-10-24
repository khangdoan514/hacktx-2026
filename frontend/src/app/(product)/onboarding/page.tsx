"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

export default function OnboardingPage() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/org");
  }, [router]);
  return <p className="text-sm text-gray-500">Opening NMC2…</p>;
}
