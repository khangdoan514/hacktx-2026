export async function apiGet<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`/api${path}`, init);
  if (!res.ok) {
    throw new Error(`GET /api${path} failed: ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export type HelloResponse = { message: string };
