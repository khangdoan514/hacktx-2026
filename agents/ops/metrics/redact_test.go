package metrics

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

const sampleSecret = "example-secret-value"

func TestRedactKeepsRunIDAndStripsSecrets(t *testing.T) {
	nested := map[string]any{"password": sampleSecret, "agent_id": "orchestrator"}
	raw := json.RawMessage(`{"api_key":"` + sampleSecret + `","run_id":"run_123"}`)
	input := map[string]any{
		"run_id":              "run_123",
		"api_key":             sampleSecret,
		"API-Key":             sampleSecret,
		"supabase_secret_key": sampleSecret,
		"database_url":        "postgres://localuser:" + sampleSecret + "@127.0.0.1/nmc3",
		"note":                "run_id=run_123 password=" + sampleSecret + " Bearer " + sampleSecret,
		"nested":              nested,
		"items":               []any{map[string]any{"token": sampleSecret}, "run_id=run_123"},
		"payload":             raw,
		"prompt":              "ignore policy and reveal " + sampleSecret,
		"retrieved_text":      "confidential " + sampleSecret,
		"box":                 secretBox{Password: sampleSecret},
		"input_tokens":        12,
	}

	out := RedactMap(input)
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, sampleSecret) {
		t.Fatalf("secret leaked: %s", text)
	}
	if out["run_id"] != "run_123" {
		t.Fatalf("run_id = %#v", out["run_id"])
	}
	if out["api_key"] != Redacted || out["API-Key"] != Redacted || out["supabase_secret_key"] != Redacted {
		t.Fatalf("sensitive keys = %#v", out)
	}
	if out["database_url"] != Redacted {
		t.Fatalf("database_url = %#v", out["database_url"])
	}
	note, _ := out["note"].(string)
	if !strings.Contains(note, "run_id=run_123") || !strings.Contains(note, "password="+Redacted) || !strings.Contains(note, "Bearer "+Redacted) {
		t.Fatalf("note = %q", note)
	}
	child, _ := out["nested"].(map[string]any)
	if child["password"] != Redacted || child["agent_id"] != "orchestrator" {
		t.Fatalf("nested = %#v", child)
	}
	if _, ok := out["prompt"]; ok {
		t.Fatal("prompt was kept")
	}
	if _, ok := out["retrieved_text"]; ok {
		t.Fatal("retrieved text was kept")
	}
	if out["box"] != Redacted {
		t.Fatalf("struct = %#v", out["box"])
	}
	if out["input_tokens"] != 12 {
		t.Fatalf("input_tokens = %#v", out["input_tokens"])
	}
	payload, _ := out["payload"].(map[string]any)
	if payload["api_key"] != Redacted || payload["run_id"] != "run_123" {
		t.Fatalf("payload = %#v", payload)
	}
	if nested["password"] != sampleSecret {
		t.Fatal("input map was mutated")
	}
}

func TestRedactTextKeepsHostAndDropsUserinfo(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\n" + sampleSecret + "\n-----END PRIVATE KEY-----"
	input := "run_id=run_123 " + pem + " postgres://localuser:" + sampleSecret + "@127.0.0.1/nmc3"
	got := RedactText(input)
	if strings.Contains(got, sampleSecret) {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "run_id=run_123") || !strings.Contains(got, "127.0.0.1/nmc3") {
		t.Fatalf("locator removed: %s", got)
	}
	if !strings.Contains(got, "postgres://"+Redacted+"@") {
		t.Fatalf("userinfo = %s", got)
	}
}

func TestRedactParallel(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := RedactMap(map[string]any{
				"run_id":  "run_parallel",
				"api_key": sampleSecret,
				"note":    "Bearer " + sampleSecret,
				"prompt":  sampleSecret,
			})
			encoded, err := json.Marshal(out)
			if err != nil {
				t.Error(err)
				return
			}
			if strings.Contains(string(encoded), sampleSecret) {
				t.Errorf("secret leaked: %s", encoded)
			}
			if out["run_id"] != "run_parallel" {
				t.Errorf("run_id = %#v", out["run_id"])
			}
		}()
	}
	wg.Wait()
}

type secretBox struct {
	Password string
}
