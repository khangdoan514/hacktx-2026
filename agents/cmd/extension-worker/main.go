// Command extension-worker serves one Go tool in a separate process.
// Credentials arrive only on an invoke request. The process listens on
// loopback and exits if a crash-mode tool is called.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/khangdoan514/hacktx-2026/agents/internal/extensions"
)

const (
	echoInput  = `{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`
	echoOutput = `{"type":"object","properties":{"echo":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}},"required":["echo"],"additionalProperties":false}`
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "loopback listen address")
	mode := flag.String("mode", "echo", "echo or crash")
	flag.Parse()
	if *mode != "echo" && *mode != "crash" {
		fmt.Fprintln(os.Stderr, "mode must be echo or crash")
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "extension worker could not listen")
		os.Exit(1)
	}
	host, _, err := net.SplitHostPort(ln.Addr().String())
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		_ = ln.Close()
		fmt.Fprintln(os.Stderr, "extension-worker listens on loopback only")
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, ln.Addr().String())
	_ = os.Stdout.Sync()

	handle := echo
	if *mode == "crash" {
		handle = crash
	}
	tool := extensions.ServiceTool{
		ID:           "extensions.worker.echo",
		Version:      "1.0.0",
		Description:  "Echoes a JSON object from a separate process",
		InputSchema:  json.RawMessage(echoInput),
		OutputSchema: json.RawMessage(echoOutput),
		SideEffect:   extensions.SideEffectReadOnly,
		Handle:       handle,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := extensions.Serve(ctx, ln, []extensions.ServiceTool{tool}); err != nil {
		fmt.Fprintln(os.Stderr, "extension worker stopped")
		os.Exit(1)
	}
}

func echo(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(struct {
		Echo json.RawMessage `json:"echo"`
	}{Echo: input})
}

func crash(context.Context, json.RawMessage) (json.RawMessage, error) {
	os.Exit(1)
	return nil, nil
}
