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

if (supabase && typeof window !== "undefined") {
  supabase.auth.onAuthStateChange((_event, session) => {
    remember(session?.access_token ?? null);
  });
}

export async function restoreSession(): Promise<string | null> {
  // Without Supabase configured (local auth), reuse the token from /auth/local-session.
  // The API still verifies it on every request.
  if (!supabase) return readToken();
  const { data } = await supabase.auth.getSession();
  const token = data.session?.access_token ?? null;
  remember(token);
  return token;
}

export async function endSession() {
  if (supabase) await supabase.auth.signOut();
  clearToken();
}
