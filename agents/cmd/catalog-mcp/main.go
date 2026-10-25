package main

// catalog-mcp is the local MCP server named in agents/opencode.jsonc.
// OpenCode starts it with the agents directory as its working directory.

import (
	"log"
	"os"

	"github.com/khangdoan514/hacktx-2026/agents/internal/catalogmcp"
)

func main() {
	log.SetOutput(os.Stderr)
	path := os.Getenv("NMC3_CATALOG_FILE")
	if path == "" {
		path = "fixtures/catalog/vendors.json"
	}
	if err := catalogmcp.Serve(os.Stdin, os.Stdout, path); err != nil {
		log.Fatal(err)
	}
}
