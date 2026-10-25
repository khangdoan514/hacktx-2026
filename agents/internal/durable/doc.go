// Package durable is the Pass 3 execution journal.
//
// Memory is the in-process reference. Postgres, opened with OpenPostgres, is
// the same journal on the nmc3 schema. A worker records attempts, checkpoints,
// leases, and cancellation, then stops. Another worker resumes from that
// journal. Completing an invocation is one-time; abandoning an in-flight
// attempt does not mark the run successful. Protected writes are refused and
// recorded as uncertain.
//
// OpenPostgres does not replace the Supabase run store. The HTTP process keeps
// that store for run history and does not resume in-flight OpenCode sessions
// until an integrator wires this executor to those sessions.
package durable
