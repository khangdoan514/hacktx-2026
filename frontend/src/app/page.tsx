"use client";

import { useEffect, useState } from "react";
import { apiGet, type HelloResponse } from "@/lib/api";

export default function Home() {
  const [message, setMessage] = useState<string>("Connecting to backend…");
  const [error, setError] = useState(false);

  useEffect(() => {
    apiGet<HelloResponse>("/hello")
      .then((data) => setMessage(data.message))
      .catch(() => {
        setError(true);
        setMessage("Could not reach the backend. Is it running on :8000?");
      });
  }, []);

  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-4 p-8">
      <h1 className="text-3xl font-bold">HackTX 2026</h1>
      <p className={error ? "text-red-500" : "text-green-600"}>{message}</p>
    </main>
  );
}
