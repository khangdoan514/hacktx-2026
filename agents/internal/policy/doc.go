// Package policy is the agent-run authorization layer.
//
// Employee identity stays in the NMC2 directory. Callers authenticate with
// platform.Verify, load the employee with organization.Load, and convert the
// resulting platform.Actor with PrincipalFromActor. This package does not mint
// sessions, provision employees, or store secret values.
package policy
