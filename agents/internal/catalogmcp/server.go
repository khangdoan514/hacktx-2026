package catalogmcp

// Serve speaks newline-delimited MCP stdio (also accepting legacy input frames) and
// exposes one tool, search. OpenCode publishes that tool as catalog_search
// when the server name is catalog and Code Mode is off.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const protocolVersion = "2024-11-05"

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func Serve(r io.Reader, w io.Writer, catalogPath string) error {
	catalog, err := Load(catalogPath)
	if err != nil {
		return err
	}
	in := bufio.NewReader(r)
	for {
		body, err := readFrame(in)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue
		}
		result, rpcErr := dispatch(catalog, req)
		if err := writeResult(w, req.ID, result, rpcErr); err != nil {
			return err
		}
	}
}

func dispatch(catalog *Catalog, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		version := protocolVersion
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		if params.ProtocolVersion != "" {
			version = params.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "nmc3-catalog", "version": "1.0.0"},
			"instructions":    "Search the fictional vendor catalog. The catalog is read-only. Cite returned source IDs.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{toolSpec()}}, nil
	case "tools/call":
		return callTool(catalog, req.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func toolSpec() map[string]any {
	return map[string]any{
		"name":        "search",
		"description": "Search a checked-in fictional vendor catalog",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"criteria":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"budget_usd": map[string]any{"type": "number"},
			},
			"additionalProperties": false,
		},
	}
}

func callTool(catalog *Catalog, params json.RawMessage) (any, *rpcError) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return toolError("invalid tool call"), nil
	}
	if call.Name != "search" {
		return toolError("unknown tool"), nil
	}
	if len(call.Arguments) == 0 {
		call.Arguments = []byte(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(call.Arguments))
	dec.DisallowUnknownFields()
	var query Query
	if err := dec.Decode(&query); err != nil {
		return toolError("invalid catalog query"), nil
	}
	result, err := catalog.Search(context.Background(), query)
	if err != nil {
		return toolError("catalog search failed"), nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return toolError("catalog search failed"), nil
	}
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(encoded)}},
		"isError": false,
	}, nil
}

func toolError(message string) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": message}},
		"isError": true,
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(strings.TrimSpace(line), "{") {
			return []byte(line), nil
		}
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			var n int
			if _, err := fmt.Sscanf(line, "Content-Length: %d", &n); err != nil {
				_, err = fmt.Sscanf(line, "content-length: %d", &n)
				if err != nil {
					return nil, fmt.Errorf("content-length header")
				}
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing content-length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeResult(w io.Writer, id json.RawMessage, result any, rpcErr *rpcError) error {
	msg := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", body)
	return err
}
