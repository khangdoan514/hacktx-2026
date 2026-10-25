package catalogmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func fixturePath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "catalog", "vendors.json")
}

func TestSearchKeepsAccessibleVendorsInsideBudget(t *testing.T) {
	catalog, err := Load(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := catalog.Search(context.Background(), Query{Criteria: []string{"price", "accessibility"}, BudgetUSD: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.SourceIDs, ",") != "vendor-a,vendor-b" {
		t.Fatalf("ids=%v", result.SourceIDs)
	}
	narrow, err := catalog.Search(context.Background(), Query{Criteria: []string{"phone"}, BudgetUSD: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(narrow.SourceIDs, ",") != "vendor-b" {
		t.Fatalf("phone ids=%v", narrow.SourceIDs)
	}
}

func TestServeSearchRoundTrip(t *testing.T) {
	var in bytes.Buffer
	writeReq := func(id int, method string, params any) {
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + string(body)); err != nil {
			t.Fatal(err)
		}
	}
	writeReq(1, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	writeReq(2, "tools/list", map[string]any{})
	writeReq(3, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"criteria": []string{"accessibility"}, "budget_usd": 700}})
	writeReq(4, "tools/call", map[string]any{"name": "search", "arguments": map[string]any{"extra": true}})

	var out bytes.Buffer
	if err := Serve(&in, &out, fixturePath(t)); err != nil {
		t.Fatal(err)
	}
	messages := readAll(&out)
	if len(messages) != 4 {
		t.Fatalf("messages=%d", len(messages))
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(messages[1], &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) != 1 || listed.Result.Tools[0].Name != "search" {
		t.Fatalf("tools=%+v", listed.Result.Tools)
	}
	var called struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(messages[2], &called); err != nil {
		t.Fatal(err)
	}
	if called.Result.IsError || len(called.Result.Content) != 1 || !strings.Contains(called.Result.Content[0].Text, `"vendor-a"`) || strings.Contains(called.Result.Content[0].Text, `"vendor-b"`) {
		t.Fatalf("call=%s", messages[2])
	}
	if err := json.Unmarshal(messages[3], &called); err != nil {
		t.Fatal(err)
	}
	if !called.Result.IsError {
		t.Fatalf("unknown field accepted: %s", messages[3])
	}
}

func readAll(r io.Reader) [][]byte {
	in := bufio.NewReader(r)
	var out [][]byte
	for {
		body, err := readFrame(in)
		if err == io.EOF {
			return out
		}
		if err != nil {
			return out
		}
		out = append(out, body)
	}
}
