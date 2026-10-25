package toolpacks

// Serve speaks the same legacy MCP stdio transport as internal/catalogmcp
// (Content-Length framing). It lists every pack tool even when a pack is not
// connected yet: OpenCode lists tools once at startup, and a call to an
// unconnected pack returns a tool error telling the user to connect it.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const protocolVersion = "2024-11-05"

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve runs until r closes.
func Serve(r io.Reader, w io.Writer, runner *Runner) error {
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
		if json.Unmarshal(body, &req) != nil || len(req.ID) == 0 || string(req.ID) == "null" {
			continue // notifications and junk get no reply
		}
		result, rpcErr := dispatch(runner, req)
		if err := writeResult(w, req.ID, result, rpcErr); err != nil {
			return err
		}
	}
}

func dispatch(runner *Runner, req rpcRequest) (any, *rpcError) {
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
			"serverInfo":      map[string]any{"name": "nmc3-tools", "version": Version},
			"instructions":    "Read-only GitHub and Google Drive tools. Cite the url field of anything you rely on.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolSpecs()}, nil
	case "tools/call":
		return callTool(runner, req.Params), nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found"}
}

// MCP tool names are <pack>_<tool>; OpenCode adds the "tools_" server prefix.
func toolSpecs() []any {
	var specs []any
	for _, p := range Packs() {
		for _, t := range p.Tools {
			specs = append(specs, map[string]any{
				"name":        p.ID + "_" + t.Name,
				"description": p.Name + ": " + t.Description,
				"inputSchema": t.inputSchema(),
			})
		}
	}
	return specs
}

func callTool(runner *Runner, params json.RawMessage) any {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(params, &call) != nil {
		return toolError("invalid tool call")
	}
	pack, tool, ok := strings.Cut(call.Name, "_")
	if !ok {
		return toolError("unknown tool")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	result, err := runner.Call(ctx, pack, tool, call.Arguments)
	if err != nil {
		return toolError(err.Error())
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return toolError("the result could not be encoded")
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}, "isError": false}
}

func toolError(message string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": message}}, "isError": true}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "content-length") {
			if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &length); err != nil {
				return nil, fmt.Errorf("content-length header")
			}
		}
	}
	if length < 0 || length > 8<<20 {
		return nil, fmt.Errorf("missing or oversized content-length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeResult(w io.Writer, id json.RawMessage, result any, rpcErr *rpcError) error {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}
