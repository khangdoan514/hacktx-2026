import { v } from "convex/values";
import { internalMutation, query } from "./_generated/server";

export const push = internalMutation({
  args: {
    workspaceId: v.string(),
    projectId: v.optional(v.string()),
    resourceType: v.string(),
    resourceId: v.string(),
    revision: v.number(),
    updatedAt: v.number(),
  },
  handler: async (ctx, args) => {
    const existing = await ctx.db
      .query("liveHints")
      .withIndex("by_workspace", (q) => q.eq("workspaceId", args.workspaceId))
      .filter((q) => q.eq(q.field("resourceId"), args.resourceId))
      .first();
    if (existing) {
      await ctx.db.patch(existing._id, {
        projectId: args.projectId,
        resourceType: args.resourceType,
        revision: args.revision,
        updatedAt: args.updatedAt,
      });
      return existing._id;
    }
    return await ctx.db.insert("liveHints", args);
  },
});

export const listForWorkspace = query({
  args: { workspaceId: v.string() },
  handler: async (ctx, args) => {
    return await ctx.db
      .query("liveHints")
      .withIndex("by_workspace", (q) => q.eq("workspaceId", args.workspaceId))
      .order("desc")
      .take(50);
  },
});
