import { v } from "convex/values";
import { internalMutation, query } from "./_generated/server";

const RETENTION_MS = 7 * 24 * 60 * 60 * 1000;

export const listMine = query({
  args: {},
  handler: async (ctx) => {
    const identity = await ctx.auth.getUserIdentity();
    if (!identity) return [];
    const cutoff = Date.now() - RETENTION_MS;
    const rows = await ctx.db
      .query("liveHints")
      .withIndex("by_user", (q) => q.eq("userId", identity.subject))
      .order("desc")
      .take(50);
    return rows.filter((row) => row.updatedAt >= cutoff);
  },
});

export const apply = internalMutation({
  args: {
    eventId: v.string(),
    recipients: v.array(v.string()),
    workspaceId: v.string(),
    projectId: v.optional(v.string()),
    resourceType: v.string(),
    resourceId: v.string(),
    revision: v.number(),
    updatedAt: v.number(),
  },
  handler: async (ctx, args) => {
    const cutoff = Date.now() - RETENTION_MS;
    const seen = await ctx.db
      .query("hintDeliveries")
      .withIndex("by_event", (q) => q.eq("eventId", args.eventId))
      .unique();
    if (seen) return { applied: false };
    await ctx.db.insert("hintDeliveries", { eventId: args.eventId });
    for (const userId of args.recipients) {
      const owned = await ctx.db
        .query("liveHints")
        .withIndex("by_user", (q) => q.eq("userId", userId))
        .collect();
      for (const row of owned) {
        if (row.updatedAt < cutoff) await ctx.db.delete(row._id);
      }
      const existing = await ctx.db
        .query("liveHints")
        .withIndex("by_user_resource", (q) => q.eq("userId", userId).eq("resourceId", args.resourceId))
        .unique();
      if (existing && existing.revision > args.revision) continue;
      const next = {
        workspaceId: args.workspaceId,
        projectId: args.projectId,
        resourceType: args.resourceType,
        revision: args.revision,
        updatedAt: args.updatedAt,
        eventId: args.eventId,
      };
      if (existing) {
        await ctx.db.patch(existing._id, next);
      } else {
        await ctx.db.insert("liveHints", { userId, resourceId: args.resourceId, ...next });
      }
    }
    return { applied: true };
  },
});

export const revoke = internalMutation({
  args: {
    eventId: v.string(),
    userId: v.string(),
    workspaceId: v.string(),
    projectId: v.optional(v.string()),
    scope: v.string(),
  },
  handler: async (ctx, args) => {
    const seen = await ctx.db
      .query("hintDeliveries")
      .withIndex("by_event", (q) => q.eq("eventId", args.eventId))
      .unique();
    if (seen) return { deleted: 0 };
    await ctx.db.insert("hintDeliveries", { eventId: args.eventId });
    const rows = await ctx.db
      .query("liveHints")
      .withIndex("by_user", (q) => q.eq("userId", args.userId))
      .collect();
    let deleted = 0;
    for (const row of rows) {
      if (row.workspaceId !== args.workspaceId) continue;
      if (args.scope === "project" && row.projectId !== args.projectId) continue;
      await ctx.db.delete(row._id);
      deleted += 1;
    }
    return { deleted };
  },
});
