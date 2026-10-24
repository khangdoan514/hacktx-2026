export async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(`/api${path}`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new Error(`API ${response.status}`);
  }
  return (await response.json()) as T;
}
