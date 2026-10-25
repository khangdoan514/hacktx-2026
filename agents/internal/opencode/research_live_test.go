package opencode

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// Opt-in: spends one real search credit. It checks the hosted MCP transport
// and result normalization, not model behavior or database persistence.
func TestSerpApiLive(t *testing.T) {
	if os.Getenv("NMC3_TEST_SERPAPI_LIVE") != "1" {
		t.Skip("set NMC3_TEST_SERPAPI_LIVE=1 and SERPAPI_API_KEY to run")
	}
	key := os.Getenv("SERPAPI_API_KEY")
	if key == "" {
		t.Fatal("SERPAPI_API_KEY is required")
	}
	client := &http.Client{Timeout: 40 * time.Second}
	rpc := func(method string, params any) json.RawMessage {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req, err := http.NewRequest(http.MethodPost, "https://mcp.serpapi.com/mcp", bytes.NewReader(body))
		if err != nil {
			t.Fatal("could not construct MCP request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal("could not connect to SerpApi MCP")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("MCP HTTP status %d", resp.StatusCode)
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&envelope) != nil || len(envelope.Result) == 0 || len(envelope.Error) != 0 {
			t.Fatal("unreadable or failed MCP response")
		}
		return envelope.Result
	}
	rpc("initialize", map[string]any{
		"protocolVersion": "2025-03-26", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "nmc3-live-test", "version": "1.0.0"},
	})
	args := map[string]any{"params": map[string]any{"engine": "google", "q": "Go context cancellation documentation"}, "mode": "compact"}
	raw := rpc("tools/call", map[string]any{"name": "search", "arguments": args})
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		t.Fatal("unreadable tool result")
	}
	if isError, _ := result["isError"].(bool); isError {
		t.Fatal("SerpApi search failed; check credentials and quota")
	}
	_, output, err := recordSearch(map[string]any{"status": "completed", "input": args, "content": result["content"]})
	if err != nil {
		t.Fatal(err)
	}
	var normalized searchResult
	if json.Unmarshal(output, &normalized) != nil || len(normalized.Sources) == 0 {
		t.Fatal("live search did not yield usable sources")
	}
	t.Logf("Authenticated search returned %d normalized sources", len(normalized.Sources))
}
