export type ApiError = Error & { status?: number };

const TOKEN_KEY = "nmc3_token";

export function readToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function writeToken(token: string) {
  window.localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken() {
  window.localStorage.removeItem(TOKEN_KEY);
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  const token = readToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body && !headers.has("Content-Type") && !(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(`/api/v1${path}`, { ...init, headers });
  const text = await response.text();
  const data = text ? (JSON.parse(text) as { message?: string }) : null;
  if (!response.ok) {
    const error = new Error(data?.message || `Request failed (${response.status})`) as ApiError;
    error.status = response.status;
    throw error;
  }
  return data as T;
}
