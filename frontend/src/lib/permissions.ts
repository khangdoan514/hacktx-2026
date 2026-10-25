const threadRoles = new Set(["ai_engineer", "operator", "administrator"]);

export function canUseThreads(roles?: string[]) {
  return roles?.some((role) => threadRoles.has(role)) ?? false;
}
