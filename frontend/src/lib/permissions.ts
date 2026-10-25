const threadRoles = new Set(["ai_engineer", "operator", "administrator"]);

export function canUseThreads(roles?: string[]) {
  return roles?.some((role) => threadRoles.has(role)) ?? false;
}

// Same roles cmd/api requires to start any agent run; an automation starts
// runs as the employee who saved it.
export const canManageAutomations = canUseThreads;
