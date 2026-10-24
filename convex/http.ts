import { httpRouter } from "convex/server";
import { internal } from "./_generated/api";
import { httpAction } from "./_generated/server";

const MAX_SKEW_MS = 5 * 60 * 1000;

async function signatureHex(secret: string, timestamp: string, body: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    "raw",
    new TextEncoder().encode(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const signed = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(`${timestamp}.${body}`));
  return [...new Uint8Array(signed)].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function safeEqual(left: string, right: string): boolean {
  if (left.length !== right.length) return false;
  let mismatch = 0;
  for (let index = 0; index < left.length; index += 1) {
    mismatch |= left.charCodeAt(index) ^ right.charCodeAt(index);
  }
  return mismatch === 0;
}

async function authorizedBody(request: Request): Promise<{ ok: true; body: Record<string, unknown> } | { ok: false; status: number }> {
  const secret = process.env.CONVEX_HINT_SECRET ?? "";
  const timestamp = request.headers.get("x-hint-timestamp") ?? "";
  const signature = request.headers.get("x-hint-signature") ?? "";
  const raw = await request.text();
  if (!secret || !timestamp || !signature) return { ok: false, status: 401 };
  const sentAt = Date.parse(timestamp);
  if (!Number.isFinite(sentAt) || Math.abs(Date.now() - sentAt) > MAX_SKEW_MS) return { ok: false, status: 401 };
  const expected = await signatureHex(secret, timestamp, raw);
  if (!safeEqual(expected, signature)) return { ok: false, status: 401 };
  try {
    return { ok: true, body: JSON.parse(raw) as Record<string, unknown> };
  } catch {
    return { ok: false, status: 400 };
  }
}

const http = httpRouter();

http.route({
  path: "/hints",
  method: "POST",
  handler: httpAction(async (ctx, request) => {
    const parsed = await authorizedBody(request);
    if (!parsed.ok) return new Response(null, { status: parsed.status });
    const body = parsed.body;
    await ctx.runMutation(internal.hints.apply, {
      eventId: String(body.eventId),
      recipients: Array.isArray(body.recipients) ? body.recipients.map(String) : [],
      workspaceId: String(body.workspaceId),
      projectId: body.projectId ? String(body.projectId) : undefined,
      resourceType: String(body.resourceType),
      resourceId: String(body.resourceId),
      revision: Number(body.revision),
      updatedAt: Number(body.updatedAt),
    });
    return new Response(null, { status: 204 });
  }),
});

http.route({
  path: "/hints/revoke",
  method: "POST",
  handler: httpAction(async (ctx, request) => {
    const parsed = await authorizedBody(request);
    if (!parsed.ok) return new Response(null, { status: parsed.status });
    const body = parsed.body;
    await ctx.runMutation(internal.hints.revoke, {
      eventId: String(body.eventId),
      userId: String(body.userId),
      workspaceId: String(body.workspaceId),
      projectId: body.projectId ? String(body.projectId) : undefined,
      scope: String(body.scope ?? "workspace"),
    });
    return new Response(null, { status: 204 });
  }),
});

export default http;
