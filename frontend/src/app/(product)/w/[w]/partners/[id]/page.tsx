"use client";

import { useParams } from "next/navigation";
import { useState } from "react";
import { api } from "@/product/api";

export default function PartnerDetailPage() {
  const params = useParams<{ id: string }>();
  const [message, setMessage] = useState("");

  return (
    <section>
      <h1 className="text-xl font-semibold">Partner</h1>
      <p className="mt-2 text-sm text-gray-600">{params.id}</p>
      <button
        className="mt-4 rounded border px-3 py-2 text-sm"
        type="button"
        onClick={() => {
          api(`/partners/${params.id}/suspend`, { method: "POST" })
            .then(() => setMessage("Partner access suspended"))
            .catch((err: Error) => setMessage(err.message));
        }}
      >
        Suspend access
      </button>
      {message ? <p className="mt-2 text-sm">{message}</p> : null}
    </section>
  );
}
