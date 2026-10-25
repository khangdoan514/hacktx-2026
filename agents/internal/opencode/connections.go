package opencode

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

// Each employee has a separate MCP location, so shared OpenCode processes never
// select another employee's credentials. Only paths, not tokens, enter config.
func connectionProject(source, owner string) (string, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(source, "opencode.jsonc"))
	if err != nil {
		return "", err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(stripJSONC(string(raw))), &cfg); err != nil {
		return "", err
	}
	base := os.Getenv("NMC3_TOOLS_DIR")
	if base == "" {
		base = filepath.Join(source, ".nmc3")
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return "", err
	}
	credentials := toolpacks.NewStore(base).ForOwner(owner).Directory()
	key := sha256.Sum256([]byte(owner + "\x00" + source + "\x00" + string(raw)))
	dir := filepath.Join(base, "projects", fmt.Sprintf("%x", key))
	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("workspace tools require Go: %w", err)
	}
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("workspace MCP configuration is missing")
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("workspace MCP servers are missing")
	}
	for name, value := range servers {
		server, ok := value.(map[string]any)
		if !ok {
			return "", fmt.Errorf("invalid MCP server: %s", name)
		}
		if server["type"] != "local" {
			continue
		}
		server["cwd"] = source
		if command, ok := server["command"].([]any); ok && len(command) > 0 && command[0] == "go" {
			command[0] = goPath
		}
		if name == "tools" {
			server["environment"] = map[string]string{"NMC3_TOOLS_DIR": credentials}
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, "opencode.jsonc")); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Client) AuthorizeSession(ctx context.Context, sessionID string) error {
	owner := toolpacks.ContextOwner(ctx)
	if owner == "" {
		return nil
	}
	var out struct {
		Data struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"data"`
	}
	if err := c.call(ctx, "GET", "/api/session/"+sessionID, nil, &out); err != nil {
		return err
	}
	if out.Data.Metadata["nmc3_owner"] != owner {
		return fmt.Errorf("thread does not belong to this employee")
	}
	return nil
}
