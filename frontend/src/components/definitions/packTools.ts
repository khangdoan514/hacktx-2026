import { readToken } from "@/product/api";

export type PackToolOption = { id: string; description: string; pack: string; connected: boolean };

export const packToolsApi = {
  async list(): Promise<PackToolOption[]> {
    const headers = new Headers();
    const token = readToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);
    const response = await fetch("/agent-api/tool-packs", { cache: "no-store", headers });
    if (!response.ok) throw new Error(`Tool packs failed (${response.status})`);
    const body = (await response.json()) as {
      packs: { name: string; connected: boolean; tools: { id: string; description: string }[] }[];
    };
    return body.packs.flatMap((pack) => pack.tools.map((tool) => ({ id: tool.id, description: tool.description, pack: pack.name, connected: pack.connected })));
  },
};
