// Package toolpacks is the built-in tool catalog: ready-made, read-only
// integrations (GitHub, Google Drive) that a user connects once and an agent
// then uses by listing the tool ids in allowed_tools.
//
// Agents reach the tools through one stdio MCP server (cmd/tools-mcp) that
// OpenCode starts as the "tools" server, so OpenCode names a tool
// tools_<pack>_<name> and the contract id is <pack>.<name>.
package toolpacks

import (
	"encoding/json"
	"strings"
)

const (
	// ServerName is the key under mcp.servers in opencode.jsonc.
	ServerName = "tools"
	Version    = "1.0.0"
)

// Tool is one callable. Name is unique inside its pack and has no dots.
type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

// Pack is one integration and the credential it needs.
type Pack struct {
	ID          string
	Name        string
	Description string
	TokenHint   string // what to paste when a token is accepted; empty if OAuth only
	Tools       []Tool
}

// ToolID is the contract id an agent lists in allowed_tools.
func (p Pack) ToolID(t Tool) string { return p.ID + "." + t.Name }

// OpenCodeName is how OpenCode publishes the tool to a model.
func (p Pack) OpenCodeName(t Tool) string { return ServerName + "_" + p.ID + "_" + t.Name }

func (t Tool) inputSchema() map[string]any {
	schema := map[string]any{"type": "object", "properties": t.Properties, "additionalProperties": false}
	if len(t.Required) > 0 {
		schema["required"] = t.Required
	}
	return schema
}

// InputSchemaJSON and OutputSchemaJSON feed the definitions tool catalog,
// whose schema validator accepts only structural keywords, so property
// descriptions (kept for models in the MCP schema) are dropped here.
func (t Tool) InputSchemaJSON() json.RawMessage {
	props := make(map[string]any, len(t.Properties))
	for name, p := range t.Properties {
		props[name] = map[string]any{"type": p.(map[string]any)["type"]}
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(t.Required) > 0 {
		schema["required"] = t.Required
	}
	b, _ := json.Marshal(schema)
	return b
}

func (Tool) OutputSchemaJSON() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":true}`)
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

var limitProp = num("Maximum results, 1 to 30 (default 10)")

// Packs returns the catalog in display order.
func Packs() []Pack {
	return []Pack{
		{
			ID:          "github",
			Name:        "GitHub",
			Description: "Search repositories and code, read files, issues and pull requests. Read-only.",
			TokenHint:   "A personal access token (classic or fine-grained, repo read access).",
			Tools: []Tool{
				{Name: "search_repositories", Description: "Search GitHub repositories", Properties: map[string]any{"query": str("GitHub repository search query"), "limit": limitProp}, Required: []string{"query"}},
				{Name: "search_code", Description: "Search code across GitHub", Properties: map[string]any{"query": str("GitHub code search query, e.g. 'foo repo:owner/name'"), "limit": limitProp}, Required: []string{"query"}},
				{Name: "get_file", Description: "Read a text file from a repository", Properties: map[string]any{"owner": str("Repository owner"), "repo": str("Repository name"), "path": str("File path"), "ref": str("Branch, tag or commit (optional)")}, Required: []string{"owner", "repo", "path"}},
				{Name: "list_issues", Description: "List issues in a repository", Properties: map[string]any{"owner": str("Repository owner"), "repo": str("Repository name"), "state": str("open, closed or all (default open)"), "limit": limitProp}, Required: []string{"owner", "repo"}},
				{Name: "get_issue", Description: "Read one issue or pull request with its body", Properties: map[string]any{"owner": str("Repository owner"), "repo": str("Repository name"), "number": num("Issue or pull request number")}, Required: []string{"owner", "repo", "number"}},
				{Name: "list_pull_requests", Description: "List pull requests in a repository", Properties: map[string]any{"owner": str("Repository owner"), "repo": str("Repository name"), "state": str("open, closed or all (default open)"), "limit": limitProp}, Required: []string{"owner", "repo"}},
			},
		},
		{
			ID:          "gdrive",
			Name:        "Google Drive",
			Description: "Search Drive, list folders and read documents, sheets and text files. Read-only.",
			Tools: []Tool{
				{Name: "search_files", Description: "Search Drive files by name or content", Properties: map[string]any{"query": str("Plain text to find in file names and contents"), "limit": limitProp}, Required: []string{"query"}},
				{Name: "list_folder", Description: "List files in a folder", Properties: map[string]any{"folder_id": str("Folder id (default: My Drive root)"), "limit": limitProp}},
				{Name: "read_file", Description: "Read a Google Doc, Sheet, Slides deck or text file as text", Properties: map[string]any{"file_id": str("Drive file id")}, Required: []string{"file_id"}},
			},
		},
		{
			ID: "notion", Name: "Notion",
			Description: "Search shared pages, read content, and query data sources in your connected Notion workspace. Read-only.",
			Tools: []Tool{
				{Name: "search", Description: "Search titles of shared Notion pages and data sources (not full page text)", Properties: map[string]any{"query": str("Title to search; omit to list accessible pages and data sources"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}},
				{Name: "get_page", Description: "Read a Notion page's properties and source URL; use list_blocks for body content", Properties: map[string]any{"page_id": str("Notion page UUID")}, Required: []string{"page_id"}},
				{Name: "list_blocks", Description: "Read a page or block's immediate children; follow has_children IDs to read nested sections", Properties: map[string]any{"block_id": str("Notion page or block UUID"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}, Required: []string{"block_id"}},
				{Name: "get_database", Description: "Read a database's metadata and available data source IDs", Properties: map[string]any{"database_id": str("Notion database UUID")}, Required: []string{"database_id"}},
				{Name: "query_data_source", Description: "Read rows of an authorized Notion data source with bounded pagination", Properties: map[string]any{"data_source_id": str("Notion data source UUID from search or get_database"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}, Required: []string{"data_source_id"}},
			},
		},
		{
			ID: "slack", Name: "Slack", Description: "Search public messages, browse public channels, read threads, and get source links as your Slack account. Read-only.",
			Tools: []Tool{
				{Name: "search_messages", Description: "Search public Slack messages with the Real-time Search API and return source permalinks", Properties: map[string]any{"query": str("Keywords or question, optionally with Slack search filters"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}, Required: []string{"query"}},
				{Name: "list_channels", Description: "List public Slack channels visible to the connected account", Properties: map[string]any{"limit": limitProp, "cursor": str("next_cursor from the previous response")}},
				{Name: "get_channel_history", Description: "Read recent channel messages; use get_permalink to cite a message", Properties: map[string]any{"channel_id": str("Slack channel ID, for example C0123456789"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}, Required: []string{"channel_id"}},
				{Name: "get_thread", Description: "Read a thread using its parent message timestamp; use get_permalink for citations", Properties: map[string]any{"channel_id": str("Slack channel ID"), "ts": str("Parent message timestamp, for example 1700000000.000001"), "limit": limitProp, "cursor": str("next_cursor from the previous response")}, Required: []string{"channel_id", "ts"}},
				{Name: "get_permalink", Description: "Get a permanent source URL for a Slack message", Properties: map[string]any{"channel_id": str("Slack channel ID"), "ts": str("Message timestamp")}, Required: []string{"channel_id", "ts"}},
			},
		},
	}
}

// Find returns the pack by id.
func Find(id string) (Pack, bool) {
	for _, p := range Packs() {
		if p.ID == id {
			return p, true
		}
	}
	return Pack{}, false
}

// ContractID maps an OpenCode tool name (tools_github_search_code) to the
// contract id (github.search_code). Other names pass through.
func ContractID(openCodeName string) string {
	rest, ok := strings.CutPrefix(openCodeName, ServerName+"_")
	if !ok {
		return openCodeName
	}
	pack, tool, ok := strings.Cut(rest, "_")
	if !ok {
		return openCodeName
	}
	if _, known := Find(pack); !known {
		return openCodeName
	}
	return pack + "." + tool
}

// OpenCodeNameFor maps a contract id (github.search_code) to the name OpenCode
// publishes (tools_github_search_code).
func OpenCodeNameFor(contractID string) (string, bool) {
	packID, name, ok := strings.Cut(contractID, ".")
	if !ok {
		return "", false
	}
	p, ok := Find(packID)
	if !ok {
		return "", false
	}
	for _, t := range p.Tools {
		if t.Name == name {
			return p.OpenCodeName(t), true
		}
	}
	return "", false
}
