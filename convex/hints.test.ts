import { convexTest } from "convex-test";
import { expect, test } from "vitest";
import { api, internal } from "./_generated/api";
import schema from "./schema";

const modules = import.meta.glob(["./hints.ts", "./http.ts", "./_generated/**/*.ts"]);

const hint = {
  eventId: "event-1",
  recipients: ["user-1"],
  workspaceId: "workspace-1",
  projectId: "project-1",
  resourceType: "task",
  resourceId: "task-1",
  revision: 2,
  updatedAt: Date.now(),
};

test("missing identity sees no hints", async () => {
  const t = convexTest(schema, modules);
  await t.mutation(internal.hints.apply, hint);
  const rows = await t.query(api.hints.listMine, {});
  expect(rows).toEqual([]);
});

test("a user sees only their current hints", async () => {
  const t = convexTest(schema, modules);
  await t.mutation(internal.hints.apply, hint);
  await t.mutation(internal.hints.apply, { ...hint, eventId: "event-2", recipients: ["user-2"], resourceId: "task-2" });
  const mine = await t.withIdentity({ subject: "user-1" }).query(api.hints.listMine, {});
  expect(mine).toHaveLength(1);
  expect(mine[0]).toMatchObject({ userId: "user-1", resourceId: "task-1", revision: 2 });
  expect(mine[0]).not.toHaveProperty("title");
});

test("duplicate events and older revisions do not replace a hint", async () => {
  const t = convexTest(schema, modules);
  await t.mutation(internal.hints.apply, hint);
  await t.mutation(internal.hints.apply, hint);
  await t.mutation(internal.hints.apply, { ...hint, eventId: "event-old", revision: 1, updatedAt: hint.updatedAt + 10 });
  const mine = await t.withIdentity({ subject: "user-1" }).query(api.hints.listMine, {});
  expect(mine).toHaveLength(1);
  expect(mine[0].revision).toBe(2);
});

test("revocation deletes the user's project hints", async () => {
  const t = convexTest(schema, modules);
  await t.mutation(internal.hints.apply, hint);
  await t.mutation(internal.hints.revoke, {
    eventId: "revoke-1",
    userId: "user-1",
    workspaceId: "workspace-1",
    projectId: "project-1",
    scope: "project",
  });
  const mine = await t.withIdentity({ subject: "user-1" }).query(api.hints.listMine, {});
  expect(mine).toEqual([]);
});

test("a bad signature is rejected", async () => {
  process.env.CONVEX_HINT_SECRET = "test-hint-secret";
  const t = convexTest(schema, modules);
  const response = await t.fetch("/hints", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-hint-timestamp": new Date().toISOString(),
      "x-hint-signature": "nope",
    },
    body: JSON.stringify(hint),
  });
  expect(response.status).toBe(401);
});
