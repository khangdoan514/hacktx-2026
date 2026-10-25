package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestClientReadsRotatedServicePassword(t *testing.T) {
	file := filepath.Join(t.TempDir(), "service.json")
	write := func(password string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(`{"password":"`+password+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("first")
	t.Setenv("OPENCODE_SERVICE_FILE", file)
	expected := "first"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, password, ok := r.BasicAuth()
		if !ok || password != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.call(context.Background(), http.MethodGet, "/", nil, nil); err != nil {
		t.Fatal(err)
	}
	write("second")
	expected = "second"
	if err := client.call(context.Background(), http.MethodGet, "/", nil, nil); err != nil {
		t.Fatalf("request with rotated password: %v", err)
	}
}
