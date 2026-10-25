"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useStore } from "@/lib/store";

// There is no separate live view: a run, live or finished, is opened from Runs.
// Old links and "new run" land on the live run's page, or the list if none is going.
export default function DashboardRedirect() {
  const router = useRouter();
  const { live } = useStore();
  useEffect(() => { router.replace(live ? `/runs/${live.id}` : "/runs"); }, [router, live]);
  return null;
}
