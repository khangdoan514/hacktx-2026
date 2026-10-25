package main

// tools-mcp is the local MCP server named "tools" in agents/opencode.jsonc.
// It serves the built-in GitHub and Google Drive packs using the credentials
// the console saved under NMC3_TOOLS_DIR.

import (
	"log"
	"os"

	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

func main() {
	log.SetOutput(os.Stderr)
	dir := os.Getenv("NMC3_TOOLS_DIR")
	if dir == "" {
		dir = ".nmc3"
	}
	if err := toolpacks.Serve(os.Stdin, os.Stdout, toolpacks.NewRunner(toolpacks.NewStore(dir))); err != nil {
		log.Fatal(err)
	}
}
