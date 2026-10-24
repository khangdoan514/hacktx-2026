import { convexTest } from "convex-test";
import { expect, test } from "vitest";
import { api, internal } from "./_generated/api";
import schema from "./schema";

const modules = import.meta.glob(["./hints.ts", "./_generated/**/*.ts"]);

test("push stores a hint without business fields and updates it in place", async () => {
  const t = convexTest(schema, modules);
  await t.mutation(internal.hints.push, {
    workspaceId: "workspace-1",
    projectId: "project-1",
    resourceType: "task",
    resourceId: "task-1",
    revision: 1,
    updatedAt: 10,
  });
  await t.mutation(internal.hints.push, {
    workspaceId: "workspace-1",
    projectId: "project-1",
    resourceType: "task",
    resourceId: "task-1",
    revision: 2,
    updatedAt: 20,
  });
  const hints = await t.query(api.hints.listForWorkspace, { workspaceId: "workspace-1" });
  expect(hints).toHaveLength(1);
  expect(hints[0]).toMatchObject({
    workspaceId: "workspace-1",
    projectId: "project-1",
    resourceType: "task",
    resourceId: "task-1",
    revision: 2,
    updatedAt: 20,
  });
  expect(hints[0]).not.toHaveProperty("title");
  expect(hints[0]).not.toHaveProperty("internal_cost");
});
