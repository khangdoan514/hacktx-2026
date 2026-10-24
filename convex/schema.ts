import { defineSchema, defineTable } from "convex/server";
import { v } from "convex/values";

// Live hints only. No business fields and no agent records.
export default defineSchema({
  liveHints: defineTable({
    workspaceId: v.string(),
    projectId: v.optional(v.string()),
    resourceType: v.string(),
    resourceId: v.string(),
    revision: v.number(),
    updatedAt: v.number(),
  }).index("by_workspace", ["workspaceId", "updatedAt"]),
});
