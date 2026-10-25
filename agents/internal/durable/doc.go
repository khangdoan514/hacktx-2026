// Package durable is the Pass 3 execution journal.
//
// A worker records attempts, checkpoints, leases, and cancellation here, then
// stops. Another worker resumes from that journal. Completing an invocation
// is one-time; abandoning an in-flight attempt does not mark the run successful.
// Protected writes are refused and recorded as uncertain.
package durable
