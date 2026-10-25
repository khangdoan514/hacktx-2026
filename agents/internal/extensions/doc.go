// Package extensions is the Pass 6 tool boundary: a versioned registry,
// one MCP client, one HTTP client for internal and external services, and a
// Go SDK served by a separate worker process. The gateway applies one
// permission check before any of those adapters dial.
package extensions
