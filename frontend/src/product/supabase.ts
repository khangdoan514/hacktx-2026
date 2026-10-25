"use client";

import { createClient, type SupabaseClient } from "@supabase/supabase-js";
import { clearToken, readToken, writeToken } from "./api";

const url = process.env.NEXT_PUBLIC_SUPABASE_URL;
const key = process.env.NEXT_PUBLIC_SUPABASE_ANON_KEY;

export const supabase: SupabaseClient | null = url && key ? createClient(url, key) : null;

function remember(token: string | null) {
  if (token) writeToken(token);
  else clearToken();
}

if (supabase && process.env.NEXT_PUBLIC_LOCAL_AUTH !== "true" && typeof window !== "undefined") {
  supabase.auth.onAuthStateChange((_event, session) => {
    if (session?.access_token) remember(session.access_token);
  });
}

export async function restoreSession(): Promise<string | null> {
  // Without Supabase configured (local auth), reuse the token from /auth/local-session.
  // The API still verifies it on every request. A seeded employee Supabase will not
  // accept, such as ali-analytics@nmc2.test, also keeps that local token.
  if (!supabase || process.env.NEXT_PUBLIC_LOCAL_AUTH === "true") return readToken();
  const { data } = await supabase.auth.getSession();
  const token = data.session?.access_token ?? null;
  if (token) {
    remember(token);
    return token;
  }
  return readToken();
}

export async function endSession() {
  if (supabase && process.env.NEXT_PUBLIC_LOCAL_AUTH !== "true") await supabase.auth.signOut();
  clearToken();
}
