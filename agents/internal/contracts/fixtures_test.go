package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAPIFixtures(t *testing.T) {
	fixtures := []struct {
		name string
		new  func() any
	}{
		{"start-request.json", func() any { return &StartRunRequest{} }},
		{"start-response.json", func() any { return &StartRunResponse{} }},
		{"run-response.json", func() any { return &RunResponse{} }},
		{"events-response.json", func() any { return &EventsResponse{} }},
		{"agents-response.json", func() any { return &AgentsResponse{} }},
		{"error-response.json", func() any { return &ErrorResponse{} }},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join("..", "..", "fixtures", "api", "v1", fixture.name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			value := fixture.new()
			if err := json.Unmarshal(data, value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var original, roundTrip any
			if err := json.Unmarshal(data, &original); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatal(err)
			}
			originalJSON, _ := json.Marshal(original)
			roundTripJSON, _ := json.Marshal(roundTrip)
			if string(originalJSON) != string(roundTripJSON) {
				t.Fatalf("fixture loses fields on round trip:\n%s\n%s", originalJSON, roundTripJSON)
			}
		})
	}
}
