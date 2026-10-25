// Package metrics is the local operations toolkit for Pass 8.
//
// It admits work under a per-organization concurrency limit, emits structured
// log and trace records that always carry a run ID, and strips secrets and
// unauthorized content before those records are encoded. Latency, throughput,
// recovery, availability, and cost targets stay unset until a representative
// workload is measured. The package does not price tokens, does not store
// credentials, and does not talk to a collector or a database.
package metrics
