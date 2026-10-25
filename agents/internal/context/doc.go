// Package context resolves scoped agent context for Pass 5.
//
// Importers should alias this package so it does not clash with the standard
// library context package:
//
//	agentctx "github.com/khangdoan514/hacktx-2026/agents/internal/context"
//
// The resolver assembles step, run, agent-memory, and organization fragments
// for one invocation. Every fragment is untrusted data with provenance.
// Tool allowlists are copied from the caller and are never derived from
// fragment text. Grants are checked before a store or retriever is read, and
// again before any loaded text is published.
//
// The coordinator calls ResolveBeforePrompt before it builds a prompt, and
// ForwardSpecialist when it prompts a depth-1 specialist. Open stores grants
// and snapshots in Postgres. This package does not change contracts.RunContext.
package context
