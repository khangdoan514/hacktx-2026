import { defineSchema, defineTable } from "convex/server";
import { v } from "convex/values";

// Per-user invalidation signals only. No titles, costs, or file bytes.
export default defineSchema({
  liveHints: defineTable({
    userId: v.string(),
    workspaceId: v.string(),
    projectId: v.optional(v.string()),
    resourceType: v.string(),
    resourceId: v.string(),
    revision: v.number(),
    updatedAt: v.number(),
    eventId: v.string(),
  })
    .index("by_user", ["userId", "updatedAt"])
    .index("by_user_resource", ["userId", "resourceId"]),
  hintDeliveries: defineTable({
    eventId: v.string(),
  }).index("by_event", ["eventId"]),
});
